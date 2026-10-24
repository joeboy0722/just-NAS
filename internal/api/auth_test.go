package api

import (
	"encoding/json"
	"io"
	"just-nas/internal/config"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAuthAndCSRF(t *testing.T) {
	a := &auth{Sessions: map[string]session{"id": {CSRF: "csrf", Expires: time.Now().Add(time.Hour)}}}
	h := a.protect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	for _, c := range []struct {
		cookie, csrf, origin string
		want                 int
	}{{"", "", "", 401}, {"id", "", "", 403}, {"id", "csrf", "http://evil.test", 403}, {"id", "csrf", "http://nas", 204}} {
		r := httptest.NewRequest("POST", "http://nas/api/shares", strings.NewReader("{}"))
		if c.cookie != "" {
			r.AddCookie(&http.Cookie{Name: "nas_session", Value: c.cookie})
		}
		r.Header.Set("X-CSRF-Token", c.csrf)
		r.Header.Set("Origin", c.origin)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != c.want {
			t.Errorf("got %d want %d", w.Code, c.want)
		}
	}
}

func TestPasswordChangeInvalidatesOtherSessions(t *testing.T) {
	oldDir := config.Dir
	config.Dir = t.TempDir()
	t.Cleanup(func() { config.Dir = oldDir })
	original := admin{Salt: "test-salt"}
	original.Hash = passwordHash("original-password", original.Salt)
	a := &auth{Admin: original, Sessions: map[string]session{"old": {CSRF: "csrf", Expires: time.Now().Add(time.Hour)}, "other": {Expires: time.Now().Add(time.Hour)}}, Attempts: map[string]time.Time{}}
	r := httptest.NewRequest("POST", "http://nas/api/auth/password", strings.NewReader(`{"current_password":"original-password","password":"updated-password"}`))
	r.AddCookie(&http.Cookie{Name: "nas_session", Value: "old"})
	r.Header.Set("X-CSRF-Token", "csrf")
	w := httptest.NewRecorder()
	a.handle(w, r)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if len(a.Sessions) != 1 {
		t.Fatal("old sessions remain")
	}
	if _, ok := a.Sessions["other"]; ok {
		t.Fatal("other session survived")
	}
	if a.Admin.Hash != passwordHash("updated-password", a.Admin.Salt) {
		t.Fatal("password not updated")
	}
	var response map[string]any
	if json.Unmarshal(w.Body.Bytes(), &response) != nil || response["csrf"] == "" {
		t.Fatal("missing renewed session")
	}
}
func TestPasswordHash(t *testing.T) {
	a := passwordHash("twelve-characters", "salt")
	if a == passwordHash("other-password", "salt") || a != passwordHash("twelve-characters", "salt") {
		t.Fatal("bad password derivation")
	}
}

func TestOriginWithRemovedPort(t *testing.T) {
	for _, c := range []struct {
		name, origin, referer, site string
		want                        bool
	}{
		{"exact", "http://127.0.0.1:8080", "", "same-origin", true},
		{"reported Chrome headers", "http://127.0.0.1", "http://127.0.0.1:8080/", "same-origin", true},
		{"missing metadata", "http://127.0.0.1", "http://127.0.0.1:8080/", "", false},
		{"missing Referer", "http://127.0.0.1", "", "same-origin", false},
		{"different Referer port", "http://127.0.0.1", "http://127.0.0.1:9000/", "same-origin", false},
		{"different explicit Origin port", "http://127.0.0.1:9000", "http://127.0.0.1:8080/", "same-origin", false},
		{"same-site cross-port attack", "http://127.0.0.1", "http://127.0.0.1:8080/", "same-site", false},
		{"cross-site attack", "http://evil.test", "http://127.0.0.1:8080/", "cross-site", false},
		{"forged metadata different host", "http://evil.test", "http://127.0.0.1:8080/", "same-origin", false},
		{"opaque origin", "null", "http://127.0.0.1:8080/", "same-origin", false},
		{"different scheme", "https://127.0.0.1", "http://127.0.0.1:8080/", "same-origin", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "http://127.0.0.1:8080/api/auth/login", nil)
			r.Header.Set("Origin", c.origin)
			r.Header.Set("Referer", c.referer)
			r.Header.Set("Sec-Fetch-Site", c.site)
			if got := sameOrigin(r); got != c.want {
				t.Fatalf("got %v want %v", got, c.want)
			}
		})
	}
}

func TestPortlessOriginDoesNotBypassCSRF(t *testing.T) {
	a := &auth{Sessions: map[string]session{"id": {CSRF: "csrf", Expires: time.Now().Add(time.Hour)}}}
	h := a.protect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	for _, token := range []string{"", "incorrect", "csrf"} {
		r := httptest.NewRequest("POST", "http://127.0.0.1:8080/api/shares", nil)
		r.AddCookie(&http.Cookie{Name: "nas_session", Value: "id"})
		r.Header.Set("Origin", "http://127.0.0.1")
		r.Header.Set("Referer", "http://127.0.0.1:8080/")
		r.Header.Set("Sec-Fetch-Site", "same-origin")
		r.Header.Set("X-CSRF-Token", token)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		want := 403
		if token == "csrf" {
			want = 204
		}
		if w.Code != want {
			t.Fatalf("token %q got %d want %d", token, w.Code, want)
		}
	}
}

func TestLargeStoreImportsKeepAuthAndOtherBodyLimits(t *testing.T) {
	old := config.Dir
	config.Dir = t.TempDir()
	defer func() { config.Dir = old }()
	a := &auth{Sessions: map[string]session{"id": {CSRF: "csrf", Expires: time.Now().Add(time.Hour)}}}
	h := a.protect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, e := io.Copy(io.Discard, r.Body)
		if e != nil {
			w.WriteHeader(413)
			return
		}
		w.WriteHeader(204)
	}))
	for _, path := range []string{"/api/extensions/import", "/api/apps/import", "/api/shares"} {
		r := httptest.NewRequest("POST", "http://nas"+path, strings.NewReader(strings.Repeat("x", 128<<10)))
		r.AddCookie(&http.Cookie{Name: "nas_session", Value: "id"})
		r.Header.Set("X-CSRF-Token", "csrf")
		r.Header.Set("Origin", "http://nas")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		want := 204
		if path == "/api/shares" {
			want = 413
		}
		if w.Code != want {
			t.Fatalf("%s: %d", path, w.Code)
		}
	}
}
