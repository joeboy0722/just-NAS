package config

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestEncryptedExportRoundTripAndTamper(t *testing.T) {
	old := backupDir
	backupDir = t.TempDir()
	defer func() { backupDir = old }()
	os.WriteFile(filepath.Join(backupDir, "admin.json"), []byte(`{"hash":"private"}`), 0600)
	b, e := EncryptExport("a sufficiently long password")
	if e != nil {
		t.Fatal(e)
	}
	if bytes.Contains(b, []byte("private")) {
		t.Fatal("plaintext leaked")
	}
	s, e := DecryptExport(b, "a sufficiently long password")
	if e != nil || !bytes.Contains(s["admin.json"], []byte("private")) {
		t.Fatal(e)
	}
	if _, e = DecryptExport(b, "wrong but long password"); e == nil {
		t.Fatal("accepted wrong key")
	}
	b[len(b)/2] ^= 1
	if _, e = DecryptExport(b, "a sufficiently long password"); e == nil {
		t.Fatal("accepted tampered archive")
	}
}
func TestCommittedGenerationIgnoresIncomplete(t *testing.T) {
	src, dst, runtime := t.TempDir(), t.TempDir(), t.TempDir()
	oldDir := Dir
	defer func() { Dir = oldDir }()
	Dir = runtime
	os.WriteFile(filepath.Join(src, "admin.json"), []byte(`{"hash":"keep"}`), 0600)
	if e := copyState(src, dst); e != nil {
		t.Fatal(e)
	}
	os.MkdirAll(filepath.Join(dst, "generations", "9999999999999999999"), 0700)
	if e := copyState(dst, runtime); e != nil {
		t.Fatal(e)
	}
	b, _ := os.ReadFile(filepath.Join(runtime, "admin.json"))
	if string(b) != `{"hash":"keep"}` {
		t.Fatal("restored uncommitted state")
	}
}
