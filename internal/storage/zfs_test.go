package storage

import (
	"errors"
	"just-nas/internal/command"
	"strings"
	"testing"
)

func stub(t *testing.T, r command.Runner) {
	old := command.Run
	command.Run = r
	t.Cleanup(func() { command.Run = old })
}
func TestMissingZFSIsNotEmptyPoolList(t *testing.T) {
	stub(t, func(string, string, ...string) ([]byte, error) { return nil, errors.New("zpool missing") })
	if _, err := NewStorageManager().GetPools(); err == nil {
		t.Fatal("missing zpool was hidden")
	}
}
func TestPoolParsing(t *testing.T) {
	stub(t, func(string, string, ...string) ([]byte, error) {
		return []byte("tank\t100\t20\t80\tONLINE\t20\t0\n"), nil
	})
	p, e := NewStorageManager().GetPools()
	if e != nil || len(p) != 1 || p[0].Free != 80 {
		t.Fatalf("%+v %v", p, e)
	}
}
func TestDatasetAndSnapshotValidation(t *testing.T) {
	for _, s := range []string{"tank", "-flag/a", "tank/../etc", "tank/.just-nas", "tank/a\nb"} {
		if ValidDataset(s) {
			t.Errorf("accepted %q", s)
		}
	}
	if !ValidDataset("tank/documents") || !validSnapshot("tank/documents@before-update") {
		t.Fatal("valid names rejected")
	}
}
func TestRejectInvalidRAIDBeforeCommands(t *testing.T) {
	stub(t, func(string, string, ...string) ([]byte, error) { t.Fatal("unexpected command"); return nil, nil })
	for _, raid := range []string{"unknown", "mirror", "raidz1", "raidz2"} {
		if NewStorageManager().CreatePool("tank", raid, []string{"/dev/x"}) == nil {
			t.Error("accepted invalid RAID", raid)
		}
	}
}
func TestDiskExclusions(t *testing.T) {
	stub(t, func(_ string, name string, args ...string) ([]byte, error) {
		if name == "zpool" {
			return []byte(" /dev/sdd ONLINE"), nil
		}
		return []byte(`{"blockdevices":[{"name":"/dev/sda","path":"/dev/sda","size":100,"type":"disk","ro":false,"rm":false,"children":[{"type":"part"}]},{"name":"/dev/sdb","path":"/dev/sdb","size":100,"type":"disk","ro":false,"rm":false,"fstype":"ext4"},{"name":"/dev/sdc","path":"/dev/sdc","size":100,"type":"disk","ro":false,"rm":false,"mountpoints":[null]},{"name":"/dev/sdd","path":"/dev/sdd","size":100,"type":"disk","ro":false,"rm":false}]}`), nil
	})
	disks, e := NewStorageManager().GetAvailableDisks()
	if e != nil {
		t.Fatal(e)
	}
	for _, d := range disks {
		if d.Available != (d.Name == "sdc") {
			t.Errorf("bad availability %+v", d)
		}
	}
}
func TestRollbackNeverDestroysNewerSnapshots(t *testing.T) {
	stub(t, func(_ string, name string, args ...string) ([]byte, error) {
		if strings.Contains(strings.Join(args, " "), "-r") {
			t.Fatal("recursive rollback enabled")
		}
		return nil, errors.New("newer snapshots exist")
	})
	if NewStorageManager().RollbackSnapshot("tank/files@old") == nil {
		t.Fatal("error suppressed")
	}
}
