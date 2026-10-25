package extensions

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"just-nas/internal/config"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var operationMu sync.Mutex
var booting atomic.Bool

func MarkBootStart()  { booting.Store(true) }
func Restoring() bool { return booting.Load() }

var Run = func(ctx context.Context, name string, args ...string) ([]byte, error) {
	return runCommand(ctx, name, args...)
}
var Save = func() error { return config.NewPersistenceManager().SaveConfigToZfs() }

const StateFile = "extensions.json"

type State struct {
	SSHPublicKey      string                  `json:"ssh_public_key,omitempty"`
	ProtectedPackages map[string]bool         `json:"protected_packages,omitempty"`
	Version           int                     `json:"version"`
	Dataset           string                  `json:"dataset"`
	GUID              string                  `json:"guid"`
	Installed         map[string]Installation `json:"installed"`
}
type Installation struct {
	Package     Verified  `json:"package"`
	Enabled     bool      `json:"enabled"`
	Status      string    `json:"status"`
	Error       string    `json:"error,omitempty"`
	InstalledAt time.Time `json:"installed_at"`
}
type Deployment struct {
	ID           string   `json:"id"`
	Package      Verified `json:"package"`
	Dataset      string   `json:"dataset"`
	Port         int      `json:"port"`
	BindIP       string   `json:"bind_ip"`
	MountDataset string   `json:"mount_dataset,omitempty"`
	ReadOnly     bool     `json:"read_only"`
	Enabled      bool     `json:"enabled"`
	Status       string   `json:"status"`
	Error        string   `json:"error,omitempty"`
}
type AppState struct {
	Version int                   `json:"version"`
	Apps    map[string]Deployment `json:"apps"`
}

func Load() (State, error) {
	s := State{Version: 1, Installed: map[string]Installation{}}
	e := config.ReadJSON(StateFile, &s)
	if os.IsNotExist(e) {
		return s, nil
	}
	if e != nil {
		return s, e
	}
	if s.Version != 1 || s.Installed == nil {
		return s, fmt.Errorf("不支援的擴充設定版本")
	}
	for id, v := range s.Installed {
		if id != v.Package.Manifest.ID || !digestID.MatchString(v.Package.Hash) {
			return s, fmt.Errorf("擴充識別不一致")
		}
		if e = v.Package.Manifest.Validate(); e != nil {
			return s, e
		}
	}
	if s.Dataset != "" && !validStoreDataset(s.Dataset) {
		return s, fmt.Errorf("擴充資料集無效")
	}
	return s, nil
}
func LoadApps() (AppState, error) {
	s := AppState{Version: 1, Apps: map[string]Deployment{}}
	e := config.ReadJSON("apps.json", &s)
	if os.IsNotExist(e) {
		return s, nil
	}
	if e != nil {
		return s, e
	}
	if s.Version != 1 || s.Apps == nil {
		return s, fmt.Errorf("不支援的應用設定版本")
	}
	for id, a := range s.Apps {
		if id != a.ID || a.ID != a.Package.Manifest.ID || !identifier.MatchString(id) || !digestID.MatchString(a.Package.Hash) || a.Package.Manifest.Kind != "app" {
			return s, fmt.Errorf("應用識別無效")
		}
		if e = a.Package.Manifest.Validate(); e != nil {
			return s, e
		}
		if e = validateDeployment(a); e != nil {
			return s, e
		}
	}
	return s, nil
}
func persist(name string, v any) error {
	old, e := os.ReadFile(filepath.Join(config.Dir, name))
	if e != nil && !os.IsNotExist(e) {
		return e
	}
	if e = config.WriteJSON(name, v); e != nil {
		return e
	}
	if e = Save(); e != nil {
		if old == nil {
			_ = os.Remove(filepath.Join(config.Dir, name))
		} else {
			_ = config.AtomicWrite(filepath.Join(config.Dir, name), old, 0600)
		}
		return e
	}
	return nil
}

