package shares

import (
	"fmt"
	"io/fs"
	"just-nas/internal/command"
	"just-nas/internal/config"
	"os"
	"path/filepath"
	"strings"
)

func (m *ShareManager) UpdateAccess(id string, access map[string]string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	old, e := readShares()
	if e != nil {
		return e
	}
	idx := -1
	for i, s := range old {
		if s.ID == id {
			idx = i
		}
	}
	if idx < 0 || old[idx].Mode != "public" {
		return fmt.Errorf("只支援公共空間權限修改")
	}
	users, e := readUsers()
	if e != nil {
		return e
	}
	known := map[string]User{}
	for _, u := range users {
		known[u.Name] = u
	}
	names := []string{}
	for n, p := range access {
		if _, ok := known[n]; !ok || (p != "ro" && p != "rw") {
			return fmt.Errorf("帳號或權限錯誤")
		}
		names = append(names, n)
	}
	if len(names) == 0 {
		return fmt.Errorf("至少保留一個授權帳號")
	}
	s := old[idx]
	acl, e := command.Run("", "getfacl", "-R", "-p", s.Path)
	if e != nil {
		return e
	}
	backup, e := os.CreateTemp(config.Dir, "acl-backup-")
	if e != nil {
		return e
	}
	defer os.Remove(backup.Name())
	backup.Chmod(0600)
	_, e = backup.Write(acl)
	backup.Close()
	if e != nil {
		return e
	}
	rollback := func() { _, _ = command.Run("", "setfacl", "--restore="+backup.Name()) }
	e = filepath.WalkDir(s.Path, func(p string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if (d.Name() == ".nas-trash" || d.Name() == ".zfs") && d.IsDir() {
			return filepath.SkipDir
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if _, e = command.Run("", "setfacl", "-b", p); e != nil {
			return e
		}
		if _, e = command.Run("", "chown", "root", p); e != nil {
			return e
		}
		a := []string{"u::rw-", "g::---", "o::---", "m::rw-"}
		if d.IsDir() {
			a = []string{"u::rwx", "g::---", "o::---", "m::rwx", "d:u::rwx", "d:g::---", "d:o::---", "d:m::rwx"}
		}
		for n, mode := range access {
			perm := "r--"
			if mode == "rw" {
				perm = "rw-"
			}
			if d.IsDir() {
				perm = "r-x"
				if mode == "rw" {
					perm = "rwx"
				}
			}
			a = append(a, "u:"+n+":"+perm)
			if d.IsDir() {
				a = append(a, "d:u:"+n+":"+perm)
			}
		}
		_, e = command.Run("", "setfacl", "-m", strings.Join(a, ","), p)
		return e
	})
	if e != nil {
		rollback()
		return e
	}
	for _, n := range names {
		p := filepath.Join(s.Path, ".nas-trash", n)
		if e = os.MkdirAll(p, 0700); e != nil {
			rollback()
			return e
		}
		if _, e = command.Run("", "chown", n, p); e != nil {
			rollback()
			return e
		}
	}
	next := append([]ShareConfig{}, old...)
	next[idx].Access = access
	next[idx].Users = names
	if e = m.commit(old, next); e != nil {
		rollback()
		return e
	}
	_, e = command.Run("", "rc-service", "samba", "restart")
	return e
}
