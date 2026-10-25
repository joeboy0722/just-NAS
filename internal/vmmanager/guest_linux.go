//go:build linux

package vmmanager

import (
	"context"

	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type processRecord struct {
	PID   int    `json:"pid"`
	Start string `json:"start"`
	UUID  string `json:"uuid"`
}
type guestConfig struct {
	Machine   Machine  `json:"machine"`
	Root      string   `json:"root"`
	ISO       string   `json:"iso"`
	Args      []string `json:"args"`
	ImageTool bool     `json:"image_tool,omitempty"`
}

// Image parsing has the same filesystem/UID boundary as a running guest.
// Never run qemu-img as the host root against guest-writable image metadata.
func imageTool(ctx context.Context, s State, m Machine, args ...string) ([]byte, error) {
	root := machineRoot(s, m.ID)
	if fi, e := os.Lstat(root + "/disk.qcow2"); e == nil && !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("磁碟必須是一般檔案")
	} else if e != nil && !os.IsNotExist(e) {
		return nil, e
	}
	dir := "/run/just-nas/vm-launch"
	if e := os.MkdirAll(dir, 0700); e != nil {
		return nil, e
	}
	file := dir + "/image-" + m.ID + ".json"
	defer os.Remove(file)
	if e := atomicJSON(file, guestConfig{Machine: m, Root: root, Args: args, ImageTool: true}); e != nil {
		return nil, e
	}
	exe, e := os.Executable()
	if e != nil {
		return nil, e
	}
	gate, release, e := os.Pipe()
	if e != nil {
		return nil, e
	}
	defer gate.Close()
	defer release.Close()
	c := exec.CommandContext(ctx, exe, "--mode", "guest-run", "--guest-config", file)
	c.ExtraFiles = []*os.File{gate}
	c.SysProcAttr = &syscall.SysProcAttr{Cloneflags: syscall.CLONE_NEWNS | syscall.CLONE_NEWIPC | syscall.CLONE_NEWUTS | syscall.CLONE_NEWPID | syscall.CLONE_NEWNET, Setsid: true}
	var output strings.Builder
	c.Stdout = &output
	c.Stderr = &output
	if e = c.Start(); e != nil {
		return nil, e
	}
	limits := m
	limits.MemoryMiB = 256
	limits.CPUs = 1
	if e = setupCgroup(limits, c.Process.Pid); e != nil {
		c.Process.Kill()
		c.Wait()
		return nil, e
	}
	defer os.Remove("/sys/fs/cgroup/just-nas-vms/" + m.ID)
	if _, e = release.Write([]byte{1}); e != nil {
		c.Process.Kill()
		c.Wait()
		return nil, e
	}
	e = c.Wait()
	if e != nil {
		return nil, fmt.Errorf("磁碟工具: %s: %w", output.String(), e)
	}
	return []byte(output.String()), nil
}

