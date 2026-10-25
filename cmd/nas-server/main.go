package main

import (
	"context"
	"fmt"
	"just-nas/internal/boot"
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
	"just-nas/internal/storage"
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

	display := startBootDisplay(port)

	// Restore mounted ZFS state before recreating users and publishing shares.
	pm := config.NewPersistenceManager()
	pm.RestoreConfigOnBootObserved(func(e config.BootEvent) { display.d.Set(e.Phase, boot.State(e.State), e.Detail) })
	if e := config.RecoverPoolDeletion(); e != nil {
		display.d.Set("settings", boot.Fail, "Pool deletion recovery failed; see logs")
		display.d.Finish(false)
		log.Fatalf("刪池清理恢復失敗，停止啟動: %v", e)
	}
	display.d.Set("network", boot.Running, "Restoring management network")
	networkErr := platform.RecoverNetwork()
	display.networkResult(networkErr)
	if e := networkErr; e != nil {
		logger.Error("NETWORK", "%v", e)
	}
	if _, e := platform.EnsureCertificate(); e != nil {
		logger.Error("TLS", "%v", e)
	}
	display.d.Set("shares", boot.Running, "Restoring file accounts and sharing services")
	shareMgr := shares.NewShareManager()
	if err := shareMgr.Restore(); err != nil {
		display.d.Set("shares", boot.Fail, "Sharing services could not be restored; see logs")
		logger.Error("SHARES", "分享服務初始化失敗: %v", err)
	} else {
		ss, e := shareMgr.GetShares()
		active := 0
		unavailable := 0
		for _, v := range ss {
			if !v.Disabled {
				active++
				if storage.MountedDataset(v.Path) != nil {
					unavailable++
				}
			}
		}
		if e != nil {
			display.d.Set("shares", boot.Fail, "Share configuration unavailable")
		} else if active == 0 {
			display.d.Set("shares", boot.Skip, "No enabled shares configured")
		} else if unavailable > 0 {
			display.d.Set("shares", boot.Warn, "Some share datasets are unavailable; see logs")
		} else {
			display.d.Set("shares", boot.OK, "Configured sharing services restored")
		}
	}
	if e := backup.RestoreMount(); e != nil {
		logger.Error("BACKUP", "%v", e)
		for _, stage := range display.d.Snapshot().Stages {
			if stage.ID == "settings" && stage.State != boot.Fail {
				display.d.Set("settings", boot.Warn, "Backup destination unavailable; see logs")
			}
		}
	}
	extensions.MarkBootStart()
	display.d.Set("extensions", boot.Running, "Restoring optional extensions in background")
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		if e := extensions.Boot(ctx); e != nil {
			logger.Error("EXTENSIONS", "%v", e)
			if rescueExtensions() {
				display.d.Set("extensions", boot.Skip, "Rescue mode: extensions bypassed")
			} else {
				display.d.Set("extensions", boot.Fail, "Extension restore failed; see logs")
			}
			return
		}
		state, e := extensions.Load()
		enabled := 0
		for _, v := range state.Installed {
			if v.Enabled {
				enabled++
			}
		}
		if e != nil {
			display.d.Set("extensions", boot.Fail, "Extension settings unavailable")
		} else if enabled == 0 {
			display.d.Set("extensions", boot.Skip, "No enabled optional extensions")
		} else {
			display.d.Set("extensions", boot.OK, "Enabled extensions restored")
		}
	}()
	pm.StartBackgroundReconciler(30 * time.Second)

	mux := http.NewServeMux()

	// 1. 註冊 REST API 路由
	router := api.NewRouter()
	router.RegisterRoutes(mux)
	if e := router.StartNativeServices(); e != nil {
		log.Printf("原生應用設定服務無法啟動: %v", e)
	}

	// 2. 載入 Go embed 嵌入式前端
	fs, err := web.GetFileSystem()
	if err != nil {
		display.failedWeb("Embedded web files unavailable")
		log.Fatalf("無法載入嵌入式 Web 檔案系統: %v", err)
	}

	// 靜態檔案服務 handler
	fileServer := http.FileServer(fs)
	mux.Handle("/", fileServer)

	addr := fmt.Sprintf(":%s", port)
	log.Printf("just-NAS v2.1.0-rc4 management service starting")
	display.d.Set("web", boot.Running, "Starting HTTP and HTTPS listeners")
	secure := &http.Server{Addr: ":8443", Handler: mux, TLSConfig: platform.ServerTLS(), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16384}
	go func() {
		httpsListener, e := net.Listen("tcp", secure.Addr)
		if e != nil {
			display.failedWeb("HTTPS listener failed; see logs")
			logger.Error("TLS", "%v", e)
			return
		}
		display.httpsBound.Store(true)
		if e := secure.ServeTLS(httpsListener, "", ""); e != nil {
			display.failedWeb("HTTPS listener failed; see logs")
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
	httpListener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		display.failedWeb("HTTP listener failed; see logs")
		log.Fatalf("HTTP listener: %v", err)
	}
	display.httpBound.Store(true)
	if err := server.Serve(httpListener); err != nil {
		display.failedWeb("HTTP listener failed; see logs")
		log.Fatalf("服務啟動失敗: %v", err)
	}
}
