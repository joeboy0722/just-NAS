package storage

import (
	"encoding/json"
	"fmt"
	"just-nas/internal/command"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Pool struct {
	Name          string  `json:"name"`
	Size          uint64  `json:"size"`
	Allocated     uint64  `json:"allocated"`
	Free          uint64  `json:"free"`
	Health        string  `json:"health"`
	CapacityPct   float64 `json:"capacity_pct"`
	Fragmentation string  `json:"fragmentation"`
}
type Dataset struct {
	Quota        uint64 `json:"quota"`
	OverSince    uint64 `json:"over_since"`
	QuotaLocked  bool   `json:"quota_locked"`
	SnapshotUsed uint64 `json:"snapshot_used"`
	Name         string `json:"name"`
	Used         uint64 `json:"used"`
	Available    uint64 `json:"available"`
	Referenced   uint64 `json:"referenced"`
	Mountpoint   string `json:"mountpoint"`
	Compression  string `json:"compression"`
}
type Snapshot struct {
	Created    uint64 `json:"created"`
	Name       string `json:"name"`
	Used       uint64 `json:"used"`
	Referenced uint64 `json:"referenced"`
}
type SmartDisk struct {
	Device       string `json:"device"`
	Model        string `json:"model"`
	Serial       string `json:"serial"`
	Size         uint64 `json:"size"`
	Temperature  *int   `json:"temperature"`
	Health       string `json:"health"`
	PowerOnHours *int   `json:"power_on_hours"`
	Error        string `json:"error,omitempty"`
}
type AvailableDisk struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	Size      uint64 `json:"size"`
	Type      string `json:"type"`
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}
type StorageManager struct{ mu sync.Mutex }

func NewStorageManager() *StorageManager { return &StorageManager{} }

var poolRE = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.-]{0,62}$`)
var partRE = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,62}$`)

