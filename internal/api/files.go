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
type filePortal struct {
	mu       sync.Mutex
	sessions map[string]fileSession
	attempts map[string]time.Time
	manager  *shares.ShareManager
}

func newFilePortal(m *shares.ShareManager) *filePortal {
	return &filePortal{sessions: map[string]fileSession{}, attempts: map[string]time.Time{}, manager: m}
}
func (f *filePortal) handle(w http.ResponseWriter, q *http.Request) {
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
	if e != nil || u.Disabled || u.Revision != s.User.Revision {
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
	if op == "versions" && q.Method == "GET" {
		snaps, e := storage.NewStorageManager().GetSnapshots()
		if e != nil {
			jsonResponse(w, 500, map[string]string{"error": e.Error()})
			return
		}
		out := []string{}
		for _, s := range snaps {
			if strings.HasPrefix(s.Name, strings.TrimPrefix(space.Path, "/mnt/")+"@") {
				out = append(out, s.Name)
			}
		}
		jsonResponse(w, 200, map[string]any{"snapshots": out})
		return
	}
	methods := map[string]string{"list": "GET", "download": "GET", "upload": "PUT", "mkdir": "POST", "move": "POST", "trash": "POST", "trash-list": "GET", "restore": "POST", "recover-version": "POST", "purge": "POST"}
	if methods[op] != q.Method {
		w.WriteHeader(405)
		return
	}
	if q.Method != "GET" && (space.ReadOnly || space.Access[u.Name] != "rw") {
		jsonResponse(w, 403, map[string]string{"error": "此空間為唯讀"})
		return
	}
	if q.Method != "GET" && op != "trash" && op != "purge" {
		props, e := storage.NewStorageManager().DatasetProperties(strings.TrimPrefix(space.Path, "/mnt/"))
		if e != nil {
			jsonResponse(w, 503, map[string]string{"error": "無法確認配額狀態"})
			return
		}
		if props["org.just-nas:quota-locked"] == "on" || props["org.just-nas:quota-locked"] == "pending" {
			jsonResponse(w, 403, map[string]string{"error": "已超過配額，目前僅可讀取、下載與移入回收區；30 天後依政策清理"})
			return
		}
	}
	rq := files.Request{Root: space.Path, Operation: op, Path: q.URL.Query().Get("path"), Target: q.URL.Query().Get("target"), User: u.Name, Replace: q.URL.Query().Get("replace") == "true"}
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
	var result bytes.Buffer
	if op == "download" {
		// Preflight in the same UID, before emitting a successful HTTP status.
		check := rq
		check.Operation = "stat"
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
	if op == "list" || op == "trash-list" {
		w.Header().Set("Content-Type", "application/json")
		w.Write(result.Bytes())
	} else {
		jsonResponse(w, 200, map[string]bool{"ok": true})
	}
}
