package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCorruptStateDoesNotReplaceAdmin(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	os.WriteFile(filepath.Join(src, "admin.json"), []byte("broken"), 0600)
	os.WriteFile(filepath.Join(dst, "admin.json"), []byte(`{"hash":"keep"}`), 0600)
	if copyState(src, dst) == nil {
		t.Fatal("corrupt JSON accepted")
	}
	b, _ := os.ReadFile(filepath.Join(dst, "admin.json"))
	if string(b) != `{"hash":"keep"}` {
		t.Fatal("overwrote valid state")
	}
}
func TestStateCopyPreservesSecrets(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	os.WriteFile(filepath.Join(src, "smbpasswd"), []byte("hashes"), 0600)
	if e := copyState(src, dst); e != nil {
		t.Fatal(e)
	}
	id, e := os.ReadFile(filepath.Join(dst, "CURRENT"))
	if e != nil {
		t.Fatal(e)
	}
	b, e := os.ReadFile(filepath.Join(dst, "generations", string(id), "smbpasswd"))
	if e != nil || string(b) != "hashes" {
		t.Fatal("lost password db")
	}
}
