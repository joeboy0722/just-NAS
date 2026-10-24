package platform

import (
	"testing"
	"time"
)

func TestRollbackRestoresConfirmedStaticAcrossReboot(t *testing.T) {
	old := NetworkSettings{Interface: "eth0", DHCP: false, Address: "192.168.1.10/24", Gateway: "192.168.1.1", DNS: []string{"192.168.1.1"}}
	candidate := NetworkSettings{Interface: "eth0", DHCP: true, Pending: true, PreviousSettings: &old, Previous: "old interfaces", Deadline: time.Now()}
	restored := rollbackSettings(candidate)
	if restored.DHCP || restored.Address != old.Address || restored.Interface != "eth0" || restored.Pending || restored.PreviousSettings != nil || restored.Previous != "" {
		t.Fatalf("lost confirmed static settings: %+v", restored)
	}
}
