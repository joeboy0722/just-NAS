package vmmanager

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

const WorkerUID = 900001
const MaxISO = int64(16 << 30)

var idPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)
var machineTypePattern = regexp.MustCompile(`^pc-q35-[0-9]+\.[0-9]+$`)
var digestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
var poolPattern = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_.-]{0,62}$`)

type Forward struct {
	Protocol  string `json:"protocol"`
	HostIP    string `json:"host_ip"`
	HostPort  int    `json:"host_port"`
	GuestPort int    `json:"guest_port"`
}
type Machine struct {
	ID                 string    `json:"id"`
	Name               string    `json:"name"`
	OS                 string    `json:"os"`
	CPUs               int       `json:"cpus"`
	MemoryMiB          int       `json:"memory_mib"`
	TCGCacheMiB        int       `json:"tcg_cache_mib,omitempty"`
	DiskGiB            int       `json:"disk_gib"`
	Firmware           string    `json:"firmware"`
	Acceleration       string    `json:"acceleration"`
	ActualAcceleration string    `json:"actual_acceleration,omitempty"`
	ISO                string    `json:"iso,omitempty"`
	BootISOOnce        bool      `json:"boot_iso_once,omitempty"`
	Autostart          bool      `json:"autostart"`
	Forwards           []Forward `json:"forwards"`
	ForwardsError      string    `json:"forwards_error,omitempty"`
	UID                uint32    `json:"uid"`
	Dataset            string    `json:"dataset"`
	MachineType        string    `json:"machine_type"`
	Created            time.Time `json:"created"`
	Error              string    `json:"error,omitempty"`
	Unclean            bool      `json:"unclean,omitempty"`
	WantedRunning      bool      `json:"wanted_running,omitempty"`
	PurgePending       bool      `json:"purge_pending,omitempty"`
}

// API requests cannot supply persisted ownership, dataset paths or process state.
type machineInput struct {
	Name         string    `json:"name"`
	OS           string    `json:"os"`
	CPUs         int       `json:"cpus"`
	MemoryMiB    int       `json:"memory_mib"`
	TCGCacheMiB  int       `json:"tcg_cache_mib"`
	DiskGiB      int       `json:"disk_gib"`
	Firmware     string    `json:"firmware"`
	Acceleration string    `json:"acceleration"`
	ISO          string    `json:"iso"`
	BootISOOnce  bool      `json:"boot_iso_once"`
	Autostart    bool      `json:"autostart"`
	Forwards     []Forward `json:"forwards"`
}

func (v machineInput) machine() Machine {
	return Machine{Name: v.Name, OS: v.OS, CPUs: v.CPUs, MemoryMiB: v.MemoryMiB, TCGCacheMiB: v.TCGCacheMiB, DiskGiB: v.DiskGiB, Firmware: v.Firmware, Acceleration: v.Acceleration, ISO: v.ISO, BootISOOnce: v.BootISOOnce, Autostart: v.Autostart, Forwards: v.Forwards}
}

type ISO struct {
	ID      string    `json:"id"`
	Name    string    `json:"name"`
	Bytes   int64     `json:"bytes"`
	SHA256  string    `json:"sha256"`
	Created time.Time `json:"created"`
}
type State struct {
	Version  int                `json:"version"`
	Dataset  string             `json:"dataset"`
	GUID     string             `json:"guid"`
	Machines map[string]Machine `json:"machines"`
	Retained map[string]Machine `json:"retained"`
	ISOs     map[string]ISO     `json:"isos"`
}
type View struct {
	Machine
	Status         string `json:"status"`
	Reason         string `json:"reason,omitempty"`
	UsedBytes      int64  `json:"used_bytes"`
	VirtualBytes   int64  `json:"virtual_bytes"`
	ProcessPresent bool   `json:"process_present"`
}
type Job struct {
	ID       string    `json:"id"`
	State    string    `json:"state"`
	Progress string    `json:"progress"`
	Error    string    `json:"error,omitempty"`
	Created  time.Time `json:"created"`
}

func identifier() string {
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b)
}
func uuid(id string) string {
	return id[:8] + "-" + id[8:12] + "-" + id[12:16] + "-" + id[16:20] + "-" + id[20:]
}
func readJSON(p string, v any) error {
	f, e := os.Open(p)
	if e != nil {
		return e
	}
	defer f.Close()
	d := json.NewDecoder(io.LimitReader(f, 8<<20))
	d.DisallowUnknownFields()
	if e = d.Decode(v); e != nil {
		return e
	}
	if !errors.Is(d.Decode(new(any)), io.EOF) {
		return fmt.Errorf("設定包含多餘內容")
	}
	return nil
}
func atomicJSON(p string, v any) error {
	b, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		return e
	}
	tmp := p + ".new"
	f, e := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	if _, e = f.Write(b); e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e != nil {
		return e
	}
	if ce != nil {
		return ce
	}
	if e = os.Rename(tmp, p); e != nil {
		return e
	}
	if runtime.GOOS == "linux" {
		dir, e := os.Open(filepath.Dir(p))
		if e != nil {
			return e
		}
		defer dir.Close()
		return dir.Sync()
	}
	return nil
}
func storeRoot(s State) string              { return "/mnt/" + strings.Split(s.Dataset, "/")[0] + "/.just-nas-vms" }
func machineRoot(s State, id string) string { return filepath.Join(storeRoot(s), "machines", id) }
func isoPath(s State, id string) string     { return filepath.Join(storeRoot(s), "iso", id+".iso") }
func validate(m Machine) error {
	if m.Name == "" || len([]rune(m.Name)) > 80 || strings.ContainsAny(m.Name, "\x00\r\n") {
		return fmt.Errorf("名稱須為 1–80 字")
	}
	if m.OS != "linux" && m.OS != "windows10" {
		return fmt.Errorf("請選擇 Linux 或 Windows 10")
	}
	if m.CPUs < 1 || m.CPUs > 64 || m.MemoryMiB < 128 || m.MemoryMiB > 262144 || m.DiskGiB < 4 || m.DiskGiB > 16384 {
		return fmt.Errorf("CPU、記憶體或磁碟容量超出支援範圍")
	}
	if m.Firmware != "uefi" && m.Firmware != "bios" {
		return fmt.Errorf("韌體無效")
	}
	if m.Acceleration != "auto" && m.Acceleration != "kvm" && m.Acceleration != "tcg" {
		return fmt.Errorf("加速模式無效")
	}
	if m.TCGCacheMiB != 0 && (m.TCGCacheMiB < 64 || m.TCGCacheMiB > 1024) {
		return fmt.Errorf("TCG 翻譯快取須介於 64–1024 MiB")
	}
	if m.ISO != "" && !idPattern.MatchString(m.ISO) {
		return fmt.Errorf("ISO 識別無效")
	}
	return validateForwards(m.Forwards)
}

func validateForwards(forwards []Forward) error {
	if len(forwards) > 32 {
		return fmt.Errorf("轉發規則最多 32 筆")
	}
	seen := map[string]bool{}
	for _, f := range forwards {
		ip := net.ParseIP(f.HostIP)
		if (f.Protocol != "tcp" && f.Protocol != "udp") || ip == nil || ip.To4() == nil || !ip.IsPrivate() || ip.IsLoopback() || f.HostPort < 1024 || f.HostPort > 65535 || f.GuestPort < 1 || f.GuestPort > 65535 {
			return fmt.Errorf("NAT 轉發欄位無效")
		}
		key := fmt.Sprintf("%s/%s/%d", f.Protocol, ip.String(), f.HostPort)
		if seen[key] {
			return fmt.Errorf("重複的轉發連接埠")
		}
		seen[key] = true
	}
	return nil
}
