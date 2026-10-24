package storage

import (
	"fmt"
	"just-nas/internal/command"
	"strings"
)

func ChildMounts(root string) (map[string]bool, error) {
	b, e := command.Run("", "zfs", "list", "-H", "-o", "name,mounted,mountpoint")
	if e != nil {
		return nil, e
	}
	out := map[string]bool{}
	for _, f := range lines(b) {
		if len(f) != 3 {
			return nil, fmt.Errorf("invalid ZFS mount list")
		}
		if f[1] == "yes" && strings.HasPrefix(f[2], root+"/") {
			out[f[2]] = true
		}
	}
	return out, nil
}
