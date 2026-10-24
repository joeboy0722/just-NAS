package api

import (
	"just-nas/internal/config"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSMTPPasswordPreservationAndRedaction(t *testing.T) {
	priorDir := config.Dir
	config.Dir = t.TempDir()
	defer func() { config.Dir = priorDir }()
	original := SMTPSettings{Host: "smtp.example.test", Port: 587, Password: "keep-this-test-secret"}
	if e := config.WriteJSON("smtp.json", original); e != nil {
		t.Fatal(e)
	}
	router := NewRouter()
	for _, c := range []struct{ body, want string }{{`{"host":"changed.example.test","port":587}`, original.Password}, {`{"host":"changed.example.test","password":"replacement"}`, "replacement"}, {`{"password":""}`, ""}} {
		w := httptest.NewRecorder()
		router.alertsAPI(w, httptest.NewRequest("POST", "/api/smtp", strings.NewReader(c.body)))
		var stored SMTPSettings
		if e := config.ReadJSON("smtp.json", &stored); e != nil {
			t.Fatal(e)
		}
		if stored.Password != c.want {
			t.Fatal("SMTP password was unexpectedly changed")
		}
		w = httptest.NewRecorder()
		router.alertsAPI(w, httptest.NewRequest("GET", "/api/smtp", nil))
		if strings.Contains(w.Body.String(), "replacement") || strings.Contains(w.Body.String(), original.Password) {
			t.Fatal("secret leaked")
		}
	}
}
