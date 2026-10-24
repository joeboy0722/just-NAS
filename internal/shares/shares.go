package shares

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"just-nas/internal/command"
	"just-nas/internal/config"
	"just-nas/internal/storage"
)

type ShareConfig struct {
	Name         string   `json:"name"`
	Protocol     string   `json:"protocol"`
	Path         string   `json:"path"`
	Comment      string   `json:"comment"`
	ReadOnly     bool     `json:"read_only"`
	GuestOk      bool     `json:"guest_ok"`
	AllowedHosts string   `json:"allowed_hosts,omitempty"`
	Users        []string `json:"users,omitempty"`
}
type User struct {
	Name string `json:"name"`
	UID  int    `json:"uid"`
}
type ShareManager struct {
	mu          sync.Mutex
	SmbConfPath string
	NfsConfPath string
}

func NewShareManager() *ShareManager {
	return &ShareManager{SmbConfPath: "/etc/samba/smb.conf", NfsConfPath: "/etc/exports"}
}

var nameRE = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,31}$`)
var userRE = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,30}$`)

func readShares() ([]ShareConfig, error) {
	r := []ShareConfig{}
	err := config.ReadJSON("shares.json", &r)
	if os.IsNotExist(err) {
		err = nil
	}
	return r, err
}
func readUsers() ([]User, error) {
	r := []User{}
	err := config.ReadJSON("users.json", &r)
	if os.IsNotExist(err) {
		err = nil
	}
	return r, err
}
func (m *ShareManager) GetShares() ([]ShareConfig, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return readShares()
}
func (m *ShareManager) GetUsers() ([]User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return readUsers()
}
func (m *ShareManager) Restore() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	users, err := readUsers()
	if err != nil {
		return err
	}
	for _, u := range users {
		if _, err = command.Run("", "id", "-u", u.Name); err != nil {
			if _, err = command.Run("", "adduser", "-D", "-H", "-s", "/sbin/nologin", "-u", strconv.Itoa(u.UID), u.Name); err != nil {
				return err
			}
		}
	}
	shares, err := readShares()
	if err != nil {
		return err
	}
	return m.apply(shares)
}
func (m *ShareManager) CreateUser(name, password string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !config.Persistent() {
		return fmt.Errorf("請先建立持久設定儲存池")
	}
	if !userRE.MatchString(name) || len(password) < 12 || len(password) > 128 || strings.ContainsAny(password, "\r\n") {
		return fmt.Errorf("帳號需為小寫英數，密碼須 12–128 字元且不含換行")
	}
	users, err := readUsers()
	if err != nil {
		return err
	}
	for _, u := range users {
		if u.Name == name {
			return fmt.Errorf("帳號已存在")
		}
	}
	if _, e := command.Run("", "id", "-u", name); e == nil {
		return fmt.Errorf("帳號與既有系統帳號衝突")
	}
	out, err := command.Run("", "adduser", "-D", "-H", "-s", "/sbin/nologin", name)
	_ = out
	if err != nil {
		return err
	}
	out, err = command.Run("", "id", "-u", name)
	if err != nil {
		return err
	}
	uid, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		return err
	}
	if _, err = command.Run(password+"\n"+password+"\n", "smbpasswd", "-s", "-a", name); err != nil {
		_, _ = command.Run("", "deluser", name)
		return err
	}
	users = append(users, User{name, uid})
	if err = config.WriteJSON("users.json", users); err != nil {
		return err
	}
	return config.NewPersistenceManager().SaveConfigToZfs()
}
func (m *ShareManager) DeleteUser(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !userRE.MatchString(name) {
		return fmt.Errorf("無效的帳號")
	}
	users, err := readUsers()
	if err != nil {
		return err
	}
	shares, err := readShares()
	if err != nil {
		return err
	}
	for _, s := range shares {
		for _, u := range s.Users {
			if u == name {
				return fmt.Errorf("請先移除此帳號的分享授權")
			}
		}
	}
	next := []User{}
	found := false
	for _, u := range users {
		if u.Name == name {
			found = true
		} else {
			next = append(next, u)
		}
	}
	if !found {
		return fmt.Errorf("找不到帳號")
	}
	if _, err = command.Run("", "smbpasswd", "-x", name); err != nil {
		return err
	}
	if _, err = command.Run("", "deluser", name); err != nil {
		return err
	}
	if err = config.WriteJSON("users.json", next); err != nil {
		return err
	}
	return config.NewPersistenceManager().SaveConfigToZfs()
}
func (m *ShareManager) CreateShare(s ShareConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !config.Persistent() {
		return fmt.Errorf("請先建立持久設定儲存池")
	}
	if !nameRE.MatchString(s.Name) || strings.ContainsAny(s.Comment, "\r\n") || strings.ContainsAny(s.Path, "\r\n\t \"") {
		return fmt.Errorf("共享名稱或路徑無效")
	}
	if strings.EqualFold(s.Name, "global") || strings.EqualFold(s.Name, "homes") || strings.EqualFold(s.Name, "printers") {
		return fmt.Errorf("此共享名稱為系統保留")
	}
	if err := storage.MountedDataset(s.Path); err != nil {
		return err
	}
	info, err := os.Stat(s.Path)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("共享路徑必須為已存在的目錄")
	}
	if s.Protocol == "smb" {
		users, e := readUsers()
		if e != nil {
			return e
		}
		allowed := map[string]bool{}
		for _, u := range users {
			allowed[u.Name] = true
		}
		if !s.GuestOk && len(s.Users) == 0 {
			return fmt.Errorf("請至少選擇一個 SMB 帳號")
		}
		for _, u := range s.Users {
			if !allowed[u] {
				return fmt.Errorf("不存在的 SMB 帳號: %s", u)
			}
		}
	} else if s.Protocol == "nfs" {
		if s.AllowedHosts == "" {
			return fmt.Errorf("NFS 必須指定允許的 IP 或網段")
		}
		for _, h := range strings.Fields(s.AllowedHosts) {
			if net.ParseIP(h) == nil {
				if _, _, e := net.ParseCIDR(h); e != nil {
					return fmt.Errorf("無效的 NFS IP／網段")
				}
			}
		}
	} else {
		return fmt.Errorf("不支援的共享協定")
	}
	old, err := readShares()
	if err != nil {
		return err
	}
	for _, v := range old {
		if v.Protocol == s.Protocol && strings.EqualFold(v.Name, s.Name) {
			return fmt.Errorf("共享名稱已存在")
		}
		if v.Protocol == "nfs" && s.Protocol == "nfs" && v.Path == s.Path {
			return fmt.Errorf("此路徑已有 NFS 分享")
		}
	}
	// Dedicated NAS datasets use a service account as the filesystem identity.
	if _, err = command.Run("", "chown", "nobody:nobody", s.Path); err != nil {
		return err
	}
	if _, err = command.Run("", "chmod", "0770", s.Path); err != nil {
		return err
	}
	return m.commit(old, append(append([]ShareConfig{}, old...), s))
}
func (m *ShareManager) DeleteShare(name, protocol string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	old, err := readShares()
	if err != nil {
		return err
	}
	next := []ShareConfig{}
	found := false
	for _, s := range old {
		if s.Name == name && s.Protocol == protocol {
			found = true
		} else {
			next = append(next, s)
		}
	}
	if !found {
		return fmt.Errorf("找不到共享")
	}
	return m.commit(old, next)
}
func (m *ShareManager) commit(old, next []ShareConfig) error {
	if err := m.apply(next); err != nil {
		_ = m.apply(old)
		return err
	}
	if err := config.WriteJSON("shares.json", next); err != nil {
		_ = m.apply(old)
		return err
	}
	if err := config.NewPersistenceManager().SaveConfigToZfs(); err != nil {
		return fmt.Errorf("設定已套用但持久保存失敗: %w", err)
	}
	return nil
}
func render(shares []ShareConfig) (string, string) {
	smb := "[global]\n   workgroup = WORKGROUP\n   server role = standalone server\n   security = user\n   map to guest = Bad User\n   guest account = nobody\n   passdb backend = smbpasswd:" + filepath.Join(config.Dir, "smbpasswd") + "\n   server min protocol = SMB2\n   load printers = no\n   disable spoolss = yes\n   unix extensions = no\n   wide links = no\n"
	exports := ""
	for _, s := range shares {
		if s.Protocol == "smb" {
			smb += fmt.Sprintf("\n[%s]\n   path = %s\n   comment = %s\n   browseable = yes\n   read only = %s\n   guest ok = %s\n   force user = nobody\n   force group = nobody\n   create mask = 0660\n   directory mask = 0770\n", s.Name, s.Path, s.Comment, yes(s.ReadOnly), yes(s.GuestOk))
			if !s.GuestOk {
				smb += "   valid users = " + strings.Join(s.Users, " ") + "\n"
			}
		} else {
			mode := "rw"
			if s.ReadOnly {
				mode = "ro"
			}
			exports += s.Path
			for _, h := range strings.Fields(s.AllowedHosts) {
				exports += fmt.Sprintf(" %s(%s,sync,no_subtree_check,root_squash,all_squash,anonuid=65534,anongid=65534)", h, mode)
			}
			exports += "\n"
		}
	}
	return smb, exports
}
func yes(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}
func (m *ShareManager) apply(shares []ShareConfig) error {
	// Missing datasets stay unavailable: never serve a directory on the RAM root.
	active := []ShareConfig{}
	for _, s := range shares {
		if err := storage.MountedDataset(s.Path); err == nil {
			active = append(active, s)
		}
	}
	smb, nfs := render(active)
	candidate := filepath.Join(config.Dir, "smb.conf.next")
	if err := config.AtomicWrite(candidate, []byte(smb), 0600); err != nil {
		return err
	}
	defer os.Remove(candidate)
	if _, err := command.Run("", "testparm", "-s", candidate); err != nil {
		return err
	}
	oldSMB, _ := os.ReadFile(m.SmbConfPath)
	oldNFS, _ := os.ReadFile(m.NfsConfPath)
	if err := config.AtomicWrite(m.SmbConfPath, []byte(smb), 0644); err != nil {
		return err
	}
	if err := config.AtomicWrite(m.NfsConfPath, []byte(nfs), 0644); err != nil {
		_ = config.AtomicWrite(m.SmbConfPath, oldSMB, 0644)
		return err
	}
	if err := m.reload(); err != nil {
		_ = config.AtomicWrite(m.SmbConfPath, oldSMB, 0644)
		_ = config.AtomicWrite(m.NfsConfPath, oldNFS, 0644)
		_ = m.reload()
		return err
	}
	return nil
}
func (m *ShareManager) reload() error {
	action := "reload"
	if _, err := command.Run("", "rc-service", "samba", "status"); err != nil {
		action = "start"
	}
	if _, err := command.Run("", "rc-service", "samba", action); err != nil {
		return err
	}
	if _, err := command.Run("", "rc-service", "nfs", "status"); err != nil {
		if _, err = command.Run("", "rc-service", "nfs", "start"); err != nil {
			return err
		}
	}
	_, err := command.Run("", "exportfs", "-ra")
	return err
}
func (m *ShareManager) InUse(dataset string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, _ := readShares()
	for _, v := range s {
		if v.Path == "/mnt/"+dataset || strings.HasPrefix(v.Path, "/mnt/"+dataset+"/") {
			return true
		}
	}
	return false
}
