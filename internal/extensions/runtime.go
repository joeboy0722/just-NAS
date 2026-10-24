package extensions

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
	"regexp"
	"sort"
	"strings"
	"time"
)

func runCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = 3 * time.Second
	var out cappedBuffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if e := cmd.Run(); e != nil {
		return nil, fmt.Errorf("%s: %s: %w", name, out.String(), e)
	}
	return out.Bytes(), nil
}

type cappedBuffer struct{ bytes.Buffer }

func (b *cappedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if b.Len() < 65536 {
		keep := 65536 - b.Len()
		if len(p) > keep {
			p = p[:keep]
		}
		_, _ = b.Buffer.Write(p)
	}
	return n, nil
}
func packagePath(s State, v Verified) string {
	return filepath.Join(Root(s), "packages", v.Manifest.ID, v.Hash)
}
func apkPaths(s State, v Verified) ([]string, error) {
	entries, e := os.ReadDir(filepath.Join(packagePath(s, v), "apks"))
	if e != nil {
		return nil, e
	}
	files := []string{}
	for _, f := range entries {
		if !f.IsDir() && strings.HasSuffix(f.Name(), ".apk") {
			files = append(files, filepath.Join(packagePath(s, v), "apks", f.Name()))
		}
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("套件缺少離線 APK")
	}
	return files, nil
}
func installAPKs(ctx context.Context, s State, v Verified) error {
	if !v.Official {
		return fmt.Errorf("原生能力需官方簽章")
	}
	files, e := apkPaths(s, v)
	if e != nil {
		return e
	}
	installed, err := installedPackages()
	if err != nil {
		return err
	}
	if len(s.ProtectedPackages) == 0 {
		return fmt.Errorf("核心套件保護清單缺失，拒絕安裝原生能力")
	}
	selected := []string{}
	for _, file := range files {
		b, err := Run(ctx, "tar", "-xzOf", file, ".PKGINFO")
		if err != nil {
			return err
		}
		name, version := "", ""
		for _, line := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(line, "pkgname = ") {
				name = strings.TrimPrefix(line, "pkgname = ")
			}
			if strings.HasPrefix(line, "pkgver = ") {
				version = strings.TrimPrefix(line, "pkgver = ")
			}
		}
		if name == "" || version == "" {
			return fmt.Errorf("離線 APK 缺少套件資訊")
		}
		if old, ok := installed[name]; ok {
			if old == version {
				continue
			}
			if s.ProtectedPackages[name] {
				return fmt.Errorf("擴充需要不同版本核心套件 %s，請改用相容套件", name)
			}
		}
		selected = append(selected, file)
	}
	if len(selected) == 0 {
		return nil
	}
	cache := filepath.Join(Root(s), "apk-cache")
	if e = os.MkdirAll(cache, 0700); e != nil {
		return e
	}
	args := []string{"add", "--no-network", "--cache-dir", cache, "--cache-packages", "--virtual", ".just-nas-" + v.Manifest.ID}
	args = append(args, selected...)
	_, e = Run(ctx, "apk", args...)
	return e
}
func activate(ctx context.Context, s State, v Verified) error {
	if e := Mounted(ctx, s); e != nil {
		return e
	}
	switch v.Manifest.Kind {
	case "driver":
		return rebuildLayers(ctx, s, &v, "")
	case "runtime":
		if e := installAPKs(ctx, s, v); e != nil {
			return e
		}
		ds := s.Dataset + "/engine"
		root := Root(s) + "/engine"
		if _, e := Run(ctx, "zfs", "list", "-H", ds); e != nil {
			if _, e = Run(ctx, "zfs", "create", "-o", "mountpoint="+root, ds); e != nil {
				return e
			}
		}
		if e := checkDataset(ctx, ds, root); e != nil {
			return e
		}
		if _, e := Run(ctx, "rc-service", "cgroups", "start"); e != nil {
			return e
		}
		// Explicitly use the classic ZFS backend; do not inherit Docker 29's containerd default.
		cfg := map[string]any{"data-root": root, "storage-driver": "zfs", "features": map[string]bool{"containerd-snapshotter": false}, "hosts": []string{"unix:///run/just-nas-docker.sock"}, "log-driver": "json-file", "log-opts": map[string]string{"max-size": "5m", "max-file": "2"}, "live-restore": false}
		b, _ := json.Marshal(cfg)
		if e := os.MkdirAll("/run/just-nas", 0700); e != nil {
			return e
		}
		if e := os.WriteFile("/run/just-nas/docker.json", b, 0600); e != nil {
			return e
		}
		// start-stop-daemon owns only our private daemon; never alter an unrelated daemon.
		if _, e := docker(ctx, s, "info", "--format", "{{.Driver}}"); e != nil {
			if _, e = Run(ctx, "start-stop-daemon", "--start", "--background", "--make-pidfile", "--pidfile", "/run/just-nas/dockerd.pid", "--stdout", "/run/just-nas/dockerd.log", "--stderr", "/run/just-nas/dockerd.log", "--exec", "/usr/bin/dockerd", "--", "--config-file=/run/just-nas/docker.json"); e != nil {
				return e
			}
		}
		for i := 0; i < 40; i++ {
			b, e := docker(ctx, s, "info", "--format", "{{.Driver}}")
			if e == nil {
				if strings.TrimSpace(string(b)) != "zfs" {
					return fmt.Errorf("容器儲存後端不是 ZFS")
				}
				return nil
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Second):
			}
		}
		logs, _ := os.ReadFile("/run/just-nas/dockerd.log")
		if len(logs) > 8192 {
			logs = logs[len(logs)-8192:]
		}
		return fmt.Errorf("容器引擎啟動失敗: %s", string(logs))
	case "ssh":
		if sshRunning() {
			return nil
		}
		if e := installAPKs(ctx, s, v); e != nil {
			return e
		}
		// SSH is key-only. Persist host keys in the selected extension dataset.
		key := filepath.Join(Root(s), "ssh-host-ed25519")
		if _, e := os.Stat(key); os.IsNotExist(e) {
			if _, e = Run(ctx, "ssh-keygen", "-t", "ed25519", "-N", "", "-f", key); e != nil {
				return e
			}
		}
		if s.SSHPublicKey != "" {
			if e := os.WriteFile(filepath.Join(Root(s), "authorized_keys"), []byte(s.SSHPublicKey+"\n"), 0600); e != nil {
				return e
			}
		}
		conf := "AllowUsers root\nHostKey " + key + "\nPasswordAuthentication no\nKbdInteractiveAuthentication no\nPermitRootLogin prohibit-password\nAuthorizedKeysFile " + filepath.Join(Root(s), "authorized_keys") + "\nSubsystem sftp internal-sftp\n"
		if e := os.MkdirAll("/run/just-nas", 0700); e != nil {
			return e
		}
		if e := os.WriteFile("/run/just-nas/sshd_config", []byte(conf), 0600); e != nil {
			return e
		}
		if _, e := os.Stat(filepath.Join(Root(s), "authorized_keys")); e != nil {
			return fmt.Errorf("請先設定 SSH 公鑰後啟用")
		}
		_, e := Run(ctx, "/usr/sbin/sshd", "-f", "/run/just-nas/sshd_config", "-o", "PidFile=/run/just-nas/sshd.pid")
		return e
	}
	return fmt.Errorf("此套件不是系統擴充")
}
func deactivate(ctx context.Context, s State, v Verified) error {
	switch v.Manifest.Kind {
	case "driver":
		return fmt.Errorf("驅動停用需下次開機套用，請使用停用排程")
	case "runtime":
		if !serviceRunning("/run/just-nas/dockerd.pid", "dockerd") {
			return nil
		}
		_, e := Run(ctx, "start-stop-daemon", "--stop", "--retry", "TERM/20/KILL/5", "--pidfile", "/run/just-nas/dockerd.pid", "--exec", "/usr/bin/dockerd")
		return e
	case "ssh":
		if !sshRunning() {
			return nil
		}
		_, e := Run(ctx, "start-stop-daemon", "--stop", "--retry", "TERM/10", "--pidfile", "/run/just-nas/sshd.pid", "--exec", "/usr/sbin/sshd")
		return e
	}
	return nil
}
func rebuildLayers(ctx context.Context, s State, extra *Verified, exclude string) error {
	// Overlay only immutable files; upper/work and depmod metadata stay small in /run.
	// /lib/modules is supplied by Alpine modloop. Preserve a bind of the original before overlaying it.
	const base = "/run/just-nas/base-modules"
	if e := os.MkdirAll(base, 0755); e != nil {
		return e
	}
	if _, e := Run(ctx, "mountpoint", "-q", base); e != nil {
		if _, e = Run(ctx, "mount", "--bind", "/lib/modules", base); e != nil {
			return e
		}
	}
	layers := []string{}
	ids := []string{}
	for id, v := range s.Installed {
		if id != exclude && v.Enabled && v.Status != "incompatible" && v.Package.Manifest.Kind == "driver" {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	add := func(v Verified) error {
		if e := Compatible(ctx, v.Manifest); e != nil {
			return e
		}
		layers = append(layers, filepath.Join(packagePath(s, v), "modules"))
		return nil
	}
	for _, id := range ids {
		if extra != nil && id == extra.Manifest.ID {
			continue
		}
		if e := add(s.Installed[id].Package); e != nil {
			return e
		}
	}
	if extra != nil {
		if e := add(*extra); e != nil {
			return e
		}
	}
	layers = append(layers, base)
	upper := "/run/just-nas/modules-upper"
	work := "/run/just-nas/modules-work"
	merged := "/run/just-nas/modules"
	for _, d := range []string{upper, work, merged} {
		if e := os.MkdirAll(d, 0755); e != nil {
			return e
		}
	}
	if _, e := Run(ctx, "mountpoint", "-q", merged); e == nil {
		return fmt.Errorf("驅動層已有啟用版本，變更需重新開機")
	}
	if _, e := Run(ctx, "mount", "-t", "overlay", "overlay", "-o", "lowerdir="+strings.Join(layers, ":")+",upperdir="+upper+",workdir="+work, merged); e != nil {
		return e
	}
	if _, e := Run(ctx, "mount", "--bind", merged, "/lib/modules"); e != nil {
		return e
	}
	b, e := Run(ctx, "uname", "-r")
	if e != nil {
		return e
	}
	if _, e = Run(ctx, "depmod", "-a", strings.TrimSpace(string(b))); e != nil {
		return e
	}
	_, e = Run(ctx, "rc-service", "hwdrivers", "restart")
	return e
}
func docker(ctx context.Context, s State, args ...string) ([]byte, error) {
	if e := Mounted(ctx, s); e != nil {
		return nil, e
	}
	return Run(ctx, "docker", append([]string{"--host", "unix:///run/just-nas-docker.sock"}, args...)...)
}
func checkDataset(ctx context.Context, ds, root string) error {
	b, e := Run(ctx, "zfs", "get", "-H", "-o", "value", "mounted,mountpoint", ds)
	if e != nil {
		return e
	}
	if strings.TrimSpace(string(b)) != "yes\n"+root {
		return fmt.Errorf("應用資料集尚未正確掛載")
	}
	b, e = Run(ctx, "findmnt", "-n", "-o", "SOURCE", "--target", root)
	if e != nil || strings.TrimSpace(string(b)) != ds {
		return fmt.Errorf("應用資料掛載識別不符")
	}
	return nil
}
func containerName(id string) string { return "just-nas-" + id }
func validateDeployment(a Deployment) error {
	if !validStoreDataset(strings.Split(a.Dataset, "/apps/")[0]) || a.Dataset != strings.Split(a.Dataset, "/apps/")[0]+"/apps/"+a.ID || a.Port < 1024 || a.Port > 65535 {
		return fmt.Errorf("應用資料集或連接埠無效")
	}
	ip := net.ParseIP(a.BindIP)
	if ip == nil || !ip.IsPrivate() || ip.IsUnspecified() {
		return fmt.Errorf("請選擇 NAS 的區網 IP")
	}
	if a.MountDataset != "" {
		if !validDataDataset(a.MountDataset) {
			return fmt.Errorf("授權資料空間無效")
		}
	}
	return nil
}

var regexpDataset = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$`)

func validDataDataset(ds string) bool {
	if strings.ContainsAny(ds, "@\\\x00") || !strings.Contains(ds, "/") {
		return false
	}
	for _, v := range strings.Split(ds, "/") {
		if !regexpDataset.MatchString(v) || strings.HasPrefix(v, ".") {
			return false
		}
	}
	return true
}
func appRoot(s State, a Deployment) string { return Root(s) + "/apps/" + a.ID }
func startApp(ctx context.Context, s State, a Deployment) error {
	if e := validateDeployment(a); e != nil {
		return e
	}
	if e := Compatible(ctx, a.Package.Manifest); e != nil {
		return e
	}
	root := appRoot(s, a)
	if e := checkDataset(ctx, a.Dataset, root); e != nil {
		return e
	}
	// A missing image is an error, never an implicit network pull.
	if _, e := docker(ctx, s, "image", "inspect", a.Package.Manifest.App.Image); e != nil {
		return e
	}
	addrs, e := net.InterfaceAddrs()
	if e != nil {
		return e
	}
	local := false
	for _, v := range addrs {
		ip, _, _ := net.ParseCIDR(v.String())
		if ip != nil && ip.String() == a.BindIP {
			local = true
		}
	}
	if !local {
		return fmt.Errorf("選定 IP 不在本機介面")
	}
	content := root + "/content"
	if a.MountDataset != "" {
		b, e := Run(ctx, "zfs", "get", "-H", "-o", "value", "mounted,mountpoint", a.MountDataset)
		if e != nil {
			return e
		}
		v := strings.Split(strings.TrimSpace(string(b)), "\n")
		if len(v) != 2 || v[0] != "yes" || !strings.HasPrefix(v[1], "/mnt/") {
			return fmt.Errorf("授權資料集尚未掛載")
		}
		content = v[1]
		if strings.ContainsAny(content, ",\n\r") {
			return fmt.Errorf("資料空間掛載路徑不適用容器掛載")
		}
		user := strings.Split(a.Package.Manifest.App.User, ":")
		if _, err := Run(ctx, "setpriv", "--reuid", user[0], "--regid", user[1], "--clear-groups", "test", "-r", content); err != nil {
			return fmt.Errorf("既有資料空間的檔案權限未允許應用讀取；請先調整授權或使用應用自己的資料夾")
		}
		if !a.ReadOnly {
			if _, err := Run(ctx, "setpriv", "--reuid", user[0], "--regid", user[1], "--clear-groups", "test", "-w", content); err != nil {
				return fmt.Errorf("既有資料空間未允許應用寫入")
			}
		}
		if e = checkDataset(ctx, a.MountDataset, content); e != nil {
			return e
		}
	}
	if _, e := os.Lstat(content); e != nil {
		return e
	}
	// Remove only a stopped container with our own ownership label.
	if b, e := docker(ctx, s, "inspect", "--format", "{{index .Config.Labels \"org.just-nas.app\"}} {{.State.Running}}", containerName(a.ID)); e == nil {
		v := strings.Fields(string(b))
		if len(v) != 2 || v[0] != a.ID {
			return fmt.Errorf("容器名稱被其他服務使用")
		}
		if v[1] == "true" {
			return nil
		}
		if _, e = docker(ctx, s, "rm", containerName(a.ID)); e != nil {
			return e
		}
	}
	l, e := net.Listen("tcp", net.JoinHostPort(a.BindIP, fmt.Sprint(a.Port)))
	if e != nil {
		return fmt.Errorf("連接埠已使用: %w", e)
	}
	_ = l.Close()
	spec := a.Package.Manifest.App
	args := []string{"run", "-d", "--pull=never", "--name", containerName(a.ID), "--label", "org.just-nas.app=" + a.ID, "--restart=no", "--user", spec.User, "--cap-drop=ALL", "--security-opt=no-new-privileges", "--read-only", "--pids-limit=128", "--memory", fmt.Sprint(spec.MemoryMB) + "m", "--cpus", fmt.Sprint(spec.CPUs), "--log-driver=json-file", "--log-opt=max-size=5m", "--log-opt=max-file=2", "--tmpfs", "/tmp:rw,noexec,nosuid,size=32m", "--publish", net.JoinHostPort(a.BindIP, fmt.Sprint(a.Port)) + ":" + fmt.Sprint(spec.ContainerPort)}
	mode := "rw"
	if a.ReadOnly {
		mode = "ro"
	}
	args = append(args, "--mount", "type=bind,src="+content+",dst=/srv/content"+map[bool]string{true: ",readonly", false: ""}[a.ReadOnly])
	_ = mode
	args = append(args, spec.Image)
	if _, e = docker(ctx, s, args...); e != nil {
		return e
	}
	client := &http.Client{Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	for i := 0; i < 20; i++ {
		req, _ := http.NewRequestWithContext(ctx, "GET", "http://"+net.JoinHostPort(a.BindIP, fmt.Sprint(a.Port))+"/", nil)
		res, err := client.Do(req)
		if err == nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 4096))
			res.Body.Close()
			if res.StatusCode >= 200 && res.StatusCode < 400 {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	_, _ = docker(context.Background(), s, "stop", "--time", "5", containerName(a.ID))
	return fmt.Errorf("應用健康檢查失敗")
}

// StopAll releases only NAS-owned services and driver overlays before pool export.
func StopAll(ctx context.Context) error {
	operationMu.Lock()
	defer operationMu.Unlock()
	s, e := Load()
	if e != nil {
		return e
	}
	if s.Dataset == "" {
		return nil
	}
	if err := Mounted(ctx, s); err != nil {
		// A cold boot with a missing optional pool has nothing to stop and must not block core shutdown.
		if !serviceRunning("/run/just-nas/dockerd.pid", "dockerd") && !sshRunning() {
			if _, mountErr := Run(ctx, "mountpoint", "-q", "/run/just-nas/modules"); mountErr != nil {
				return nil
			}
		}
		return err
	}
	a, e := LoadApps()
	if e != nil {
		return e
	}
	for _, v := range a.Apps {
		if v.Enabled {
			if _, e = docker(ctx, s, "stop", "--time", "15", containerName(v.ID)); e != nil {
				return e
			}
		}
	}
	for _, v := range s.Installed {
		if v.Enabled && v.Package.Manifest.Kind != "driver" {
			if e = deactivate(ctx, s, v.Package); e != nil {
				return e
			}
		}
	}
	if _, e = Run(ctx, "mountpoint", "-q", "/run/just-nas/modules"); e == nil {
		for _, target := range []string{"/lib/modules", "/run/just-nas/modules", "/run/just-nas/base-modules"} {
			if _, e = Run(ctx, "umount", target); e != nil {
				return e
			}
		}
	}
	return nil
}

func installedPackages() (map[string]string, error) {
	b, e := os.ReadFile("/lib/apk/db/installed")
	if e != nil {
		return nil, e
	}
	result := map[string]string{}
	name := ""
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "P:") {
			name = strings.TrimPrefix(line, "P:")
		}
		if strings.HasPrefix(line, "V:") && name != "" {
			result[name] = strings.TrimPrefix(line, "V:")
		}
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("讀取核心套件清單失敗")
	}
	return result, nil
}
