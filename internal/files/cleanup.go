package files

import (
	"fmt"
	"io/fs"
	"just-nas/internal/command"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// CleanUploads removes only abandoned upload temporaries. Requests expire after 30 minutes.
func CleanUploads(root string, now time.Time) error {
	r, e := os.OpenRoot(root)
	if e != nil {
		return e
	}
	defer r.Close()
	out, e := command.Run("", "zfs", "list", "-H", "-o", "mountpoint")
	if e != nil {
		return e
	}
	mounts := map[string]bool{}
	for _, v := range strings.Fields(string(out)) {
		if v != root && strings.HasPrefix(v, root+"/") {
			rel, e := filepath.Rel(root, v)
			if e == nil {
				mounts[filepath.ToSlash(rel)] = true
			}
		}
	}
	return fs.WalkDir(r.FS(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (mounts[p] || d.Name() == ".zfs" || d.Name() == ".nas-trash") {
			return fs.SkipDir
		}
		if !strings.HasPrefix(d.Name(), ".nas-upload-") || !d.Type().IsRegular() {
			return nil
		}
		info, e := d.Info()
		if e != nil {
			return e
		}
		if now.Sub(info.ModTime()) < 48*time.Hour {
			return nil
		}
		if e = r.Remove(p); e != nil {
			return fmt.Errorf("temporary upload cleanup: %w", e)
		}
		return nil
	})
}
