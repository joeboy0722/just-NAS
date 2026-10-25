package vmmanager

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type Broker struct {
	operationGate sync.Mutex
	stopping      atomic.Bool
	uploads       atomic.Int32
	startMu       sync.Mutex
	root          string
	mu            sync.Mutex
	state         State
	jobsMu        sync.Mutex
	jobs          map[string]Job
	ops           sync.Map
}

func command(ctx context.Context, name string, args ...string) ([]byte, error) {
	c := exec.CommandContext(ctx, name, args...)
	c.WaitDelay = 2 * time.Second
	b, e := c.CombinedOutput()
	if len(b) > 65536 {
		b = b[len(b)-65536:]
	}
	if e != nil {
		return b, fmt.Errorf("%s: %s: %w", name, b, e)
	}
	return b, nil
}
func stateClient() *http.Client {
	return &http.Client{Timeout: 20 * time.Second, Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", "/run/just-nas/native-state.sock")
	}}}
}
func NewBroker(root string) (*Broker, error) {
	b := &Broker{root: root, state: State{Version: 1, Machines: map[string]Machine{}, Retained: map[string]Machine{}, ISOs: map[string]ISO{}}, jobs: map[string]Job{}}
	res, e := stateClient().Get("http://core/state/vm-manager")
	if e != nil {
		return nil, fmt.Errorf("核心原生應用設定服務無法連線: %w", e)
	}
	defer res.Body.Close()
	if res.StatusCode == 200 {
		if e = json.NewDecoder(io.LimitReader(res.Body, 4<<20)).Decode(&b.state); e != nil {
			return nil, e
		}
	} else if res.StatusCode != 404 {
		return nil, fmt.Errorf("核心拒絕讀取應用設定")
	}
	if b.state.Version != 1 || b.state.Machines == nil || b.state.Retained == nil || b.state.ISOs == nil {
		return nil, fmt.Errorf("不相容的 VM 設定版本")
	}
	if b.state.Dataset != "" {
		parts := strings.Split(b.state.Dataset, "/")
		if len(parts) != 2 || !poolPattern.MatchString(parts[0]) || parts[1] != ".just-nas-vms" {
			return nil, fmt.Errorf("VM 儲存設定無效")
		}
	}
	for id, m := range b.state.Machines {
		if !idPattern.MatchString(id) || id != m.ID || m.UID < 1000000 || m.UID > 2000000 {
			return nil, fmt.Errorf("VM 識別設定損壞")
		}
		if e = validate(m); e != nil {
			return nil, e
		}
		if m.Dataset != b.state.Dataset+"/machines/"+id {
			return nil, fmt.Errorf("VM 資料集設定損壞")
		}
	}
	if e = readJSON(filepath.Join(root, "jobs.json"), &b.jobs); e != nil && !os.IsNotExist(e) {
		return nil, e
	}
	for id, j := range b.jobs {
		if j.State == "running" || j.State == "queued" {
			j.State = "interrupted"
			j.Error = "管理服務重新啟動，前次任務未完成"
			b.jobs[id] = j
		}
	}
	return b, nil
}
func (b *Broker) snapshot() State {
	b.mu.Lock()
	defer b.mu.Unlock()
	raw, _ := json.Marshal(b.state)
	var s State
	json.Unmarshal(raw, &s)
	return s
}
func (b *Broker) saveLocked() error {
	stateJSON, e := json.Marshal(b.state)
	if e != nil {
		return e
	}
	refs := []string{}
	if b.state.Dataset != "" {
		refs = append(refs, b.state.Dataset)
	}
	raw, _ := json.Marshal(map[string]any{"version": 1, "data_references": refs, "state": json.RawMessage(stateJSON)})
	refFile := filepath.Join(b.root, "data-references.json")
	oldRefs, _ := os.ReadFile(refFile)
	if e = atomicJSON(refFile, refs); e != nil {
		return e
	}
	req, _ := http.NewRequest("PUT", "http://core/state/vm-manager", bytes.NewReader(raw))
	res, e := stateClient().Do(req)
	if e != nil {
		if oldRefs != nil {
			_ = os.WriteFile(refFile, oldRefs, 0600)
		} else {
			_ = os.Remove(refFile)
		}
		return e
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		if oldRefs != nil {
			_ = os.WriteFile(refFile, oldRefs, 0600)
		} else {
			_ = os.Remove(refFile)
		}
		message, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return fmt.Errorf("保存 VM 設定失敗: %s", message)
	}
	return nil
}
func mounted(ctx context.Context, s State) error {
	if s.Dataset == "" || s.GUID == "" {
		return fmt.Errorf("請先設定 VM 儲存池")
	}
	raw, e := command(ctx, "zfs", "get", "-H", "-o", "value", "guid,mounted,mountpoint,org.just-nas:vm-store", s.Dataset)
	if e != nil {
		return e
	}
	if strings.TrimSpace(string(raw)) != s.GUID+"\nyes\n"+storeRoot(s)+"\nvm-manager" {
		return fmt.Errorf("VM 儲存識別或掛載失效；拒絕寫入")
	}
	raw, e = command(ctx, "findmnt", "-n", "-o", "SOURCE", "--target", storeRoot(s))
	if e != nil || strings.TrimSpace(string(raw)) != s.Dataset {
		return fmt.Errorf("VM 路徑不是指定資料集的掛載點")
	}
	return nil
}
func freeBytes(ctx context.Context, s State) (int64, error) {
	if e := mounted(ctx, s); e != nil {
		return 0, e
	}
	r, e := command(ctx, "zfs", "get", "-H", "-p", "-o", "value", "available", s.Dataset)
	if e != nil {
		return 0, e
	}
	return strconv.ParseInt(strings.TrimSpace(string(r)), 10, 64)
}
func (b *Broker) configure(ctx context.Context, pool string) error {
	b.operationGate.Lock()
	defer b.operationGate.Unlock()
	if b.stopping.Load() {
		return fmt.Errorf("管理器正在停止")
	}
	if !poolPattern.MatchString(pool) {
		return fmt.Errorf("儲存池名稱無效")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.state.Dataset != "" {
		return fmt.Errorf("VM 儲存位置已設定，第一版不提供搬移")
	}
	ds := pool + "/.just-nas-vms"
	root := "/mnt/" + pool + "/.just-nas-vms"
	if _, e := command(ctx, "zpool", "list", "-H", pool); e != nil {
		return e
	}
	if _, e := command(ctx, "zfs", "list", "-H", ds); e == nil {
		return fmt.Errorf("已有同名資料集，拒絕接管")
	}
	if _, e := command(ctx, "zfs", "create", "-o", "mountpoint="+root, "-o", "org.just-nas:vm-store=vm-manager", ds); e != nil {
		return e
	}
	guid, e := command(ctx, "zfs", "get", "-H", "-o", "value", "guid", ds)
	if e != nil {
		return e
	}
	for _, n := range []string{"machines", "iso"} {
		if _, e = command(ctx, "zfs", "create", "-o", "mountpoint="+root+"/"+n, ds+"/"+n); e != nil {
			return e
		}
	}
	old := b.state
	b.state.Dataset = ds
	b.state.GUID = strings.TrimSpace(string(guid))
	if e = b.saveLocked(); e != nil {
		b.state = old
		return e
	}
	return nil
}
func (b *Broker) newJob(label string, fn func(context.Context) error) string {
	id := identifier()
	j := Job{ID: id, State: "queued", Progress: label, Created: time.Now()}
	b.jobsMu.Lock()
	b.jobs[id] = j
	_ = atomicJSON(filepath.Join(b.root, "jobs.json"), b.jobs)
	b.jobsMu.Unlock()
	go func() {
		b.operationGate.Lock()
		defer b.operationGate.Unlock()
		b.jobsMu.Lock()
		j.State = "running"
		b.jobs[id] = j
		_ = atomicJSON(filepath.Join(b.root, "jobs.json"), b.jobs)
		b.jobsMu.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		var e error
		if b.stopping.Load() {
			e = fmt.Errorf("管理器正在停止，任務未執行")
		} else {
			e = fn(ctx)
		}
		b.jobsMu.Lock()
		defer b.jobsMu.Unlock()
		j.State = "succeeded"
		j.Progress = "完成"
		if e != nil {
			j.State = "failed"
			j.Error = e.Error()
			j.Progress = "失敗"
		}
		b.jobs[id] = j
		_ = atomicJSON(filepath.Join(b.root, "jobs.json"), b.jobs)
	}()
	return id
}
func (b *Broker) lockVM(id string) func() {
	lock, _ := b.ops.LoadOrStore(id, &sync.Mutex{})
	m := lock.(*sync.Mutex)
	m.Lock()
	return m.Unlock
}
func (b *Broker) create(ctx context.Context, m Machine) error {
	defer b.lockVM("create")()
	if e := validate(m); e != nil {
		return e
	}
	s := b.snapshot()
	if e := mounted(ctx, s); e != nil {
		return e
	}
	if m.ISO != "" {
		if _, ok := s.ISOs[m.ISO]; !ok {
			return fmt.Errorf("找不到 ISO")
		}
	}
	available, e := freeBytes(ctx, s)
	if e != nil {
		return e
	}
	if available < 512<<20 {
		return fmt.Errorf("儲存空間不足")
	}
	m.ID = identifier()
	m.UID = 1000000
	for _, v := range s.Machines {
		if v.UID >= m.UID {
			m.UID = v.UID + 1
		}
	}
	for _, v := range s.Retained {
		if v.UID >= m.UID {
			m.UID = v.UID + 1
		}
	}
	if m.UID > 2000000 {
		return fmt.Errorf("VM 身分已耗盡")
	}
	m.Dataset = s.Dataset + "/machines/" + m.ID
	m.Created = time.Now()
	m.MachineType, e = defaultMachineType(ctx)
	if e != nil {
		return e
	}
	root := machineRoot(s, m.ID)
	if _, e = command(ctx, "zfs", "create", "-o", "mountpoint="+root, "-o", "org.just-nas:vm="+m.ID, m.Dataset); e != nil {
		return e
	}
	if _, e = command(ctx, "qemu-img", "create", "-f", "qcow2", "-o", "compat=1.1,lazy_refcounts=on", root+"/disk.qcow2", fmt.Sprint(m.DiskGiB)+"G"); e != nil {
		return e
	}
	if m.Firmware == "uefi" {
		if e = copyFile("/usr/share/OVMF/OVMF_VARS.fd", root+"/uefi-vars.fd", 0600); e != nil {
			return e
		}
	}
	if e = os.Chmod(root, 0700); e != nil {
		return e
	}
	if e = os.Chown(root, int(m.UID), int(m.UID)); e != nil {
		return e
	}
	for _, n := range []string{"disk.qcow2", "uefi-vars.fd"} {
		if e = os.Chown(root+"/"+n, int(m.UID), int(m.UID)); e != nil && !os.IsNotExist(e) {
			return e
		}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.state.Machines[m.ID] = m
	if e = b.saveLocked(); e != nil {
		delete(b.state.Machines, m.ID)
		return e
	}
	return nil
}
func copyFile(from, to string, mode os.FileMode) error {
	src, e := os.Open(from)
	if e != nil {
		return e
	}
	defer src.Close()
	dst, e := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if e != nil {
		return e
	}
	_, e = io.Copy(dst, src)
	ce := dst.Close()
	if e != nil {
		return e
	}
	return ce
}
func (b *Broker) edit(ctx context.Context, id string, m Machine) error {
	defer b.lockVM(id)()
	s := b.snapshot()
	old, ok := s.Machines[id]
	if !ok {
		return fmt.Errorf("找不到 VM")
	}
	v := b.view(ctx, s, old)
	if v.Status != "stopped" && v.Status != "failed" {
		return fmt.Errorf("請先關機")
	}
	if e := mounted(ctx, s); e != nil {
		return e
	}
	if e := validate(m); e != nil {
		return e
	}
	if m.DiskGiB < old.DiskGiB {
		return fmt.Errorf("磁碟不能縮小")
	}
	if m.Firmware == "uefi" && old.Firmware != "uefi" {
		vars := machineRoot(s, id) + "/uefi-vars.fd"
		if _, e := os.Stat(vars); os.IsNotExist(e) {
			if e = copyFile("/usr/share/OVMF/OVMF_VARS.fd", vars, 0600); e != nil {
				return e
			}
			if e = os.Chown(vars, int(old.UID), int(old.UID)); e != nil {
				return e
			}
		}
	}
	if m.ISO != "" {
		if _, ok := s.ISOs[m.ISO]; !ok {
			return fmt.Errorf("找不到 ISO")
		}
	}
	if m.DiskGiB > old.DiskGiB {
		if _, e := command(ctx, "qemu-img", "resize", machineRoot(s, id)+"/disk.qcow2", fmt.Sprint(m.DiskGiB)+"G"); e != nil {
			return e
		}
	}
	m.ID = old.ID
	m.UID = old.UID
	m.Dataset = old.Dataset
	m.Created = old.Created
	m.MachineType = old.MachineType
	m.Unclean = old.Unclean
	b.mu.Lock()
	defer b.mu.Unlock()
	b.state.Machines[id] = m
	if e := b.saveLocked(); e != nil {
		b.state.Machines[id] = old
		return e
	}
	return nil
}
