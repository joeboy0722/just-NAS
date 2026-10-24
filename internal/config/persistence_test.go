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

func TestGenerationRejectsFutureVersionAndTamperedBytes(t *testing.T) {
	source, saved, runtimeDir := t.TempDir(), t.TempDir(), t.TempDir()
	os.WriteFile(filepath.Join(source, "admin.json"), []byte(`{"hash":"original"}`), 0600)
	if e := copyState(source, saved); e != nil {
		t.Fatal(e)
	}
	id, _ := os.ReadFile(filepath.Join(saved, "CURRENT"))
	generation := filepath.Join(saved, "generations", string(id))
	manifest := filepath.Join(generation, "manifest.json")
	original, _ := os.ReadFile(manifest)
	oldDir := Dir
	Dir = runtimeDir
	defer func() { Dir = oldDir }()
	os.WriteFile(filepath.Join(runtimeDir, "admin.json"), []byte(`{"hash":"keep"}`), 0600)
	os.WriteFile(manifest, []byte(`{"version":999}`), 0600)
	if e := copyState(saved, runtimeDir); e == nil {
		t.Fatal("future generation accepted")
	}
	os.WriteFile(manifest, original, 0600)
	os.WriteFile(filepath.Join(generation, "admin.json"), []byte(`{"hash":"tampered"}`), 0600)
	if e := copyState(saved, runtimeDir); e == nil {
		t.Fatal("checksum mismatch accepted")
	}
	b, _ := os.ReadFile(filepath.Join(runtimeDir, "admin.json"))
	if string(b) != `{"hash":"keep"}` {
		t.Fatal("failure replaced active state")
	}
	os.WriteFile(filepath.Join(generation, "admin.json"), []byte(`{"hash":"original"}`), 0600)
	if e := copyState(saved, runtimeDir); e != nil {
		t.Fatal(e)
	}
}
