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
func networkWarning(ctx context.Context) string {
	resolver, _ := os.ReadFile("/etc/resolv.conf")
	hasDNS := false
	for _, line := range strings.Split(string(resolver), "\n") {
		f := strings.Fields(line)
		if len(f) > 1 && f[0] == "nameserver" && net.ParseIP(strings.SplitN(f[1], "%", 2)[0]) != nil {
			hasDNS = true
			break
		}
	}
	if !hasDNS {
		return "NAS 尚未設定 DNS，客體可能無法解析網站名稱。請檢查系統網路設定；不影響離線安裝。"
	}
	route, e := command(ctx, "ip", "-4", "route", "show", "default")
	if e != nil || len(bytes.TrimSpace(route)) == 0 {
		return "NAS 尚無 IPv4 預設路由，客體 NAT 可能無法連外；區網與離線操作仍可使用。"
	}
	return ""
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
	freshState := res.StatusCode == 404
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
	identities := map[uint32]bool{}
	for id, m := range mergedMachines(b.state) {
		if !idPattern.MatchString(id) || id != m.ID || m.UID < 1000000 || m.UID > 2000000 {
			return nil, fmt.Errorf("VM 識別設定損壞")
		}
		if e = validate(m); e != nil {
			return nil, e
		}
		if m.Dataset != b.state.Dataset+"/machines/"+id {
			return nil, fmt.Errorf("VM 資料集設定損壞")
		}
		if identities[m.UID] || !machineTypePattern.MatchString(m.MachineType) {
			return nil, fmt.Errorf("VM 身分重複或機型設定損壞")
		}
		identities[m.UID] = true
	}
	for id, iso := range b.state.ISOs {
		if !idPattern.MatchString(id) || iso.ID != id || iso.Bytes <= 0 || iso.Bytes > MaxISO || !digestPattern.MatchString(iso.SHA256) {
			return nil, fmt.Errorf("ISO 登錄設定損壞")
		}
	}
	if !freshState {
		if e = readJSON(filepath.Join(root, "jobs.json"), &b.jobs); e != nil && !os.IsNotExist(e) {
			return nil, e
		}
	}
	if b.jobs == nil || len(b.jobs) > 500 {
		return nil, fmt.Errorf("任務記錄格式或筆數無效")
	}
	for id, j := range b.jobs {
		if !idPattern.MatchString(id) || id != j.ID {
			return nil, fmt.Errorf("任務識別無效")
		}
		if j.State == "running" || j.State == "queued" {
			j.State = "interrupted"
			j.Error = "管理服務重新啟動，前次任務未完成"
			b.jobs[id] = j
		}
	}
	if e = atomicJSON(filepath.Join(root, "jobs.json"), b.jobs); e != nil {
		return nil, e
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if mounted(ctx, b.state) == nil {
		entries, _ := os.ReadDir(filepath.Join(storeRoot(b.state), "iso"))
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasPrefix(entry.Name(), ".upload-") && idPattern.MatchString(strings.TrimPrefix(entry.Name(), ".upload-")) {
				_ = os.Remove(filepath.Join(storeRoot(b.state), "iso", entry.Name()))
			}
		}
	}
	if freshState {
		if e = b.saveLocked(); e != nil {
			return nil, e
		}
	}
	if e = syncResolver(); e != nil {
		return nil, e
	}
	return b, nil
}
func mergedMachines(s State) map[string]Machine {
	result := map[string]Machine{}
	for id, m := range s.Machines {
		result[id] = m
	}
	for id, m := range s.Retained {
		if _, ok := result[id]; ok {
			m.UID = 0
		}
		result[id] = m
	}
	return result
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
func freeISOBytes(ctx context.Context, s State) (int64, error) {
	if e := mounted(ctx, s); e != nil {
		return 0, e
	}
	ds := s.Dataset + "/iso"
	path := filepath.Join(storeRoot(s), "iso")
	info, e := command(ctx, "zfs", "get", "-H", "-o", "value", "mounted,mountpoint", ds)
	if e != nil || strings.TrimSpace(string(info)) != "yes\n"+path {
		return 0, fmt.Errorf("ISO 資料集掛載失效，拒絕寫入")
	}
	source, e := command(ctx, "findmnt", "-n", "-o", "SOURCE", "--target", path)
	if e != nil || strings.TrimSpace(string(source)) != ds {
		return 0, fmt.Errorf("ISO 掛載來源失效")
	}
	r, e := command(ctx, "zfs", "get", "-Hp", "-o", "value", "available", ds)
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
func (b *Broker) beginJob(label string) (Job, error) {
	j := Job{ID: identifier(), State: "queued", Progress: label, Created: time.Now()}
	b.jobsMu.Lock()
	defer b.jobsMu.Unlock()
	if len(b.jobs) >= 500 {
		var oldest string
		for id, old := range b.jobs {
			if old.State == "queued" || old.State == "running" {
				continue
			}
			if oldest == "" || old.Created.Before(b.jobs[oldest].Created) {
				oldest = id
			}
		}
		if oldest == "" {
			return j, fmt.Errorf("待處理任務過多")
		}
		delete(b.jobs, oldest)
	}
	b.jobs[j.ID] = j
	if e := atomicJSON(filepath.Join(b.root, "jobs.json"), b.jobs); e != nil {
		delete(b.jobs, j.ID)
		return j, e
	}
	return j, nil
}
func (b *Broker) updateJob(j Job) error {
	if len(j.Error) > 4096 {
		j.Error = j.Error[:4096]
	}
	if len(j.Progress) > 1024 {
		j.Progress = j.Progress[:1024]
	}
	b.jobsMu.Lock()
	defer b.jobsMu.Unlock()
	b.jobs[j.ID] = j
	if e := atomicJSON(filepath.Join(b.root, "jobs.json"), b.jobs); e != nil {
		j.State = "failed"
		j.Error = "任務狀態保存失敗: " + e.Error()
		b.jobs[j.ID] = j
		return e
	}
	return nil
}
func (b *Broker) newJob(label string, fn func(context.Context) error) (string, error) {
	j, e := b.beginJob(label)
	if e != nil {
		return "", e
	}
	go func() {
		b.operationGate.Lock()
		defer b.operationGate.Unlock()
		j.State = "running"
		if e := b.updateJob(j); e != nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		var e error
		if b.stopping.Load() {
			e = fmt.Errorf("管理器正在停止，任務未執行")
		} else {
			e = fn(ctx)
		}
		j.State = "succeeded"
		j.Progress = "完成"
		if e != nil {
			j.State = "failed"
			j.Error = e.Error()
			j.Progress = "失敗"
		}
		_ = b.updateJob(j)
	}()
	return j.ID, nil
}
func (b *Broker) lockVM(id string) func() {
	lock, _ := b.ops.LoadOrStore(id, &sync.Mutex{})
	m := lock.(*sync.Mutex)
	m.Lock()
	return m.Unlock
}
func (b *Broker) create(ctx context.Context, m Machine) (err error) {
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
	m.BootISOOnce = m.ISO != ""
	m.MachineType, e = defaultMachineType(ctx)
	if e != nil {
		return e
	}
	root := machineRoot(s, m.ID)
	if _, e = command(ctx, "zfs", "create", "-o", "mountpoint="+root, "-o", "org.just-nas:vm="+m.ID, m.Dataset); e != nil {
		return e
	}
	committed := false
	defer func() {
		if !committed {
			cleanup, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			if e := verifyMachineDataset(cleanup, s, m); e == nil {
				if _, e = command(cleanup, "zfs", "destroy", "-r", m.Dataset); e != nil {
					err = fmt.Errorf("%v；未完成資料集保留於 %s: %w", err, m.Dataset, e)
				}
			}
		}
	}()
	if e = os.Chmod(root, 0700); e != nil {
		return e
	}
	if e = os.Chown(root, int(m.UID), int(m.UID)); e != nil {
		return e
	}
	if _, e = imageTool(ctx, s, m, "create", "-f", "qcow2", "-o", "compat=1.1,lazy_refcounts=on", "/vm/disk.qcow2", fmt.Sprint(m.DiskGiB)+"G"); e != nil {
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
	committed = true
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
	if v.ProcessPresent || v.Status != "stopped" && v.Status != "failed" {
		return fmt.Errorf("請先關機")
	}
	if e := mounted(ctx, s); e != nil {
		return e
	}
	m = inheritMachineSettings(m, old)
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
		if _, e := imageTool(ctx, s, old, "resize", "-f", "qcow2", "/vm/disk.qcow2", fmt.Sprint(m.DiskGiB)+"G"); e != nil {
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

// Resource forms omit independently managed forwarding rules. Preserve the
// latest table rather than overwriting it with a stale dialog's snapshot.
func inheritMachineSettings(m, old Machine) Machine {
	if m.TCGCacheMiB == 0 {
		m.TCGCacheMiB = old.TCGCacheMiB
	}
	if m.Forwards == nil {
		m.Forwards = old.Forwards
	}
	return m
}
