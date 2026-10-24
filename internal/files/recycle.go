package files

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path"
	"strings"
	"time"
)

const recycleMetadata = ".nas-metadata"

type RecycleRecord struct {
	Path, Original, User string
	Deleted              time.Time
}

func metadataName(p string) string {
	h := sha256.Sum256([]byte(p))
	return path.Join(recycleMetadata, hex.EncodeToString(h[:])+".json")
}
func recordRecycle(r *os.Root, target, original, user string) {
	name := metadataName(target)
	// An over-quota rename must remain possible even if allocating metadata fails.
	// Remove obsolete metadata first; inode/ancestor ctime remains the safe fallback.
	_ = r.Remove(name)
	if r.MkdirAll(recycleMetadata, 0700) != nil {
		return
	}
	f, e := r.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return
	}
	e = json.NewEncoder(f).Encode(RecycleRecord{target, original, user, time.Now()})
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e != nil || ce != nil {
		_ = r.Remove(name)
	}
}

// RecycleTime follows the record of a recycled directory for all its descendants.
// Older SMB entries fall back to the latest inode or containing-directory ctime.
func RecycleTime(r *os.Root, p string, st os.FileInfo) time.Time {
	fallback := DeletionTime(st)
	for current := p; current != "." && current != ""; current = path.Dir(current) {
		f, e := r.Open(metadataName(current))
		if e == nil {
			var v RecycleRecord
			e = json.NewDecoder(f).Decode(&v)
			f.Close()
			if e == nil && v.Path == current && !v.Deleted.IsZero() {
				if info, err := r.Stat(current); err == nil {
					if t := DeletionTime(info); t.After(v.Deleted) {
						return t
					}
				}
				return v.Deleted
			}
			return time.Now()
		}
		if current != p {
			if info, e := r.Stat(current); e == nil {
				if t := DeletionTime(info); t.After(fallback) {
					fallback = t
				}
			}
		}
	}
	return fallback
}

func forgetRecycle(r *os.Root, p string) {
	f, e := r.Open(recycleMetadata)
	if e != nil {
		return
	}
	defer f.Close()
	for {
		batch, e := f.ReadDir(128)
		for _, entry := range batch {
			name := path.Join(recycleMetadata, entry.Name())
			record, err := r.Open(name)
			if err != nil {
				continue
			}
			var v RecycleRecord
			err = json.NewDecoder(record).Decode(&v)
			record.Close()
			if err == nil && (v.Path == p || strings.HasPrefix(v.Path, p+"/")) {
				_ = r.Remove(name)
			}
		}
		if e != nil {
			return
		}
	}
}

// CleanRecycleMetadata removes expired records only after their recycled object is gone.
func CleanRecycleMetadata(r *os.Root, now time.Time) {
	f, e := r.Open(recycleMetadata)
	if e != nil {
		return
	}
	defer f.Close()
	for {
		batch, e := f.ReadDir(128)
		for _, entry := range batch {
			name := path.Join(recycleMetadata, entry.Name())
			record, err := r.Open(name)
			if err != nil {
				continue
			}
			var v RecycleRecord
			err = json.NewDecoder(record).Decode(&v)
			record.Close()
			if err != nil || !safe(v.Path) || now.Sub(v.Deleted) < 30*24*time.Hour {
				continue
			}
			if _, err = r.Lstat(v.Path); os.IsNotExist(err) {
				_ = r.Remove(name)
			}
		}
		if e != nil {
			return
		}
	}
}

// RecycleOriginal exposes metadata already scoped to the current user's trash root.
func RecycleOriginal(r *os.Root, p string) string {
	for current := p; current != "." && current != ""; current = path.Dir(current) {
		f, e := r.Open(metadataName(current))
		if e != nil {
			continue
		}
		var v RecycleRecord
		e = json.NewDecoder(f).Decode(&v)
		f.Close()
		if e == nil && v.Path == current && safe(v.Original) {
			suffix := strings.TrimPrefix(p, current)
			return path.Join(v.Original, strings.TrimPrefix(suffix, "/"))
		}
	}
	return p
}
