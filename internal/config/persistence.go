package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"just-nas/internal/command"
	"just-nas/internal/logger"
)

var Dir = "/var/lib/just-nas"
var mu sync.Mutex
var backupDir string
var restoreError string
var writesFrozen bool

func RestoreError() string { mu.Lock(); defer mu.Unlock(); return restoreError }

func AtomicWrite(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".nas-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(mode); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err := os.Rename(f.Name(), path); err != nil {
		return err
	}
	if runtime.GOOS == "linux" {
		d, err := os.Open(filepath.Dir(path))
		if err != nil {
			return err
		}
		defer d.Close()
		return d.Sync()
	}
	return nil
}

func WriteJSON(name string, value any) error {
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return AtomicWrite(filepath.Join(Dir, name), b, 0600)
}
func ReadJSON(name string, value any) error {
	b, err := os.ReadFile(filepath.Join(Dir, name))
	if err != nil {
		return err
	}
	return json.Unmarshal(b, value)
}

type PersistenceManager struct{}

func NewPersistenceManager() *PersistenceManager { return &PersistenceManager{} }

func locateBackup() (string, error) {
	out, err := command.Run("", "zfs", "get", "-H", "-o", "name,value", "-s", "local", "org.just-nas:config")
	if err != nil {
		return "", err
	}
	var candidates []string
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && f[1] == "1" {
			candidates = append(candidates, f[0])
		}
	}
	if len(candidates) > 1 {
		return "", fmt.Errorf("多個設定資料集，請先選擇唯一設定來源")
	}
	if len(candidates) == 0 {
		return "", nil
	}
	out, err = command.Run("", "zfs", "get", "-H", "-o", "value", "mounted,mountpoint", candidates[0])
	if err != nil {
		return "", err
	}
	f := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(f) != 2 || f[0] != "yes" || !filepath.IsAbs(f[1]) {
		return "", fmt.Errorf("設定資料集尚未掛載")
	}
	return f[1], nil
}

type BootEvent struct{ Phase, State, Detail string }

func (m *PersistenceManager) RestoreConfigOnBoot() { m.RestoreConfigOnBootObserved(nil) }
func (m *PersistenceManager) RestoreConfigOnBootObserved(observe func(BootEvent)) {
	if runtime.GOOS != "linux" {
		return
	}
	m.restoreConfigOnBootObserved(observe)
}
func (m *PersistenceManager) restoreConfigOnBootObserved(observe func(BootEvent)) {
	event := func(p, s, d string) {
		if observe != nil {
			observe(BootEvent{p, s, d})
		}
	}
	event("pools", "running", "Loading ZFS and importing pools")
	if _, err := command.Run("", "modprobe", "zfs"); err != nil {
		logger.Error("BOOT", "%v", err)
		event("pools", "fail", "ZFS module failed; see logs")
		event("settings", "skip", "Persistent settings unavailable")
		return
	}
	_, importErr := command.Run("", "zpool", "import", "-a")
	if importErr != nil {
		logger.Warn("BOOT", "%v", importErr)
	}
	_, mountErr := command.Run("", "zfs", "mount", "-a")
	if mountErr != nil {
		logger.Warn("BOOT", "%v", mountErr)
	}
	pools, listErr := command.Run("", "zpool", "list", "-H", "-o", "name,health")
	switch {
	case listErr != nil:
		event("pools", "fail", "Cannot verify imported pools; see logs")
	case mountErr != nil:
		event("pools", "fail", "Dataset mount failed; see logs")
	case importErr != nil && !strings.Contains(strings.ToLower(importErr.Error()), "no pools available"):
		event("pools", "fail", "One or more pools could not be imported; see logs")
	case strings.TrimSpace(string(pools)) == "":
		event("pools", "skip", "First setup: no storage pools")
	default:
		healthy := true
		for _, line := range strings.Split(strings.TrimSpace(string(pools)), "\n") {
			f := strings.Fields(line)
			if len(f) != 2 || f[1] != "ONLINE" {
				healthy = false
			}
		}
		if healthy {
			event("pools", "ok", "Pools imported and datasets mounted")
		} else {
			event("pools", "warn", "Pool health requires attention")
		}
	}
	event("settings", "running", "Restoring persistent settings")
	mu.Lock()
	defer mu.Unlock()
	path, err := locateBackup()
	if err != nil {
		restoreError = err.Error()
		logger.Error("PERSISTENCE", "%v", err)
		event("settings", "fail", "Persistent settings could not be located; see logs")
		return
	}
	backupDir = path
	if path == "" {
		logger.Info("PERSISTENCE", "尚無持久設定資料集；建立第一個池後保存設定")
		event("settings", "skip", "First setup: create a storage pool in the web console")
		return
	}
	if err = copyState(path, Dir); err != nil {
		restoreError = err.Error()
		logger.Error("PERSISTENCE", "還原失敗: %v", err)
		event("settings", "fail", "Settings restore failed; see logs")
	} else {
		logger.Info("PERSISTENCE", "已從 %s 還原設定", path)
		event("settings", "ok", "Persistent settings restored")
	}
}

