package api

import (
	"encoding/json"
	"fmt"
	"just-nas/internal/command"
	"just-nas/internal/config"
	"just-nas/internal/platform"
	"just-nas/internal/shares"
	"just-nas/internal/storage"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

var restoring atomic.Bool

func (r *Router) platformAPI(w http.ResponseWriter, q *http.Request) {
	var e error
	switch q.URL.Path {
	case "/api/network":
		if q.Method == "GET" {
			var v map[string]any
			v, e = platform.NetworkState()
			if e == nil {
				jsonResponse(w, 200, v)
				return
			}
		} else if q.Method == "POST" {
			var s platform.NetworkSettings
			if e = json.NewDecoder(q.Body).Decode(&s); e == nil {
				e = platform.ConfigureNetwork(s)
			}
		} else {
			w.WriteHeader(405)
			return
		}
	case "/api/network/confirm":
		if q.Method != "POST" {
			w.WriteHeader(405)
			return
		}
		e = platform.ConfirmNetwork()
	case "/api/tls":
		if q.Method == "GET" {
			var s platform.TLSSettings
			s, e = platform.EnsureCertificate()
			s.PrivateKey = ""
			if e == nil {
				jsonResponse(w, 200, s)
				return
			}
		} else if q.Method == "POST" {
			var s platform.TLSSettings
			if e = json.NewDecoder(q.Body).Decode(&s); e == nil {
				e = platform.SetTLS(s)
			}
		} else {
			w.WriteHeader(405)
			return
		}
	case "/api/settings/import":
		if q.Method != "POST" {
			w.WriteHeader(405)
			return
		}
		var b struct {
			Password string
			Data     json.RawMessage
			Confirm  string
		}
		if e = json.NewDecoder(q.Body).Decode(&b); e != nil {
			break
		}
		if b.Confirm != "RESTORE SETTINGS AND REBOOT" {
			e = fmt.Errorf("請確認恢復設定並重新啟動")
			break
		}
		var state map[string][]byte
		state, e = config.DecryptExport(b.Data, b.Password)
		if e != nil {
			break
		}
		var users []shares.User
		var ss []shares.ShareConfig
		var a admin
		if json.Unmarshal(state["admin.json"], &a) != nil || len(a.Hash) != 64 || len(a.Salt) != 64 {
			e = fmt.Errorf("管理員設定無效")
			break
		}
		if len(state["users.json"]) > 0 {
			e = json.Unmarshal(state["users.json"], &users)
		}
		if e != nil {
			break
		}
		if len(state["shares.json"]) > 0 {
			e = json.Unmarshal(state["shares.json"], &ss)
		}
		if e != nil {
			break
		}
		for _, u := range users {
			if u.UID < 1000 || u.UID >= 65534 || strings.ContainsAny(u.Name, " /\\\r\n:") || u.Name == "" {
				e = fmt.Errorf("備份帳號識別無效")
			}
		}
		for _, s := range ss {
			if !strings.HasPrefix(s.Path, "/mnt/") || !storage.ValidDataset(strings.TrimPrefix(s.Path, "/mnt/")) {
				e = fmt.Errorf("分享路徑無效")
			}
		}
		if e != nil {
			break
		}
		if e = r.jobs.Pause(true); e != nil {
			break
		}
		restoring.Store(true)
		e = config.ImportFiles(state)
		if e != nil {
			restoring.Store(false)
			_ = r.jobs.Pause(false)
			break
		}
		jsonResponse(w, 200, map[string]string{"message": "設定已還原，NAS 即將重新啟動"})
		go func() {
			time.Sleep(2 * time.Second)
			for _, args := range [][]string{{"rc-service", "samba", "stop"}, {"rc-service", "nfs", "stop"}, {"sync"}, {"zpool", "export", "-a"}, {"reboot"}} {
				if _, e := command.Run("", args[0], args[1:]...); e != nil {
					return
				}
			}
		}()
		return
	default:
		w.WriteHeader(404)
		return
	}
	if e != nil {
		jsonResponse(w, 400, map[string]string{"error": e.Error()})
		return
	}
	jsonResponse(w, 200, map[string]bool{"ok": true})
}
