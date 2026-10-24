package shares

import (
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
