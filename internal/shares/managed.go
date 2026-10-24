package shares

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"just-nas/internal/command"
	"just-nas/internal/config"
	"just-nas/internal/storage"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func HashPassword(password string) (string, string) {
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	salt := hex.EncodeToString(b)
	k, e := pbkdf2.Key(sha256.New, password, []byte(salt), 600000, 32)
	if e != nil {
		panic(e)
	}
	return salt, hex.EncodeToString(k)
}
func (m *ShareManager) Authenticate(name, password string) (User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	users, e := readUsers()
	if e != nil {
		return User{}, e
	}
	for _, u := range users {
		if u.Name == name && !u.Disabled && u.Hash != "" && len(password) <= 128 {
			k, _ := pbkdf2.Key(sha256.New, password, []byte(u.Salt), 600000, 32)
			if subtle.ConstantTimeCompare([]byte(hex.EncodeToString(k)), []byte(u.Hash)) == 1 {
				u.Salt = ""
				u.Hash = ""
				return u, nil
			}
		}
	}
	return User{}, fmt.Errorf("帳號或密碼錯誤，舊帳號請由管理員先設定網頁登入密碼")
}
func (m *ShareManager) Lookup(name string) (User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	us, e := readUsers()
	if e != nil {
		return User{}, e
	}
	for _, u := range us {
		if u.Name == name {
			u.Hash = ""
			u.Salt = ""
			return u, nil
		}
	}
	return User{}, fmt.Errorf("找不到帳號")
}
func (m *ShareManager) UpdateUser(id string, disabled *bool, password *string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	users, e := readUsers()
	if e != nil {
		return e
	}
	old := append([]User{}, users...)
	idx := -1
	for i, u := range users {
		if u.ID == id {
			idx = i
		}
	}
	if idx < 0 {
		return fmt.Errorf("找不到帳號")
	}
	u := &users[idx]
	oldDB, e := os.ReadFile(filepath.Join(config.Dir, "smbpasswd"))
	if e != nil {
		return e
	}
	rollback := func() {
		_ = config.AtomicWrite(filepath.Join(config.Dir, "smbpasswd"), oldDB, 0600)
		_ = config.WriteJSON("users.json", old)
	}
	if password != nil {
		if len(*password) < 12 || len(*password) > 128 || strings.ContainsAny(*password, "\r\n") {
			return fmt.Errorf("密碼須 12–128 字元且不含換行")
		}
		if _, e = command.Run(*password+"\n"+*password+"\n", "smbpasswd", "-s", u.Name); e != nil {
			return e
		}
		u.Salt, u.Hash = HashPassword(*password)
	}
	if disabled != nil {
		u.Disabled = *disabled
	}
	flag := "-e"
	if u.Disabled {
		flag = "-d"
	}
	if _, e = command.Run("", "smbpasswd", flag, u.Name); e != nil {
		rollback()
		return e
	}
	u.Revision++
	if e = config.WriteJSON("users.json", users); e == nil {
		e = config.NewPersistenceManager().SaveConfigToZfs()
	}
	if e != nil {
		rollback()
		return e
	}
	// Samba's reload-config does not revoke established sessions: terminate them explicitly.
	if _, e = command.Run("", "rc-service", "samba", "restart"); e != nil {
		return fmt.Errorf("帳號已保存；中止 SMB 工作階段失敗，請重啟 SMB: %w", e)
	}
	return nil
}
func (m *ShareManager) UpdateShare(id string, change map[string]any) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	old, e := readShares()
	if e != nil {
		return e
	}
	next := append([]ShareConfig{}, old...)
	idx := -1
	for i, s := range next {
		if s.ID == id {
			idx = i
		}
	}
	if idx < 0 {
		return fmt.Errorf("找不到分享")
	}
	s := &next[idx]
	for k, v := range change {
		switch k {
		case "name", "protocol", "id", "mode":
			return fmt.Errorf("名稱、協定、模式與 ID 建立後固定")
		case "comment", "path", "allowed_hosts":
			x, ok := v.(string)
			if !ok {
				return fmt.Errorf("欄位格式錯誤")
			}
			switch k {
			case "comment":
				s.Comment = x
			case "path":
				if s.Mode != "legacy" && x != s.Path {
					return fmt.Errorf("受管理空間不可更改實體路徑")
				}
				s.Path = x
			case "allowed_hosts":
				s.AllowedHosts = x
			}
		case "disabled", "read_only", "guest_ok":
			x, ok := v.(bool)
			if !ok {
				return fmt.Errorf("欄位格式錯誤")
			}
			switch k {
			case "disabled":
				s.Disabled = x
			case "read_only":
				s.ReadOnly = x
			case "guest_ok":
				s.GuestOk = x
			}
		case "users":
			if s.Mode != "legacy" {
				return fmt.Errorf("受管理空間授權需使用空間權限介面")
			}
			a, ok := v.([]any)
			if !ok {
				return fmt.Errorf("帳號格式錯誤")
			}
			s.Users = nil
			for _, v := range a {
				x, ok := v.(string)
				if !ok {
					return fmt.Errorf("帳號格式錯誤")
				}
				s.Users = append(s.Users, x)
			}
		default:
			return fmt.Errorf("不支援的修改欄位: %s", k)
		}
	}
	if e = m.validateShare(*s); e != nil {
		return e
	}
	if e = m.commit(old, next); e != nil {
		return e
	}
	if s.Protocol == "smb" {
		_, e = command.Run("", "smbcontrol", "smbd", "close-share", s.Name)
	}
	return e
}

