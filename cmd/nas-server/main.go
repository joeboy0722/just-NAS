package main

import (
	"context"
	"fmt"
	"just-nas/internal/extensions"
	"log"
	"net"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"just-nas/internal/api"
	"just-nas/internal/backup"
	"just-nas/internal/config"
	"just-nas/internal/files"
	"just-nas/internal/logger"
	"just-nas/internal/platform"
	"just-nas/internal/shares"
	"just-nas/web"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--file-worker" {
		if e := files.Worker(os.Stdin, os.Stdout); e != nil {
			fmt.Fprintln(os.Stderr, e)
			os.Exit(1)
		}
		return
	}

	if runtime.GOOS != "linux" {
		log.Fatal("NAS 必須以 Linux ISO／VM 開機；Windows 僅用於建置與驗收")
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// Restore mounted ZFS state before recreating users and publishing shares.
	pm := config.NewPersistenceManager()
	pm.RestoreConfigOnBoot()
	if e := config.RecoverPoolDeletion(); e != nil {
		log.Fatalf("刪池清理恢復失敗，停止啟動: %v", e)
	}
	if e := platform.RecoverNetwork(); e != nil {
		logger.Error("NETWORK", "%v", e)
	}
	if _, e := platform.EnsureCertificate(); e != nil {
		logger.Error("TLS", "%v", e)
	}
	if err := shares.NewShareManager().Restore(); err != nil {
		logger.Error("SHARES", "分享服務初始化失敗: %v", err)
	}
	if e := backup.RestoreMount(); e != nil {
		logger.Error("BACKUP", "%v", e)
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		if e := extensions.Boot(ctx); e != nil {
			logger.Error("EXTENSIONS", "%v", e)
		}
	}()
	pm.StartBackgroundReconciler(30 * time.Second)

	mux := http.NewServeMux()

	// 1. 註冊 REST API 路由
	router := api.NewRouter()
	router.RegisterRoutes(mux)

	// 2. 載入 Go embed 嵌入式前端
	fs, err := web.GetFileSystem()
	if err != nil {
		log.Fatalf("無法載入嵌入式 Web 檔案系統: %v", err)
	}

	// 靜態檔案服務 handler
	fileServer := http.FileServer(fs)
	mux.Handle("/", fileServer)

	addr := fmt.Sprintf(":%s", port)
	log.Printf("==================================================")
	log.Printf(" ⚡ Alpine ZFS NAS Web Control Center 啟動中...")
	log.Printf(" 💾 ZFS 設定持久化: %t", config.Persistent())
	log.Printf(" ZFS 掛載巡檢: 每 30 秒")
	log.Printf(" 🌐 監聽位址: http://localhost%s", addr)
	log.Printf("==================================================")

	secure := &http.Server{Addr: ":8443", Handler: mux, TLSConfig: platform.ServerTLS(), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16384}
	go func() {
		if e := secure.ListenAndServeTLS("", ""); e != nil {
			logger.Error("TLS", "%v", e)
		}
	}()
	redirect := http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
		if q.URL.Path == "/certificate.pem" {
			s, e := platform.TLSConfig()
			if e != nil {
				http.Error(w, "certificate unavailable", 503)
				return
			}
			w.Header().Set("Content-Type", "application/x-pem-file")
			w.Write([]byte(s.Certificate))
			return
		}
		s, e := platform.TLSConfig()
		if e == nil && s.Enabled {
			host := q.Host
			if h, _, e := net.SplitHostPort(host); e == nil {
				host = h
			}
			host = strings.Trim(host, "[]")
			http.Redirect(w, q, "https://"+net.JoinHostPort(host, "8443")+q.URL.RequestURI(), http.StatusTemporaryRedirect)
			return
		}
		mux.ServeHTTP(w, q)
	})
	server := &http.Server{Addr: addr, Handler: redirect, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 0, WriteTimeout: 0, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16384}
	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("服務啟動失敗: %v", err)
	}
}
