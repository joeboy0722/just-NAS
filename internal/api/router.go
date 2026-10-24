package api

import (
	"encoding/json"
	"just-nas/internal/config"
	"net/http"
	"strconv"

	"just-nas/internal/logger"
	"just-nas/internal/shares"
	"just-nas/internal/storage"
	"just-nas/internal/system"
)

type Router struct {
	storageMgr *storage.StorageManager
	shareMgr   *shares.ShareManager
}

func NewRouter() *Router {
	return &Router{
		storageMgr: storage.NewStorageManager(),
		shareMgr:   shares.NewShareManager(),
	}
}

func (r *Router) RegisterRoutes(mux *http.ServeMux) {
	a := newAuth()
	mux.HandleFunc("/api/auth/", a.handle)
	protected := http.NewServeMux()
	r.registerProtected(protected)
	mux.Handle("/api/", a.protect(protected))
}

func (r *Router) registerProtected(mux *http.ServeMux) {
	mux.HandleFunc("/api/system/diagnostics", r.handleDiagnostics)
	mux.HandleFunc("/api/system/reboot", r.handleReboot)
	mux.HandleFunc("/api/system/shutdown", r.handleReboot)
	mux.HandleFunc("/api/users", r.handleUsers)
	mux.HandleFunc("/api/system/status", r.handleSystemStatus)
	mux.HandleFunc("/api/system/logs", r.handleSystemLogs)
	mux.HandleFunc("/api/storage/pools", r.handleStoragePools)
	mux.HandleFunc("/api/storage/disks", r.handleAvailableDisks)
	mux.HandleFunc("/api/storage/datasets", r.handleStorageDatasets)
	mux.HandleFunc("/api/storage/snapshots", r.handleStorageSnapshots)
	mux.HandleFunc("/api/storage/snapshots/rollback", r.handleSnapshotRollback)
	mux.HandleFunc("/api/storage/smart", r.handleSmartDisks)
	mux.HandleFunc("/api/shares", r.handleShares)
}

