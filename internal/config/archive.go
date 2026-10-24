package config

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func ExportFiles() (map[string][]byte, error) {
	mu.Lock()
	defer mu.Unlock()
	if backupDir == "" {
		return nil, fmt.Errorf("設定尚未保存")
	}
	b, e := os.ReadFile(filepath.Join(backupDir, "CURRENT"))
	root := backupDir
	if e == nil {
		root = filepath.Join(root, "generations", strings.TrimSpace(string(b)))
	} else if !os.IsNotExist(e) {
		return nil, e
	}
	out := map[string][]byte{}
	for _, name := range StateFiles {
		b, e := os.ReadFile(filepath.Join(root, name))
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return nil, e
		}
		out[name] = b
	}
	return out, nil
}
func EncryptExport(password string) ([]byte, error) {
	if len(password) < 12 || len(password) > 256 {
		return nil, fmt.Errorf("備份密碼須 12–256 字元")
	}
	state, e := ExportFiles()
	if e != nil {
		return nil, e
	}
	raw, e := json.Marshal(state)
	if e != nil {
		return nil, e
	}
	salt := make([]byte, 32)
	if _, e = rand.Read(salt); e != nil {
		return nil, e
	}
	key, e := pbkdf2.Key(sha256.New, password, salt, 600000, 32)
	if e != nil {
		return nil, e
	}
	block, _ := aes.NewCipher(key)
	gcm, _ := cipher.NewGCM(block)
	nonce := make([]byte, gcm.NonceSize())
	if _, e = rand.Read(nonce); e != nil {
		return nil, e
	}
	encrypted := gcm.Seal(nil, nonce, raw, []byte("just-nas-settings-v2"))
	return json.Marshal(struct {
		Version           int
		Salt, Nonce, Data []byte
	}{2, salt, nonce, encrypted})
}
func DecryptExport(raw []byte, password string) (map[string][]byte, error) {
	if len(raw) > 16<<20 || len(password) < 12 || len(password) > 256 {
		return nil, fmt.Errorf("設定備份格式或密碼無效")
	}
	var b struct {
		Version           int
		Salt, Nonce, Data []byte
	}
	if json.Unmarshal(raw, &b) != nil || b.Version != 2 || len(b.Salt) != 32 || len(b.Nonce) != 12 {
		return nil, fmt.Errorf("不支援的備份格式")
	}
	key, _ := pbkdf2.Key(sha256.New, password, b.Salt, 600000, 32)
	block, _ := aes.NewCipher(key)
	gcm, _ := cipher.NewGCM(block)
	plain, e := gcm.Open(nil, b.Nonce, b.Data, []byte("just-nas-settings-v2"))
	if e != nil {
		return nil, fmt.Errorf("備份密碼錯誤或檔案損壞")
	}
	var state map[string][]byte
	if json.Unmarshal(plain, &state) != nil {
		return nil, fmt.Errorf("備份內容無效")
	}
	allowed := map[string]bool{}
	for _, n := range StateFiles {
		allowed[n] = true
	}
	for n, v := range state {
		if !allowed[n] || (strings.HasSuffix(n, ".json") && !json.Valid(v)) {
			return nil, fmt.Errorf("不允許的設定項目")
		}
	}
	if len(state["admin.json"]) == 0 {
		return nil, fmt.Errorf("缺少管理員設定")
	}
	return state, nil
}
func ImportFiles(state map[string][]byte) error {
	allowed := map[string]bool{}
	for _, n := range StateFiles {
		allowed[n] = true
	}
	for n := range state {
		if !allowed[n] {
			return fmt.Errorf("unknown configuration file")
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if backupDir == "" {
		return fmt.Errorf("請先建立設定池")
	}
	stage, e := os.MkdirTemp(Dir, "import-")
	if e != nil {
		return e
	}
	defer os.RemoveAll(stage)
	for n, b := range state {
		if e = AtomicWrite(filepath.Join(stage, n), b, 0600); e != nil {
			return e
		}
	}
	writesFrozen = true
	if e = copyState(stage, backupDir); e != nil {
		writesFrozen = false
	}
	return e
}
