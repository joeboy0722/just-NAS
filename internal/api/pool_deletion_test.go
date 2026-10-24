package api

import (
	"context"
	"encoding/json"
	"fmt"
	"just-nas/internal/backup"
	"just-nas/internal/command"
	"just-nas/internal/config"
	"just-nas/internal/shares"
	"just-nas/internal/tasks"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPoolTaskReferenceBoundaries(t *testing.T) {
	ids := map[string]bool{"share-id": true}
	devices := map[string]bool{"/dev/testdisk": true}
	for _, c := range []struct {
		kind, resource string
		match          bool
	}{{"scrub", "tank", true}, {"scrub", "tank2", false}, {"quota", "tank/a", true}, {"quota", "tank2/a", false}, {"quota", "other/tank", false}, {"permissions", "share-id|{}", true}, {"permissions", "other-id|{}", false}, {"replace", "tank|/dev/a|/dev/b", true}, {"replace", "tank2|/dev/a|/dev/b", false}, {"backup-restore", "snap:tank/restore", true}, {"backup-restore", "snap:tank2/restore", false}, {"smart-short", "/dev/testdisk", true}, {"smart-short", "/dev/other", false}, {"backup", "backup", true}} {
		if got := cleanupTaskMatch("tank", c.kind, c.resource, ids, devices, true); got != c.match {
			t.Errorf("%s %s: %t", c.kind, c.resource, got)
		}
	}
}
func TestPoolDeletionCleanupAndRollback(t *testing.T) {
	for _, mode := range []string{"success", "destroy-fails", "reload-fails", "config-pool", "metadata-fails"} {
		t.Run(mode, func(t *testing.T) {
			oldDir, oldRun := config.Dir, command.Run
			config.Dir = t.TempDir()
			defer func() { config.Dir = oldDir; command.Run = oldRun }()
			saved := t.TempDir()
			alive := true
			destroyed := false
			reloadFailed := false
			command.Run = func(_ string, name string, args ...string) ([]byte, error) {
				a := strings.Join(args, " ")
				if name == "zpool" && strings.HasPrefix(a, "get ") {
					return []byte("7\n"), nil
				}
				if name == "zpool" && strings.HasPrefix(a, "list ") {
					if alive {
						return []byte("system\t1\ntank\t7\n"), nil
					}
					return []byte("system\t1\n"), nil
				}
				if name == "zpool" && strings.HasPrefix(a, "status ") {
					return []byte(`{"pools":{"tank":{"vdevs":{"d":{"vdev_type":"disk","path":"/dev/testdisk","state":"ONLINE"}}}}}`), nil
				}
				if name == "lsblk" {
					return []byte("/dev/testdisk\n"), nil
				}
				if name == "zfs" && strings.Contains(a, "name,value") {
					if mode == "metadata-fails" {
						return nil, fmt.Errorf("cannot inspect settings pool")
					}
					if mode == "config-pool" {
						return []byte("tank/.just-nas\t1\n"), nil
					}
					return []byte("system/.just-nas\t1\n"), nil
				}
				if name == "zfs" && strings.Contains(a, "mounted,mountpoint") {
					return []byte("yes\n" + saved + "\n"), nil
				}
				if name == "rc-service" && strings.Contains(a, "samba reload") && mode == "reload-fails" && !reloadFailed {
					reloadFailed = true
					return nil, fmt.Errorf("reload failed")
				}
				if name == "zpool" && strings.HasPrefix(a, "destroy ") {
					if mode == "destroy-fails" {
						return nil, fmt.Errorf("pool is busy")
					}
					destroyed = true
					alive = false
				}
				return nil, nil
			}
			ss := []shares.ShareConfig{{ID: "share-id", Name: "Gone", Protocol: "smb", Path: "/mnt/tank/files"}, {ID: "other-id", Name: "Keep", Protocol: "nfs", Path: "/mnt/tank2/files", AllowedHosts: "192.168.1.1"}}
			users := []shares.User{{Name: "alice", UID: 1000, ID: "alice-id", Home: "/mnt/tank/private", Hash: "preserve-secret"}}
			jobs := []tasks.Job{{ID: "gone", Kind: "quota", Resource: "tank/files", State: "failed"}, {ID: "stay", Kind: "quota", Resource: "tank2/files", State: "succeeded"}, {ID: "perm", Kind: "permissions", Resource: "share-id|{}", State: "succeeded"}}
			schedules := []tasks.Schedule{{ID: "gone", Kind: "quota", Resource: "tank/files"}, {ID: "stay", Kind: "quota", Resource: "tank2/files"}, {ID: "disk", Kind: "smart-short", Resource: "/dev/testdisk"}}
			for name, value := range map[string]any{"shares.json": ss, "users.json": users, "jobs.json": jobs, "schedules.json": schedules, "backup.json": backup.Settings{Mount: "/mnt/backup", Password: "keep-password", Datasets: []string{"tank/files", "tank2/files"}}, "alerts.json": []Alert{{ID: "quota-tank/files"}, {ID: "pool-health-tank"}, {ID: "quota:tank/aged-out"}, {ID: "quota-tank2/files"}}} {
				if e := config.WriteJSON(name, value); e != nil {
					t.Fatal(e)
				}
			}
			r := NewRouter()
			r.jobs = tasks.New()
			r.shareMgr = &shares.ShareManager{SmbConfPath: filepath.Join(config.Dir, "smb.conf"), NfsConfPath: filepath.Join(config.Dir, "exports")}
			r.portal = newFilePortal(r.shareMgr)
			summary, e := r.deletePool(context.Background(), "tank")
			if mode != "success" {
				if e == nil {
					t.Fatal("failure reported success")
				}
				if destroyed {
					t.Fatal("destroyed on preflight failure")
				}
				var got []shares.ShareConfig
				config.ReadJSON("shares.json", &got)
				if len(got) != 2 {
					t.Fatal("lost share settings", got)
				}
				if len(r.jobs.Schedules()) != 3 {
					t.Fatal("lost schedules")
				}
				if config.PoolDeletionPending() {
					t.Fatal("rollback left a pending transaction")
				}
				return
			}
			if e != nil {
				t.Fatal(e)
			}
			if !destroyed || summary.Jobs != 2 || summary.Schedules != 2 || summary.Alerts != 3 {
				t.Fatalf("%+v", summary)
			}
			got, e := r.shareMgr.GetShares()
			if e != nil || len(got) != 1 || got[0].Path != "/mnt/tank2/files" {
				t.Fatal(got, e)
			}
			if len(r.jobs.Jobs()) != 1 || len(r.jobs.Schedules()) != 1 {
				t.Fatal("orphaned tasks")
			}
			var settings backup.Settings
			config.ReadJSON("backup.json", &settings)
			if len(settings.Datasets) != 1 || settings.Datasets[0] != "tank2/files" || settings.Password != "keep-password" {
				t.Fatal("mixed backup damaged")
			}
			var us []shares.User
			config.ReadJSON("users.json", &us)
			if len(us) != 1 || us[0].Home != "" || us[0].Hash != "preserve-secret" {
				t.Fatal("account was removed or changed")
			}
			if _, e = os.Stat(filepath.Join(config.Dir, "pool-deletion.json")); !os.IsNotExist(e) {
				t.Fatal("transaction not completed")
			}
			if _, e = os.Stat(filepath.Join(saved, "CURRENT")); e != nil {
				t.Fatal("state not persistent", e)
			}
			if e = r.jobs.ReloadPaused(); e == nil {
				t.Fatal("engine was left paused")
			}
			// Persisted generations no longer contain a recoverable old backup password in the journal.
			var alerts []Alert
			config.ReadJSON("alerts.json", &alerts)
			if len(alerts) != 1 || alerts[0].ID != "quota-tank2/files" {
				b, _ := json.Marshal(alerts)
				t.Fatal(string(b))
			}
		})
	}
}
func TestLastBackupSourceRemovesBackupSchedules(t *testing.T) {
	oldDir, oldRun := config.Dir, command.Run
	config.Dir = t.TempDir()
	defer func() { config.Dir = oldDir; command.Run = oldRun }()
	command.Run = func(_ string, name string, args ...string) ([]byte, error) { return []byte(`{"pools":{}}`), nil }
	config.WriteJSON("backup.json", backup.Settings{Datasets: []string{"tank/files"}, Password: "remove-this-credential"})
	config.WriteJSON("jobs.json", []tasks.Job{{ID: "b", Kind: "backup", Resource: "backup"}, {ID: "other", Kind: "quota", Resource: "tank2/files"}})
	config.WriteJSON("schedules.json", []tasks.Schedule{{ID: "b", Kind: "backup", Resource: "backup"}, {ID: "c", Kind: "backup-check", Resource: "backup"}, {ID: "other", Kind: "quota", Resource: "tank2/files"}})
	r := NewRouter()
	r.jobs = tasks.New()
	record, summary, _, err := r.poolCleanupPlan("tank", "7")
	if err != nil {
		t.Fatal(err)
	}
	if !summary.BackupRemoved || record.After["backup.json"] != nil || summary.Jobs != 1 || summary.Schedules != 2 {
		t.Fatalf("%+v", summary)
	}
	var schedules []tasks.Schedule
	json.Unmarshal(record.After["schedules.json"], &schedules)
	if len(schedules) != 1 || schedules[0].Resource != "tank2/files" {
		t.Fatal("other pool schedule removed")
	}
}
func TestPoolDeletionStopsAndFencesFileWrites(t *testing.T) {
	f := newFilePortal(shares.NewShareManager())
	cancelled, done := make(chan struct{}), make(chan struct{})
	f.active["write"] = fileWrite{Space: "target", Cancel: func() { close(cancelled) }, Done: done}
	finished := make(chan error, 1)
	go func() { finished <- f.blockSpaces(context.Background(), []string{"target"}) }()
	<-cancelled
	select {
	case <-finished:
		t.Fatal("did not wait for active worker")
	default:
	}
	close(done)
	if e := <-finished; e != nil {
		t.Fatal(e)
	}
	if !f.blocked["target"] {
		t.Fatal("new writes not fenced")
	}
	f.unblockSpaces([]string{"target"})
	if f.blocked["target"] {
		t.Fatal("rollback did not release fence")
	}
}
