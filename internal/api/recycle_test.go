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
	if e := pruneTrash(root, now); e != nil {
		t.Fatal(e)
	}
	if _, e := os.Stat(child); e != nil {
		t.Fatal("recently recycled directory lost old child", e)
	}
	if _, e := os.Stat(expired); !os.IsNotExist(e) {
		t.Fatal("expired direct entry not removed")
	}
}
