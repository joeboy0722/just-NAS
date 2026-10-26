package vmmanager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
)

func checkForwardBindings(rows []Forward) error {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return err
	}
	local := map[string]bool{}
	for _, addr := range addrs {
		ip, _, _ := net.ParseCIDR(addr.String())
		if ip != nil {
			local[ip.String()] = true
		}
	}
	for _, f := range rows {
		if !local[f.HostIP] {
			return fmt.Errorf("轉發位址不在 NAS 網卡: %s", f.HostIP)
		}
		address := net.JoinHostPort(f.HostIP, strconv.Itoa(f.HostPort))
		if f.Protocol == "tcp" {
			listener, err := net.Listen("tcp", address)
			if err != nil {
				return fmt.Errorf("連接埠衝突: %w", err)
			}
			listener.Close()
		} else {
			listener, err := net.ListenPacket("udp", address)
			if err != nil {
				return fmt.Errorf("連接埠衝突: %w", err)
			}
			listener.Close()
		}
	}
	return nil
}

func forwardCommand(add bool, f Forward) string {
	prefix := fmt.Sprintf("%s:%s:%d", f.Protocol, f.HostIP, f.HostPort)
	if add {
		return fmt.Sprintf("hostfwd_add net0 %s-:%d", prefix, f.GuestPort)
	}
	return "hostfwd_remove net0 " + prefix
}

// info usernet is queried after each transaction; a successful QMP envelope
// alone does not mean a human-monitor command actually succeeded.
func parseUsernet(text string) ([]Forward, error) {
	rows := []Forward{}
	found := false
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "Hub ") {
			if found || !strings.HasSuffix(strings.TrimSpace(line), "(net0):") {
				return nil, fmt.Errorf("無法辨識 NAT 網路")
			}
			found = true
		}
		if !strings.Contains(line, "[HOST_FORWARD]") {
			continue
		}
		fields := strings.Fields(line)
		if !found || len(fields) != 8 || fields[4] != "10.0.2.15" {
			return nil, fmt.Errorf("無法辨識 NAT 轉發狀態")
		}
		protocol := strings.TrimSuffix(fields[0], "[HOST_FORWARD]")
		if protocol != "TCP" && protocol != "UDP" {
			return nil, fmt.Errorf("未知轉發協定")
		}
		host, e1 := strconv.Atoi(fields[3])
		guest, e2 := strconv.Atoi(fields[5])
		if e1 != nil || e2 != nil || net.ParseIP(fields[2]) == nil {
			return nil, fmt.Errorf("無法辨識 NAT 轉發連接埠")
		}
		rows = append(rows, Forward{Protocol: strings.ToLower(protocol), HostIP: fields[2], HostPort: host, GuestPort: guest})
	}
	if !found {
		return nil, fmt.Errorf("找不到 net0 NAT 狀態")
	}
	return rows, nil
}
func sameForwards(a, b []Forward) bool {
	if len(a) != len(b) {
		return false
	}
	x, y := append([]Forward(nil), a...), append([]Forward(nil), b...)
	key := func(f Forward) string {
		return fmt.Sprintf("%s/%s/%d/%d", f.Protocol, f.HostIP, f.HostPort, f.GuestPort)
	}
	sort.Slice(x, func(i, j int) bool { return key(x[i]) < key(x[j]) })
	sort.Slice(y, func(i, j int) bool { return key(y[i]) < key(y[j]) })
	for i := range x {
		if x[i] != y[i] {
			return false
		}
	}
	return true
}
func forwardDifference(a, b []Forward) []Forward {
	result := []Forward{}
	for _, f := range a {
		found := false
		for _, g := range b {
			if f == g {
				found = true
				break
			}
		}
		if !found {
			result = append(result, f)
		}
	}
	return result
}

type forwardUpdateError struct {
	cause     error
	uncertain bool
}

func (e *forwardUpdateError) Error() string { return e.cause.Error() }
func (e *forwardUpdateError) Unwrap() error { return e.cause }

