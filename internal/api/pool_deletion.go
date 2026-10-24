package api

import (
	"context"
	"encoding/json"
	"fmt"
	"just-nas/internal/backup"
	"just-nas/internal/command"
	"just-nas/internal/config"
	"just-nas/internal/extensions"
	"just-nas/internal/shares"
	"just-nas/internal/tasks"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type poolCleanupSummary struct {
	Extensions      []string `json:"extensions"`
	Apps            []string `json:"apps"`
	RetainedAppData []string `json:"retained_app_data"`
	Pool            string   `json:"pool"`
	Shares          []string `json:"shares"`
	Jobs            int      `json:"jobs"`
	Schedules       int      `json:"schedules"`
	BackupSources   []string `json:"backup_sources"`
	BackupRemoved   bool     `json:"backup_removed"`
	Alerts          int      `json:"alerts"`
	HomeLinks       []string `json:"home_links"`
}

func poolRef(pool, value string) bool {
	return value == pool || strings.HasPrefix(value, pool+"/") || strings.HasPrefix(value, pool+"@")
}
func poolPath(pool, value string) bool {
	return poolRef(pool, strings.TrimPrefix(value, "/mnt/")) && strings.HasPrefix(value, "/mnt/")
}
func canonicalDevice(value string) string {
	if p, e := filepath.EvalSymlinks(value); e == nil {
		return p
	}
	return value
}
func poolDeviceReferences(pool string) (map[string]bool, error) {
	b, e := command.Run("", "zpool", "status", "-j", "-P")
	if e != nil {
		return nil, e
	}
	members, e := uiPoolMembers(b)
	if e != nil {
		return nil, e
	}
	owners := map[string]map[string]bool{}
	for _, m := range members {
		paths := []string{m.Path}
		// Include the whole physical device used by per-disk SMART schedules.
		if strings.HasPrefix(m.Path, "/dev/") {
			b, e = command.Run("", "lsblk", "-s", "-n", "-r", "-p", "-o", "PATH", m.Path)
			if e != nil {
				return nil, e
			}
			paths = append(paths, strings.Fields(string(b))...)
		}
		for _, p := range paths {
			p = canonicalDevice(p)
			if owners[p] == nil {
				owners[p] = map[string]bool{}
			}
			owners[p][m.Pool] = true
		}
	}
	refs := map[string]bool{}
	for device, ps := range owners {
		if ps[pool] && len(ps) == 1 {
			refs[device] = true
		}
	}
	return refs, nil
}
func cleanupTaskMatch(pool, kind, resource string, shareIDs, devices map[string]bool, backupChanged bool) bool {
	switch kind {
	case "store-install", "store-action", "store-download":
		return poolRef(pool, strings.SplitN(resource, "|", 2)[0])
	case "permissions":
		return shareIDs[strings.SplitN(resource, "|", 2)[0]]
	case "replace":
		return strings.SplitN(resource, "|", 2)[0] == pool
	case "backup-restore":
		v := strings.SplitN(resource, ":", 2)
		return len(v) == 2 && poolRef(pool, v[1])
	case "smart-short":
		return devices[canonicalDevice(resource)]
	case "backup", "backup-check":
		return backupChanged
	default:
		return poolRef(pool, resource)
	}
}
func (r *Router) poolCleanupPlan(pool, guid string) (config.PoolDeletion, poolCleanupSummary, []shares.ShareConfig, error) {
	record := config.PoolDeletion{Pool: pool, GUID: guid, Before: map[string][]byte{}, After: map[string][]byte{}}
	summary := poolCleanupSummary{Pool: pool, Shares: []string{}, BackupSources: []string{}, HomeLinks: []string{}}
	for _, name := range []string{"shares.json", "users.json", "jobs.json", "schedules.json", "backup.json", "alerts.json", "extensions.json", "apps.json"} {
		b, e := os.ReadFile(filepath.Join(config.Dir, name))
		if e != nil && !os.IsNotExist(e) {
			return record, summary, nil, e
		}
		if len(b) > 0 && !json.Valid(b) {
			return record, summary, nil, fmt.Errorf("設定檔 %s 損壞，停止刪池", name)
		}
		record.Before[name] = b
		record.After[name] = b
	}
	extIDs, appIDs, refErr := extensions.References(pool)
	summary.Extensions, summary.Apps = extIDs, appIDs
	if refErr != nil {
		return record, summary, nil, refErr
	}
	next, retained, err := extensions.MakePoolState(pool)
	if err != nil {
		return record, summary, nil, err
	}
	summary.RetainedAppData = retained
	for name, b := range next {
		if record.Before[name] != nil {
			record.After[name] = b
		}
	}
	marshal := func(v any) []byte { b, _ := json.MarshalIndent(v, "", "  "); return b }
	ss, e := r.shareMgr.GetShares()
	if e != nil {
		return record, summary, nil, e
	}
	keep := []shares.ShareConfig{}
	removed := []shares.ShareConfig{}
	ids := map[string]bool{}
	for _, s := range ss {
		if poolPath(pool, s.Path) {
			removed = append(removed, s)
			ids[s.ID] = true
			summary.Shares = append(summary.Shares, s.Name+" ("+s.Protocol+")")
		} else {
			keep = append(keep, s)
		}
	}
	record.After["shares.json"] = marshal(keep)
	users := []shares.User{}
	if b := record.Before["users.json"]; len(b) > 0 {
		if e = json.Unmarshal(b, &users); e != nil {
			return record, summary, nil, e
		}
		for i := range users {
			if poolPath(pool, users[i].Home) || poolRef(pool, users[i].Home) {
				summary.HomeLinks = append(summary.HomeLinks, users[i].Name)
				users[i].Home = ""
			}
		}
		record.After["users.json"] = marshal(users)
	}
	backupChanged := false
	if b := record.Before["backup.json"]; len(b) > 0 {
		var settings backup.Settings
		if e = json.Unmarshal(b, &settings); e != nil {
			return record, summary, nil, e
		}
		next := []string{}
		for _, ds := range settings.Datasets {
			if poolRef(pool, ds) {
				backupChanged = true
				summary.BackupSources = append(summary.BackupSources, ds)
			} else {
				next = append(next, ds)
			}
		}
		if backupChanged {
			settings.Datasets = next
			if len(next) == 0 {
				summary.BackupRemoved = true
				record.After["backup.json"] = nil
			} else {
				record.After["backup.json"] = marshal(settings)
			}
		}
	}
	devices, e := poolDeviceReferences(pool)
	if e != nil {
		return record, summary, nil, e
	}
	jobs := r.jobs.Jobs()
	record.Before["jobs.json"] = marshal(jobs)
	keptJobs := []tasks.Job{}
	for _, j := range jobs {
		if cleanupTaskMatch(pool, j.Kind, j.Resource, ids, devices, backupChanged) {
			summary.Jobs++
		} else {
			keptJobs = append(keptJobs, j)
		}
	}
	record.After["jobs.json"] = marshal(keptJobs)
	schedules := r.jobs.Schedules()
	record.Before["schedules.json"] = marshal(schedules)
	keptSchedules := []tasks.Schedule{}
	for _, s := range schedules {
		if cleanupTaskMatch(pool, s.Kind, s.Resource, ids, devices, summary.BackupRemoved) {
			summary.Schedules++
		} else {
			keptSchedules = append(keptSchedules, s)
		}
	}
	record.After["schedules.json"] = marshal(keptSchedules)
	alerts := []Alert{}
	if b := record.Before["alerts.json"]; len(b) > 0 {
		if e = json.Unmarshal(b, &alerts); e != nil {
			return record, summary, nil, e
		}
		keepAlerts := []Alert{}
		for _, a := range alerts {
			related := a.ID == "pool-health-"+pool || a.ID == "pool-capacity-"+pool || poolRef(pool, strings.TrimPrefix(a.ID, "quota-")) && strings.HasPrefix(a.ID, "quota-")
			parts := strings.SplitN(a.ID, ":", 2)
			if len(parts) == 2 && cleanupTaskMatch(pool, parts[0], parts[1], ids, devices, backupChanged) {
				related = true
			}
			for _, j := range jobs {
				if a.ID == j.Kind+":"+j.Resource && cleanupTaskMatch(pool, j.Kind, j.Resource, ids, devices, backupChanged) {
					related = true
				}
			}
			if strings.HasPrefix(a.ID, "smart-") && devices[canonicalDevice(strings.TrimPrefix(a.ID, "smart-"))] {
				related = true
			}
			if related {
				summary.Alerts++
			} else {
				keepAlerts = append(keepAlerts, a)
			}
		}
		record.After["alerts.json"] = marshal(keepAlerts)
	}
	return record, summary, removed, nil
}
func (r *Router) handlePoolDeletionPreview(w http.ResponseWriter, q *http.Request) {
	if q.Method != "GET" {
		w.WriteHeader(405)
		return
	}
	pool := q.URL.Query().Get("name")
	guid, e := r.storageMgr.CheckDestroyPool(pool)
	if e != nil {
		jsonResponse(w, 409, map[string]string{"error": e.Error()})
		return
	}
	_, summary, _, e := r.poolCleanupPlan(pool, guid)
	if e != nil {
		jsonResponse(w, 500, map[string]string{"error": e.Error()})
		return
	}
	jsonResponse(w, 200, summary)
}
func (r *Router) deletePool(ctx context.Context, pool string) (poolCleanupSummary, error) {
	guid, e := r.storageMgr.CheckDestroyPool(pool)
	if e != nil {
		return poolCleanupSummary{}, e
	}
	if e = r.jobs.Pause(true); e != nil {
		return poolCleanupSummary{}, fmt.Errorf("刪池前需先等待或中止進行中的任務: %w", e)
	}
	resume := true
	defer func() {
		if resume {
			_ = r.jobs.Pause(false)
		}
	}()
	record, summary, removed, e := r.poolCleanupPlan(pool, guid)
	if e != nil {
		return summary, e
	}
	ids := []string{}
	for _, s := range removed {
		ids = append(ids, s.ID)
	}
	if r.portal != nil {
		defer func() {
			if resume {
				r.portal.unblockSpaces(ids)
			}
		}()
		wait, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		if e = r.portal.blockSpaces(wait, ids); e != nil {
			return summary, e
		}
	}
	if e = config.BeginPoolDeletion(record); e != nil {
		return summary, e
	}
	rollback := func(cause error) (poolCleanupSummary, error) {
		// Recovery decides undo vs redo using the actual pool GUID, including ambiguous command failures.
		if err := config.RecoverPoolDeletion(); err != nil {
			resume = false
			return summary, fmt.Errorf("%v；設定恢復失敗，已停止排程，請重新啟動以重試恢復: %w", cause, err)
		}
		if err := r.jobs.ReloadPaused(); err != nil {
			resume = false
			return summary, err
		}
		if err := r.shareMgr.ReloadConfiguration(); err != nil {
			return summary, fmt.Errorf("%v；分享重新載入失敗: %w", cause, err)
		}
		return summary, cause
	}
	if e = extensions.StopForPool(ctx, pool); e != nil {
		return rollback(e)
	}
	if e = config.ApplyPoolState(record.After); e != nil {
		return rollback(e)
	}
	if e = r.jobs.ReloadPaused(); e != nil {
		return rollback(e)
	}
	if e = r.shareMgr.ReloadConfiguration(); e != nil {
		return rollback(e)
	}
	for _, s := range removed {
		if s.Protocol == "smb" {
			if _, e = command.Run("", "smbcontrol", "smbd", "close-share", s.Name); e != nil {
				return rollback(e)
			}
		}
	}
	current, e := r.storageMgr.CheckDestroyPool(pool)
	if e != nil {
		return rollback(e)
	}
	if current != guid {
		return rollback(fmt.Errorf("儲存池識別已改變，停止銷毀"))
	}
	if e = r.storageMgr.DestroyPool(pool); e != nil {
		return rollback(e)
	}
	if e = config.FinishPoolDeletion(); e != nil {
		resume = false
		return summary, fmt.Errorf("池已銷毀，清理提交尚未完成；已停止排程，重啟後會自動完成清理: %w", e)
	}
	config.RecordAudit(fmt.Sprintf("刪除儲存池 %s；分享 %d、任務 %d、排程 %d、告警 %d", pool, len(removed), summary.Jobs, summary.Schedules, summary.Alerts))
	return summary, nil
}
