package vmmanager

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTCGCacheCompatibilityAndMemoryBudget(t *testing.T) {
	m := validMachine()
	if tcgCacheMiB(m) != 64 || accelerationArgument(m, "tcg") != "tcg,tb-size=64" {
		t.Fatal("legacy cache changed")
	}
	baseline := machineMemoryMiB(m)
	for _, size := range []int{64, 128, 256, 512, 1024} {
		m.TCGCacheMiB = size
		if err := validate(m); err != nil {
			t.Fatal(err)
		}
		if machineMemoryMiB(m) != baseline+int64(size-64) {
			t.Fatal("cache not reserved", size)
		}
		raw, _ := json.Marshal(m)
		var loaded Machine
		if json.Unmarshal(raw, &loaded) != nil || loaded.TCGCacheMiB != size {
			t.Fatal("cache not preserved")
		}
	}
	for _, size := range []int{-1, 1, 63, 1025} {
		m.TCGCacheMiB = size
		if validate(m) == nil {
			t.Fatal("invalid cache", size)
		}
	}
	m.TCGCacheMiB = 512
	if accelerationArgument(m, "tcg") != "tcg,tb-size=512" {
		t.Fatal("cache argument ignored")
	}
	// 64 MiB fits this available-memory boundary; 512 MiB must not.
	if err := machineResourceBudget(8192, 6144, 0, m, 0, 4); err == nil {
		t.Fatal("extra cache overcommits available memory")
	}
	m.Acceleration = "kvm"
	if machineMemoryMiB(m) != baseline || accelerationArgument(m, "kvm") != "kvm" {
		t.Fatal("KVM charged a TCG cache")
	}
	if err := machineResourceBudget(8192, 6144, 0, m, 0, 4); err != nil {
		t.Fatal(err)
	}
	var input machineInput
	if err := json.Unmarshal([]byte(`{"tcg_cache_mib":256}`), &input); err != nil || input.machine().TCGCacheMiB != 256 {
		t.Fatal("cache API input lost", err)
	}
}

func sampleForward(port int) Forward { return Forward{"tcp", "192.168.56.101", port, 80} }
func TestUsernetTableAndCommandWhitelist(t *testing.T) {
	text := "Hub -1 (net0):\r\n  Protocol[State] FD Source Address Port Dest. Address Port RecvQ SendQ\r\n TCP[HOST_FORWARD] aac 192.168.56.101 24001 10.0.2.15 80 0 0\r\n UDP[HOST_FORWARD] bbc 192.168.56.101 24002 10.0.2.15 53 0 0\n TCP[ESTABLISHED] 17 10.0.2.15 12345 1.1.1.1 443 0 0"
	rows, err := parseUsernet(text)
	if err != nil || len(rows) != 2 || rows[0] != sampleForward(24001) || rows[1].Protocol != "udp" {
		t.Fatal(rows, err)
	}
	if forwardCommand(true, rows[0]) != "hostfwd_add net0 tcp:192.168.56.101:24001-:80" || forwardCommand(false, rows[0]) != "hostfwd_remove net0 tcp:192.168.56.101:24001" {
		t.Fatal("bad monitor command")
	}
	for _, bad := range []string{"", strings.ReplaceAll(text, "(net0)", "(other)"), strings.ReplaceAll(text, "10.0.2.15", "10.0.2.99"), strings.ReplaceAll(text, "24001", "oops"), text + "\nHub 0 (net1):"} {
		if _, err := parseUsernet(bad); err == nil {
			t.Fatal("unrecognized table accepted")
		}
	}
}

func TestForwardTransactionRollbackAndPersistence(t *testing.T) {
	for _, mode := range []string{"success", "add-failure", "ambiguous-add", "save-failure", "rollback-failure", "drift"} {
		t.Run(mode, func(t *testing.T) {
			untouched := sampleForward(24000)
			old := []Forward{untouched, sampleForward(24001)}
			next := []Forward{untouched, sampleForward(24002)}
			actual := append([]Forward(nil), old...)
			saved := append([]Forward(nil), old...)
			calls := 0
			if mode == "drift" {
				actual = []Forward{sampleForward(24003)}
			}
			read := func() ([]Forward, error) { return append([]Forward(nil), actual...), nil }
			apply := func(add bool, f Forward) error {
				calls++
				if f == untouched {
					t.Fatal("unchanged rule interrupted")
				}
				if add && f.HostPort == 24002 && (mode == "add-failure" || mode == "rollback-failure") {
					return errors.New("port conflict")
				}
				if add && f.HostPort == 24001 && mode == "rollback-failure" {
					return errors.New("restore port taken")
				}
				if add {
					actual = append(actual, f)
				} else {
					actual = forwardDifference(actual, []Forward{f})
				}
				if add && f.HostPort == 24002 && mode == "ambiguous-add" {
					return errors.New("timeout after command applied")
				}
				return nil
			}
			persist := func(v []Forward) error {
				saved = append([]Forward(nil), v...)
				if sameForwards(v, next) && mode == "save-failure" {
					return errors.New("storage failed after possible commit")
				}
				return nil
			}
			err := updateForwardTransaction(old, next, read, apply, persist)
			if mode == "success" {
				if err != nil || !sameForwards(actual, next) || !sameForwards(saved, next) {
					t.Fatal(actual, saved, err)
				}
				return
			}
			var failed *forwardUpdateError
			if !errors.As(err, &failed) {
				t.Fatal("failure reported as success", err)
			}
			if mode == "drift" {
				if calls != 0 || !failed.uncertain {
					t.Fatal("modified unrecognized runtime")
				}
				return
			}
			if mode == "rollback-failure" {
				if !failed.uncertain {
					t.Fatal("rollback failure hidden")
				}
				return
			}
			if failed.uncertain || !sameForwards(actual, old) || !sameForwards(saved, old) {
				t.Fatal("rollback incomplete", actual, saved, err)
			}
		})
	}
}

func TestForwardsEndpointRejectsOmissionAndOtherSettings(t *testing.T) {
	id := strings.Repeat("a", 32)
	b := &Broker{state: State{Machines: map[string]Machine{id: validMachine()}}}
	for _, body := range []string{`{}`, `{"forwards":null}`, `{"forwards":[],"memory_mib":128}`, `{"forwards":[{"protocol":"tcp","host_ip":"0.0.0.0","host_port":2000,"guest_port":80}]}`} {
		w := httptest.NewRecorder()
		q := httptest.NewRequest("PATCH", "/api/vms/"+id+"/forwards", strings.NewReader(body))
		b.ServeHTTP(w, q)
		if w.Code != 409 {
			t.Fatal("unsafe forwards request accepted", body, w.Code)
		}
	}
}
