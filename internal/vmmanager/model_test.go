package vmmanager

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func validMachine() Machine {
	return Machine{Name: "家庭測試", OS: "linux", CPUs: 2, MemoryMiB: 4096, DiskGiB: 32, Firmware: "uefi", Acceleration: "auto", Forwards: []Forward{{Protocol: "tcp", HostIP: "192.168.56.101", HostPort: 18090, GuestPort: 80}}}
}
func TestInputDoesNotGrantHostPathsOrInvalidForwards(t *testing.T) {
	m := validMachine()
	if e := validate(m); e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*Machine)
	}{
		{"public bind", func(m *Machine) { m.Forwards[0].HostIP = "8.8.8.8" }}, {"all interfaces", func(m *Machine) { m.Forwards[0].HostIP = "0.0.0.0" }}, {"privileged port", func(m *Machine) { m.Forwards[0].HostPort = 445 }}, {"invalid protocol", func(m *Machine) { m.Forwards[0].Protocol = "exec" }}, {"duplicate forward", func(m *Machine) { m.Forwards = append(m.Forwards, m.Forwards[0]) }}, {"ISO path", func(m *Machine) { m.ISO = "../../etc/shadow" }}, {"accel injection", func(m *Machine) { m.Acceleration = "tcg,thread=single" }}, {"negative memory", func(m *Machine) { m.MemoryMiB = -1 }}, {"too many cores", func(m *Machine) { m.CPUs = 65 }}, {"invalid firmware", func(m *Machine) { m.Firmware = "/etc/passwd" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := validMachine()
			tc.mutate(&m)
			if validate(m) == nil {
				t.Fatal("unsafe input accepted")
			}
		})
	}
}
func TestResourceBudgetIncludesQEMUAndNASReserve(t *testing.T) {
	if resourceBudget(8192, 6144, 0, 4096, 0, 2, 4) != nil {
		t.Fatal("valid 4 GiB guest rejected")
	}
	for _, x := range []struct {
		available, used      int64
		memory, usedCPU, cpu int
	}{{6144, 5120, 4096, 2, 2}, {3000, 0, 4096, 0, 2}, {6144, 0, 1024, 3, 2}, {6144, 0, 8192, 0, 1}} {
		if resourceBudget(8192, x.available, x.used, x.memory, x.usedCPU, x.cpu, 4) == nil {
			t.Fatal("overcommit accepted", x)
		}
	}
	if guestOverhead(512) < 768 || guestOverhead(4096) < 1024 {
		t.Fatal("missing QEMU overhead")
	}
}
func TestStateWritesAreCompleteAndVersioned(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.json")
	s := State{Version: 1, Machines: map[string]Machine{"test": validMachine()}}
	if e := atomicJSON(p, s); e != nil {
		t.Fatal(e)
	}
	raw, _ := os.ReadFile(p)
	if !json.Valid(raw) {
		t.Fatal("partial JSON")
	}
	var restored State
	if e := readJSON(p, &restored); e != nil || restored.Version != 1 || restored.Machines["test"].Name != s.Machines["test"].Name {
		t.Fatal(e)
	}
	os.WriteFile(p, append(raw, []byte("{}")...), 0600)
	if readJSON(p, &restored) == nil {
		t.Fatal("trailing state accepted")
	}
}
