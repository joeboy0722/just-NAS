package tasks

import (
	"strings"
	"testing"
	"time"
)

func TestRemovedOrChangedScheduledSnapshotCannotStart(t *testing.T) {
	candidate := Schedule{ID: "id", Kind: "quota", Resource: "tank/files", Enabled: true, EverySeconds: 60, LastRun: time.Now().Add(-time.Hour)}
	for _, current := range [][]Schedule{nil, {{ID: "id", Kind: "quota", Resource: "tank2/files", Enabled: true}}, {{ID: "id", Kind: "quota", Resource: "tank/files", Enabled: false}}} {
		e := &Engine{schedules: current}
		if _, err := e.startScheduled(candidate); err == nil || !strings.Contains(err.Error(), "排程已移除") {
			t.Fatal("stale scheduler snapshot was accepted", err)
		}
	}
}
