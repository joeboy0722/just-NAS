package extensions

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"just-nas/internal/config"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T, mutate func(*Manifest), entryName string, link bool, sign bool, corrupt bool) (string, ed25519.PublicKey) {
	t.Helper()
	pub, key, _ := ed25519.GenerateKey(rand.Reader)
	content := []byte("test payload")
	h := sha256.Sum256(content)
	m := Manifest{Format: 1, ID: "gpu-test", Name: "Test", Version: "1.0", Kind: "driver", ABI: NASABI, Arch: "x86_64", Kernel: "6.18.58-0-lts", License: "MIT", Files: map[string]File{"modules/6.18.58-0-lts/extra/test.ko": {SHA256: hex.EncodeToString(h[:]), Size: int64(len(content)), Mode: 0644}}, InstalledBytes: int64(len(content))}
	if mutate != nil {
		mutate(&m)
	}
	raw, _ := json.Marshal(m)
	sig := ""
	if sign {
		sig = base64.StdEncoding.EncodeToString(ed25519.Sign(key, raw))
	}
	if corrupt {
		sig = base64.StdEncoding.EncodeToString(make([]byte, 64))
	}
	env, _ := json.Marshal(Envelope{Manifest: raw, Signature: sig})
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	tw := tar.NewWriter(gz)
	tw.WriteHeader(&tar.Header{Name: "package.json", Mode: 0644, Size: int64(len(env))})
	tw.Write(env)
	if entryName == "" {
		entryName = "payload/modules/6.18.58-0-lts/extra/test.ko"
	}
	typ := byte(tar.TypeReg)
	if link {
		typ = tar.TypeSymlink
	}
	size := int64(len(content))
	if link {
		size = 0
	}
	tw.WriteHeader(&tar.Header{Name: entryName, Mode: 0644, Size: size, Typeflag: typ, Linkname: "/etc/passwd"})
	if !link {
		tw.Write(content)
	}
	tw.Close()
	gz.Close()
	p := filepath.Join(t.TempDir(), "fixture.jnpkg")
	os.WriteFile(p, b.Bytes(), 0600)
	return p, pub
}
func TestVerifyTrustAndExtraction(t *testing.T) {
	p, key := fixture(t, nil, "", false, true, false)
	dst := t.TempDir()
	v, e := Verify(p, []ed25519.PublicKey{key}, dst)
	if e != nil || !v.Official {
		t.Fatal(v, e)
	}
	if _, e = os.Stat(filepath.Join(dst, "modules/6.18.58-0-lts/extra/test.ko")); e != nil {
		t.Fatal(e)
	}
	if _, e = Verify(p, nil, ""); e == nil {
		t.Fatal("unknown signer accepted")
	}
	p, _ = fixture(t, nil, "", false, false, false)
	v, e = Verify(p, nil, "")
	if e != nil || v.Official {
		t.Fatal(v, e)
	}
}
func TestRejectUnsafePackages(t *testing.T) {
	cases := []struct {
		name, entry   string
		link, corrupt bool
		mutate        func(*Manifest)
	}{
		{name: "traversal", entry: "payload/../../etc/passwd"},
		{name: "absolute", entry: "/etc/passwd"},
		{name: "symlink", link: true},
		{name: "signature", corrupt: true},
		{name: "hash", mutate: func(m *Manifest) {
			f := m.Files["modules/6.18.58-0-lts/extra/test.ko"]
			f.SHA256 = strings.Repeat("0", 64)
			m.Files["modules/6.18.58-0-lts/extra/test.ko"] = f
		}},
		{name: "unknownkind", mutate: func(m *Manifest) { m.Kind = "script" }},
		{name: "size", mutate: func(m *Manifest) { m.InstalledBytes++ }},
		{name: "setuid", mutate: func(m *Manifest) {
			f := m.Files["modules/6.18.58-0-lts/extra/test.ko"]
			f.Mode = 04755
			m.Files["modules/6.18.58-0-lts/extra/test.ko"] = f
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, key := fixture(t, c.mutate, c.entry, c.link, true, c.corrupt)
			if _, e := Verify(p, []ed25519.PublicKey{key}, t.TempDir()); e == nil {
				t.Fatal("unsafe package accepted")
			}
		})
	}
}
func TestMountIdentityAndMissingDataset(t *testing.T) {
	old := Run
	defer func() { Run = old }()
	s := State{Dataset: "tank/.just-nas-extensions", GUID: "123"}
	for _, mode := range []string{"correct", "missing", "wrong-guid", "parent-mount"} {
		t.Run(mode, func(t *testing.T) {
			Run = func(_ context.Context, n string, args ...string) ([]byte, error) {
				if mode == "missing" {
					return nil, fmt.Errorf("missing")
				}
				if n == "findmnt" {
					if mode == "parent-mount" {
						return []byte("tank"), nil
					}
					return []byte(s.Dataset), nil
				}
				if mode == "wrong-guid" {
					return []byte("999\nyes\n" + Root(s)), nil
				}
				return []byte("123\nyes\n" + Root(s)), nil
			}
			e := Mounted(context.Background(), s)
			if (mode == "correct") != (e == nil) {
				t.Fatal(mode, e)
			}
		})
	}
}
func TestStateFailureRestoresPrevious(t *testing.T) {
	oldDir, oldSave := config.Dir, Save
	config.Dir = t.TempDir()
	defer func() { config.Dir = oldDir; Save = oldSave }()
	os.WriteFile(filepath.Join(config.Dir, "extensions.json"), []byte(`{"original":true}`), 0600)
	Save = func() error { return fmt.Errorf("disk full") }
	if e := persist(StateFile, State{Version: 1}); e == nil {
		t.Fatal("false success")
	}
	b, _ := os.ReadFile(filepath.Join(config.Dir, StateFile))
	if string(b) != `{"original":true}` {
		t.Fatal(string(b))
	}
}
func TestDriverCompatibilityAndNetworkFirmwarePreservation(t *testing.T) {
	p, _ := fixture(t, nil, "", false, false, false)
	v, e := Verify(p, nil, "")
	if e != nil {
		t.Fatal(e)
	}
	old := Run
	defer func() { Run = old }()
	Run = func(context.Context, string, ...string) ([]byte, error) { return []byte("wrong-kernel"), nil }
	if e = Compatible(context.Background(), v.Manifest); e == nil {
		t.Fatal("wrong kernel accepted")
	}
}
func TestApplicationBoundaries(t *testing.T) {
	for _, ds := range []string{"tank/.just-nas/secret", "tank/../etc", "/etc", "tank/a@x", "tank/a,b"} {
		if validDataDataset(ds) {
			t.Fatal(ds)
		}
	}
	a := Deployment{ID: "example", Dataset: "tank/.just-nas-extensions/apps/example", Port: 18080, BindIP: "192.168.56.101"}
	if e := validateDeployment(a); e != nil {
		t.Fatal(e)
	}
	a.BindIP = "0.0.0.0"
	if e := validateDeployment(a); e == nil {
		t.Fatal("public bind accepted")
	}
}
