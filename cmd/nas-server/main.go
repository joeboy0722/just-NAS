package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"just-nas/internal/api"
	"just-nas/internal/config"
	"just-nas/internal/logger"
	"just-nas/internal/shares"
	"just-nas/web"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// Restore mounted ZFS state before recreating users and publishing shares.
	pm := config.NewPersistenceManager()
	pm.RestoreConfigOnBoot()
	if err := shares.NewShareManager().Restore(); err != nil {
		logger.Error("SHARES", "分享服務初始化失敗: %v", err)
	}
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

	server := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 120 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16384}
	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("服務啟動失敗: %v", err)
	}
}