func ValidDataset(s string) bool {
	p := strings.Split(s, "/")
	if len(p) < 2 || !poolRE.MatchString(p[0]) {
		return false
	}
	for _, v := range p[1:] {
		if !partRE.MatchString(v) || v == "." || v == ".." {
			return false
		}
	}
	return !strings.Contains(s, "/.just-nas")
}
func num(s string) uint64 { n, _ := strconv.ParseUint(strings.TrimSuffix(s, "%"), 10, 64); return n }
func lines(b []byte) [][]string {
	r := [][]string{}
	for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if l != "" {
			r = append(r, strings.Split(l, "\t"))
		}
	}
	return r
}
func (m *StorageManager) GetPools() ([]Pool, error) {
	out, err := command.Run("", "zpool", "list", "-H", "-p", "-o", "name,size,alloc,free,health,cap,frag")
	if err != nil {
		return nil, err
	}
	r := []Pool{}
	for _, f := range lines(out) {
		if len(f) != 7 {
			return nil, fmt.Errorf("無法解析 zpool 輸出")
		}
		r = append(r, Pool{f[0], num(f[1]), num(f[2]), num(f[3]), f[4], float64(num(f[5])), f[6]})
	}
	return r, nil
}
func (m *StorageManager) CreatePool(name, raid string, devices []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !poolRE.MatchString(name) || strings.HasPrefix(name, "raidz") || strings.HasPrefix(name, "mirror") || name == "log" || name == "spare" {
		return fmt.Errorf("無效的儲存池名稱")
	}
	min := map[string]int{"stripe": 1, "single": 1, "mirror": 2, "raidz1": 3, "raidz2": 4}
	required, ok := min[raid]
	if !ok || len(devices) < required {
		return fmt.Errorf("RAID 類型或硬碟數量不符")
	}
	if _, err := command.Run("", "modprobe", "zfs"); err != nil {
		return err
	}
	disks, err := m.GetAvailableDisks()
	if err != nil {
		return err
	}
	allowed := map[string]bool{}
	for _, d := range disks {
		if d.Available {
			p, _ := filepath.EvalSymlinks(d.Path)
			allowed[p] = true
		}
	}
	seen := map[string]bool{}
	for _, d := range devices {
		p, e := filepath.EvalSymlinks(d)
		if e != nil || !allowed[p] || seen[p] {
			return fmt.Errorf("硬碟 %s 不可用、已有資料或重複選取", d)
		}
		seen[p] = true
	}
	args := []string{"create", "-o", "ashift=12", "-O", "compression=lz4", "-O", "atime=off", "-m", "/mnt/" + name, name}
	if raid != "single" && raid != "stripe" {
		args = append(args, raid)
	}
	args = append(args, devices...)
	_, err = command.Run("", "zpool", args...)
	return err
}
func (m *StorageManager) DestroyPool(name string) error {
	if !poolRE.MatchString(name) {
		return fmt.Errorf("無效的池名稱")
	}
	out, err := command.Run("", "zfs", "get", "-H", "-o", "value", "org.just-nas:config", name+"/.just-nas")
	if err == nil && strings.TrimSpace(string(out)) == "1" {
		return fmt.Errorf("此池保存系統設定，不能從控制台銷毀")
	}
	_, err = command.Run("", "zpool", "destroy", name)
	return err
}
func (m *StorageManager) GetDatasets() ([]Dataset, error) {
	out, err := command.Run("", "zfs", "list", "-H", "-p", "-t", "filesystem", "-o", "name,used,avail,refer,mountpoint,compression,usedbysnapshots,org.just-nas:quota,org.just-nas:over-since,org.just-nas:quota-locked")
	if err != nil {
		return nil, err
	}
	r := []Dataset{}
	for _, f := range lines(out) {
		if len(f) != 10 {
			return nil, fmt.Errorf("無法解析 zfs 輸出")
		}
		if strings.Contains(f[0], "/.just-nas") {
			continue
		}
		r = append(r, Dataset{Name: f[0], Used: num(f[1]), Available: num(f[2]), Referenced: num(f[3]), Mountpoint: f[4], Compression: f[5], SnapshotUsed: num(f[6]), Quota: num(f[7]), OverSince: num(f[8]), QuotaLocked: f[9] == "on" || f[9] == "pending"})
	}
	return r, nil
}
func (m *StorageManager) CreateDataset(name, mount, compression string) error {
	if !ValidDataset(name) {
		return fmt.Errorf("無效的資料集名稱，請先選擇儲存池")
	}
	if compression != "lz4" && compression != "zstd" && compression != "off" {
		return fmt.Errorf("不支援的壓縮設定")
	}
	expected := "/mnt/" + name
	if mount != "" && filepath.Clean(mount) != expected {
		return fmt.Errorf("掛載點必須為 %s", expected)
	}
	_, err := command.Run("", "zfs", "create", "-o", "mountpoint="+expected, "-o", "compression="+compression, "-o", "acltype=posixacl", "-o", "xattr=sa", name)
	if err != nil {
		return err
	}
	if _, err = command.Run("", "zfs", "project", "-s", "-p", "1", expected); err != nil {
		return err
	}
	_, err = command.Run("", "zfs", "set", "org.just-nas:quota-project=1", name)
	return err
}
func (m *StorageManager) DestroyDataset(name string) error {
	if !ValidDataset(name) {
		return fmt.Errorf("無效的資料集名稱")
	}
	_, err := command.Run("", "zfs", "destroy", name)
	return err
}
func (m *StorageManager) GetSnapshots() ([]Snapshot, error) {
	out, err := command.Run("", "zfs", "list", "-H", "-p", "-t", "snapshot", "-o", "name,used,refer,creation")
	if err != nil {
		return nil, err
	}
	r := []Snapshot{}
	for _, f := range lines(out) {
		if len(f) != 4 {
			return nil, fmt.Errorf("無法解析快照輸出")
		}
		if !strings.Contains(f[0], "/.just-nas") {
			r = append(r, Snapshot{Name: f[0], Used: num(f[1]), Referenced: num(f[2]), Created: num(f[3])})
		}
	}
	return r, nil
}
func validSnapshot(s string) bool {
	p := strings.Split(s, "@")
	return len(p) == 2 && ValidDataset(p[0]) && partRE.MatchString(p[1])
}
func (m *StorageManager) CreateSnapshot(ds, tag string) error {
	if tag == "" {
		tag = time.Now().Format("manual-20060102-150405")
	}
	target := ds + "@" + tag
	if !validSnapshot(target) {
		return fmt.Errorf("無效的快照名稱")
	}
	_, err := command.Run("", "zfs", "snapshot", target)
	return err
}
func (m *StorageManager) RollbackSnapshot(s string) error {
	if !validSnapshot(s) {
		return fmt.Errorf("無效的快照名稱")
	}
	_, err := command.Run("", "zfs", "rollback", s)
	return err
}
func (m *StorageManager) DestroySnapshot(s string) error {
	if !validSnapshot(s) {
		return fmt.Errorf("無效的快照名稱")
	}
	_, err := command.Run("", "zfs", "destroy", s)
	return err
}