func jsonResponse(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func (r *Router) handleSystemStatus(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	status, err := system.GetStatus()
	if err != nil {
		jsonResponse(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	jsonResponse(w, http.StatusOK, status)
}

func (r *Router) handleSystemLogs(w http.ResponseWriter, req *http.Request) {
	switch req.Method {
	case http.MethodGet:
		limitStr := req.URL.Query().Get("limit")
		limit := 100
		if limitStr != "" {
			if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
				limit = l
			}
		}
		logs := logger.GetLogger().GetLogs(limit)
		jsonResponse(w, http.StatusOK, logs)
	case http.MethodDelete:
		logger.GetLogger().Clear()
		jsonResponse(w, http.StatusOK, map[string]string{"message": "日誌紀錄已成功清除"})
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (r *Router) handleStoragePools(w http.ResponseWriter, req *http.Request) {
	switch req.Method {
	case http.MethodGet:
		pools, err := r.storageMgr.GetPools()
		if err != nil {
			jsonResponse(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		jsonResponse(w, http.StatusOK, pools)
	case http.MethodPost:
		var body struct {
			Name     string   `json:"name"`
			RaidType string   `json:"raid_type"`
			Devices  []string `json:"devices"`
			Confirm  string   `json:"confirm"`
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			jsonResponse(w, http.StatusBadRequest, map[string]string{"error": "無效的請求格式"})
			return
		}
		if body.Name == "" || body.Confirm != body.Name {
			jsonResponse(w, 400, map[string]string{"error": "請輸入儲存池名稱確認初始化硬碟"})
			return
		}
		if err := r.storageMgr.CreatePool(body.Name, body.RaidType, body.Devices); err != nil {
			jsonResponse(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		logger.Info("ZFS", "建立儲存池成功: %s", body.Name)
		if err := config.NewPersistenceManager().EnsurePool(body.Name); err != nil {
			jsonResponse(w, 500, map[string]string{"error": "儲存池已建立，但設定持久化失敗: " + err.Error()})
			return
		}
		jsonResponse(w, http.StatusOK, map[string]string{"message": "ZFS 儲存池建立成功"})
	case http.MethodDelete:
		name := req.URL.Query().Get("name")
		if name == "" {
			jsonResponse(w, http.StatusBadRequest, map[string]string{"error": "請提供儲存池名稱"})
			return
		}
		if req.URL.Query().Get("confirm") != name {
			jsonResponse(w, 400, map[string]string{"error": "請輸入名稱確認銷毀"})
			return
		}
		if r.shareMgr.InUse(name) {
			jsonResponse(w, 409, map[string]string{"error": "請先移除使用此池的分享"})
			return
		}
		if err := r.storageMgr.DestroyPool(name); err != nil {
			jsonResponse(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		logger.Warn("ZFS", "銷毀儲存池: %s", name)
		jsonResponse(w, http.StatusOK, map[string]string{"message": "ZFS 儲存池銷毀成功"})
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (r *Router) handleAvailableDisks(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	disks, err := r.storageMgr.GetAvailableDisks()
	if err != nil {
		jsonResponse(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	jsonResponse(w, http.StatusOK, disks)
}

func (r *Router) handleStorageDatasets(w http.ResponseWriter, req *http.Request) {
	switch req.Method {
	case http.MethodGet:
		datasets, err := r.storageMgr.GetDatasets()
		if err != nil {
			jsonResponse(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		jsonResponse(w, http.StatusOK, datasets)
	case http.MethodPost:
		var body struct {
			Name        string `json:"name"`
			Mountpoint  string `json:"mountpoint"`
			Compression string `json:"compression"`
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			jsonResponse(w, http.StatusBadRequest, map[string]string{"error": "無效的請求格式"})
			return
		}
		if err := r.storageMgr.CreateDataset(body.Name, body.Mountpoint, body.Compression); err != nil {
			jsonResponse(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		logger.Info("ZFS", "建立資料集成功: %s", body.Name)
		jsonResponse(w, http.StatusOK, map[string]string{"message": "ZFS 資料集建立成功"})
	case http.MethodDelete:
		name := req.URL.Query().Get("name")
		if name == "" {
			jsonResponse(w, http.StatusBadRequest, map[string]string{"error": "請提供資料集名稱"})
			return
		}
		if r.shareMgr.InUse(name) {
			jsonResponse(w, 409, map[string]string{"error": "請先移除使用此資料集的分享"})
			return
		}
		if req.URL.Query().Get("confirm") != name {
			jsonResponse(w, 400, map[string]string{"error": "請輸入名稱確認刪除"})
			return
		}
		if err := r.storageMgr.DestroyDataset(name); err != nil {
			jsonResponse(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		logger.Warn("ZFS", "刪除資料集: %s", name)
		jsonResponse(w, http.StatusOK, map[string]string{"message": "ZFS 資料集刪除成功"})
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (r *Router) handleStorageSnapshots(w http.ResponseWriter, req *http.Request) {
	switch req.Method {
	case http.MethodGet:
		snapshots, err := r.storageMgr.GetSnapshots()
		if err != nil {
			jsonResponse(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		jsonResponse(w, http.StatusOK, snapshots)
	case http.MethodPost:
		var body struct {
			Dataset string `json:"dataset"`
			Tag     string `json:"tag"`
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			jsonResponse(w, http.StatusBadRequest, map[string]string{"error": "無效的請求格式"})
			return
		}
		if err := r.storageMgr.CreateSnapshot(body.Dataset, body.Tag); err != nil {
			jsonResponse(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		logger.Info("ZFS", "建立快照成功: %s@%s", body.Dataset, body.Tag)
		jsonResponse(w, http.StatusOK, map[string]string{"message": "ZFS 快照建立成功"})
	case http.MethodDelete:
		name := req.URL.Query().Get("name")
		if name == "" {
			jsonResponse(w, http.StatusBadRequest, map[string]string{"error": "請提供快照名稱"})
			return
		}
		if err := r.storageMgr.DestroySnapshot(name); err != nil {
			jsonResponse(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		logger.Warn("ZFS", "刪除快照: %s", name)
		jsonResponse(w, http.StatusOK, map[string]string{"message": "ZFS 快照刪除成功"})
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (r *Router) handleSnapshotRollback(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Name    string `json:"name"`
		Confirm string `json:"confirm"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		jsonResponse(w, http.StatusBadRequest, map[string]string{"error": "無效的請求格式"})
		return
	}
	if body.Name == "" || body.Confirm != body.Name {
		jsonResponse(w, 400, map[string]string{"error": "請輸入快照名稱確認還原"})
		return
	}
	if err := r.storageMgr.RollbackSnapshot(body.Name); err != nil {
		jsonResponse(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	logger.Warn("ZFS", "還原快照成功: %s", body.Name)
	jsonResponse(w, http.StatusOK, map[string]string{"message": "快照還原成功"})
}

func (r *Router) handleSmartDisks(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	disks, err := r.storageMgr.GetSmartDisks()
	if err != nil {
		jsonResponse(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	jsonResponse(w, http.StatusOK, disks)
}

func (r *Router) handleShares(w http.ResponseWriter, req *http.Request) {
	switch req.Method {
	case http.MethodGet:
		shs, err := r.shareMgr.GetShares()
		if err != nil {
			jsonResponse(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		jsonResponse(w, http.StatusOK, shs)
	case http.MethodPost:
		var cfg shares.ShareConfig
		if err := json.NewDecoder(req.Body).Decode(&cfg); err != nil {
			jsonResponse(w, http.StatusBadRequest, map[string]string{"error": "無效的請求格式"})
			return
		}
		if err := r.shareMgr.CreateShare(cfg); err != nil {
			jsonResponse(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		logger.Info("SHARE", "建立檔案共享成功 [%s]: %s (%s)", cfg.Protocol, cfg.Name, cfg.Path)
		jsonResponse(w, http.StatusOK, map[string]string{"message": "檔案共享建立成功"})
	case http.MethodDelete:
		name := req.URL.Query().Get("name")
		protocol := req.URL.Query().Get("protocol")
		if name == "" || protocol == "" {
			jsonResponse(w, http.StatusBadRequest, map[string]string{"error": "請提供共享名稱與協定"})
			return
		}
		if err := r.shareMgr.DeleteShare(name, protocol); err != nil {
			jsonResponse(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		logger.Warn("SHARE", "移除檔案共享 [%s]: %s", protocol, name)
		jsonResponse(w, http.StatusOK, map[string]string{"message": "檔案共享刪除成功"})
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}
