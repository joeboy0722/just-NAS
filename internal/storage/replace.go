package storage

import (
	"fmt"
	"just-nas/internal/command"
	"strings"
)

func (m *StorageManager) Replace(pool, old, newDevice string) error {
	if !poolRE.MatchString(pool) || strings.ContainsAny(old+newDevice, "\r\n\t ") {
		return fmt.Errorf("替換參數無效")
	}
	b, e := command.Run("", "zpool", "status", "-P", pool)
	if e != nil {
		return e
	}
	found := false
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) > 0 && f[0] == old && strings.HasPrefix(old, "/dev/") {
			found = true
		}
	}
	if !found {
		return fmt.Errorf("原裝置不是此池成員，請使用完整裝置路徑")
	}
	disks, e := m.GetAvailableDisks()
	if e != nil {
		return e
	}
	free := false
	for _, d := range disks {
		if d.Path == newDevice && d.Available {
			free = true
		}
	}
	if !free {
		return fmt.Errorf("新裝置必須為未使用且無簽章的資料碟")
	}
	_, e = command.Run("", "zpool", "replace", pool, old, newDevice)
	return e
}