type block struct {
	Name        string    `json:"name"`
	Path        string    `json:"path"`
	Size        uint64    `json:"size"`
	Type        string    `json:"type"`
	RO          bool      `json:"ro"`
	RM          bool      `json:"rm"`
	FSType      *string   `json:"fstype"`
	Mountpoints []*string `json:"mountpoints"`
	Children    []block   `json:"children"`
}

func occupied(b block) bool {
	if b.RO || b.RM || b.FSType != nil && *b.FSType != "" || len(b.Children) > 0 {
		return true
	}
	for _, p := range b.Mountpoints {
		if p != nil && *p != "" {
			return true
		}
	}
	return false
}
func (m *StorageManager) GetAvailableDisks() ([]AvailableDisk, error) {
	out, err := command.Run("", "lsblk", "--json", "--bytes", "--paths", "-o", "NAME,PATH,SIZE,TYPE,RO,RM,FSTYPE,MOUNTPOINTS")
	if err != nil {
		return nil, err
	}
	var v struct {
		Devices []block `json:"blockdevices"`
	}
	if err = json.Unmarshal(out, &v); err != nil {
		return nil, err
	}
	members, err := command.Run("", "zpool", "status", "-P")
	if err != nil {
		return nil, err
	}
	r := []AvailableDisk{}
	for _, b := range v.Devices {
		if b.Type != "disk" {
			continue
		}
		reason := ""
		if occupied(b) {
			reason = "含分割區、檔案系統、掛載或受保護"
		}
		if strings.Contains(string(members), b.Path) {
			reason = "已屬於 ZFS 儲存池"
		}
		path := b.Path
		ids, _ := filepath.Glob("/dev/disk/by-id/*")
		for _, id := range ids {
			p, _ := filepath.EvalSymlinks(id)
			if p == b.Path && !strings.Contains(id, "-part") {
				path = id
				break
			}
		}
		r = append(r, AvailableDisk{filepath.Base(b.Name), path, b.Size, "Disk", reason == "", reason})
	}
	return r, nil
}
func (m *StorageManager) GetSmartDisks() ([]SmartDisk, error) {
	disks, err := m.GetAvailableDisks()
	if err != nil {
		return nil, err
	}
	r := []SmartDisk{}
	for _, d := range disks {
		s := SmartDisk{Device: d.Path, Model: "未知", Serial: "未知", Size: d.Size, Health: "UNKNOWN"}
		out, e := command.Run("", "smartctl", "-a", "-j", d.Path)
		var v struct {
			Model  string `json:"model_name"`
			Serial string `json:"serial_number"`
			Temp   struct {
				Current *int `json:"current"`
			} `json:"temperature"`
			Hours struct {
				Hours *int `json:"hours"`
			} `json:"power_on_time"`
			Status *struct {
				Passed bool `json:"passed"`
			} `json:"smart_status"`
		}
		if json.Unmarshal(out, &v) == nil {
			s.Model = v.Model
			s.Serial = v.Serial
			s.Temperature = v.Temp.Current
			s.PowerOnHours = v.Hours.Hours
			if v.Status != nil {
				if v.Status.Passed {
					s.Health = "PASSED"
				} else {
					s.Health = "FAILED"
				}
			}
		}
		if e != nil && v.Status == nil {
			s.Error = e.Error()
		}
		r = append(r, s)
	}
	return r, nil
}
func MountedDataset(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || strings.ContainsAny(path, "\n\r\t") {
		return fmt.Errorf("共享路徑無效")
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return err
	}
	out, err := command.Run("", "zfs", "list", "-H", "-o", "name,mounted,mountpoint")
	if err != nil {
		return err
	}
	for _, f := range lines(out) {
		if len(f) == 3 && ValidDataset(f[0]) && f[1] == "yes" && filepath.IsAbs(f[2]) && (resolved == f[2] || strings.HasPrefix(resolved, f[2]+"/")) {
			return nil
		}
	}
	return fmt.Errorf("路徑必須位於已掛載的資料集內")
}