var poolIdentifier = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.-]{0,62}$`)

func validStoreDataset(ds string) bool {
	v := strings.Split(ds, "/")
	return len(v) == 2 && poolIdentifier.MatchString(v[0]) && v[1] == ".just-nas-extensions"
}
func Root(s State) string {
	return "/mnt/" + strings.Split(s.Dataset, "/")[0] + "/.just-nas-extensions"
}
func Keys() ([]ed25519.PublicKey, error) {
	files, e := filepath.Glob("/usr/share/just-nas/keys/*.pub")
	if e != nil {
		return nil, e
	}
	keys := []ed25519.PublicKey{}
	for _, f := range files {
		b, e := os.ReadFile(f)
		if e != nil {
			return nil, e
		}
		v, e := base64.StdEncoding.DecodeString(strings.TrimSpace(string(b)))
		if e != nil || len(v) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("官方公鑰無效")
		}
		keys = append(keys, ed25519.PublicKey(v))
	}
	return keys, nil
}
func Mounted(ctx context.Context, s State) error {
	if !validStoreDataset(s.Dataset) || s.GUID == "" {
		return fmt.Errorf("請先選擇擴充儲存池")
	}
	b, e := Run(ctx, "zfs", "get", "-H", "-o", "value", "guid,mounted,mountpoint", s.Dataset)
	if e != nil {
		return e
	}
	v := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(v) != 3 || v[0] != s.GUID || v[1] != "yes" || v[2] != Root(s) {
		return fmt.Errorf("擴充資料集識別或掛載不符，拒絕寫入")
	}
	b, e = Run(ctx, "findmnt", "-n", "-o", "SOURCE", "--target", Root(s))
	if e != nil || strings.TrimSpace(string(b)) != s.Dataset {
		return fmt.Errorf("擴充目錄不是指定資料集的掛載點")
	}
	return nil
}
func Configure(ctx context.Context, pool string) error {
	operationMu.Lock()
	defer operationMu.Unlock()
	if !poolIdentifier.MatchString(pool) {
		return fmt.Errorf("儲存池名稱無效")
	}
	if !config.Persistent() {
		return fmt.Errorf("請先建立持久設定池")
	}
	s, e := Load()
	if e != nil {
		return e
	}
	if s.Dataset != "" {
		if s.Dataset == pool+"/.just-nas-extensions" {
			return Mounted(ctx, s)
		}
		return fmt.Errorf("已有擴充儲存位置；第一版不提供搬移")
	}
	pkgs, err := installedPackages()
	if err != nil {
		return err
	}
	s.ProtectedPackages = map[string]bool{}
	for name := range pkgs {
		s.ProtectedPackages[name] = true
	}
	s.Dataset = pool + "/.just-nas-extensions"
	// Never adopt a pre-existing dataset or silently overwrite its properties.
	if _, e = Run(ctx, "zfs", "list", "-H", s.Dataset); e == nil {
		return fmt.Errorf("已有同名資料集，拒絕接管")
	}
	if _, e = Run(ctx, "zpool", "list", "-H", "-o", "name", pool); e != nil {
		return e
	}
	if _, e = Run(ctx, "zfs", "create", "-o", "mountpoint="+Root(s), "-o", "org.just-nas:extensions=1", s.Dataset); e != nil {
		return e
	}
	b, e := Run(ctx, "zfs", "get", "-H", "-o", "value", "guid", s.Dataset)
	if e != nil {
		return e
	}
	s.GUID = strings.TrimSpace(string(b))
	if e = Mounted(ctx, s); e != nil {
		return e
	}
	if e = os.Chmod(Root(s), 0700); e != nil {
		return e
	}
	for _, n := range []string{"packages", "imports", "staging", "journal"} {
		if e = os.MkdirAll(filepath.Join(Root(s), n), 0700); e != nil {
			return e
		}
	}
	return persist(StateFile, s)
}
func Compatible(ctx context.Context, m Manifest) error {
	if e := m.Validate(); e != nil {
		return e
	}
	if m.Kind == "driver" {
		b, e := Run(ctx, "uname", "-r")
		if e != nil {
			return e
		}
		if strings.TrimSpace(string(b)) != m.Kernel {
			return fmt.Errorf("驅動與目前核心版本不符")
		}
	}
	s, e := Load()
	if e != nil {
		return e
	}
	for id, version := range m.Dependencies {
		v, ok := s.Installed[id]
		if !ok || v.Package.Manifest.Version != version || !v.Enabled || v.Status != "active" {
			return fmt.Errorf("需要系統擴充 %s %s", id, version)
		}
	}
	return nil
}
func SetEnabled(ctx context.Context, id string, enabled bool) error {
	operationMu.Lock()
	defer operationMu.Unlock()
	s, e := Load()
	if e != nil {
		return e
	}
	if e = Mounted(ctx, s); e != nil {
		return e
	}
	v, ok := s.Installed[id]
	if !ok {
		return fmt.Errorf("找不到擴充")
	}
	if !enabled {
		if e = dependencyCheck(s, id); e != nil {
			return e
		}
	}
	if v.Package.Manifest.Kind == "driver" {
		if enabled {
			if e = Compatible(ctx, v.Package.Manifest); e != nil {
				return e
			}
		}
		v.Enabled = enabled
		v.Status = "pending-reboot"
		v.Error = ""
		s.Installed[id] = v
		return persist(StateFile, s)
	}
	if enabled {
		if e = Compatible(ctx, v.Package.Manifest); e != nil {
			return e
		}
		if e = activate(ctx, s, v.Package); e != nil {
			return e
		}
	} else {
		if e = deactivate(ctx, s, v.Package); e != nil {
			return e
		}
	}
	old := v
	v.Enabled = enabled
	v.Status = "disabled"
	if enabled {
		v.Status = "active"
	}
	v.Error = ""
	s.Installed[id] = v
	if e = persist(StateFile, s); e != nil {
		if old.Enabled {
			_ = activate(ctx, s, old.Package)
		} else {
			_ = deactivate(ctx, s, old.Package)
		}
		return e
	}
	return nil
}
func dependencyCheck(s State, id string) error {
	for other, v := range s.Installed {
		if _, ok := v.Package.Manifest.Dependencies[id]; ok {
			return fmt.Errorf("%s 仍依賴此擴充", other)
		}
	}
	apps, e := LoadApps()
	if e != nil {
		return e
	}
	for _, a := range apps.Apps {
		if _, ok := a.Package.Manifest.Dependencies[id]; ok {
			return fmt.Errorf("應用 %s 仍依賴此擴充", a.ID)
		}
	}
	return nil
}
func Remove(ctx context.Context, id string) error {
	operationMu.Lock()
	defer operationMu.Unlock()
	s, e := Load()
	if e != nil {
		return e
	}
	if e = Mounted(ctx, s); e != nil {
		return e
	}
	v, ok := s.Installed[id]
	if !ok {
		return fmt.Errorf("找不到擴充")
	}
	if e = dependencyCheck(s, id); e != nil {
		return e
	}
	if v.Package.Manifest.Kind == "driver" {
		if v.Enabled || v.Status != "disabled" {
			return fmt.Errorf("請先停用驅動並重新開機，再移除")
		}
	} else if v.Enabled {
		if e = deactivate(ctx, s, v.Package); e != nil {
			return e
		}
	}
	delete(s.Installed, id)
	if e = persist(StateFile, s); e != nil {
		if v.Enabled {
			_ = activate(ctx, s, v.Package)
		}
		return e
	}
	if v.Package.Manifest.Kind == "runtime" || v.Package.Manifest.Kind == "ssh" {
		if _, exists := Run(ctx, "apk", "info", "-e", ".just-nas-"+id); exists != nil {
			return nil
		}
		if _, err := Run(ctx, "apk", "del", "--no-network", ".just-nas-"+id); err != nil {
			s.Installed[id] = v
			_ = persist(StateFile, s)
			if v.Enabled {
				_ = activate(ctx, s, v.Package)
			}
			return err
		}
	}
	// Keep immutable package cache for offline reinstall and rollback; explicit cache cleanup is separate.
	return nil
}
func References(pool string) ([]string, []string, error) {
	s, e := Load()
	if e != nil {
		return nil, nil, e
	}
	ext := []string{}
	apps := []string{}
	if strings.HasPrefix(s.Dataset, pool+"/") {
		for id := range s.Installed {
			ext = append(ext, id)
		}
	}
	a, e := LoadApps()
	if e != nil {
		return nil, nil, e
	}
	for id, v := range a.Apps {
		if ApplicationReferences(s, v, pool) {
			apps = append(apps, id)
		}
	}
	return ext, apps, nil
}

// MakePoolState is pure: deletion's existing durable undo/redo journal commits these changes.
func MakePoolState(pool string) (map[string][]byte, []string, error) {
	s, e := Load()
	if e != nil {
		return nil, nil, e
	}
	a, e := LoadApps()
	if e != nil {
		return nil, nil, e
	}
	kept := []string{}
	removedNative := map[string]bool{}
	keep := func(ds string) {
		if ds == "" || strings.HasPrefix(ds, pool+"/") {
			return
		}
		for _, old := range kept {
			if old == ds {
				return
			}
		}
		kept = append(kept, ds)
	}
	for id, v := range a.Apps {
		if ApplicationReferences(s, v, pool) {
			keep(v.Dataset)
			keep(v.MountDataset)
			if v.Package.Manifest.IsNative() {
				removedNative[id] = true
				for _, ds := range NativeReferences(id) {
					keep(ds)
				}
			}
			delete(a.Apps, id)
		}
	}
	if strings.HasPrefix(s.Dataset, pool+"/") {
		s = State{Version: 1, Installed: map[string]Installation{}}
	}
	sb, _ := json.Marshal(s)
	ab, _ := json.Marshal(a)
	files := map[string][]byte{StateFile: sb, "apps.json": ab}
	var native map[string]NativeStateRecord
	if err := config.ReadJSON(NativeStatesFile, &native); err == nil {
		for id, record := range native {
			if removedNative[id] {
				delete(native, id)
				continue
			}
			for _, ds := range record.DataReferences {
				if strings.HasPrefix(ds, pool+"/") {
					delete(native, id)
					break
				}
			}
		}
		files[NativeStatesFile], _ = json.Marshal(native)
	} else if !os.IsNotExist(err) {
		return nil, nil, err
	}
	return files, kept, nil
}
func StopForPool(ctx context.Context, pool string) error {
	operationMu.Lock()
	defer operationMu.Unlock()
	s, e := Load()
	if e != nil {
		return e
	}
	_, ids, e := References(pool)
	if e != nil {
		return e
	}
	if strings.HasPrefix(s.Dataset, pool+"/") {
		for _, v := range s.Installed {
			if v.Package.Manifest.Kind == "driver" && v.Status != "disabled" {
				return fmt.Errorf("請先停用驅動並重新啟動，再刪除此儲存池")
			}
		}
	}
	for _, id := range ids {
		apps, err := LoadApps()
		if err != nil {
			return err
		}
		if e = stopApplication(ctx, s, apps.Apps[id]); e != nil {
			return e
		}
		if !apps.Apps[id].Package.Manifest.IsNative() {
			_, e = docker(ctx, s, "rm", containerName(id))
		}
		if e != nil {
			return e
		}
	}
	if strings.HasPrefix(s.Dataset, pool+"/") {
		for _, v := range s.Installed {
			if v.Package.Manifest.Kind != "driver" {
				if v.Enabled {
					if e = deactivate(ctx, s, v.Package); e != nil {
						return e
					}
				}
				if _, e = Run(ctx, "apk", "del", "--no-network", ".just-nas-"+v.Package.Manifest.ID); e != nil {
					return e
				}
			}
		}
		for _, v := range s.Installed {
			if v.Enabled && v.Package.Manifest.Kind == "driver" {
				return fmt.Errorf("請先停用驅動擴充並重新啟動，再刪除此儲存池")
			}
		}
	}
	return nil
}

func SetSSHKey(ctx context.Context, key string) error {
	operationMu.Lock()
	defer operationMu.Unlock()
	s, e := Load()
	if e != nil {
		return e
	}
	if e = Mounted(ctx, s); e != nil {
		return e
	}
	key = strings.TrimSpace(key)
	parts := strings.Fields(key)
	if len(parts) < 2 || parts[0] != "ssh-ed25519" || strings.ContainsAny(key, "\r\n") {
		return fmt.Errorf("請提供單一 Ed25519 SSH 公鑰")
	}
	raw, e := base64.StdEncoding.DecodeString(parts[1])
	if e != nil || len(raw) != 51 || binary.BigEndian.Uint32(raw[:4]) != 11 || string(raw[4:15]) != "ssh-ed25519" || binary.BigEndian.Uint32(raw[15:19]) != 32 {
		return fmt.Errorf("SSH 公鑰格式無效")
	}
	file := filepath.Join(Root(s), "authorized_keys")
	old, e := os.ReadFile(file)
	if e != nil && !os.IsNotExist(e) {
		return e
	}
	if e = config.AtomicWrite(file, []byte(key+"\n"), 0600); e != nil {
		return e
	}
	s.SSHPublicKey = key
	if e = persist(StateFile, s); e != nil {
		if old == nil {
			os.Remove(file)
		} else {
			_ = config.AtomicWrite(file, old, 0600)
		}
		return e
	}
	return nil
}
func SSHHostKey(ctx context.Context, s State) (string, string) {
	if Mounted(ctx, s) != nil {
		return "", ""
	}
	b, e := os.ReadFile(filepath.Join(Root(s), "ssh-host-ed25519.pub"))
	if e != nil || len(b) > 4096 {
		return "", ""
	}
	parts := strings.Fields(string(b))
	if len(parts) < 2 {
		return "", ""
	}
	raw, e := base64.StdEncoding.DecodeString(parts[1])
	if e != nil {
		return "", ""
	}
	sum := sha256.Sum256(raw)
	return strings.TrimSpace(string(b)), "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:])
}
