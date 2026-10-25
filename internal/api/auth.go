package api

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"just-nas/internal/config"
	"just-nas/internal/extensions"
)

type admin struct {
	Salt string `json:"salt"`
	Hash string `json:"hash"`
}
type session struct {
	CSRF    string
	Expires time.Time
}
type auth struct {
	mu         sync.Mutex
	Admin      admin
	SetupToken string
	Sessions   map[string]session
	Attempts   map[string]time.Time
}

func randomToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
func passwordHash(pass, salt string) string {
	k, err := pbkdf2.Key(sha256.New, pass, []byte(salt), 600000, 32)
	if err != nil {
		panic(err)
	}
	return hex.EncodeToString(k)
}
func newAuth() *auth {
	a := &auth{Sessions: map[string]session{}, Attempts: map[string]time.Time{}}
	if config.RestoreError() != "" {
		return a
	}
	if err := config.ReadJSON("admin.json", &a.Admin); err != nil {
		if !os.IsNotExist(err) {
			panic(fmt.Errorf("管理員設定損壞，拒絕重新初始化: %w", err))
		}
		a.SetupToken = randomToken()
		_ = config.AtomicWrite(filepath.Join(config.Dir, "setup-token"), []byte(a.SetupToken+"\n"), 0600)
		// Local console only, never expose this token through the public API.
		if f, err := os.OpenFile("/dev/console", os.O_WRONLY, 0); err == nil {
			fmt.Fprintf(f, "\nJust NAS setup token: %s\nRead local /var/lib/just-nas/setup-token if needed.\n", a.SetupToken)
			f.Close()
		}
	}
	return a
}
func sameOrigin(req *http.Request) bool {
	site := req.Header.Get("Sec-Fetch-Site")
	if site == "cross-site" {
		return false
	}
	if origin := req.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		scheme := "http"
		if req.TLS != nil {
			scheme = "https"
		}
		if err != nil || u.Scheme != scheme || u.Hostname() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
			return false
		}
		target, err := url.Parse(scheme + "://" + req.Host)
		if err != nil || target.Hostname() == "" {
			return false
		}
		if originAuthority(u) == originAuthority(target) {
			return true
		}
		// Some local browser/proxy configurations remove only Origin's port.
		// Accept that case only with browser-controlled same-origin metadata
		// AND a Referer retaining the exact destination scheme, host and port.
		if u.Port() != "" || target.Port() == "" || site != "same-origin" || !strings.EqualFold(u.Hostname(), target.Hostname()) {
			return false
		}
		ref, err := url.Parse(req.Referer())
		return err == nil && ref.User == nil && ref.Scheme == scheme && originAuthority(ref) == originAuthority(target)
	}
	return true
}

