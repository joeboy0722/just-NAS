package config

import (
	"errors"
	"just-nas/internal/command"
	"strings"
	"testing"
)

func TestObservedBootDistinguishesFirstSetupAndFailures(t *testing.T) {
	for _, tc := range []struct{ name, failedCommand, pools, wantPool, wantSettings string }{{"fresh", "", "", "skip", "skip"}, {"healthy", "", "tank ONLINE\n", "ok", "skip"}, {"degraded", "", "tank DEGRADED\n", "warn", "skip"}, {"module", "modprobe", "", "fail", "skip"}, {"mount", "zfs mount -a", "tank ONLINE\n", "fail", "skip"}, {"import", "zpool import -a", "tank ONLINE\n", "fail", "skip"}, {"settings", "zfs get -H", "tank ONLINE\n", "ok", "fail"}} {
		t.Run(tc.name, func(t *testing.T) {
			old := command.Run
			oldBackup, oldError := backupDir, restoreError
			defer func() { command.Run = old; backupDir = oldBackup; restoreError = oldError }()
			command.Run = func(_ string, name string, args ...string) ([]byte, error) {
				cmd := name + " " + strings.Join(args, " ")
				if tc.failedCommand != "" && strings.HasPrefix(cmd, tc.failedCommand) {
					return nil, errors.New("injected failure")
				}
				if strings.HasPrefix(cmd, "zpool list") {
					return []byte(tc.pools), nil
				}
				return nil, nil
			}
			states := map[string]string{}
			NewPersistenceManager().restoreConfigOnBootObserved(func(e BootEvent) { states[e.Phase] = e.State })
			if states["pools"] != tc.wantPool || states["settings"] != tc.wantSettings {
				t.Fatalf("wrong states: %+v", states)
			}
		})
	}
}
