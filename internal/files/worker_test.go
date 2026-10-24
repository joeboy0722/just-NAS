package files

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func runTest(t *testing.T, q Request, data string) (string, error) {
	t.Helper()
	b, _ := json.Marshal(q)
	in := strings.NewReader(string(b) + "\n" + data)
	var out bytes.Buffer
	e := Worker(in, &out)
	return out.String(), e
}
func TestUploadDoesNotOverwriteAndTrashRestores(t *testing.T) {
	root := t.TempDir()
	if e := os.MkdirAll(filepath.Join(root, ".nas-trash", "alice"), 0700); e != nil {
		t.Fatal(e)
	}
	q := Request{Root: root, User: "alice", Path: "hello.txt", Operation: "upload"}
	if _, e := runTest(t, q, "original"); e != nil {
		t.Fatal(e)
	}
	if _, e := runTest(t, q, "overwrite"); e == nil {
		t.Fatal("overwrote without consent")
	}
	b, _ := os.ReadFile(filepath.Join(root, "hello.txt"))
	if string(b) != "original" {
		t.Fatal("original damaged")
	}
	q.Operation = "trash"
	if _, e := runTest(t, q, ""); e != nil {
		t.Fatal(e)
	}
	if _, e := os.Stat(filepath.Join(root, "hello.txt")); !os.IsNotExist(e) {
		t.Fatal("not moved to trash")
	}
	q.Operation = "restore"
	q.Target = "restored.txt"
	if _, e := runTest(t, q, ""); e != nil {
		t.Fatal(e)
	}
	b, _ = os.ReadFile(filepath.Join(root, "restored.txt"))
	if string(b) != "original" {
		t.Fatal("restore mismatch")
	}
}
func TestWorkerRejectsEscapesAndMetadata(t *testing.T) {
	root := t.TempDir()
	for _, p := range []string{"../secret", "/etc/passwd", "a/../../secret", ".nas-trash/bob/secret", ".zfs/snapshot/x", "a\\..\\secret"} {
		if _, e := runTest(t, Request{Root: root, Operation: "download", Path: p}, ""); e == nil {
			t.Fatalf("accepted %q", p)
		}
	}
}
func TestWorkerRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	os.WriteFile(filepath.Join(outside, "secret"), []byte("secret"), 0600)
	if e := os.Symlink(outside, filepath.Join(root, "link")); e != nil {
		t.Skip(e)
	}
	if _, e := runTest(t, Request{Root: root, Operation: "download", Path: "link/secret"}, ""); e == nil {
		t.Fatal("followed symlink")
	}
}
func TestPaginationNoDuplicates(t *testing.T) {
	root := t.TempDir()
	for _, n := range []string{"a", "b", "c"} {
		os.WriteFile(filepath.Join(root, n), nil, 0600)
	}
	seen := map[string]bool{}
	for offset := 0; offset < 3; offset++ {
		s, e := runTest(t, Request{Root: root, Operation: "list", Limit: 1, Offset: offset}, "")
		if e != nil {
			t.Fatal(e)
		}
		var b struct{ Entries []Entry }
		if json.Unmarshal([]byte(s), &b) != nil || len(b.Entries) != 1 {
			t.Fatal(s)
		}
		if seen[b.Entries[0].Name] {
			t.Fatal("duplicate")
		}
		seen[b.Entries[0].Name] = true
	}
}

func TestUploadBudgetNeverPublishesPartialFile(t *testing.T) {
	root := t.TempDir()
	q := Request{Root: root, Operation: "upload", Path: "too-large", Limited: true, MaxBytes: 3}
	if _, e := runTest(t, q, "1234"); e == nil {
		t.Fatal("accepted over-budget stream")
	}
	entries, _ := os.ReadDir(root)
	if len(entries) != 0 {
		t.Fatal("partial destination or temporary survived", entries)
	}
	if _, e := runTest(t, q, "123"); e != nil {
		t.Fatal(e)
	}
}

func TestRecycledDirectoryRecordsOriginalAndDescendantDeletionTime(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, ".nas-trash", "alice"), 0700)
	os.Mkdir(filepath.Join(root, "folder"), 0700)
	os.WriteFile(filepath.Join(root, "folder", "old.txt"), []byte("old data"), 0600)
	old := time.Now().Add(-90 * 24 * time.Hour)
	os.Chtimes(filepath.Join(root, "folder", "old.txt"), old, old)
	q := Request{Root: root, User: "alice", Operation: "trash", Path: "folder"}
	before := time.Now()
	if _, e := runTest(t, q, ""); e != nil {
		t.Fatal(e)
	}
	tr, e := os.OpenRoot(filepath.Join(root, ".nas-trash", "alice"))
	if e != nil {
		t.Fatal(e)
	}
	defer tr.Close()
	file, e := tr.Open(metadataName("folder"))
	if e != nil {
		t.Fatal(e)
	}
	var record RecycleRecord
	json.NewDecoder(file).Decode(&record)
	file.Close()
	if record.Original != "folder" || record.User != "alice" || record.Deleted.Before(before) {
		t.Fatal(record)
	}
	if got := RecycleOriginal(tr, "folder/old.txt"); got != "folder/old.txt" {
		t.Fatal("incorrect original path", got)
	}
	st, _ := tr.Stat("folder/old.txt")
	if RecycleTime(tr, "folder/old.txt", st).Before(before) {
		t.Fatal("descendant kept old deletion date")
	}
	q.Operation = "trash-list"
	q.Path = "."
	listed, e := runTest(t, q, "")
	if e != nil || strings.Contains(listed, recycleMetadata) {
		t.Fatal("metadata exposed", listed, e)
	}
	q.Operation = "restore"
	q.Path = "folder"
	q.Target = "restored"
	if _, e := runTest(t, q, ""); e != nil {
		t.Fatal(e)
	}
	if _, e := tr.Stat(metadataName("folder")); !os.IsNotExist(e) {
		t.Fatal("stale metadata survived restore")
	}
}