func originAuthority(u *url.URL) string {
	port := u.Port()
	if port == "" {
		if u.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	return net.JoinHostPort(strings.ToLower(u.Hostname()), port)
}
func (a *auth) handle(w http.ResponseWriter, req *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if req.Method == http.MethodGet && req.URL.Path == "/api/auth/status" {
		logged, csrf := false, ""
		if c, e := req.Cookie("nas_session"); e == nil {
			if s, ok := a.Sessions[c.Value]; ok && time.Now().Before(s.Expires) {
				logged = true
				csrf = s.CSRF
			}
		}
		jsonResponse(w, 200, map[string]any{"initialized": a.Admin.Hash != "", "authenticated": logged, "csrf": csrf, "persistent": config.Persistent(), "recovery_error": config.RestoreError()})
		return
	}
	if req.Method != http.MethodPost || !sameOrigin(req) {
		jsonResponse(w, 403, map[string]string{"error": "請求來源或方法不允許"})
		return
	}
	if req.URL.Path == "/api/auth/logout" {
		if c, e := req.Cookie("nas_session"); e == nil {
			if s, ok := a.Sessions[c.Value]; !ok || s.CSRF != req.Header.Get("X-CSRF-Token") {
				jsonResponse(w, 403, map[string]string{"error": "CSRF 驗證失敗"})
				return
			}
			delete(a.Sessions, c.Value)
		}
		http.SetCookie(w, &http.Cookie{Name: "nas_session", Value: "", Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: -1})
		jsonResponse(w, 200, map[string]string{"message": "已登出"})
		return
	}
	var body struct {
		Password        string `json:"password"`
		Token           string `json:"token"`
		CurrentPassword string `json:"current_password"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, req.Body, 4096)).Decode(&body) != nil {
		jsonResponse(w, 400, map[string]string{"error": "請求格式無效"})
		return
	}
	host, _, _ := net.SplitHostPort(req.RemoteAddr)
	if len(a.Attempts) > 1024 {
		for k, v := range a.Attempts {
			if time.Since(v) > time.Minute {
				delete(a.Attempts, k)
			}
		}
	}
	if time.Since(a.Attempts[host]) < time.Second {
		jsonResponse(w, 429, map[string]string{"error": "請稍候再試"})
		return
	}
	a.Attempts[host] = time.Now()
	switch req.URL.Path {
	case "/api/auth/password":
		c, e := req.Cookie("nas_session")
		valid := false
		if e == nil {
			if sess, ok := a.Sessions[c.Value]; ok && time.Now().Before(sess.Expires) && sess.CSRF == req.Header.Get("X-CSRF-Token") {
				valid = true
			}
		}
		if !valid {
			jsonResponse(w, 403, map[string]string{"error": "登入或 CSRF 驗證失敗"})
			return
		}
		if len(body.Password) < 12 || len(body.Password) > 128 || len(body.CurrentPassword) > 128 {
			jsonResponse(w, 400, map[string]string{"error": "新密碼長度須為 12–128 字元"})
			return
		}
		if subtle.ConstantTimeCompare([]byte(passwordHash(body.CurrentPassword, a.Admin.Salt)), []byte(a.Admin.Hash)) != 1 {
			jsonResponse(w, 401, map[string]string{"error": "目前密碼錯誤"})
			return
		}
		value := admin{Salt: randomToken()}
		value.Hash = passwordHash(body.Password, value.Salt)
		if err := config.WriteJSON("admin.json", value); err != nil {
			jsonResponse(w, 500, map[string]string{"error": err.Error()})
			return
		}
		if config.Persistent() {
			if err := config.NewPersistenceManager().SaveConfigToZfs(); err != nil {
				_ = config.WriteJSON("admin.json", a.Admin)
				jsonResponse(w, 500, map[string]string{"error": err.Error()})
				return
			}
		}
		a.Admin = value
		a.Sessions = map[string]session{}
	case "/api/auth/setup":
		if a.Admin.Hash != "" || a.SetupToken == "" || subtle.ConstantTimeCompare([]byte(body.Token), []byte(a.SetupToken)) != 1 {
			jsonResponse(w, 403, map[string]string{"error": "初始化憑證無效"})
			return
		}
		if len(body.Password) < 12 || len(body.Password) > 128 {
			jsonResponse(w, 400, map[string]string{"error": "密碼長度須為 12–128 字元"})
			return
		}
		value := admin{Salt: randomToken()}
		value.Hash = passwordHash(body.Password, value.Salt)
		if err := config.WriteJSON("admin.json", value); err != nil {
			jsonResponse(w, 500, map[string]string{"error": err.Error()})
			return
		}
		a.Admin = value
		a.SetupToken = ""
		_ = os.Remove(filepath.Join(config.Dir, "setup-token"))
		if config.Persistent() {
			if err := config.NewPersistenceManager().SaveConfigToZfs(); err != nil {
				jsonResponse(w, 500, map[string]string{"error": err.Error()})
				return
			}
		}
	case "/api/auth/login":
		if a.Admin.Hash == "" || len(body.Password) > 128 || subtle.ConstantTimeCompare([]byte(passwordHash(body.Password, a.Admin.Salt)), []byte(a.Admin.Hash)) != 1 {
			jsonResponse(w, 401, map[string]string{"error": "密碼錯誤"})
			return
		}
	default:
		jsonResponse(w, 404, map[string]string{"error": "找不到此 API"})
		return
	}
	for id, s := range a.Sessions {
		if time.Now().After(s.Expires) {
			delete(a.Sessions, id)
		}
	}
	id, csrf := randomToken(), randomToken()
	a.Sessions[id] = session{csrf, time.Now().Add(12 * time.Hour)}
	http.SetCookie(w, &http.Cookie{Name: "nas_session", Value: id, Path: "/", HttpOnly: true, Secure: req.TLS != nil, SameSite: http.SameSiteStrictMode, MaxAge: 43200})
	jsonResponse(w, 200, map[string]any{"csrf": csrf, "persistent": config.Persistent()})
}
func (a *auth) protect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		a.mu.Lock()
		var s session
		var ok bool
		if c, e := req.Cookie("nas_session"); e == nil {
			s, ok = a.Sessions[c.Value]
		}
		a.mu.Unlock()
		if !ok || time.Now().After(s.Expires) {
			jsonResponse(w, 401, map[string]string{"error": "請先登入"})
			return
		}
		if req.Method != http.MethodGet && (!sameOrigin(req) || req.Header.Get("X-CSRF-Token") != s.CSRF) {
			jsonResponse(w, 403, map[string]string{"error": "CSRF 驗證失敗"})
			return
		}
		if restoring.Load() && req.Method != "GET" {
			jsonResponse(w, 503, map[string]string{"error": "設定恢復中，請等待重新啟動"})
			return
		}
		limit := int64(64 * 1024)
		if req.Method == "POST" && req.URL.Path == "/api/app-services/vm-manager/isos" {
			limit = 16 << 30
		}
		if req.URL.Path == "/api/settings/import" {
			limit = 24 << 20
		}
		if req.Method == "POST" && (req.URL.Path == "/api/extensions/import" || req.URL.Path == "/api/apps/import") {
			limit = extensions.MaxBundle
		}
		req.Body = http.MaxBytesReader(w, req.Body, limit)
		next.ServeHTTP(w, req)
		if req.Method != "GET" && !restoring.Load() {
			config.RecordAudit(req.Method + " " + req.URL.Path)
		}
	})
}
