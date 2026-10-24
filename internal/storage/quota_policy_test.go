package storage

import (
	"context"
	"fmt"
	"io/fs"
	"just-nas/internal/command"
	"just-nas/internal/config"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestQuotaGraceAndExpiredVictimOrder(t *testing.T) {
	oldConfigDir := config.Dir
	config.Dir = t.TempDir()
	defer func() { config.Dir = oldConfigDir }()
	root := t.TempDir()
	oldRoot, oldRun := quotaRoot, command.Run
	defer func() { quotaRoot = oldRoot; command.Run = oldRun }()
	quotaRoot = func(string) string { return root }
	now := time.Now().Add(60 * 24 * time.Hour)
	put := func(name, content string, mtime time.Time) {
		p := filepath.Join(root, filepath.FromSlash(name))
		os.MkdirAll(filepath.Dir(p), 0700)
		if e := os.WriteFile(p, []byte(content), 0600); e != nil {
			t.Fatal(e)
		}
		if e := os.Chtimes(p, mtime, mtime); e != nil {
			t.Fatal(e)
		}
	}
	put("old.bin", "123456", now.Add(-120*24*time.Hour))
	put("new.bin", "123456", now.Add(-10*24*time.Hour))
	put(".nas-trash/alice/expired.bin", "12", now.Add(-90*24*time.Hour))
	put(".nas-trash/alice/recent.bin", "12", now.Add(29*24*time.Hour))
	put(".zfs/snapshot/manual-keep/proof", "manual snapshot", now.Add(-120*24*time.Hour))
	props := map[string]string{"org.just-nas:quota-project": "1", "org.just-nas:quota": "8", "org.just-nas:over-since": fmt.Sprint(now.Unix()), "org.just-nas:quota-locked": "off", "refquota": "0"}
	used := func() int64 {
		var n int64
		filepath.WalkDir(root, func(p string, d fs.DirEntry, e error) error {
			if e != nil {
				return e
			}
			if d.IsDir() && d.Name() == ".zfs" {
				return filepath.SkipDir
			}
			if !d.IsDir() {
				st, _ := d.Info()
				n += st.Size()
			}
			return nil
		})
		return n
	}
	command.Run = func(_ string, name string, args ...string) ([]byte, error) {
		if name == "zfs" && args[0] == "list" {
			return []byte("testpool/QuotaTest\tyes\t" + root + "\n"), nil
		}
		if name == "zfs" && args[0] == "get" {
			props["referenced"] = fmt.Sprint(used())
			props["projectused@1"] = fmt.Sprint(used())
			var b strings.Builder
			for k, v := range props {
				fmt.Fprintf(&b, "%s\t%s\n", k, v)
			}
			return []byte(b.String()), nil
		}
		if name == "zfs" && args[0] == "set" {
			for _, a := range args[1 : len(args)-1] {
				p := strings.SplitN(a, "=", 2)
				if len(p) == 2 {
					props[p[0]] = p[1]
				}
			}
		}
		return nil, nil
	}
	if e := EnforceQuotaContext(context.Background(), "testpool/QuotaTest", false, now); e != nil {
		t.Fatal(e)
	}
	if used() != 16 {
		t.Fatal("quota change deleted data")
	}
	deadline := now.Add(31 * 24 * time.Hour)
	if e := EnforceQuotaContext(context.Background(), "testpool/QuotaTest", true, deadline); e != nil {
		t.Fatal(e)
	}
	for _, p := range []string{"old.bin", ".nas-trash/alice/expired.bin"} {
		if _, e := os.Stat(filepath.Join(root, filepath.FromSlash(p))); !os.IsNotExist(e) {
			t.Fatal("expected cleanup", p)
		}
	}
	for _, p := range []string{"new.bin", ".nas-trash/alice/recent.bin", ".zfs/snapshot/manual-keep/proof"} {
		if _, e := os.Stat(filepath.Join(root, filepath.FromSlash(p))); e != nil {
			t.Fatal("deleted protected/recent data", p, e)
		}
	}
	n, _ := strconv.ParseUint(props["org.just-nas:quota"], 10, 64)
	if used() != int64(n) {
		t.Fatal("did not stop at quota")
	}
}
