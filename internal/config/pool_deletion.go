package config

import (
	"encoding/json"
	"fmt"
	"just-nas/internal/command"
	"os"
	"path/filepath"
	"strings"
)

// PoolDeletion is a durable undo/redo record, persisted before removing any references.
// Missing files are represented by nil; credentials stay in mode-0600 state files.
type PoolDeletion struct {
	Pool   string            `json:"pool"`
	GUID   string            `json:"guid"`
	Before map[string][]byte `json:"before"`
	After  map[string][]byte `json:"after"`
}

const poolDeletionFile = "pool-deletion.json"

var poolDeletionFiles = map[string]bool{"shares.json": true, "users.json": true, "jobs.json": true, "schedules.json": true, "backup.json": true, "alerts.json": true}

func ApplyPoolState(files map[string][]byte) error {
	for name, b := range files {
		if !poolDeletionFiles[name] {
			return fmt.Errorf("不允許的刪池設定檔")
		}
		if b != nil && !json.Valid(b) {
			return fmt.Errorf("刪池設定格式無效")
		}
	}
	for name, b := range files {
		if b == nil {
			if e := os.Remove(filepath.Join(Dir, name)); e != nil && !os.IsNotExist(e) {
				return e
			}
		} else if e := AtomicWrite(filepath.Join(Dir, name), b, 0600); e != nil {
			return e
		}
	}
	return NewPersistenceManager().SaveConfigToZfs()
}
func BeginPoolDeletion(record PoolDeletion) error {
	var pending PoolDeletion
	if e := ReadJSON(poolDeletionFile, &pending); e == nil {
		return fmt.Errorf("尚有未完成的刪池清理，請先恢復")
	} else if !os.IsNotExist(e) {
		return e
	}
	if e := WriteJSON(poolDeletionFile, record); e != nil {
		return e
	}
	if e := NewPersistenceManager().SaveConfigToZfs(); e != nil {
		_ = os.Remove(filepath.Join(Dir, poolDeletionFile))
		return e
	}
	return nil
}
func FinishPoolDeletion() error {
	b, e := os.ReadFile(filepath.Join(Dir, poolDeletionFile))
	if os.IsNotExist(e) {
		return nil
	}
	if e != nil {
		return e
	}
	if e = os.Remove(filepath.Join(Dir, poolDeletionFile)); e != nil {
		return e
	}
	if e = NewPersistenceManager().SaveConfigToZfs(); e != nil {
		_ = AtomicWrite(filepath.Join(Dir, poolDeletionFile), b, 0600)
		return e
	}
	return nil
}
func RecoverPoolDeletion() error {
	var record PoolDeletion
	if e := ReadJSON(poolDeletionFile, &record); os.IsNotExist(e) {
		return nil
	} else if e != nil {
		return e
	}
	b, e := command.Run("", "zpool", "list", "-H", "-o", "name,guid")
	if e != nil {
		return e
	}
	files := record.After
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && f[0] == record.Pool {
			if f[1] != record.GUID {
				return fmt.Errorf("同名池 GUID 已改變，拒絕自動恢復刪池設定")
			}
			files = record.Before
		}
	}
	if e = ApplyPoolState(files); e != nil {
		return e
	}
	return FinishPoolDeletion()
}

func PoolDeletionPending() bool {
	_, e := os.Stat(filepath.Join(Dir, poolDeletionFile))
	return e == nil || !os.IsNotExist(e)
}
