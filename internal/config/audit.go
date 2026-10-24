package config

import (
	"sync"
	"time"
)

var auditMu sync.Mutex

type AuditEntry struct {
	At     time.Time `json:"at"`
	Action string    `json:"action"`
}

func RecordAudit(action string) {
	auditMu.Lock()
	defer auditMu.Unlock()
	a := []AuditEntry{}
	_ = ReadJSON("audit.json", &a)
	a = append(a, AuditEntry{time.Now(), action})
	if len(a) > 1000 {
		a = a[len(a)-1000:]
	}
	if WriteJSON("audit.json", a) == nil && Persistent() {
		_ = NewPersistenceManager().SaveConfigToZfs()
	}
}
