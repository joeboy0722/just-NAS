package vmmanager

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

func defaultMachineType(ctx context.Context) (string, error) {
	raw, e := command(ctx, "qemu-system-x86_64", "-machine", "help")
	if e != nil {
		return "", e
	}
	for _, l := range strings.Split(string(raw), "\n") {
		f := strings.Fields(l)
		if len(f) > 0 && strings.HasPrefix(f[0], "pc-q35-") {
			return f[0], nil
		}
	}
	return "", fmt.Errorf("QEMU 未提供固定版本 q35 機型")
}
func processStatus(s State, m Machine) (string, string) {
	p := machineRoot(s, m.ID) + "/run/qmp.sock"
	raw, e := qmp(p, "query-name", nil)
	if e != nil {
		if processAlive(s, m) {
			return "unknown", "程序仍存在，但 QMP 無法連線"
		}
		if m.Error != "" {
			return "failed", m.Error
		}
		return "stopped", ""
	}
	var name struct{ Name string }
	if json.Unmarshal(raw, &name) != nil || name.Name != "just-nas-vm-"+m.ID {
		return "unknown", "程序身分不符"
	}
	raw, e = qmp(p, "query-status", nil)
	if e != nil {
		return "unknown", e.Error()
	}
	var status struct{ Status string }
	if json.Unmarshal(raw, &status) != nil {
		return "unknown", "QMP 回應損壞"
	}
	switch status.Status {
	case "running":
		return "running", ""
	case "paused", "prelaunch":
		return "paused", ""
	case "shutdown":
		return "stopping", ""
	case "guest-panicked", "internal-error", "io-error":
		return "failed", status.Status
	default:
		return "unknown", status.Status
	}
}
func statusHoldsResources(status, reason string) bool {
	return status == "running" || status == "paused" || status == "stopping" || status == "unknown" || status == "failed" && (reason == "io-error" || reason == "internal-error" || reason == "guest-panicked")
}
func holdsResources(s State, m Machine, status, reason string) bool {
	return processAlive(s, m) || statusHoldsResources(status, reason)
}
func (b *Broker) view(ctx context.Context, s State, m Machine) View {
	v := View{Machine: m, VirtualBytes: int64(m.DiskGiB) << 30}
	v.ProcessPresent = processAlive(s, m)
	if e := mounted(ctx, s); e != nil {
		v.Status = "unavailable"
		v.Reason = e.Error()
		return v
	}
	v.Status, v.Reason = processStatus(s, m)
	v.ProcessPresent = v.ProcessPresent || statusHoldsResources(v.Status, v.Reason)
	if raw, e := qmp(machineRoot(s, m.ID)+"/run/qmp.sock", "query-kvm", nil); e == nil {
		var k struct{ Enabled bool }
		json.Unmarshal(raw, &k)
		v.ActualAcceleration = "tcg"
		if k.Enabled {
			v.ActualAcceleration = "kvm"
		}
	}
	v.UsedBytes = allocatedBytes(machineRoot(s, m.ID) + "/disk.qcow2")
	return v
}
func (b *Broker) start(ctx context.Context, id string) (err error) {
	defer func() {
		if err != nil {
			b.mu.Lock()
			defer b.mu.Unlock()
			if m, ok := b.state.Machines[id]; ok {
				m.Error = err.Error()
				b.state.Machines[id] = m
				_ = b.saveLocked()
			}
		}
	}()
	defer b.lockVM(id)()
	b.startMu.Lock()
	defer b.startMu.Unlock()
	s := b.snapshot()
	m, ok := s.Machines[id]
	if !ok {
		return fmt.Errorf("找不到 VM")
	}
	if e := mounted(ctx, s); e != nil {
		return e
	}
	status, reason := processStatus(s, m)
	if status == "running" || status == "paused" {
		return nil
	}
	if holdsResources(s, m, status, reason) {
		return fmt.Errorf("無法確認既有程序已停止，拒絕重複啟動")
	}
	available, e := freeBytes(ctx, s)
	if e != nil {
		return e
	}
	if available < 512<<20 {
		return fmt.Errorf("池可用空間少於 512 MiB，拒絕啟動")
	}
	if e = b.admit(ctx, s, m); e != nil {
		return e
	}
	accel, reason := chooseAcceleration(ctx, m.Acceleration)
	if accel == "" {
		return fmt.Errorf("%s", reason)
	}
	root := machineRoot(s, id)
	if e = verifyMachineDataset(ctx, s, m); e != nil {
		return e
	}
	if e = os.MkdirAll(root+"/run", 0700); e != nil {
		return e
	}
	if e = os.Chown(root+"/run", int(m.UID), int(m.UID)); e != nil {
		return e
	}
	for _, n := range []string{"qmp.sock", "vnc.sock", "serial.sock"} {
		if e = os.Remove(root + "/run/" + n); e != nil && !os.IsNotExist(e) {
			return e
		}
	}
	for _, f := range m.Forwards {
		addrs, _ := net.InterfaceAddrs()
		local := false
		for _, a := range addrs {
			ip, _, _ := net.ParseCIDR(a.String())
			if ip != nil && ip.String() == f.HostIP {
				local = true
			}
		}
		if !local {
			return fmt.Errorf("轉發位址不在 NAS 網卡")
		}
		address := net.JoinHostPort(f.HostIP, strconv.Itoa(f.HostPort))
		if f.Protocol == "tcp" {
			l, e := net.Listen("tcp", address)
			if e != nil {
				return fmt.Errorf("連接埠衝突: %w", e)
			}
			l.Close()
		} else {
			l, e := net.ListenPacket("udp", address)
			if e != nil {
				return fmt.Errorf("連接埠衝突: %w", e)
			}
			l.Close()
		}
	}
	args := []string{"-name", "just-nas-vm-" + id, "-uuid", uuid(id), "-machine", m.MachineType, "-accel", accel, "-cpu", "qemu64", "-smp", fmt.Sprint(m.CPUs), "-m", fmt.Sprint(m.MemoryMiB), "-display", "none", "-vga", "std", "-nodefaults", "-action", "shutdown=pause", "-rtc", "base=utc", "-boot", "order=c,menu=on", "-sandbox", "on,obsolete=deny,elevateprivileges=deny,spawn=deny,resourcecontrol=deny", "-qmp", "unix:/vm/run/qmp.sock,server=on,wait=off", "-vnc", "unix:/vm/run/vnc.sock", "-serial", "unix:/vm/run/serial.sock,server=on,wait=off", "-device", "ich9-ahci,id=ahci", "-drive", "if=none,id=osdisk,file=/vm/disk.qcow2,format=qcow2,cache=none,discard=unmap", "-device", "ide-hd,drive=osdisk,bus=ahci.0", "-device", "qemu-xhci,id=usb", "-device", "usb-tablet,bus=usb.0"}
	if accel == "tcg" {
		for i, a := range args {
			if a == "-accel" {
				args[i+1] = "tcg,tb-size=64"
				break
			}
		}
	}
	// Linux includes the virtio 2D DRM driver. Windows keeps standard VGA so
	// installation needs no separately downloaded display driver.
	if m.OS == "linux" {
		for i, a := range args {
			if a == "-vga" {
				args[i+1] = "virtio"
				break
			}
		}
	}
	if m.Firmware == "uefi" {
		args = append(args, "-drive", "if=pflash,format=raw,readonly=on,file=/usr/share/OVMF/OVMF_CODE.fd", "-drive", "if=pflash,format=raw,file=/vm/uefi-vars.fd")
	} else {
		args = append(args, "-bios", "/usr/share/seabios/bios.bin")
	}
	if m.ISO != "" {
		if _, ok = s.ISOs[m.ISO]; !ok {
			return fmt.Errorf("ISO 已不存在")
		}
		args = append(args, "-drive", "if=none,id=cd,file=/install.iso,format=raw,readonly=on,media=cdrom", "-device", "ide-cd,drive=cd,bus=ahci.1")
		if m.BootISOOnce {
			for i, a := range args {
				if a == "order=c,menu=on" {
					args[i] = "order=c,once=d,menu=on"
				}
			}
		}
	}
	network := "user,id=net0"
	for _, f := range m.Forwards {
		network += fmt.Sprintf(",hostfwd=%s:%s:%d-:%d", f.Protocol, f.HostIP, f.HostPort, f.GuestPort)
	}
	args = append(args, "-netdev", network, "-device", "e1000,netdev=net0,mac=52:54:00:"+id[:2]+":"+id[2:4]+":"+id[4:6])
	if e = launchGuest(ctx, s, m, args); e != nil {
		return e
	}
	for n := 0; n < 100; n++ {
		status, _ = processStatus(s, m)
		if status == "running" || status == "paused" {
			b.mu.Lock()
			m.Error = ""
			m.ActualAcceleration = accel
			m.WantedRunning = true
			m.BootISOOnce = false
			b.state.Machines[id] = m
			e = b.saveLocked()
			b.mu.Unlock()
			if e != nil {
				_ = signalGuest(s, m)
				return e
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	_ = signalGuest(s, m)
	raw, _ := readMachineLog(root, 4096)
	return fmt.Errorf("QEMU 啟動失敗: %s", raw)
}

func readMachineLog(path string, limit int64) ([]byte, error) {
	root, e := os.OpenRoot(path)
	if e != nil {
		return nil, e
	}
	defer root.Close()
	f, e := openRegular(root, "qemu.log", os.O_RDONLY, 0)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	st, e := f.Stat()
	if e != nil {
		return nil, e
	}
	if st.Size() > limit {
		if _, e = f.Seek(-limit, io.SeekEnd); e != nil {
			return nil, e
		}
	}
	return io.ReadAll(io.LimitReader(f, limit))
}
func verifyMachineDataset(ctx context.Context, s State, m Machine) error {
	if m.Dataset != s.Dataset+"/machines/"+m.ID {
		return fmt.Errorf("VM 資料集不屬於指定儲存")
	}
	raw, e := command(ctx, "zfs", "get", "-H", "-o", "value", "mounted,mountpoint,org.just-nas:vm", m.Dataset)
	if e != nil {
		return e
	}
	if strings.TrimSpace(string(raw)) != "yes\n"+machineRoot(s, m.ID)+"\n"+m.ID {
		return fmt.Errorf("VM 資料集識別失效")
	}
	return nil
}
func (b *Broker) admit(ctx context.Context, s State, m Machine) error {
	total, available := memoryInfo()
	reserve := total / 4
	if reserve < 2048 {
		reserve = 2048
	}
	used := int64(0)
	cpus := 0
	for _, other := range s.Machines {
		if other.ID == m.ID {
			continue
		}
		state, reason := processStatus(s, other)
		if holdsResources(s, other, state, reason) {
			used += int64(other.MemoryMiB) + guestOverhead(other.MemoryMiB)
			cpus += other.CPUs
		}
	}
	return resourceBudget(total, available, used, m.MemoryMiB, cpus, m.CPUs, hostCPUs())
}
func resourceBudget(total, available, used int64, memory, usedCPUs, cpus, totalCPUs int) error {
	reserve := total / 4
	if reserve < 2048 {
		reserve = 2048
	}
	required := int64(memory) + guestOverhead(memory)
	if required+used > total-reserve || required > available-reserve/2 {
		return fmt.Errorf("記憶體不足：需計入客體及 QEMU 額外空間，並保留 NAS 資源")
	}
	if cpus+usedCPUs > totalCPUs {
		return fmt.Errorf("執行中 VM 的虛擬 CPU 總數超過 NAS 可用核心")
	}
	return nil
}
func guestOverhead(memory int) int64 {
	v := int64(memory) / 4
	if v < 768 {
		v = 768
	}
	return v
}
func (b *Broker) action(ctx context.Context, id, action, confirm string) error {
	if action == "restart" {
		if e := b.action(ctx, id, "shutdown", ""); e != nil {
			return e
		}
		return b.start(ctx, id)
	}
	if action == "start" {
		return b.start(ctx, id)
	}
	defer b.lockVM(id)()
	s := b.snapshot()
	m, ok := s.Machines[id]
	if !ok {
		return fmt.Errorf("找不到 VM")
	}
	if e := mounted(ctx, s); e != nil && action != "force-stop" {
		return e
	}
	sock := machineRoot(s, id) + "/run/qmp.sock"
	switch action {
	case "pause":
		_, e := qmp(sock, "stop", nil)
		return e
	case "resume":
		_, e := qmp(sock, "cont", nil)
		if e != nil {
			return e
		}
		b.mu.Lock()
		defer b.mu.Unlock()
		current := b.state.Machines[id]
		current.Error = ""
		b.state.Machines[id] = current
		return b.saveLocked()
	case "shutdown":
		status, reason := processStatus(s, m)
		if !holdsResources(s, m, status, reason) {
			return nil
		}
		if status == "paused" || status == "failed" {
			if _, e := qmp(sock, "cont", nil); e != nil {
				return e
			}
		}
		if status == "stopping" {
			return b.finishPoweroff(s, m)
		}
		if _, e := qmp(sock, "system_powerdown", nil); e != nil {
			return e
		}
		deadline := time.Now().Add(60 * time.Second)
		for time.Now().Before(deadline) {
			if status, _ := processStatus(s, m); status == "stopping" {
				return b.finishPoweroff(s, m)
			}
			if !processAlive(s, m) {
				return nil
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Second):
			}
		}
		return fmt.Errorf("正常關機等待 60 秒仍未停止；可繼續等待或明確確認強制關機")
	case "force-stop":
		if confirm != m.Name {
			return fmt.Errorf("強制關機須輸入 VM 名稱確認")
		}
		status, reason := processStatus(s, m)
		if !holdsResources(s, m, status, reason) {
			return nil
		}
		if !processAlive(s, m) {
			return fmt.Errorf("程序識別紀錄無法確認，拒絕不安全的強制關機")
		}
		b.mu.Lock()
		m.WantedRunning = false
		m.Unclean = true
		b.state.Machines[id] = m
		saveError := b.saveLocked()
		b.mu.Unlock()
		if saveError != nil {
			return saveError
		}
		if e := signalGuest(s, m); e != nil {
			return e
		}
		b.mu.Lock()
		m.Unclean = true
		b.state.Machines[id] = m
		e := b.saveLocked()
		b.mu.Unlock()
		return e
	default:
		return fmt.Errorf("不支援的 VM 操作")
	}
}
func (b *Broker) remove(ctx context.Context, id, confirm string, purge bool) error {
	defer b.lockVM(id)()
	s := b.snapshot()
	m, active := s.Machines[id]
	if !active {
		var ok bool
		m, ok = s.Retained[id]
		if !ok {
			return fmt.Errorf("找不到 VM")
		}
	}
	status, reason := processStatus(s, m)
	if holdsResources(s, m, status, reason) || status != "stopped" && status != "failed" {
		return fmt.Errorf("請先關閉 VM")
	}
	if e := mounted(ctx, s); e != nil {
		return e
	}
	if purge && confirm != m.Name {
		return fmt.Errorf("永久刪除須輸入 VM 名稱")
	}
	if !purge && m.PurgePending {
		return fmt.Errorf("永久刪除尚未完成，請重試刪除")
	}
	// Persist the deletion intent before destroying any dataset. After a crash or
	// failed persistence, no live registration can silently point at a deleted disk.
	b.mu.Lock()
	delete(b.state.Machines, id)
	m.PurgePending = purge
	b.state.Retained[id] = m
	if e := b.saveLocked(); e != nil {
		if active {
			b.state.Machines[id] = s.Machines[id]
			delete(b.state.Retained, id)
		} else {
			b.state.Retained[id] = s.Retained[id]
		}
		b.mu.Unlock()
		return e
	}
	b.mu.Unlock()
	if !purge {
		return nil
	}
	_, e := command(ctx, "zfs", "list", "-H", m.Dataset)
	if e == nil {
		if e = verifyMachineDataset(ctx, s, m); e != nil {
			return e
		}
		if _, e = command(ctx, "zfs", "destroy", "-r", m.Dataset); e != nil {
			return e
		}
	} else {
		// Only a previously committed deletion intent permits a missing dataset.
		if !s.Retained[id].PurgePending {
			return e
		}
		var names []byte
		if names, e = command(ctx, "zfs", "list", "-H", "-o", "name", "-r", s.Dataset); e != nil {
			return e
		}
		for _, name := range strings.Fields(string(names)) {
			if name == m.Dataset {
				return fmt.Errorf("無法確認資料集刪除狀態，請重試")
			}
		}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.state.Retained, id)
	if e = b.saveLocked(); e != nil {
		b.state.Retained[id] = m
		return fmt.Errorf("資料已刪除；刪除紀錄待重試完成: %w", e)
	}
	return nil
}
func (b *Broker) restore(ctx context.Context, id string) error {
	defer b.lockVM(id)()
	s := b.snapshot()
	m, ok := s.Retained[id]
	if !ok {
		return fmt.Errorf("找不到保留資料")
	}
	if m.PurgePending {
		return fmt.Errorf("永久刪除已開始，請重試完成刪除")
	}
	if e := mounted(ctx, s); e != nil {
		return e
	}
	if e := verifyMachineDataset(ctx, s, m); e != nil {
		return e
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.state.Retained, id)
	b.state.Machines[id] = m
	if e := b.saveLocked(); e != nil {
		delete(b.state.Machines, id)
		b.state.Retained[id] = m
		return e
	}
	return nil
}
func (b *Broker) prepareStop(ctx context.Context) (err error) {
	b.stopping.Store(true)
	b.operationGate.Lock()
	defer b.operationGate.Unlock()
	defer func() {
		if err != nil {
			b.stopping.Store(false)
		}
	}()
	if b.uploads.Load() > 0 {
		return fmt.Errorf("ISO 上傳尚未完成，請完成或取消後再停止管理器")
	}
	s := b.snapshot()
	ids := make([]string, 0, len(s.Machines))
	for id := range s.Machines {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		status, reason := processStatus(s, s.Machines[id])
		if holdsResources(s, s.Machines[id], status, reason) {
			if e := b.action(ctx, id, "shutdown", ""); e != nil {
				return e
			}
		}
	}
	return nil
}
func (b *Broker) AutoStart() {
	boot, _ := os.ReadFile("/proc/sys/kernel/random/boot_id")
	last, _ := os.ReadFile(filepath.Join(b.root, "last-boot-id"))
	if strings.TrimSpace(string(last)) == strings.TrimSpace(string(boot)) {
		return
	}
	if e := os.WriteFile(filepath.Join(b.root, "last-boot-id"), boot, 0600); e != nil {
		return
	}
	s := b.snapshot()
	ids := []string{}
	for id, m := range s.Machines {
		if m.Autostart {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	for _, id := range ids {
		id := id
		job, e := b.newJob("自動啟動 "+s.Machines[id].Name, func(ctx context.Context) error { return b.start(ctx, id) })
		if e != nil {
			continue
		}
		for {
			b.jobsMu.Lock()
			j := b.jobs[job]
			b.jobsMu.Unlock()
			if j.State != "queued" && j.State != "running" {
				break
			}
			time.Sleep(time.Second)
		}
	}
}
func (b *Broker) finishPoweroff(s State, m Machine) error {
	b.mu.Lock()
	current := b.state.Machines[m.ID]
	current.WantedRunning = false
	current.Error = ""
	current.Unclean = false
	b.state.Machines[m.ID] = current
	e := b.saveLocked()
	b.mu.Unlock()
	if e != nil {
		return e
	}
	_, e = qmp(machineRoot(s, m.ID)+"/run/qmp.sock", "quit", nil)
	// QEMU may close QMP immediately after accepting quit.
	for i := 0; i < 50; i++ {
		if !processAlive(s, m) {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	if e != nil {
		return e
	}
	return fmt.Errorf("客體已關機，但 QEMU 尚未退出")
}
func (b *Broker) Watchdog() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		_ = syncResolver()
		if !b.operationGate.TryLock() {
			continue
		}
		s := b.snapshot()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if mounted(ctx, s) == nil {
			for id, m := range s.Machines {
				status, _ := processStatus(s, m)
				if status == "stopping" {
					_ = b.finishPoweroff(s, m)
				} else if m.WantedRunning && !processAlive(s, m) {
					b.mu.Lock()
					v := b.state.Machines[id]
					v.Error = guestExitReason(s, m)
					v.WantedRunning = false
					v.Unclean = true
					b.state.Machines[id] = v
					_ = b.saveLocked()
					b.mu.Unlock()
				}
			}
		}
		cancel()
		b.operationGate.Unlock()
	}
}
