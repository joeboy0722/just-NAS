package storage

import (
	"context"
	"fmt"
	"io/fs"
	"just-nas/internal/command"
	"just-nas/internal/config"
	"just-nas/internal/files"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

func quotaBytes(v string) (uint64, error) {
	if v == "none" {
		return 0, nil
	}
	if !quotaRE.MatchString(v) {
		return 0, fmt.Errorf("配額格式無效")
	}
	unit := uint64(1)
	last := strings.ToUpper(strings.TrimSuffix(strings.TrimSuffix(v, "B"), "i"))
	if len(last) > 0 {
		switch last[len(last)-1] {
		case 'K':
			unit = 1 << 10
		case 'M':
			unit = 1 << 20
		case 'G':
			unit = 1 << 30
		case 'T':
			unit = 1 << 40
		}
		if unit != 1 {
			last = last[:len(last)-1]
		}
	}
	n, e := strconv.ParseUint(last, 10, 64)
	if e != nil || n > ^uint64(0)/unit {
		return 0, fmt.Errorf("配額超出範圍")
	}
	return n * unit, nil
}
func datasetValues(ds string) (map[string]string, error) {
	b, e := command.Run("", "zfs", "get", "-Hp", "-o", "property,value", "referenced,refquota,projectused@1,projectquota@1,org.just-nas:quota-project,org.just-nas:quota,org.just-nas:over-since,org.just-nas:quota-locked", ds)
	if e != nil {
		return nil, e
	}
	r := map[string]string{}
	for _, f := range lines(b) {
		if len(f) == 2 {
			r[f[0]] = f[1]
		}
	}
	return r, nil
}

var quotaMu sync.Mutex
var quotaRoot = func(ds string) string { return "/mnt/" + ds }

func SetQuota(ds, v string) error {
	quotaMu.Lock()
	defer quotaMu.Unlock()
	target, e := quotaBytes(v)
	if e != nil {
		return e
	}
	if _, e = command.Run("", "zpool", "sync", strings.Split(ds, "/")[0]); e != nil {
		return e
	}
	props, e := datasetValues(ds)
	if e != nil {
		return e
	}
	if props["org.just-nas:quota-project"] != "1" {
		return fmt.Errorf("此舊資料集尚未啟用 project quota；請使用新受管理空間，系統不會自動改動既有資料")
	}
	limit := "none"
	if target > 0 {
		limit = strconv.FormatUint(target, 10)
	}
	// Project quotas account live objects independently of dataset metadata headroom.
	if _, e = command.Run("", "zfs", "set", "projectquota@1="+limit, "refquota=none", "org.just-nas:quota="+strconv.FormatUint(target, 10), ds); e != nil {
		return e
	}

	return enforceQuota(context.Background(), ds, false, time.Now())
}

// EnforceQuota never deletes during a settings request. Expiry deletion is enabled only by the scheduled worker.
func EnforceQuota(ds string, expire bool, now time.Time) error {
	return EnforceQuotaContext(context.Background(), ds, expire, now)
}
func EnforceQuotaContext(ctx context.Context, ds string, expire bool, now time.Time) error {
	quotaMu.Lock()
	defer quotaMu.Unlock()
	return enforceQuota(ctx, ds, expire, now)
}
func enforceQuota(ctx context.Context, ds string, expire bool, now time.Time) error {
	if !ValidDataset(ds) {
		return fmt.Errorf("invalid quota dataset")
	}
	v, e := datasetValues(ds)
	if e != nil {
		return e
	}
	target := num(v["org.just-nas:quota"])
	used := num(v["projectused@1"])
	root := quotaRoot(ds)
	if e = MountedDataset(root); e != nil {
		return e
	}
	if v["org.just-nas:quota-project"] != "1" {
		return fmt.Errorf("project quota accounting is not initialized")
	}
	if target == 0 || used <= target {
		_, e = command.Run("", "zfs", "set", "org.just-nas:over-since=0", "org.just-nas:quota-locked=off", ds)
		return e
	}

	since := int64(num(v["org.just-nas:over-since"]))
	if since == 0 {
		since = now.Unix()
		if _, e = command.Run("", "zfs", "set", "org.just-nas:over-since="+strconv.FormatInt(since, 10), ds); e != nil {
			return e
		}
	}

	if expire && now.Unix()-since >= int64(30*24*time.Hour/time.Second) {
		type candidate struct {
			path     string
			modified time.Time
			trash    bool
		}
		candidates := []candidate{}
		children, err := ChildMounts(root)
		if err != nil {
			return err
		}
		e = filepath.WalkDir(root, func(p string, d fs.DirEntry, e error) error {
			if e != nil {
				return e
			}
			if children[p] && d.IsDir() {
				return filepath.SkipDir
			}
			if (d.Name() == ".zfs" || (d.Name() == ".nas-metadata" && strings.Contains(p, string(filepath.Separator)+".nas-trash"+string(filepath.Separator)))) && d.IsDir() {
				return filepath.SkipDir
			}
			if d.Type()&os.ModeSymlink != 0 || d.IsDir() {
				return nil
			}
			st, e := d.Info()
			if e != nil {
				return e
			}
			if !st.Mode().IsRegular() {
				return nil
			}
			trash := strings.Contains(p, string(filepath.Separator)+".nas-trash"+string(filepath.Separator))
			if trash {
				rel := strings.SplitN(p, string(filepath.Separator)+".nas-trash"+string(filepath.Separator), 2)[1]
				parts := strings.Split(filepath.ToSlash(rel), "/")
				if len(parts) < 2 {
					return nil
				}
				tr, err := os.OpenRoot(filepath.Join(root, ".nas-trash", parts[0]))
				if err != nil {
					return err
				}
				deleted := files.RecycleTime(tr, strings.Join(parts[1:], "/"), st)
				tr.Close()
				if now.Sub(deleted) < 30*24*time.Hour {
					return nil
				}
			}
			candidates = append(candidates, candidate{p, st.ModTime(), trash})
			return nil
		})
		if e != nil {
			return e
		}
		sort.Slice(candidates, func(i, j int) bool {
			if candidates[i].trash != candidates[j].trash {
				return candidates[i].trash
			}
			return candidates[i].modified.Before(candidates[j].modified)
		})
		for _, c := range candidates {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
			if used <= target {
				break
			}
			config.RecordAudit("Expired quota cleanup: " + c.path)
			if e = os.Remove(c.path); e != nil {
				return e
			}
			if _, e = command.Run("", "zpool", "sync", strings.Split(ds, "/")[0]); e != nil {
				return e
			}
			v, e = datasetValues(ds)
			if e != nil {
				return e
			}
			used = num(v["projectused@1"])
		}
	}
	if used <= target {
		return enforceQuota(ctx, ds, false, now)
	}
	if _, e = command.Run("", "zpool", "sync", strings.Split(ds, "/")[0]); e != nil {
		return e
	}
	v, e = datasetValues(ds)
	if e != nil {
		return e
	}
	used = num(v["projectused@1"])
	if used <= target {
		return enforceQuota(ctx, ds, false, now)
	}
	_, e = command.Run("", "zfs", "set", "org.just-nas:quota-locked=on", ds)
	return e
}
