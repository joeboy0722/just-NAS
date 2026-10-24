package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"just-nas/internal/files"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTrashPruneUsesDirectoryDeletionRecord(t *testing.T) {
	root := t.TempDir()
	userRoot := filepath.Join(root, "alice")
	os.MkdirAll(filepath.Join(userRoot, "folder"), 0700)
	os.Mkdir(filepath.Join(userRoot, ".nas-metadata"), 0700)
	child := filepath.Join(userRoot, "folder", "old.txt")
	expired := filepath.Join(userRoot, "expired.txt")
	os.WriteFile(child, []byte("preserve"), 0600)
	os.WriteFile(expired, []byte("expired"), 0600)
	now := time.Now().Add(60 * 24 * time.Hour)
	h := sha256.Sum256([]byte("folder"))
	record, _ := json.Marshal(files.RecycleRecord{Path: "folder", Original: "folder", User: "alice", Deleted: now.Add(-15 * 24 * time.Hour)})
	os.WriteFile(filepath.Join(userRoot, ".nas-metadata", hex.EncodeToString(h[:])+".json"), record, 0600)
	expiredFolder := filepath.Join(userRoot, "expired-folder")
	os.Mkdir(expiredFolder, 0700)
	os.WriteFile(filepath.Join(expiredFolder, "old"), []byte("expired"), 0600)
	h2 := sha256.Sum256([]byte("expired-folder"))
	name2 := filepath.Join(userRoot, ".nas-metadata", hex.EncodeToString(h2[:])+".json")
	record2, _ := json.Marshal(files.RecycleRecord{Path: "expired-folder", Deleted: now.Add(-45 * 24 * time.Hour)})
	os.WriteFile(name2, record2, 0600)
	if e := pruneTrash(root, now); e != nil {
		t.Fatal(e)
	}
	if _, e := os.Stat(child); e != nil {
		t.Fatal("recently recycled directory lost old child", e)
	}
	if _, e := os.Stat(expired); !os.IsNotExist(e) {
		t.Fatal("expired direct entry not removed")
	}
	if _, e := os.Stat(expiredFolder); !os.IsNotExist(e) {
		t.Fatal("empty expired directory survived")
	}
	if _, e := os.Stat(name2); !os.IsNotExist(e) {
		t.Fatal("orphan metadata survived")
	}
}
