//go:build linux

package vmmanager

import (
	"os"
	"syscall"
	"testing"
)

func TestGuestArtifactsCannotRedirectOrBlockPrivilegedFileReads(t *testing.T) {
	dir := t.TempDir()
	root, e := os.OpenRoot(dir)
	if e != nil {
		t.Fatal(e)
	}
	defer root.Close()
	if e = os.WriteFile(dir+"/regular", []byte("log"), 0600); e != nil {
		t.Fatal(e)
	}
	f, e := openRegular(root, "regular", os.O_RDONLY, 0)
	if e != nil {
		t.Fatal(e)
	}
	f.Close()
	if e = syscall.Mkfifo(dir+"/fifo", 0600); e != nil {
		t.Fatal(e)
	}
	if e = os.Symlink("/etc/passwd", dir+"/link"); e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"fifo", "link"} {
		if f, e = openRegular(root, name, os.O_RDONLY, 0); e == nil {
			f.Close()
			t.Fatal("unsafe artifact accepted", name)
		}
	}
	if e = os.Symlink("/etc/passwd", dir+"/qemu.log"); e != nil {
		t.Fatal(e)
	}
	if _, e = readMachineLog(dir, 4096); e == nil {
		t.Fatal("startup error followed an untrusted log symlink")
	}
	if e = os.Remove(dir + "/qemu.log"); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(dir+"/qemu.log", []byte("prefix:last"), 0600); e != nil {
		t.Fatal(e)
	}
	if raw, e := readMachineLog(dir, 4); e != nil || string(raw) != "last" {
		t.Fatal("log tail", string(raw), e)
	}
}
