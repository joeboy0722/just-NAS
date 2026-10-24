package api

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"just-nas/internal/config"
	"net"
	"net/http"
	"net/smtp"
	"os"
	"strings"
	"sync"
	"time"
)

type Alert struct {
	ID, Severity, Message string
	Active                bool
	FirstSeen, LastSeen   time.Time
}
type SMTPSettings struct {
	Enabled                      bool
	Host                         string
	Port                         int
	Username, Password, From, To string
}

var alertMu sync.Mutex

func (r *Router) pollAlerts() {
	if restoring.Load() || !config.Persistent() {
		return
	}
	active := map[string]Alert{}
	pools, e := r.storageMgr.GetPools()
	if e != nil {
		active["pools-unavailable"] = Alert{ID: "pools-unavailable", Severity: "critical", Message: "無法讀取儲存池: " + e.Error()}
	}
	for _, p := range pools {
		if p.Health != "ONLINE" {
			id := "pool-health-" + p.Name
			active[id] = Alert{ID: id, Severity: "critical", Message: p.Name + " 狀態 " + p.Health}
		}
		if p.CapacityPct >= 80 {
			sev := "warning"
			if p.CapacityPct >= 90 {
				sev = "critical"
			}
			id := "pool-capacity-" + p.Name
			active[id] = Alert{ID: id, Severity: sev, Message: fmt.Sprintf("%s 使用率 %.0f%%", p.Name, p.CapacityPct)}
		}
	}
	latest := map[string]struct{ state, err string }{}
	for _, j := range r.jobs.Jobs() {
		latest[j.Kind+":"+j.Resource] = struct{ state, err string }{j.State, j.Error}
	}
	for id, j := range latest {
		if j.state == "failed" || j.state == "interrupted" {
			active[id] = Alert{ID: id, Severity: "warning", Message: id + ": " + j.err}
		}
	}
	alertMu.Lock()
	defer alertMu.Unlock()
	previous := []Alert{}
	_ = config.ReadJSON("alerts.json", &previous)
	old := map[string]Alert{}
	for _, a := range previous {
		old[a.ID] = a
	}
	out := []Alert{}
	changed := false
	newAlerts := []Alert{}
	for id, a := range active {
		a.Active = true
		a.LastSeen = time.Now()
		a.FirstSeen = a.LastSeen
		if p, ok := old[id]; ok {
			a.FirstSeen = p.FirstSeen
			if !p.Active || p.Severity != a.Severity || p.Message != a.Message {
				changed = true
				newAlerts = append(newAlerts, a)
			}
		} else {
			changed = true
			newAlerts = append(newAlerts, a)
		}
		out = append(out, a)
		delete(old, id)
	}
	for _, a := range old {
		if a.Active {
			changed = true
			a.Active = false
			a.LastSeen = time.Now()
		}
		out = append(out, a)
	}
	if len(out) > 200 {
		out = out[:200]
	}
	if changed {
		if config.WriteJSON("alerts.json", out) == nil {
			_ = config.NewPersistenceManager().SaveConfigToZfs()
		}
		var s SMTPSettings
		if config.ReadJSON("smtp.json", &s) == nil && s.Enabled {
			for _, a := range newAlerts {
				if e := sendAlert(s, a); e != nil {
					config.RecordAudit("SMTP delivery failed: " + e.Error())
				}
			}
		}
	}
}
func sendAlert(s SMTPSettings, a Alert) error {
	if s.Port < 1 || s.Port > 65535 || strings.ContainsAny(s.From+s.To+s.Host, "\r\n") {
		return fmt.Errorf("SMTP 設定無效")
	}
	conn, e := net.DialTimeout("tcp", net.JoinHostPort(s.Host, fmt.Sprint(s.Port)), 10*time.Second)
	if e != nil {
		return e
	}
	conn.SetDeadline(time.Now().Add(20 * time.Second))
	c, e := smtp.NewClient(conn, s.Host)
	if e != nil {
		conn.Close()
		return e
	}
	defer c.Close()
	if e = c.StartTLS(&tls.Config{ServerName: s.Host, MinVersion: tls.VersionTLS12}); e != nil {
		return e
	}
	if s.Username != "" {
		if e = c.Auth(smtp.PlainAuth("", s.Username, s.Password, s.Host)); e != nil {
			return e
		}
	}
	if e = c.Mail(s.From); e != nil {
		return e
	}
	if e = c.Rcpt(s.To); e != nil {
		return e
	}
	w, e := c.Data()
	if e != nil {
		return e
	}
	_, e = fmt.Fprintf(w, "From: %s\r\nTo: %s\r\nSubject: just-NAS %s alert\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s\r\n", s.From, s.To, a.Severity, a.Message)
	if e != nil {
		return e
	}
	return w.Close()
}
func (r *Router) alertsAPI(w http.ResponseWriter, q *http.Request) {
	if q.URL.Path == "/api/audit" && q.Method == "GET" {
		var a []config.AuditEntry
		_ = config.ReadJSON("audit.json", &a)
		jsonResponse(w, 200, a)
		return
	}
	if q.URL.Path == "/api/alerts" && q.Method == "GET" {
		var a []Alert
		_ = config.ReadJSON("alerts.json", &a)
		jsonResponse(w, 200, a)
		return
	}
	if q.URL.Path != "/api/smtp" {
		w.WriteHeader(405)
		return
	}
	if q.Method == "GET" {
		var s SMTPSettings
		err := config.ReadJSON("smtp.json", &s)
		if err != nil && !os.IsNotExist(err) {
			jsonResponse(w, 500, map[string]string{"error": err.Error()})
			return
		}
		s.Password = ""
		jsonResponse(w, 200, s)
		return
	}
	if q.Method != "POST" {
		w.WriteHeader(405)
		return
	}
	var s SMTPSettings
	if json.NewDecoder(q.Body).Decode(&s) != nil {
		w.WriteHeader(400)
		return
	}
	if s.Enabled && (s.Host == "" || s.Port < 1 || s.Port > 65535 || s.From == "" || s.To == "" || strings.ContainsAny(s.From+s.To+s.Host, "\r\n")) {
		jsonResponse(w, 400, map[string]string{"error": "SMTP 設定不完整"})
		return
	}
	if e := config.WriteJSON("smtp.json", s); e != nil {
		w.WriteHeader(500)
		return
	}
	if e := config.NewPersistenceManager().SaveConfigToZfs(); e != nil {
		jsonResponse(w, 500, map[string]string{"error": e.Error()})
		return
	}
	jsonResponse(w, 200, map[string]bool{"ok": true})
}
