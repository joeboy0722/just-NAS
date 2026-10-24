package api

import (
	"encoding/json"
	"just-nas/internal/command"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUIPoolMembersStructured(t *testing.T) {
	members, e := uiPoolMembers([]byte(`{"pools":{"home":{"vdevs":{"mirror":{"vdev_type":"mirror","vdevs":{"disk1":{"vdev_type":"disk","path":"/dev/disk/by-id/test","state":"ONLINE"},"disk2":{"vdev_type":"disk","name":"12345","state":"UNAVAIL"}}}}}}}`))
	if e != nil || len(members) != 2 {
		t.Fatalf("members=%v error=%v", members, e)
	}
	for _, m := range members {
		if m.Pool != "home" || m.Path == "" {
			t.Fatal(m)
		}
	}
	if _, e = uiPoolMembers([]byte(`not JSON`)); e == nil {
		t.Fatal("accepted human-readable output")
	}
}
func TestUIStorageOptionsOnlyListsSafeBackupDevices(t *testing.T) {
	old := command.Run
	defer func() { command.Run = old }()
	command.Run = func(_ string, name string, args ...string) ([]byte, error) {
		if name == "lsblk" && strings.Contains(strings.Join(args, ","), "UUID") {
			return []byte(`{"blockdevices":[{"path":"/dev/system","children":[{"path":"/dev/system1","fstype":"ext4","uuid":"root","mountpoints":["/"]},{"path":"/dev/system2","fstype":"ext4","uuid":"sibling"}]},{"path":"/dev/pool","children":[{"path":"/dev/pool1","fstype":"zfs_member"},{"path":"/dev/pool2","fstype":"ext4","uuid":"pool-sibling"}]},{"path":"/dev/backup","fstype":"ext4","uuid":"backup","size":4096,"mountpoints":["/mnt/backups/test"]},{"path":"/dev/ro","fstype":"ext4","uuid":"readonly","ro":true}]}`), nil
		}
		if name == "lsblk" {
			return []byte(`{"blockdevices":[]}`), nil
		}
		if strings.Contains(strings.Join(args, ","), "-j") {
			return []byte(`{"pools":{}}`), nil
		}
		return []byte(""), nil
	}
	r := NewRouter()
	w := httptest.NewRecorder()
	r.handleUIStorage(w, httptest.NewRequest("GET", "/api/ui/storage-options", nil))
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var result struct {
		Devices []uiBackupDevice `json:"backup_devices"`
	}
	if e := json.Unmarshal(w.Body.Bytes(), &result); e != nil {
		t.Fatal(e)
	}
	if len(result.Devices) != 1 || result.Devices[0].UUID != "backup" {
		t.Fatal(result.Devices)
	}
	w = httptest.NewRecorder()
	r.handleUIStorage(w, httptest.NewRequest("POST", "/api/ui/storage-options", nil))
	if w.Code != 405 {
		t.Fatal(w.Code)
	}
}
