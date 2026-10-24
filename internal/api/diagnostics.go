package api

import (
	"encoding/json"
	"just-nas/internal/command"
	"just-nas/internal/config"
	"just-nas/internal/logger"
	"net/http"
	"os"
	"strings"
	"time"
)

func (r *Router) handleReboot(w http.ResponseWriter, req *http.Request) {
	if req.Method != "POST" {
		w.WriteHeader(405)
		return
	}
	var b struct {
		Confirm string `json:"confirm"`
	}
	action := "reboot"
	if req.URL.Path == "/api/system/shutdown" {
		action = "poweroff"
	}
	if json.NewDecoder(req.Body).Decode(&b) != nil || b.Confirm != action {
		jsonResponse(w, 400, map[string]string{"error": "請確認重新啟動"})
		return
	}
	if r.jobs != nil {
		if e := r.jobs.Pause(true); e != nil {
			jsonResponse(w, 409, map[string]string{"error": "請先等待或中止背景任務：" + e.Error()})
			return
		}
	}
	r.portal.stopAllWrites()
	jsonResponse(w, 200, map[string]string{"message": "系統即將重新啟動"})
	go func() {
		time.Sleep(2 * time.Second)
		for _, cmd := range [][]string{{"rc-service", "samba", "stop"}, {"rc-service", "nfs", "stop"}, {"sync"}, {"zpool", "export", "-a"}, {action}} {
			if _, err := command.Run("", cmd[0], cmd[1:]...); err != nil {
				logger.Error("POWER", "%s: %v", action, err)
				return
			}
		}
	}()
}

func (r *Router) handleDiagnostics(w http.ResponseWriter, req *http.Request) {
	if req.Method != "GET" {
		w.WriteHeader(405)
		return
	}
	checks := []map[string]any{}
	for _, c := range [][]string{{"uname", "-r"}, {"zpool", "--version"}, {"zfs", "list", "-H", "-o", "name"}, {"testparm", "-s"}, {"rc-service", "samba", "status"}, {"rc-service", "nfs", "status"}} {
		out, err := command.Run("", c[0], c[1:]...)
		detail := strings.TrimSpace(string(out))
		if err != nil {
			detail = err.Error()
		}
		checks = append(checks, map[string]any{"name": strings.Join(c, " "), "ok": err == nil, "detail": detail})
	}
	_, err := os.Stat("/sys/module/zfs")
	checks = append(checks, map[string]any{"name": "ZFS kernel module", "ok": err == nil})
	jsonResponse(w, 200, map[string]any{"checks": checks, "persistent": config.Persistent()})
}
func (r *Router) handleUsers(w http.ResponseWriter, req *http.Request) {
	switch req.Method {
	case "GET":
		u, e := r.shareMgr.GetUsers()
		if e != nil {
			jsonResponse(w, 500, map[string]string{"error": e.Error()})
			return
		}
		jsonResponse(w, 200, u)
	case "POST":
		var b struct {
			Name     string `json:"name"`
			Password string `json:"password"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, req.Body, 4096)).Decode(&b) != nil {
			jsonResponse(w, 400, map[string]string{"error": "無效的請求"})
			return
		}
		if e := r.shareMgr.CreateUser(b.Name, b.Password); e != nil {
			jsonResponse(w, 400, map[string]string{"error": e.Error()})
			return
		}
		jsonResponse(w, 200, map[string]string{"message": "檔案帳號已建立"})
	case "DELETE":
		if e := r.shareMgr.DeleteUser(req.URL.Query().Get("name")); e != nil {
			jsonResponse(w, 400, map[string]string{"error": e.Error()})
			return
		}
		r.portal.mu.Lock()
		for token, s := range r.portal.sessions {
			if s.User.Name == req.URL.Query().Get("name") {
				delete(r.portal.sessions, token)
			}
		}
		r.portal.mu.Unlock()
		r.portal.cancelWrites(req.URL.Query().Get("name"), "")
		jsonResponse(w, 200, map[string]string{"message": "帳號已移除"})
	default:
		w.WriteHeader(405)
	}
}
