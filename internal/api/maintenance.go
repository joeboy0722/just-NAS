package api

import (
	"context"
	"encoding/json"
	"fmt"
	"just-nas/internal/backup"
	"just-nas/internal/command"
	"just-nas/internal/config"
	"just-nas/internal/files"
	"just-nas/internal/storage"
	"just-nas/internal/tasks"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func (r *Router) registerMaintenance() {
	r.jobs = tasks.New()
	r.jobs.Register("replace", func(ctx context.Context, resource string, p func(string)) error {
		v := strings.Split(resource, "|")
		if len(v) != 3 {
			return fmt.Errorf("替換參數無效")
		}
		if e := r.storageMgr.Replace(v[0], v[1], v[2]); e != nil {
			return e
		}
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(5 * time.Second):
				status, e := r.storageMgr.PoolStatus(v[0])
				if e != nil {
					return e
				}
				p(status)
				if !strings.Contains(status, "resilver in progress") {
					return nil
				}
			}
		}
	})
	r.jobs.Register("permissions", func(ctx context.Context, resource string, p func(string)) error {
		v := strings.SplitN(resource, "|", 2)
		if len(v) != 2 {
			return fmt.Errorf("權限參數無效")
		}
		var access map[string]string
		if json.Unmarshal([]byte(v[1]), &access) != nil {
			return fmt.Errorf("權限格式無效")
		}
		return r.shareMgr.UpdateAccess(v[0], access)
	})
	for _, kind := range []string{"backup", "backup-check"} {
		k := kind
		r.jobs.Register(k, func(ctx context.Context, _ string, p func(string)) error { return backup.Execute(ctx, k, p) })
	}
	r.jobs.Register("backup-restore", func(ctx context.Context, resource string, p func(string)) error {
		v := strings.SplitN(resource, ":", 2)
		if len(v) != 2 {
			return fmt.Errorf("恢復參數無效")
		}
		return backup.Restore(ctx, v[0], v[1], p)
	})
	for _, tier := range []string{"hourly", "daily", "weekly"} {
		t := tier
		r.jobs.Register("snapshot-"+t, func(ctx context.Context, ds string, p func(string)) error {
			if !storage.ValidDataset(ds) {
				return fmt.Errorf("dataset invalid")
			}
			tag := "nas-auto-" + t + "-" + time.Now().UTC().Format("20060102-150405")
			p("建立 " + ds + "@" + tag)
			if _, e := command.Run("", "zfs", "snapshot", "-o", "org.just-nas:auto="+t, ds+"@"+tag); e != nil {
				return e
			}
			out, e := command.Run("", "zfs", "get", "-H", "-o", "name,value", "-t", "snapshot", "-s", "local", "org.just-nas:auto")
			if e != nil {
				return e
			}
			owned := []string{}
			for _, line := range strings.Split(string(out), "\n") {
				v := strings.Fields(line)
				if len(v) == 2 && v[1] == t && strings.HasPrefix(v[0], ds+"@nas-auto-"+t+"-") {
					owned = append(owned, v[0])
				}
			}
			sort.Strings(owned)
			keep := map[string]int{"hourly": 24, "daily": 14, "weekly": 8}[t]
			for _, schedule := range r.jobs.Schedules() {
				if schedule.Kind == "snapshot-"+t && schedule.Resource == ds && schedule.Retain > 0 {
					keep = schedule.Retain
					break
				}
			}
			if len(owned) > keep {
				for _, s := range owned[:len(owned)-keep] {
					if e = r.storageMgr.DestroySnapshot(s); e != nil {
						return e
					}
				}
			}
			return nil
		})
	}
	r.jobs.Register("quota", func(ctx context.Context, ds string, p func(string)) error {
		return storage.EnforceQuotaContext(ctx, ds, true, time.Now())
	})
	r.jobs.Register("scrub", func(ctx context.Context, pool string, p func(string)) error {
		if strings.Contains(pool, "/") {
			return fmt.Errorf("pool invalid")
		}
		if _, e := r.storageMgr.PoolStatus(pool); e != nil {
			return e
		}
		if _, e := command.Run("", "zpool", "scrub", pool); e != nil {
			return e
		}
		for {
			select {
			case <-ctx.Done():
				_, _ = command.Run("", "zpool", "scrub", "-s", pool)
				return ctx.Err()
			case <-time.After(5 * time.Second):
				status, e := r.storageMgr.PoolStatus(pool)
				if e != nil {
					return e
				}
				p(status)
				if !strings.Contains(status, "scrub in progress") {
					return nil
				}
			}
		}
	})
	r.jobs.Register("smart-short", func(ctx context.Context, device string, p func(string)) error {
		disks, e := r.storageMgr.GetSmartDisks()
		if e != nil {
			return e
		}
		valid := false
		for _, d := range disks {
			if d.Device == device {
				valid = true
			}
		}
		if !valid {
			return fmt.Errorf("未知硬碟")
		}
		_, e = command.Run("", "smartctl", "-t", "short", device)
		return e
	})
	r.jobs.Loop()
	go func() {
		for {
			time.Sleep(time.Minute)
			r.pollAlerts()
			ss, e := r.shareMgr.GetShares()
			if e == nil {
				for _, s := range ss {
					if s.Mode == "private" || s.Mode == "public" {
						_ = pruneTrash(filepath.Join(s.Path, ".nas-trash"), time.Now())
						_ = files.CleanUploads(s.Path, time.Now())
					}
				}
			}
		}
	}()
}
func (r *Router) maintenanceAPI(w http.ResponseWriter, q *http.Request) {
	var e error
	if strings.HasPrefix(q.URL.Path, "/api/tasks/") && q.Method == "POST" {
		e = r.jobs.Cancel(strings.TrimPrefix(q.URL.Path, "/api/tasks/"))
		if e != nil {
			jsonResponse(w, 400, map[string]string{"error": e.Error()})
		} else {
			jsonResponse(w, 200, map[string]bool{"ok": true})
		}
		return
	}
	switch q.URL.Path {
	case "/api/tasks":
		if q.Method == "GET" {
			jsonResponse(w, 200, r.jobs.Jobs())
			return
		}
		if q.Method != "POST" {
			w.WriteHeader(405)
			return
		}
		var b struct{ Kind, Resource string }
		if e = json.NewDecoder(q.Body).Decode(&b); e == nil {
			var id string
			id, e = r.jobs.Start(b.Kind, b.Resource)
			if e == nil {
				jsonResponse(w, 202, map[string]string{"id": id})
				return
			}
		}
	case "/api/schedules":
		if q.Method == "GET" {
			jsonResponse(w, 200, r.jobs.Schedules())
			return
		}
		if q.Method != "POST" {
			w.WriteHeader(405)
			return
		}
		var s tasks.Schedule
		if e = json.NewDecoder(q.Body).Decode(&s); e == nil {
			e = r.jobs.SetSchedule(s)
		}
	case "/api/backup/mount":
		if q.Method != "POST" {
			w.WriteHeader(405)
			return
		}
		var b struct{ UUID string }
		if e = json.NewDecoder(q.Body).Decode(&b); e == nil {
			var p string
			p, e = backup.MountUUID(b.UUID)
			if e == nil {
				jsonResponse(w, 200, map[string]string{"mount": p})
				return
			}
		}
	case "/api/backup":
		if q.Method == "GET" {
			s, err := backup.Public()
			if os.IsNotExist(err) {
				jsonResponse(w, 200, map[string]bool{"configured": false})
				return
			}
			if err != nil {
				e = err
				break
			}
			jsonResponse(w, 200, s)
			return
		}
		if q.Method != "POST" {
			w.WriteHeader(405)
			return
		}
		var s backup.Settings
		if e = json.NewDecoder(q.Body).Decode(&s); e == nil {
			e = backup.Configure(s)
			if e == nil {
				for kind, seconds := range map[string]int64{"backup": 86400, "backup-check": 30 * 86400} {
					if err := r.jobs.SetSchedule(tasks.Schedule{ID: kind, Kind: kind, Resource: "backup", EverySeconds: seconds, Enabled: true, LastRun: time.Now()}); err != nil {
						e = err
						break
					}
				}
			}
		}
	case "/api/backup/snapshots":
		if q.Method != "GET" {
			w.WriteHeader(405)
			return
		}
		var s json.RawMessage
		s, e = backup.Snapshots(q.Context())
		if e == nil {
			jsonResponse(w, 200, s)
			return
		}
	case "/api/settings/export":
		if q.Method != "POST" {
			w.WriteHeader(405)
			return
		}
		var b struct{ Password string }
		if e = json.NewDecoder(q.Body).Decode(&b); e == nil {
			var data []byte
			data, e = config.EncryptExport(b.Password)
			if e == nil {
				w.Header().Set("Content-Type", "application/octet-stream")
				w.Header().Set("Content-Disposition", "attachment; filename=just-nas-settings.enc")
				w.Write(data)
				return
			}
		}
	case "/api/diagnostics/download":
		if q.Method != "GET" {
			w.WriteHeader(405)
			return
		}
		pools, err := r.storageMgr.GetPools()
		if err != nil {
			e = err
			break
		}
		status, err := command.Run("", "uname", "-a")
		if err != nil {
			e = err
			break
		}
		w.Header().Set("Content-Disposition", "attachment; filename=just-nas-diagnostics.json")
		jsonResponse(w, 200, map[string]any{"system": string(status), "pools": pools, "jobs": r.jobs.Jobs()})
		return
	case "/api/services":
		if q.Method != "POST" {
			w.WriteHeader(405)
			return
		}
		var b struct{ Service, Action string }
		if e = json.NewDecoder(q.Body).Decode(&b); e == nil {
			if (b.Service != "samba" && b.Service != "nfs") || (b.Action != "restart" && b.Action != "reload") {
				e = fmt.Errorf("服務操作無效")
			} else {
				_, e = command.Run("", "rc-service", b.Service, b.Action)
			}
		}
	default:
		w.WriteHeader(404)
		return
	}
	if e != nil {
		jsonResponse(w, 400, map[string]string{"error": e.Error()})
		return
	}
	jsonResponse(w, 200, map[string]bool{"ok": true})
}

// Remove only expired recycle entries, never snapshots or live files.
func pruneTrash(root string, now time.Time) error {
	return filepath.WalkDir(root, func(p string, d os.DirEntry, e error) error {
		if os.IsNotExist(e) {
			return nil
		}
		if e != nil {
			return e
		}
		if d.IsDir() || d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		st, e := d.Info()
		if e == nil && now.Sub(files.DeletionTime(st)) >= 30*24*time.Hour {
			return os.Remove(p)
		}
		return e
	})
}
