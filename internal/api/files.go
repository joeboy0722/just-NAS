package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"just-nas/internal/command"
	"just-nas/internal/files"
	"just-nas/internal/shares"
	"just-nas/internal/storage"
	"net"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"
)

type fileSession struct {
	User    shares.User
	CSRF    string
	Expires time.Time
}
type fileWrite struct {
	User, Space string
	Cancel      context.CancelFunc
}
type filePortal struct {
	writes   map[string]chan struct{}
	active   map[string]fileWrite
	mu       sync.Mutex
	sessions map[string]fileSession
	attempts map[string]time.Time
	manager  *shares.ShareManager
}

func newFilePortal(m *shares.ShareManager) *filePortal {
	return &filePortal{writes: map[string]chan struct{}{}, active: map[string]fileWrite{}, sessions: map[string]fileSession{}, attempts: map[string]time.Time{}, manager: m}
}
func (f *filePortal) handle(w http.ResponseWriter, q *http.Request) {
	if restoring.Load() && q.Method != "GET" {
		jsonResponse(w, 503, map[string]string{"error": "設定恢復中，請稍後重新登入"})
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if q.URL.Path == "/files-api/login" {
		if q.Method != "POST" || !sameOrigin(q) {
			w.WriteHeader(403)
			return
		}
		host, _, _ := net.SplitHostPort(q.RemoteAddr)
		f.mu.Lock()
		last := f.attempts[host]
		if len(f.attempts) > 2048 {
			for k, t := range f.attempts {
				if time.Since(t) > time.Minute {
					delete(f.attempts, k)
				}
			}
		}
		f.attempts[host] = time.Now()
		f.mu.Unlock()
		if time.Since(last) < time.Second {
			jsonResponse(w, 429, map[string]string{"error": "請稍後重試"})
			return
		}
		var b struct{ Name, Password string }
		if json.NewDecoder(http.MaxBytesReader(w, q.Body, 4096)).Decode(&b) != nil {
			w.WriteHeader(400)
			return
		}
		u, e := f.manager.Authenticate(b.Name, b.Password)
		if e != nil {
			jsonResponse(w, 401, map[string]string{"error": e.Error()})
			return
		}
		id, csrf := randomToken(), randomToken()
		f.mu.Lock()
		for k, s := range f.sessions {
			if time.Now().After(s.Expires) {
				delete(f.sessions, k)
			}
		}
		f.sessions[id] = fileSession{u, csrf, time.Now().Add(12 * time.Hour)}
		f.mu.Unlock()
		http.SetCookie(w, &http.Cookie{Name: "nas_files", Value: id, Path: "/files-api/", HttpOnly: true, Secure: q.TLS != nil, SameSite: http.SameSiteStrictMode, MaxAge: 43200})
		jsonResponse(w, 200, map[string]any{"csrf": csrf, "user": u.Name})
		return
	}
	c, e := q.Cookie("nas_files")
	if e != nil {
		jsonResponse(w, 401, map[string]string{"error": "請登入檔案帳號"})
		return
	}
	f.mu.Lock()
	s, ok := f.sessions[c.Value]
	f.mu.Unlock()
	if !ok || time.Now().After(s.Expires) {
		w.WriteHeader(401)
		return
	}
	u, e := f.manager.Lookup(s.User.Name)
	if e != nil || u.Disabled || u.Revision != s.User.Revision || u.UID != s.User.UID {
		jsonResponse(w, 401, map[string]string{"error": "帳號或密碼已變更，請重新登入"})
		return
	}
	if q.Method != "GET" && (!sameOrigin(q) || q.Header.Get("X-CSRF-Token") != s.CSRF) {
		w.WriteHeader(403)
		return
	}
	if q.URL.Path == "/files-api/logout" && q.Method == "POST" {
		f.mu.Lock()
		delete(f.sessions, c.Value)
		f.mu.Unlock()
		jsonResponse(w, 200, map[string]bool{"ok": true})
		return
	}
	all, e := f.manager.GetShares()
	if e != nil {
		jsonResponse(w, 500, map[string]string{"error": e.Error()})
		return
	}
	allowed := []shares.ShareConfig{}
	for _, v := range all {
		if v.Protocol == "smb" && !v.Disabled && (v.Mode == "public" || v.Mode == "private") {
			for _, n := range v.Users {
				if n == u.Name {
					allowed = append(allowed, v)
					break
				}
			}
		}
	}
	if q.URL.Path == "/files-api/spaces" && q.Method == "GET" {
		jsonResponse(w, 200, map[string]any{"spaces": allowed, "csrf": s.CSRF, "user": u.Name})
		return
	}
	var space *shares.ShareConfig
	for i := range allowed {
		if allowed[i].ID == q.URL.Query().Get("space") {
			space = &allowed[i]
			break
		}
	}
	if space == nil {
		jsonResponse(w, 403, map[string]string{"error": "沒有此空間的權限"})
		return
	}
	if e = storage.MountedDataset(space.Path); e != nil {
		jsonResponse(w, 503, map[string]string{"error": "資料集未掛載"})
		return
	}
	op := strings.TrimPrefix(q.URL.Path, "/files-api/")

	if op == "budget" && q.Method == "GET" {
		props, err := storage.NewStorageManager().DatasetProperties(strings.TrimPrefix(space.Path, "/mnt/"))
		if err != nil {
			jsonResponse(w, 503, map[string]string{"error": err.Error()})
			return
		}
		target, _ := strconv.ParseInt(props["org.just-nas:quota"], 10, 64)
		used, _ := strconv.ParseInt(props["projectused@1"], 10, 64)
		remaining := target - used
		if remaining < 0 {
			remaining = 0
		}
		jsonResponse(w, 200, map[string]any{"limited": target > 0, "remaining": remaining, "used": used, "quota": target})
		return
	}
	if op == "versions" && q.Method == "GET" {
		snaps, e := storage.NewStorageManager().GetSnapshots()
		if e != nil {
			jsonResponse(w, 500, map[string]string{"error": e.Error()})
			return
		}
		out := []string{}
		versions := []storage.Snapshot{}
		for _, s := range snaps {
			if strings.HasPrefix(s.Name, strings.TrimPrefix(space.Path, "/mnt/")+"@") {
				out = append(out, s.Name)
				versions = append(versions, s)
			}
		}
		jsonResponse(w, 200, map[string]any{"snapshots": out, "versions": versions})
		return
	}
	methods := map[string]string{"list": "GET", "download": "GET", "trash-download": "GET", "upload": "PUT", "mkdir": "POST", "move": "POST", "trash": "POST", "trash-list": "GET", "restore": "POST", "recover-version": "POST", "purge": "POST"}
	if methods[op] != q.Method {
		w.WriteHeader(405)
		return
	}
	if q.Method != "GET" && (space.ReadOnly || space.Access[u.Name] != "rw") {
		jsonResponse(w, 403, map[string]string{"error": "此空間為唯讀"})
		return
	}

	if q.Method != "GET" {
		f.mu.Lock()
		gate := f.writes[space.Path]
		if gate == nil {
			gate = make(chan struct{}, 1)
			f.writes[space.Path] = gate
		}
		f.mu.Unlock()
		select {
		case gate <- struct{}{}:
			defer func() { <-gate }()
		case <-q.Context().Done():
			return
		}
	}

	if q.Method != "GET" {
		fresh, err := f.manager.Lookup(u.Name)
		if err != nil || fresh.Disabled || fresh.UID != u.UID || fresh.Revision != u.Revision {
			w.WriteHeader(401)
			return
		}
		current, err := f.manager.GetShares()
		if err != nil {
			w.WriteHeader(503)
			return
		}
		permitted := false
		for _, s := range current {
			if s.ID == space.ID && !s.Disabled && !s.ReadOnly && s.Access[u.Name] == "rw" {
				permitted = true
				break
			}
		}
		if !permitted {
			w.WriteHeader(403)
			return
		}
	}
	limited, maxBytes := false, int64(0)
	if q.Method != "GET" && op != "trash" && op != "purge" {
		props, e := storage.NewStorageManager().DatasetProperties(strings.TrimPrefix(space.Path, "/mnt/"))
		if e != nil {
			jsonResponse(w, 503, map[string]string{"error": "無法確認配額狀態"})
			return
		}
		if op == "upload" || op == "recover-version" {
			target, _ := strconv.ParseInt(props["org.just-nas:quota"], 10, 64)
			used, _ := strconv.ParseInt(props["projectused@1"], 10, 64)
			if target > 0 {
				limited = true
				maxBytes = target - used
				if maxBytes < 0 {
					maxBytes = 0
				}
			}
		}
		if props["org.just-nas:quota-locked"] == "on" || props["org.just-nas:quota-locked"] == "pending" {
			jsonResponse(w, 403, map[string]string{"error": "已超過配額，目前僅可讀取、下載與移入回收區；30 天後依政策清理"})
			return
		}
	}
	rq := files.Request{Limited: limited, MaxBytes: maxBytes, Root: space.Path, Operation: op, Path: q.URL.Query().Get("path"), Target: q.URL.Query().Get("target"), User: u.Name, Replace: q.URL.Query().Get("replace") == "true"}
	if op == "recover-version" {
		snapshot := q.URL.Query().Get("snapshot")
		all, e := storage.NewStorageManager().GetSnapshots()
		if e != nil {
			w.WriteHeader(500)
			return
		}
		valid := false
		for _, s := range all {
			if s.Name == snapshot && strings.HasPrefix(snapshot, strings.TrimPrefix(space.Path, "/mnt/")+"@") {
				valid = true
			}
		}
		if !valid {
			jsonResponse(w, 403, map[string]string{"error": "此快照不屬於授權空間"})
			return
		}
		rq.SourceRoot = space.Path + "/.zfs/snapshot/" + strings.SplitN(snapshot, "@", 2)[1]
	}
	rq.Offset, _ = strconv.Atoi(q.URL.Query().Get("offset"))
	rq.Limit = 100
	gidOut, e := command.Run("", "id", "-g", u.Name)
	if e != nil {
		jsonResponse(w, 500, map[string]string{"error": "無法取得檔案帳號身分"})
		return
	}
	gid, e := strconv.ParseUint(strings.TrimSpace(string(gidOut)), 10, 32)
	if e != nil {
		w.WriteHeader(500)
		return
	}
	ctx, cancel := context.WithTimeout(q.Context(), 30*time.Minute)
	defer cancel()
	if q.Method != "GET" {
		key := randomToken()
		f.mu.Lock()
		f.active[key] = fileWrite{User: u.Name, Space: space.ID, Cancel: cancel}
		f.mu.Unlock()
		defer func() { f.mu.Lock(); delete(f.active, key); f.mu.Unlock() }()
	}
	var result bytes.Buffer
	if op == "download" || op == "trash-download" {
		// Preflight in the same UID, before emitting a successful HTTP status.
		check := rq
		check.Operation = "stat"
		if op == "trash-download" {
			check.Operation = "trash-stat"
		}
		if e = files.Run(ctx, check, uint32(u.UID), uint32(gid), strings.NewReader(""), &result); e != nil {
			jsonResponse(w, 400, map[string]string{"error": e.Error()})
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename*=UTF-8''%s", url.PathEscape(path.Base(rq.Path))))
		if e = files.Run(ctx, rq, uint32(u.UID), uint32(gid), strings.NewReader(""), w); e != nil {
			panic(http.ErrAbortHandler)
		}
		return
	}
	input := io.Reader(strings.NewReader(""))
	if op == "upload" {
		input = q.Body
	}
	if e = files.Run(ctx, rq, uint32(u.UID), uint32(gid), input, &result); e != nil {
		jsonResponse(w, 400, map[string]string{"error": e.Error()})
		return
	}
	if q.Method != "GET" {
		if _, e = command.Run("", "zpool", "sync", strings.Split(strings.TrimPrefix(space.Path, "/mnt/"), "/")[0]); e != nil {
			jsonResponse(w, 500, map[string]string{"error": "操作完成，但同步失敗: " + e.Error()})
			return
		}
		if e = storage.EnforceQuotaContext(q.Context(), strings.TrimPrefix(space.Path, "/mnt/"), false, time.Now()); e != nil {
			jsonResponse(w, 500, map[string]string{"error": "操作完成，但配額狀態更新失敗: " + e.Error()})
			return
		}
	}
	if op == "list" || op == "trash-list" {
		w.Header().Set("Content-Type", "application/json")
		w.Write(result.Bytes())
	} else {
		jsonResponse(w, 200, map[string]bool{"ok": true})
	}
}

func (f *filePortal) cancelWrites(user, space string) {
	if f == nil {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, w := range f.active {
		if (user != "" && w.User == user) || (space != "" && w.Space == space) {
			w.Cancel()
		}
	}
}

func (f *filePortal) stopAllWrites() {
	if f == nil {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, w := range f.active {
		w.Cancel()
	}
}
