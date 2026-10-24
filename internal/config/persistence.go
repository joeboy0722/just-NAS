package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
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
	return os.Rename(f.Name(), path)
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

func (m *PersistenceManager) RestoreConfigOnBoot() {
	if runtime.GOOS != "linux" {
		return
	}
	if _, err := command.Run("", "modprobe", "zfs"); err != nil {
		logger.Error("BOOT", "%v", err)
		return
	}
	if _, err := command.Run("", "zpool", "import", "-a"); err != nil {
		logger.Warn("BOOT", "%v", err)
	}
	if _, err := command.Run("", "zfs", "mount", "-a"); err != nil {
		logger.Warn("BOOT", "%v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	path, err := locateBackup()
	if err != nil {
		restoreError = err.Error()
		logger.Error("PERSISTENCE", "%v", err)
		return
	}
	backupDir = path
	if path == "" {
		logger.Info("PERSISTENCE", "尚無持久設定資料集；建立第一個池後保存設定")
		return
	}
	if err = copyState(path, Dir); err != nil {
		restoreError = err.Error()
		logger.Error("PERSISTENCE", "還原失敗: %v", err)
	} else {
		logger.Info("PERSISTENCE", "已從 %s 還原設定", path)
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

func copyState(src, dst string) error {
	if err := os.MkdirAll(dst, 0700); err != nil {
		return err
	}
	files := map[string][]byte{}
	for _, name := range []string{"admin.json", "shares.json", "users.json", "smbpasswd"} {
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
	for name, b := range files {
		if err := AtomicWrite(filepath.Join(dst, name), b, 0600); err != nil {
			return err
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
