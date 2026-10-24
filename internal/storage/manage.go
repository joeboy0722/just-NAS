package storage

import (
	"fmt"
	"just-nas/internal/command"
	"regexp"
	"strings"
)

var quotaRE = regexp.MustCompile(`^[1-9][0-9]*(?:[KMGT]i?B?|[kmgt])?$`)

func (m *StorageManager) UpdateDataset(name, compression, quota string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !ValidDataset(name) {
		return fmt.Errorf("資料集無效")
	}
	if quota != "" {
		if _, e := quotaBytes(quota); e != nil {
			return e
		}
	}
	args := []string{"set"}
	if compression != "" {
		if compression != "lz4" && compression != "zstd" && compression != "off" {
			return fmt.Errorf("壓縮格式無效")
		}
		args = append(args, "compression="+compression)
	}

	if len(args) == 1 {
		if quota != "" {
			return SetQuota(name, quota)
		}
		return fmt.Errorf("沒有要修改的屬性")
	}
	args = append(args, name)
	_, e := command.Run("", "zfs", args...)
	if e == nil && quota != "" {
		return SetQuota(name, quota)
	}
	return e
}
func (m *StorageManager) PoolStatus(name string) (string, error) {
	if !poolRE.MatchString(name) {
		return "", fmt.Errorf("池名稱無效")
	}
	b, e := command.Run("", "zpool", "status", "-gLP", name)
	return string(b), e
}
func (m *StorageManager) DatasetProperties(name string) (map[string]string, error) {
	if !ValidDataset(name) {
		return nil, fmt.Errorf("資料集無效")
	}
	b, e := command.Run("", "zfs", "get", "-Hp", "-o", "property,value", "used,available,referenced,usedbysnapshots,refquota,compression,org.just-nas:quota,org.just-nas:over-since,org.just-nas:quota-locked", name)
	if e != nil {
		return nil, e
	}
	r := map[string]string{}
	for _, f := range lines(b) {
		if len(f) == 2 {
			r[strings.TrimSpace(f[0])] = f[1]
		}
	}
	return r, nil
}
