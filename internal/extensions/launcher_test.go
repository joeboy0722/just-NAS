package extensions

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBatchAppStateAndSafeLaunchURLs(t *testing.T) {
	old := Run
	defer func() { Run = old }()
	calls := 0
	Run = func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls++
		if name != "docker" || !strings.Contains(strings.Join(args, " "), "ps --all") {
			t.Fatal(name, args)
		}
		return []byte(`{"Names":"just-nas-live","State":"running","Labels":"org.just-nas.app=live"}
{"Names":"just-nas-off","State":"exited","Labels":"org.just-nas.app=off"}
{"Names":"just-nas-crash","State":"exited","Labels":"org.just-nas.app=crash"}
{"Names":"just-nas-impostor","State":"running","Labels":"org.just-nas.app=other"}`), nil
	}
	apps := AppState{Apps: map[string]Deployment{}}
	for _, id := range []string{"live", "off", "crash", "missing", "impostor"} {
		apps.Apps[id] = Deployment{ID: id, Enabled: id != "off", Status: "active", BindIP: "192.168.1.2", Port: 18080, Package: Verified{Manifest: Manifest{Name: id}}}
	}
	view := AppStatusSnapshot(context.Background(), apps, true)
	if calls != 1 {
		t.Fatal(calls)
	}
	for id, want := range map[string]string{"live": "active", "off": "stopped", "crash": "failed", "missing": "failed", "impostor": "failed"} {
		if view.Apps[id].Status != want {
			t.Fatal(id, view.Apps[id])
		}
	}
	if apps.Apps["off"].Status != "active" {
		t.Fatal("mutated persisted state")
	}
	for _, v := range LauncherEntries(view, true) {
		if v.ID == "live" {
			if v.URL != "http://192.168.1.2:18080/" {
				t.Fatal(v)
			}
		} else if v.URL != "" {
			t.Fatal("inactive launch URL", v)
		}
	}
	live := view.Apps["live"]
	live.BindIP = "javascript:alert(1)"
	view.Apps["live"] = live
	for _, v := range LauncherEntries(view, true) {
		if v.URL != "" {
			t.Fatal("unsafe URL", v)
		}
	}
	Run = func(context.Context, string, ...string) ([]byte, error) { return nil, fmt.Errorf("timeout") }
	view = AppStatusSnapshot(context.Background(), apps, true)
	for _, v := range view.Apps {
		if v.Status != "unknown" {
			t.Fatal(v)
		}
	}
	view = AppStatusSnapshot(context.Background(), apps, false)
	for _, v := range view.Apps {
		if v.Status != "unavailable" {
			t.Fatal(v)
		}
	}
}

func iconBundle(t *testing.T, data []byte, mutate func(*Manifest)) string {
	t.Helper()
	files := map[string][]byte{"image.tar": []byte("fixture image"), "assets/icon.png": data}
	m := Manifest{Format: 1, ID: "icon-test", Name: "Icon test", Version: "1", Kind: "app", ABI: NASABI, Arch: "x86_64", License: "MIT", Icon: "assets/icon.png", MinNASVersion: "2.1.0-rc3", Dependencies: map[string]string{"container-engine": "1.0.0"}, Files: map[string]File{}, App: &AppSpec{Image: "sha256:" + strings.Repeat("a", 64), User: "65532:65532", ContainerPort: 8080, ContentPath: "/srv/content", HealthPath: "/", MemoryMB: 64, CPUs: 0.5}}
	for n, b := range files {
		sum := sha256.Sum256(b)
		m.Files[n] = File{SHA256: hex.EncodeToString(sum[:]), Size: int64(len(b)), Mode: 0644}
		m.InstalledBytes += int64(len(b))
	}
	if mutate != nil {
		mutate(&m)
	}
	raw, _ := json.Marshal(m)
	env, _ := json.Marshal(Envelope{Manifest: raw})
	var out bytes.Buffer
	gz := gzip.NewWriter(&out)
	a := tar.NewWriter(gz)
	write := func(n string, b []byte) {
		a.WriteHeader(&tar.Header{Name: n, Mode: 0644, Size: int64(len(b))})
		a.Write(b)
	}
	write("package.json", env)
	for n, b := range files {
		write("payload/"+n, b)
	}
	a.Close()
	gz.Close()
	p := filepath.Join(t.TempDir(), "icon.jnpkg")
	os.WriteFile(p, out.Bytes(), 0600)
	return p
}

func TestPackageIconsValidateBeforeInstall(t *testing.T) {
	webp, _ := base64.StdEncoding.DecodeString("UklGRjwAAABXRUJQVlA4IDAAAADQAQCdASoCAAIAAUAmJaACdLoB+AADsAD+8JtD/2Dv5wH5wH9MH/5+ZfLfvjMAAAA=")
	if mime, e := validateIcon(webp, "assets/icon.webp"); e != nil || mime != "image/webp" {
		t.Fatal(mime, e)
	}
	var b bytes.Buffer
	png.Encode(&b, image.NewNRGBA(image.Rect(0, 0, 64, 64)))
	p := iconBundle(t, b.Bytes(), nil)
	v, e := Verify(p, nil, t.TempDir())
	if e != nil || v.Manifest.Icon == "" {
		t.Fatal(v, e)
	}
	for _, tc := range []struct {
		name   string
		data   []byte
		mutate func(*Manifest)
	}{
		{"corrupt", []byte("<svg onload=evil>"), nil},
		{"no minimum", b.Bytes(), func(m *Manifest) { m.MinNASVersion = "" }},
		{"future minimum", b.Bytes(), func(m *Manifest) { m.MinNASVersion = "99.0.0" }},
		{"remote path", b.Bytes(), func(m *Manifest) { m.Icon = "https://example.com/icon.png" }},
		{"oversize", make([]byte, (256<<10)+1), nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, e := Verify(iconBundle(t, tc.data, tc.mutate), nil, ""); e == nil {
				t.Fatal("unsafe icon accepted")
			}
		})
	}
	var large bytes.Buffer
	png.Encode(&large, image.NewNRGBA(image.Rect(0, 0, 513, 1)))
	if _, e := Verify(iconBundle(t, large.Bytes(), nil), nil, ""); e == nil {
		t.Fatal("oversized dimensions")
	}
	if _, e := validateIcon(b.Bytes(), "assets/icon.webp"); e == nil {
		t.Fatal("extension mismatch")
	}
}