func memoryInfo() (int64, int64) {
	raw, _ := os.ReadFile("/proc/meminfo")
	var total, available int64
	for _, l := range strings.Split(string(raw), "\n") {
		f := strings.Fields(l)
		if len(f) < 2 {
			continue
		}
		v, _ := strconv.ParseInt(f[1], 10, 64)
		if f[0] == "MemTotal:" {
			total = v / 1024
		}
		if f[0] == "MemAvailable:" {
			available = v / 1024
		}
	}
	return total, available
}
func hostCPUs() int { return runtime.NumCPU() }
func allocatedBytes(path string) int64 {
	var s syscall.Stat_t
	if syscall.Stat(path, &s) != nil {
		return 0
	}
	return s.Blocks * 512
}
func probeKVM() (bool, string) {
	f, e := os.OpenFile("/dev/kvm", os.O_RDWR, 0)
	if e != nil {
		return false, e.Error()
	}
	defer f.Close()
	version, _, err := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), 0xae00, 0)
	if err != 0 || version != 12 {
		return false, "KVM API 無法使用"
	}
	fd, _, err := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), 0xae01, 0)
	if err != 0 {
		return false, err.Error()
	}
	syscall.Close(int(fd))
	return true, ""
}
func chooseAcceleration(ctx context.Context, requested string) (string, string) {
	if requested == "tcg" {
		return "tcg", "管理員指定 TCG"
	}
	ok, reason := probeKVM()
	if ok {
		return "kvm", ""
	}
	if requested == "kvm" {
		return "", "KVM 不可用: " + reason
	}
	return "tcg", "KVM 不可用，改用 TCG: " + reason
}
func processStart(pid int) string {
	raw, e := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if e != nil {
		return ""
	}
	i := strings.LastIndex(string(raw), ")")
	if i < 0 {
		return ""
	}
	f := strings.Fields(string(raw[i+1:]))
	if len(f) <= 19 {
		return ""
	}
	return f[19]
}
func processFile(id string) string { return "/run/just-nas/vm-records/" + id + ".json" }
func exitFile(id string) string    { return "/run/just-nas/vm-records/" + id + "-exit.json" }
func processAlive(s State, m Machine) bool {
	var p processRecord
	if readJSON(processFile(m.ID), &p) != nil || p.UUID != uuid(m.ID) || p.Start == "" || processStart(p.PID) != p.Start {
		return false
	}
	exe, e := os.Readlink(fmt.Sprintf("/proc/%d/exe", p.PID))
	if e != nil || filepath.Base(exe) != "qemu-system-x86_64" {
		return false
	}
	raw, e := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", p.PID))
	return e == nil && strings.Contains(string(raw), "\x00"+uuid(m.ID)+"\x00")
}
func signalGuest(s State, m Machine) error {
	if !processAlive(s, m) {
		return nil
	}
	var p processRecord
	if e := readJSON(processFile(m.ID), &p); e != nil {
		return e
	}
	if e := syscall.Kill(p.PID, syscall.SIGTERM); e != nil {
		return e
	}
	for n := 0; n < 50; n++ {
		if !processAlive(s, m) {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	if processAlive(s, m) {
		if e := syscall.Kill(p.PID, syscall.SIGKILL); e != nil {
			return e
		}
	}
	return nil
}
func setupCgroup(m Machine, pid int) error {
	base := "/sys/fs/cgroup"
	raw, e := os.ReadFile(base + "/cgroup.controllers")
	if e != nil {
		return fmt.Errorf("需要 cgroup v2 資源隔離: %w", e)
	}
	for _, n := range []string{"cpu", "memory", "pids"} {
		if !strings.Contains(string(raw), n) {
			return fmt.Errorf("cgroup 控制器缺少 %s", n)
		}
	}
	if e = os.WriteFile(base+"/cgroup.subtree_control", []byte("+cpu +memory +pids"), 0600); e != nil {
		return e
	}
	parent := base + "/just-nas-vms"
	if e = os.MkdirAll(parent, 0755); e != nil {
		return e
	}
	if e = os.WriteFile(parent+"/cgroup.subtree_control", []byte("+cpu +memory +pids"), 0600); e != nil {
		return e
	}
	total, _ := memoryInfo()
	reserve := total / 4
	if reserve < 2048 {
		reserve = 2048
	}
	maxCPU := hostCPUs() - 1
	if maxCPU < 1 {
		maxCPU = 1
	}
	for n, v := range map[string]string{"memory.max": fmt.Sprint((total - reserve) << 20), "cpu.max": fmt.Sprintf("%d 100000", maxCPU*100000)} {
		if e = os.WriteFile(parent+"/"+n, []byte(v), 0600); e != nil {
			return e
		}
	}
	group := parent + "/" + m.ID
	if e = os.MkdirAll(group, 0755); e != nil {
		return e
	}
	for n, v := range map[string]string{"memory.max": fmt.Sprint((int64(m.MemoryMiB) + guestOverhead(m.MemoryMiB)) << 20), "memory.swap.max": "0", "cpu.max": fmt.Sprintf("%d 100000", m.CPUs*100000), "pids.max": "256", "cgroup.procs": fmt.Sprint(pid)} {
		if e = os.WriteFile(group+"/"+n, []byte(v), 0600); e != nil {
			return e
		}
	}
	return nil
}

type rollingLog struct {
	mu   sync.Mutex
	path string
}

func (w *rollingLog) Write(b []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	root, e := os.OpenRoot(filepath.Dir(w.path))
	if e != nil {
		return 0, e
	}
	defer root.Close()
	name := filepath.Base(w.path)
	if st, e := root.Stat(name); e == nil && st.Size() > 5<<20 {
		_ = root.Rename(name, name+".1")
	}
	f, e := root.OpenFile(name, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if e != nil {
		return 0, e
	}
	defer f.Close()
	return f.Write(b)
}
func launchGuest(ctx context.Context, s State, m Machine, args []string) error {
	dir := "/run/just-nas/vm-launch"
	if e := os.MkdirAll(dir, 0700); e != nil {
		return e
	}
	root := machineRoot(s, m.ID)
	if e := os.MkdirAll("/run/just-nas/vm-records", 0700); e != nil {
		return e
	}
	_ = os.Remove(exitFile(m.ID))
	iso := ""
	if m.ISO != "" {
		iso = isoPath(s, m.ISO)
	}
	file := dir + "/" + m.ID + ".json"
	if e := atomicJSON(file, guestConfig{Machine: m, Root: root, ISO: iso, Args: args}); e != nil {
		return e
	}
	executable, e := os.Executable()
	if e != nil {
		return e
	}
	barrier, release, e := os.Pipe()
	if e != nil {
		return e
	}
	defer barrier.Close()
	defer release.Close()
	cmd := exec.Command(executable, "--mode", "guest-run", "--guest-config", file)
	cmd.ExtraFiles = []*os.File{barrier}
	cmd.SysProcAttr = &syscall.SysProcAttr{Cloneflags: syscall.CLONE_NEWNS | syscall.CLONE_NEWIPC | syscall.CLONE_NEWUTS | syscall.CLONE_NEWPID, Setsid: true}
	log := &rollingLog{path: root + "/qemu.log"}
	cmd.Stdout = log
	cmd.Stderr = log
	if e = cmd.Start(); e != nil {
		return e
	}
	if e = setupCgroup(m, cmd.Process.Pid); e != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return e
	}
	p := processRecord{PID: cmd.Process.Pid, Start: processStart(cmd.Process.Pid), UUID: uuid(m.ID)}
	if e = atomicJSON(processFile(m.ID), p); e != nil {
		cmd.Process.Kill()
		cmd.Wait()
		return e
	}
	go func() {
		err := cmd.Wait()
		group := "/sys/fs/cgroup/just-nas-vms/" + m.ID
		events, _ := os.ReadFile(group + "/memory.events")
		message := "程序正常退出"
		if err != nil {
			message = err.Error()
		}
		record := map[string]any{"time": time.Now(), "result": message, "memory_events": string(events)}
		_ = atomicJSON(exitFile(m.ID), record)
		_, _ = log.Write([]byte(fmt.Sprintf("\n[process exit] %s\n%s", message, events)))
		_ = os.Remove(group)
	}()
	if _, e = release.Write([]byte{1}); e != nil {
		return e
	}
	return nil
}
func guestExitReason(s State, m Machine) string {
	var result struct {
		Time         time.Time `json:"time"`
		Result       string    `json:"result"`
		MemoryEvents string    `json:"memory_events"`
	}
	if readJSON(exitFile(m.ID), &result) == nil {
		for _, line := range strings.Split(result.MemoryEvents, "\n") {
			f := strings.Fields(line)
			if len(f) == 2 && f[0] == "oom_kill" && f[1] != "0" {
				return "QEMU 超過記憶體資源上限，已由 cgroup 停止；詳見日誌"
			}
		}
		return "QEMU 程序非預期退出: " + result.Result
	}
	return "QEMU 程序非預期退出，請查看日誌"
}
func GuestRun(config string) error {
	gate := os.NewFile(3, "startup-gate")
	var one [1]byte
	if _, e := io.ReadFull(gate, one[:]); e != nil {
		return fmt.Errorf("資源隔離未完成")
	}
	gate.Close()
	var c guestConfig
	if e := readJSON(config, &c); e != nil {
		return e
	}
	if !idPattern.MatchString(c.Machine.ID) || c.Machine.UID < 1000000 || c.Machine.UID > 2000000 {
		return fmt.Errorf("客體身分無效")
	}
	if e := syscall.Mount("", "/", "", syscall.MS_REC|syscall.MS_PRIVATE, ""); e != nil {
		return e
	}
	root := "/run/just-nas/guest-roots/" + c.Machine.ID
	if e := os.MkdirAll(root, 0700); e != nil {
		return e
	}
	if e := syscall.Mount("tmpfs", root, "tmpfs", syscall.MS_NOSUID|syscall.MS_NODEV, "size=8m,mode=755"); e != nil {
		return e
	}
	bind := func(from, to string, ro bool) error {
		target := root + to
		st, e := os.Stat(from)
		if e != nil {
			return e
		}
		if st.IsDir() {
			e = os.MkdirAll(target, 0755)
		} else {
			if e = os.MkdirAll(filepath.Dir(target), 0755); e == nil {
				var f *os.File
				f, e = os.OpenFile(target, os.O_CREATE|os.O_WRONLY, 0644)
				if e == nil {
					f.Close()
				}
			}
		}
		if e != nil {
			return e
		}
		if e = syscall.Mount(from, target, "", syscall.MS_BIND, ""); e != nil {
			return e
		}
		if ro {
			return syscall.Mount("", target, "", syscall.MS_BIND|syscall.MS_REMOUNT|syscall.MS_RDONLY|syscall.MS_NOSUID|syscall.MS_NODEV, "")
		}
		return nil
	}
	for _, p := range []string{"/usr", "/lib", "/bin"} {
		if e := bind(p, p, true); e != nil {
			return e
		}
	}
	if e := bind(c.Root, "/vm", false); e != nil {
		return e
	}
	if c.ISO != "" {
		if e := bind(c.ISO, "/install.iso", true); e != nil {
			return e
		}
	}
	if _, e := os.Stat("/etc/resolv.conf"); e == nil {
		if e = bind("/etc/resolv.conf", "/etc/resolv.conf", true); e != nil {
			return e
		}
	}
	for _, p := range []string{"/tmp", "/dev", "/proc"} {
		if e := os.MkdirAll(root+p, 0755); e != nil {
			return e
		}
	}
	if e := syscall.Mount("tmpfs", root+"/tmp", "tmpfs", syscall.MS_NOSUID|syscall.MS_NODEV, "size=64m,mode=1777"); e != nil {
		return e
	}
	if e := syscall.Mount("tmpfs", root+"/dev", "tmpfs", syscall.MS_NOSUID, "size=1m,mode=755"); e != nil {
		return e
	}
	devices := map[string]uint64{"null": 1<<8 | 3, "zero": 1<<8 | 5, "random": 1<<8 | 8, "urandom": 1<<8 | 9}
	if len(c.Args) > 0 {
		for i, a := range c.Args {
			if a == "-accel" && i+1 < len(c.Args) && c.Args[i+1] == "kvm" {
				devices["kvm"] = 10<<8 | 232
			}
		}
	}
	for n, dev := range devices {
		p := root + "/dev/" + n
		if e := syscall.Mknod(p, syscall.S_IFCHR|0600, int(dev)); e != nil {
			return e
		}
		if e := os.Chown(p, int(c.Machine.UID), int(c.Machine.UID)); e != nil {
			return e
		}
	}
	if e := syscall.Mount("proc", root+"/proc", "proc", syscall.MS_NOSUID|syscall.MS_NODEV|syscall.MS_NOEXEC, ""); e != nil {
		return e
	}
	if e := syscall.Chroot(root); e != nil {
		return e
	}
	if e := os.Chdir("/vm"); e != nil {
		return e
	}
	if e := syscall.Setgroups(nil); e != nil {
		return e
	}
	if e := syscall.Setgid(int(c.Machine.UID)); e != nil {
		return e
	}
	if e := syscall.Setuid(int(c.Machine.UID)); e != nil {
		return e
	}
	if _, _, e := syscall.Syscall6(syscall.SYS_PRCTL, 38, 1, 0, 0, 0, 0); e != 0 {
		return e
	}
	tool := "qemu-system-x86_64"
	if c.ImageTool {
		tool = "qemu-img"
	}
	return syscall.Exec("/usr/bin/"+tool, append([]string{tool}, c.Args...), []string{"PATH=/usr/bin:/bin", "HOME=/vm", "LANG=C.UTF-8", "TMPDIR=/tmp"})
}