func (m *PersistenceManager) EnsurePool(pool string) error {
	mu.Lock()
	defer mu.Unlock()
	path, err := locateBackup()
	if err != nil {
		return err
	}
	if path == "" {
		path = "/mnt/" + pool + "/.system_config"
		if _, err = command.Run("", "zfs", "create", "-o", "mountpoint="+path, "-o", "org.just-nas:config=1", pool+"/.just-nas"); err != nil {
			return err
		}
	}
	backupDir = path
	return copyState(Dir, path)
}

func (m *PersistenceManager) SaveConfigToZfs() error {
	mu.Lock()
	defer mu.Unlock()
	if writesFrozen {
		return fmt.Errorf("設定已恢復，等待重新啟動；拒絕覆寫新設定")
	}
	path, err := locateBackup()
	if err != nil {
		return err
	}
	backupDir = path
	if path == "" {
		return fmt.Errorf("尚未設定持久設定池")
	}
	return copyState(Dir, path)
}

func Persistent() bool { mu.Lock(); defer mu.Unlock(); return backupDir != "" }

var StateFiles = []string{"native-apps.json", "extensions.json", "apps.json", "pool-deletion.json", "admin.json", "shares.json", "users.json", "uid-state.json", "retired-users.json", "smbpasswd", "jobs.json", "schedules.json", "alerts.json", "backup.json", "network.json", "tls.json", "audit.json", "smtp.json"}

func copyState(src, dst string) error {
	if err := os.MkdirAll(dst, 0700); err != nil {
		return err
	}
	var expected map[string]string
	if b, err := os.ReadFile(filepath.Join(src, "CURRENT")); err == nil {
		id := strings.TrimSpace(string(b))
		if !regexp.MustCompile(`^[0-9]+$`).MatchString(id) {
			return fmt.Errorf("invalid configuration generation")
		}
		src = filepath.Join(src, "generations", id)
		b, err = os.ReadFile(filepath.Join(src, "manifest.json"))
		var manifest struct {
			Version int               `json:"version"`
			Files   map[string]string `json:"files"`
		}
		if err != nil || json.Unmarshal(b, &manifest) != nil || manifest.Version != 2 {
			return fmt.Errorf("unsupported configuration generation")
		}
		expected = manifest.Files
	}
	files := map[string][]byte{}
	for _, name := range StateFiles {
		b, err := os.ReadFile(filepath.Join(src, name))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if strings.HasSuffix(name, ".json") && !json.Valid(b) {
			return fmt.Errorf("設定檔 %s 損壞，保留現況", name)
		}
		files[name] = b
	}
	if len(expected) > 0 {
		if len(expected) != len(files) {
			return fmt.Errorf("incomplete configuration generation")
		}
		for n, b := range files {
			h := sha256.Sum256(b)
			if expected[n] != hex.EncodeToString(h[:]) {
				return fmt.Errorf("configuration checksum mismatch: %s", n)
			}
		}
	}
	if dst == Dir {
		for _, name := range StateFiles {
			if b, ok := files[name]; ok {
				if err := AtomicWrite(filepath.Join(dst, name), b, 0600); err != nil {
					return err
				}
			} else if err := os.Remove(filepath.Join(dst, name)); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
		return nil
	}
	id := strconv.FormatInt(time.Now().UnixNano(), 10)
	generation := filepath.Join(dst, "generations", id)
	for name, b := range files {
		if err := AtomicWrite(filepath.Join(generation, name), b, 0600); err != nil {
			return err
		}
	}
	checksums := map[string]string{}
	for n, b := range files {
		h := sha256.Sum256(b)
		checksums[n] = hex.EncodeToString(h[:])
	}
	manifest, _ := json.Marshal(map[string]any{"version": 2, "files": checksums})
	if err := AtomicWrite(filepath.Join(generation, "manifest.json"), manifest, 0600); err != nil {
		return err
	}
	// Retire legacy files before committing. An old image must not publish stale shares.
	for _, name := range []string{"admin.json", "shares.json", "users.json"} {
		if err := AtomicWrite(filepath.Join(dst, name), []byte(`{"requires_just_nas_schema":2}`), 0600); err != nil {
			return err
		}
	}
	if err := AtomicWrite(filepath.Join(dst, "CURRENT"), []byte(id), 0600); err != nil {
		return err
	}
	entries, _ := os.ReadDir(filepath.Join(dst, "generations"))
	if len(entries) > 32 {
		for _, v := range entries[:len(entries)-32] {
			if v.IsDir() && regexp.MustCompile(`^[0-9]+$`).MatchString(v.Name()) && v.Name() != id {
				_ = os.RemoveAll(filepath.Join(dst, "generations", v.Name()))
			}
		}
	}
	return nil
}

func (m *PersistenceManager) StartBackgroundReconciler(interval time.Duration) {
	if runtime.GOOS != "linux" {
		return
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			if _, err := command.Run("", "zfs", "mount", "-a"); err != nil {
				logger.Warn("MOUNT", "%v", err)
			}
		}
	}()
}
