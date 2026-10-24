package backup

import (
	"fmt"
	"just-nas/internal/command"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

func MountUUID(uuid string) (string, error) {
	if !regexp.MustCompile(`^[a-fA-F0-9-]{32,36}$`).MatchString(uuid) {
		return "", fmt.Errorf("裝置 UUID 無效")
	}
	b, e := command.Run("", "blkid", "-U", uuid)
	if e != nil {
		return "", e
	}
	device := strings.TrimSpace(string(b))
	if !strings.HasPrefix(device, "/dev/") {
		return "", fmt.Errorf("找不到裝置")
	}
	b, e = command.Run("", "blkid", "-s", "TYPE", "-o", "value", device)
	if e != nil || strings.TrimSpace(string(b)) != "ext4" {
		return "", fmt.Errorf("只接受已格式化的 ext4 備份裝置")
	}
	target := filepath.Join("/mnt/backups", uuid)
	if u, e := mountIdentity(target); e == nil && u == uuid {
		return target, nil
	}
	if e = os.MkdirAll(target, 0700); e != nil {
		return "", e
	}
	if _, e = command.Run("", "mount", "-t", "ext4", "-o", "nodev,nosuid,noexec", device, target); e != nil {
		return "", e
	}
	if _, e = mountIdentity(target); e != nil {
		_, _ = command.Run("", "umount", target)
		return "", e
	}
	return target, nil
}
func RestoreMount() error {
	s, e := Get()
	if os.IsNotExist(e) {
		return nil
	}
	if e != nil {
		return e
	}
	if s.Mount != filepath.Join("/mnt/backups", s.UUID) {
		return nil
	}
	_, e = MountUUID(s.UUID)
	return e
}
