package api

import (
	"encoding/json"
	"just-nas/internal/command"
	"just-nas/internal/storage"
	"net/http"
	"strings"
)

type uiBlock struct {
	Path     string    `json:"path"`
	UUID     string    `json:"uuid"`
	Label    string    `json:"label"`
	Size     uint64    `json:"size"`
	FSType   string    `json:"fstype"`
	RO       bool      `json:"ro"`
	Mounts   []*string `json:"mountpoints"`
	Children []uiBlock `json:"children"`
}
type uiBackupDevice struct {
	Path  string `json:"path"`
	UUID  string `json:"uuid"`
	Label string `json:"label"`
	Size  uint64 `json:"size"`
	Mount string `json:"mount"`
}
type uiPoolMember struct {
	Pool  string `json:"pool"`
	Path  string `json:"path"`
	State string `json:"state"`
}

// uiPoolMembers consumes OpenZFS structured output, never human-readable status.
func uiPoolMembers(data []byte) ([]uiPoolMember, error) {
	var root struct {
		Pools map[string]json.RawMessage `json:"pools"`
	}
	if e := json.Unmarshal(data, &root); e != nil {
		return nil, e
	}
	out := []uiPoolMember{}
	var visit func(any, string)
	visit = func(value any, pool string) {
		switch x := value.(type) {
		case map[string]any:
			path, _ := x["path"].(string)
			kind, _ := x["vdev_type"].(string)
			if path == "" && kind == "disk" {
				path, _ = x["name"].(string)
			}
			if path != "" && (kind == "disk" || strings.HasPrefix(path, "/dev/")) {
				state, _ := x["state"].(string)
				if state == "" {
					state, _ = x["health"].(string)
				}
				out = append(out, uiPoolMember{pool, path, state})
				return
			}
			for _, v := range x {
				visit(v, pool)
			}
		case []any:
			for _, v := range x {
				visit(v, pool)
			}
		}
	}
	for pool, raw := range root.Pools {
		var x any
		if e := json.Unmarshal(raw, &x); e != nil {
			return nil, e
		}
		visit(x, pool)
	}
	return out, nil
}
func (r *Router) handleUIStorage(w http.ResponseWriter, q *http.Request) {
	if q.Method != "GET" {
		w.WriteHeader(405)
		return
	}
	b, e := command.Run("", "lsblk", "--json", "--bytes", "--paths", "-o", "PATH,UUID,LABEL,SIZE,FSTYPE,RO,MOUNTPOINTS")
	if e != nil {
		jsonResponse(w, 503, map[string]string{"error": e.Error()})
		return
	}
	var blocks struct {
		Devices []uiBlock `json:"blockdevices"`
	}
	if e = json.Unmarshal(b, &blocks); e != nil {
		jsonResponse(w, 503, map[string]string{"error": "裝置資訊無法解析"})
		return
	}
	devices := []uiBackupDevice{}
	var scan func(uiBlock, bool)
	scan = func(d uiBlock, protected bool) {
		for _, p := range d.Mounts {
			if p != nil && (*p == "/" || *p == "/boot" || *p == "/boot/efi") {
				protected = true
			}
		}
		// A system partition protects its whole parent device and siblings.
		var systemChild func(uiBlock) bool
		systemChild = func(x uiBlock) bool {
			if x.FSType == "zfs_member" {
				return true
			}
			for _, p := range x.Mounts {
				if p != nil && (*p == "/" || *p == "/boot" || *p == "/boot/efi") {
					return true
				}
			}
			for _, c := range x.Children {
				if systemChild(c) {
					return true
				}
			}
			return false
		}
		protected = protected || systemChild(d)
		if !protected && !d.RO && d.FSType == "ext4" && d.UUID != "" && strings.HasPrefix(d.Path, "/dev/") {
			mount := ""
			for _, p := range d.Mounts {
				if p != nil && *p != "" {
					mount = *p
					break
				}
			}
			devices = append(devices, uiBackupDevice{d.Path, d.UUID, d.Label, d.Size, mount})
		}
		for _, c := range d.Children {
			scan(c, protected)
		}
	}
	for _, d := range blocks.Devices {
		scan(d, false)
	}
	disks, e := storage.NewStorageManager().GetAvailableDisks()
	if e != nil {
		jsonResponse(w, 503, map[string]string{"error": e.Error()})
		return
	}
	members := []uiPoolMember{}
	warning := ""
	b, e = command.Run("", "zpool", "status", "-j", "-P")
	if e == nil {
		members, e = uiPoolMembers(b)
	}
	if e != nil {
		warning = "目前無法取得結構化成員資訊，換碟選擇器暫不可用"
	}
	jsonResponse(w, 200, map[string]any{"backup_devices": devices, "available_disks": disks, "pool_members": members, "member_warning": warning})
}
