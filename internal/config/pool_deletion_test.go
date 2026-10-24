package config

import (
	"just-nas/internal/command"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPoolDeletionRecoveryBeforeAndAfterDestroy(t *testing.T) {
	for _, mode := range []string{"pool-present", "pool-gone", "wrong-guid"} {
		t.Run(mode, func(t *testing.T) {
			oldDir, oldBackup, oldRun := Dir, backupDir, command.Run
			Dir = t.TempDir()
			backupDir = ""
			defer func() { Dir = oldDir; backupDir = oldBackup; command.Run = oldRun }()
			saved := t.TempDir()
			command.Run = func(_ string, name string, args ...string) ([]byte, error) {
				a := strings.Join(args, " ")
				if name == "zpool" {
					switch mode {
					case "pool-present":
						return []byte("tank\t7\nsystem\t1\n"), nil
					case "wrong-guid":
						return []byte("tank\t99\nsystem\t1\n"), nil
					default:
						return []byte("system\t1\n"), nil
					}
				}
				if strings.Contains(a, "name,value") {
					return []byte("system/.just-nas\t1\n"), nil
				}
				return []byte("yes\n" + saved + "\n"), nil
			}
			old := []byte(`[{"name":"old"}]`)
			next := []byte(`[]`)
			record := PoolDeletion{Pool: "tank", GUID: "7", Before: map[string][]byte{"shares.json": old}, After: map[string][]byte{"shares.json": next}}
			AtomicWrite(filepath.Join(Dir, "shares.json"), old, 0600)
			if e := BeginPoolDeletion(record); e != nil {
				t.Fatal(e)
			}
			if e := ApplyPoolState(record.After); e != nil {
				t.Fatal(e)
			}
			e := RecoverPoolDeletion()
			if mode == "wrong-guid" {
				if e == nil || !PoolDeletionPending() {
					t.Fatal("accepted a new same-name pool")
				}
				return
			}
			if e != nil {
				t.Fatal(e)
			}
			got, _ := os.ReadFile(filepath.Join(Dir, "shares.json"))
			want := old
			if mode == "pool-gone" {
				want = next
			}
			if string(got) != string(want) {
				t.Fatal("incorrect undo/redo", string(got))
			}
			if PoolDeletionPending() {
				t.Fatal("journal retained")
			}
		})
	}
}