// CreateSpace only provisions a new dataset. Existing content is never chmod/chowned recursively.
func (m *ShareManager) CreateSpace(pool, name, kind, owner, quota string, access map[string]string) error {
	if kind != "private" && kind != "public" {
		return fmt.Errorf("空間類型無效")
	}
	if !nameRE.MatchString(name) {
		return fmt.Errorf("名稱無效")
	}
	users, e := m.GetUsers()
	if e != nil {
		return e
	}
	known := map[string]User{}
	for _, u := range users {
		known[u.Name] = u
	}
	allowed := []string{}
	for n, mode := range access {
		if _, ok := known[n]; !ok || (mode != "ro" && mode != "rw") {
			return fmt.Errorf("帳號或權限無效")
		}
		allowed = append(allowed, n)
	}
	if kind == "private" {
		if _, ok := known[owner]; !ok {
			return fmt.Errorf("私人空間必須指定有效帳號")
		}
		allowed = []string{owner}
		access = map[string]string{owner: "rw"}
	}
	if len(allowed) == 0 {
		return fmt.Errorf("請選擇授權帳號")
	}
	ds := pool + "/" + name
	if !storage.ValidDataset(ds) {
		return fmt.Errorf("資料集無效")
	}
	sm := storage.NewStorageManager()
	if e = sm.CreateDataset(ds, "", "lz4"); e != nil {
		return e
	}
	path := "/mnt/" + ds
	if quota != "" {
		if e = sm.UpdateDataset(ds, "", quota); e != nil {
			return e
		}
	}
	// One named group per public dataset; POSIX ACLs enforce each user's read/write grant.
	group := "nas_" + stableID(ds)[:12]
	if _, e = command.Run("", "addgroup", "-S", group); e != nil {
		return e
	}
	for _, n := range allowed {
		if _, e = command.Run("", "addgroup", n, group); e != nil {
			return e
		}
	}
	if _, e = command.Run("", "chown", "root:"+group, path); e != nil {
		return e
	}
	if _, e = command.Run("", "chmod", "2770", path); e != nil {
		return e
	}
	if kind == "private" {
		if _, e = command.Run("", "chown", owner+":"+group, path); e != nil {
			return e
		}
		if _, e = command.Run("", "chmod", "0700", path); e != nil {
			return e
		}
	} else {
		if _, e = command.Run("", "setfacl", "-m", "u::rwx,g::---,o::---,m::rwx,d:u::rwx,d:g::---,d:o::---,d:m::rwx", path); e != nil {
			return e
		}
		for n, mode := range access {
			perm := "r-x"
			if mode == "rw" {
				perm = "rwx"
			}
			if _, e = command.Run("", "setfacl", "-m", "u:"+n+":"+perm+",d:u:"+n+":"+perm, path); e != nil {
				return e
			}
		}
	}
	trash := filepath.Join(path, ".nas-trash")
	if e = os.Mkdir(trash, 0711); e != nil {
		return e
	}
	for _, n := range allowed {
		dir := filepath.Join(trash, n)
		if e = os.Mkdir(dir, 0700); e != nil {
			return e
		}
		if _, e = command.Run("", "chown", strconv.Itoa(known[n].UID), dir); e != nil {
			return e
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	old, e := readShares()
	if e != nil {
		return e
	}
	for _, s := range old {
		if strings.EqualFold(s.Name, name) {
			return fmt.Errorf("分享名稱已存在；資料集已保留，請檢查")
		}
	}
	return m.commit(old, append(old, ShareConfig{ID: stableID("smb:" + name), Name: name, Protocol: "smb", Path: path, Mode: kind, Users: allowed, Access: access}))
}
