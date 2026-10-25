package api

import (
	"just-nas/internal/config"
	"just-nas/internal/tasks"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestLauncherRequiresAdministratorAndIsReadOnly(t *testing.T) {
	old := config.Dir
	config.Dir = t.TempDir()
	defer func() { config.Dir = old }()
	r := &Router{jobs: tasks.New()}
	a := &auth{Sessions: map[string]session{"admin": {CSRF: "proof", Expires: time.Now().Add(time.Hour)}}}
	h := a.protect(http.HandlerFunc(r.storeAPI))
	for _, tc := range []struct {
		method, path, cookie, value string
		want                        int
	}{
		{"GET", "/api/apps/launcher", "", "", 401},
		{"GET", "/api/apps/launcher", "nas_file_session", "family", 401},
		{"GET", "/api/apps/site/icon", "", "", 401},
		{"GET", "/api/apps/launcher", "nas_session", "admin", 200},
		{"POST", "/api/apps/launcher", "nas_session", "admin", 405},
	} {
		q := httptest.NewRequest(tc.method, "http://nas"+tc.path, nil)
		if tc.cookie != "" {
			q.AddCookie(&http.Cookie{Name: tc.cookie, Value: tc.value})
		}
		q.Header.Set("X-CSRF-Token", "proof")
		q.Header.Set("Origin", "http://nas")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, q)
		if w.Code != tc.want {
			t.Fatalf("%s %s %s got %d: %s", tc.method, tc.path, tc.cookie, w.Code, w.Body.String())
		}
	}
}
