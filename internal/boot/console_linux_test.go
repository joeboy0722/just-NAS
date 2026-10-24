package boot

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConsoleReopensClosedDescriptorAndRecoversAfterMissingDevice(t *testing.T) {
	path := filepath.Join(t.TempDir(), "terminal")
	if e := os.WriteFile(path, nil, 0600); e != nil {
		t.Fatal(e)
	}
	c := Console(path)
	if c == nil {
		t.Fatal("console open")
	}
	defer func() {
		if c.file != nil {
			c.file.Close()
		}
	}()
	c.file.Close()
	if n, e := c.Write([]byte("recovered")); e != nil || n != 9 {
		t.Fatal("closed descriptor did not reopen", n, e)
	}
	c.file.Close()
	if e := os.Remove(path); e != nil {
		t.Fatal(e)
	}
	if _, e := c.Write([]byte("missing")); e == nil {
		t.Fatal("missing device must not report success")
	}
	if e := os.WriteFile(path, nil, 0600); e != nil {
		t.Fatal(e)
	}
	if n, e := c.Write([]byte("again")); e != nil || n != 5 {
		t.Fatal("missing console did not recover", n, e)
	}
}
