package shares

import (
	"fmt"
	"just-nas/internal/command"
	"just-nas/internal/config"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderPermissions(t *testing.T) {
	smb, nfs := render([]ShareConfig{{Name: "Private", Protocol: "smb", Path: "/mnt/tank/files", Users: []string{"family"}, ReadOnly: true}, {Name: "Linux", Protocol: "nfs", Path: "/mnt/tank/files", AllowedHosts: "10.0.0.0/24 192.168.1.2"}})
	for _, s := range []string{"read only = yes", "guest ok = no", "valid users = family", "server min protocol = SMB2"} {
		if !strings.Contains(smb, s) {
			t.Errorf("missing %s", s)
		}
	}
	if strings.Contains(nfs, "no_root_squash") || !strings.Contains(nfs, "10.0.0.0/24(rw,sync,no_subtree_check,root_squash") {
		t.Fatal(nfs)
	}
}

func TestApplyRestoresServiceFilesAfterReloadFailure(t *testing.T) {
	oldDir, oldRun := config.Dir, command.Run
	config.Dir = t.TempDir()
	defer func() { config.Dir = oldDir; command.Run = oldRun }()
	m := &ShareManager{SmbConfPath: filepath.Join(config.Dir, "smb.conf"), NfsConfPath: filepath.Join(config.Dir, "exports")}
	os.WriteFile(m.SmbConfPath, []byte("old smb"), 0600)
	os.WriteFile(m.NfsConfPath, []byte("old nfs"), 0600)
	failed := false
	command.Run = func(_ string, name string, args ...string) ([]byte, error) {
		if name == "rc-service" && len(args) > 1 && args[0] == "samba" && args[1] == "reload" && !failed {
			failed = true
			return nil, fmt.Errorf("injected reload failure")
		}
		return nil, nil
	}
	if e := m.apply(nil); e == nil {
		t.Fatal("reload failure reported success")
	}
	smb, _ := os.ReadFile(m.SmbConfPath)
	nfs, _ := os.ReadFile(m.NfsConfPath)
	if string(smb) != "old smb" || string(nfs) != "old nfs" {
		t.Fatal("service configuration not restored")
	}
}