// Both runtime rules and persisted definitions must agree. On a failure, read
// the actual table again: a transport timeout may have applied a command.
func updateForwardTransaction(old, next []Forward, read func() ([]Forward, error), apply func(bool, Forward) error, persist func([]Forward) error) error {
	current, err := read()
	if err != nil || !sameForwards(current, old) {
		return &forwardUpdateError{fmt.Errorf("NAT 實際狀態與設定無法確認一致，請關機後重試: %v", err), true}
	}
	reconcile := func(from, to []Forward) error {
		for _, f := range forwardDifference(from, to) {
			if err := apply(false, f); err != nil {
				return err
			}
		}
		for _, f := range forwardDifference(to, from) {
			if err := apply(true, f); err != nil {
				return err
			}
		}
		actual, err := read()
		if err != nil {
			return err
		}
		if !sameForwards(actual, to) {
			return fmt.Errorf("QEMU 未套用預期的轉發規則")
		}
		return nil
	}
	persistAttempted := false
	err = reconcile(old, next)
	if err == nil {
		persistAttempted = true
		err = persist(next)
	}
	if err == nil {
		return nil
	}
	current, rollbackErr := read()
	if rollbackErr == nil {
		rollbackErr = reconcile(current, old)
	}
	if rollbackErr == nil && persistAttempted {
		rollbackErr = persist(old)
	}
	if rollbackErr != nil {
		return &forwardUpdateError{errors.Join(err, fmt.Errorf("NAT 回復未完成；請關機後檢查設定: %w", rollbackErr)), true}
	}
	return &forwardUpdateError{fmt.Errorf("NAT 更新失敗，已回復原規則: %w", err), false}
}

func (b *Broker) editForwards(ctx context.Context, id string, rows []Forward) error {
	defer b.lockVM(id)()
	b.startMu.Lock()
	defer b.startMu.Unlock()
	s := b.snapshot()
	m, ok := s.Machines[id]
	if !ok {
		return fmt.Errorf("找不到 VM")
	}
	if err := validateForwards(rows); err != nil {
		return err
	}
	if err := mounted(ctx, s); err != nil {
		return err
	}
	status, reason := processStatus(s, m)
	live := status == "running" || status == "paused"
	if !live && (holdsResources(s, m, status, reason) || status != "stopped" && status != "failed") {
		return fmt.Errorf("VM 狀態不明或正在關機，拒絕修改轉發")
	}
	// Reject reserved ports from other registered VMs as well as OS listeners.
	for otherID, other := range s.Machines {
		if otherID == id {
			continue
		}
		for _, f := range rows {
			for _, g := range other.Forwards {
				if f.Protocol == g.Protocol && f.HostIP == g.HostIP && f.HostPort == g.HostPort {
					return fmt.Errorf("連接埠已由其他 VM 設定使用")
				}
			}
		}
	}
	newlyBound := rows
	if live {
		newlyBound = nil
		for _, f := range rows {
			held := false
			for _, g := range m.Forwards {
				if f.Protocol == g.Protocol && f.HostIP == g.HostIP && f.HostPort == g.HostPort {
					held = true
					break
				}
			}
			if !held {
				newlyBound = append(newlyBound, f)
			}
		}
	}
	if err := checkForwardBindings(newlyBound); err != nil {
		return err
	}
	persist := func(value []Forward) error {
		b.mu.Lock()
		defer b.mu.Unlock()
		previous := b.state.Machines[id]
		updated := previous
		updated.Forwards = append([]Forward(nil), value...)
		updated.ForwardsError = ""
		b.state.Machines[id] = updated
		if err := b.saveLocked(); err != nil {
			b.state.Machines[id] = previous
			return err
		}
		return nil
	}
	if !live {
		return persist(rows)
	}
	socket := machineRoot(s, id) + "/run/qmp.sock"
	monitor := func(command string) (string, error) {
		raw, err := qmp(socket, "human-monitor-command", map[string]string{"command-line": command})
		if err != nil {
			return "", err
		}
		var text string
		err = json.Unmarshal(raw, &text)
		return text, err
	}
	read := func() ([]Forward, error) {
		text, err := monitor("info usernet")
		if err != nil {
			return nil, err
		}
		return parseUsernet(text)
	}
	apply := func(add bool, f Forward) error {
		text, err := monitor(forwardCommand(add, f))
		if err != nil {
			return err
		}
		if add && strings.TrimSpace(text) != "" || !add && !strings.HasSuffix(strings.TrimSpace(text), " removed") {
			return fmt.Errorf("QEMU NAT: %s", strings.TrimSpace(text))
		}
		return nil
	}
	err := updateForwardTransaction(m.Forwards, rows, read, apply, persist)
	var failure *forwardUpdateError
	if errors.As(err, &failure) && failure.uncertain {
		b.mu.Lock()
		updated := b.state.Machines[id]
		updated.ForwardsError = err.Error()
		b.state.Machines[id] = updated
		saveErr := b.saveLocked()
		b.mu.Unlock()
		return errors.Join(err, saveErr)
	}
	return err
}
