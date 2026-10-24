package api

import (
	"encoding/json"
	"just-nas/internal/storage"
	"just-nas/internal/tasks"
	"net/http"
	"strings"
	"time"
)

func (r *Router) handleShareUpdate(w http.ResponseWriter, q *http.Request) {
	if q.Method != "PATCH" {
		w.WriteHeader(405)
		return
	}
	var b map[string]any
	if json.NewDecoder(q.Body).Decode(&b) != nil {
		jsonResponse(w, 400, map[string]string{"error": "格式錯誤"})
		return
	}
	if e := r.shareMgr.UpdateShare(strings.TrimPrefix(q.URL.Path, "/api/shares/"), b); e != nil {
		jsonResponse(w, 400, map[string]string{"error": e.Error()})
		return
	}
	jsonResponse(w, 200, map[string]bool{"ok": true})
}
func (r *Router) handleUserUpdate(w http.ResponseWriter, q *http.Request) {
	if q.Method != "PATCH" {
		w.WriteHeader(405)
		return
	}
	var b struct {
		Disabled *bool   `json:"disabled"`
		Password *string `json:"password"`
	}
	if json.NewDecoder(q.Body).Decode(&b) != nil {
		jsonResponse(w, 400, map[string]string{"error": "格式錯誤"})
		return
	}
	if e := r.shareMgr.UpdateUser(strings.TrimPrefix(q.URL.Path, "/api/users/"), b.Disabled, b.Password); e != nil {
		jsonResponse(w, 400, map[string]string{"error": e.Error()})
		return
	}
	jsonResponse(w, 200, map[string]bool{"ok": true})
}
func (r *Router) handleSpaces(w http.ResponseWriter, q *http.Request) {
	if q.Method != "POST" {
		w.WriteHeader(405)
		return
	}
	var b struct {
		Pool, Name, Kind, Owner, Quota string
		Access                         map[string]string
	}
	if json.NewDecoder(q.Body).Decode(&b) != nil {
		jsonResponse(w, 400, map[string]string{"error": "格式錯誤"})
		return
	}
	if e := r.shareMgr.CreateSpace(b.Pool, b.Name, b.Kind, b.Owner, b.Quota, b.Access); e != nil {
		jsonResponse(w, 400, map[string]string{"error": e.Error()})
		return
	}
	for kind, seconds := range map[string]int64{"snapshot-hourly": 3600, "snapshot-daily": 86400, "snapshot-weekly": 604800, "quota": 60} {
		if err := r.jobs.SetSchedule(tasks.Schedule{ID: kind + ":" + b.Pool + "/" + b.Name, Kind: kind, Resource: b.Pool + "/" + b.Name, EverySeconds: seconds, Enabled: true, LastRun: time.Now()}); err != nil {
			jsonResponse(w, 500, map[string]string{"error": "空間已建立，但預設排程保存失敗: " + err.Error()})
			return
		}
	}
	jsonResponse(w, 200, map[string]bool{"ok": true})
}
func (r *Router) handleDatasetUpdate(w http.ResponseWriter, q *http.Request) {
	name := q.URL.Query().Get("name")
	if q.Method == "GET" {
		v, e := r.storageMgr.DatasetProperties(name)
		if e != nil {
			jsonResponse(w, 400, map[string]string{"error": e.Error()})
			return
		}
		jsonResponse(w, 200, v)
		return
	}
	if q.Method != "PATCH" {
		w.WriteHeader(405)
		return
	}
	var b struct{ Compression, Quota string }
	if json.NewDecoder(q.Body).Decode(&b) != nil {
		w.WriteHeader(400)
		return
	}
	if e := r.storageMgr.UpdateDataset(name, b.Compression, b.Quota); e != nil {
		jsonResponse(w, 400, map[string]string{"error": e.Error()})
		return
	}
	jsonResponse(w, 200, map[string]bool{"ok": true})
}
func (r *Router) handlePoolStatus(w http.ResponseWriter, q *http.Request) {
	if q.Method != "GET" {
		w.WriteHeader(405)
		return
	}
	out, e := storage.NewStorageManager().PoolStatus(q.URL.Query().Get("name"))
	if e != nil {
		jsonResponse(w, 400, map[string]string{"error": e.Error()})
		return
	}
	jsonResponse(w, 200, map[string]string{"status": out})
}
