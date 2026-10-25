package extensions

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const NativeUID = 900001

var nativeTransitions sync.Map

func NativeTransitionBusy(id string) bool { _, ok := nativeTransitions.Load(id); return ok }

func ensureNativeIdentity(ctx context.Context) error {
	const name = "nas-app-vm"
	for _, file := range []string{"/etc/passwd", "/etc/group"} {
		raw, e := os.ReadFile(file)
		if e != nil {
			return e
		}
		for _, line := range strings.Split(string(raw), "\n") {
			f := strings.Split(line, ":")
			if len(f) > 2 && f[2] == fmt.Sprint(NativeUID) && f[0] != name {
				return fmt.Errorf("原生應用專用身分已被使用")
			}
			if len(f) > 2 && f[0] == name && f[2] != fmt.Sprint(NativeUID) {
				return fmt.Errorf("原生應用帳號 UID 不符")
			}
		}
	}
	if _, e := Run(ctx, "id", "-u", name); e == nil {
		return nil
	}
	if _, e := Run(ctx, "getent", "group", name); e != nil {
		if _, e = Run(ctx, "addgroup", "-g", fmt.Sprint(NativeUID), name); e != nil {
			return e
		}
	}
	_, e := Run(ctx, "adduser", "-D", "-H", "-s", "/sbin/nologin", "-u", fmt.Sprint(NativeUID), "-G", name, name)
	return e
}

func nativeDir(id string) string    { return "/run/just-nas-native/" + id }
func NativeSocket(id string) string { return nativeDir(id) + "/web/service.sock" }
func nativePID(id, mode string) string {
	if mode == "web" {
		return nativeDir(id) + "/web/service.pid"
	}
	return nativeDir(id) + "/broker.pid"
}
func nativeLog(id, mode string) string {
	if mode == "web" {
		return nativeDir(id) + "/web/service.log"
	}
	return nativeDir(id) + "/broker.log"
}

type NativeJob struct {
	ID       string `json:"id"`
	State    string `json:"state"`
	Progress string `json:"progress"`
	Error    string `json:"error"`
}

func NativeJobs(ctx context.Context, id, job string) ([]NativeJob, error) {
	path := "/api/tasks"
	if job != "" {
		if len(job) != 32 || strings.Trim(job, "abcdef0123456789") != "" {
			return nil, fmt.Errorf("原生任務識別無效")
		}
		path += "/" + job
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", "http://native"+path, nil)
	res, e := nativeClient(id, false).Do(req)
	if e != nil {
		return nil, e
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("原生任務查詢失敗")
	}
	decoder := json.NewDecoder(io.LimitReader(res.Body, 1<<20))
	if job != "" {
		var j NativeJob
		e = decoder.Decode(&j)
		return []NativeJob{j}, e
	}
	var jobs []NativeJob
	e = decoder.Decode(&jobs)
	return jobs, e
}
func nativeClient(id string, broker bool) *http.Client {
	socket := NativeSocket(id)
	if broker {
		socket = nativeDir(id) + "/broker.sock"
	}
	return &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}, Timeout: 75 * time.Second}
}
func nativeStatus(ctx context.Context, a Deployment) (string, string) {
	req, _ := http.NewRequestWithContext(ctx, "GET", "http://native/health", nil)
	res, e := nativeClient(a.ID, false).Do(req)
	if e != nil {
		if !a.Enabled {
			return "stopped", ""
		}
		return "failed", "原生應用服務未回應"
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return "failed", "原生應用健康檢查失敗"
	}
	return "active", ""
}
func stopApplication(ctx context.Context, s State, a Deployment) error {
	if a.Package.Manifest.IsNative() {
		return stopNative(ctx, a)
	}
	_, e := docker(ctx, s, "stop", "--time", "15", containerName(a.ID))
	return e
}
func stopNative(ctx context.Context, a Deployment) error {
	if !serviceRunning(nativeDir(a.ID)+"/broker.pid", a.ID) {
		// A crashed broker must not strand live QEMU guests during uninstall.
		entries, e := os.ReadDir("/proc")
		if e != nil {
			return e
		}
		for _, entry := range entries {
			raw, _ := os.ReadFile("/proc/" + entry.Name() + "/cmdline")
			args := strings.Split(string(raw), "\x00")
			if len(args) > 2 && args[0] == "qemu-system-x86_64" && args[1] == "-name" && strings.HasPrefix(args[2], "just-nas-vm-") {
				return fmt.Errorf("本機管理代理未回應且客體仍存在；請先啟動應用恢復程序管理")
			}
		}
	} else {
		req, _ := http.NewRequestWithContext(ctx, "POST", "http://native/control/prepare-stop", strings.NewReader("{}"))
		res, e := nativeClient(a.ID, true).Do(req)
		if e != nil {
			return e
		}
		b, _ := io.ReadAll(io.LimitReader(res.Body, 8192))
		res.Body.Close()
		if res.StatusCode != 200 {
			return fmt.Errorf("%s", b)
		}
	}
	for _, mode := range []string{"web", "broker"} {
		if !serviceRunning(nativePID(a.ID, mode), a.ID) {
			continue
		}
		if _, e := Run(ctx, "start-stop-daemon", "--stop", "--retry", "TERM/10", "--pidfile", nativePID(a.ID, mode), "--exec", nativeDir(a.ID)+"/"+a.ID); e != nil {
			return e
		}
	}
	return nil
}
func startNative(ctx context.Context, s State, a Deployment) error {
	if e := ensureNativeIdentity(ctx); e != nil {
		return e
	}
	if e := validateDeployment(a); e != nil {
		return e
	}
	if e := Mounted(ctx, s); e != nil {
		return e
	}
	if e := Compatible(ctx, a.Package.Manifest); e != nil {
		return e
	}
	if e := checkDataset(ctx, a.Dataset, appRoot(s, a)); e != nil {
		return e
	}
	if status, _ := nativeStatus(ctx, a); status == "active" {
		return nil
	}
	dir := nativeDir(a.ID)
	if e := os.MkdirAll("/run/just-nas-native", 0755); e != nil {
		return e
	}
	if e := os.MkdirAll(dir+"/web", 0750); e != nil {
		return e
	}
	if e := os.Chown(dir, 0, NativeUID); e != nil {
		return e
	}
	if e := os.Chmod(dir, 0750); e != nil {
		return e
	}
	if e := os.Chown(dir+"/web", NativeUID, NativeUID); e != nil {
		return e
	}
	// Verify the exact executable before copying it out of the root-only package cache.
	root, e := os.OpenRoot(packagePath(s, a.Package))
	if e != nil {
		return e
	}
	defer root.Close()
	src, e := root.Open("bin/" + a.ID)
	if e != nil {
		return e
	}
	defer src.Close()
	tmp := dir + "/" + a.ID + ".new"
	dst, e := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0555)
	if e != nil {
		return e
	}
	hash := sha256.New()
	_, e = io.Copy(io.MultiWriter(dst, hash), src)
	dst.Close()
	if e != nil {
		return e
	}
	if hex.EncodeToString(hash.Sum(nil)) != a.Package.Manifest.Files["bin/"+a.ID].SHA256 {
		os.Remove(tmp)
		return fmt.Errorf("原生服務執行檔校驗失敗")
	}
	brokerAlive := serviceRunning(dir+"/broker.pid", a.ID)
	if brokerAlive {
		current, e := os.ReadFile(dir + "/" + a.ID)
		if e != nil {
			return e
		}
		sum := sha256.Sum256(current)
		if hex.EncodeToString(sum[:]) != a.Package.Manifest.Files["bin/"+a.ID].SHA256 {
			return fmt.Errorf("舊版管理服務尚未停止，拒絕更換執行檔")
		}
		os.Remove(tmp)
	} else {
		if serviceRunning(nativePID(a.ID, "web"), a.ID) {
			if _, e = Run(ctx, "start-stop-daemon", "--stop", "--retry", "TERM/10", "--pidfile", nativePID(a.ID, "web"), "--exec", dir+"/"+a.ID); e != nil {
				return e
			}
		}
		if e = os.Rename(tmp, dir+"/"+a.ID); e != nil {
			return e
		}
	}
	for _, mode := range []string{"broker", "web"} {
		if serviceRunning(nativePID(a.ID, mode), a.ID) {
			continue
		}
		sock := dir + "/broker.sock"
		if mode == "web" {
			sock = NativeSocket(a.ID)
		}
		args := []string{"--start", "--background", "--make-pidfile", "--pidfile", nativePID(a.ID, mode), "--stdout", nativeLog(a.ID, mode), "--stderr", nativeLog(a.ID, mode)}
		if mode == "web" {
			args = append(args, "--user", "nas-app-vm:nas-app-vm")
		}
		args = append(args, "--exec", dir+"/"+a.ID, "--", "--mode", mode, "--root", appRoot(s, a), "--socket", sock, "--broker", dir+"/broker.sock")
		if _, e = Run(ctx, "start-stop-daemon", args...); e != nil {
			return e
		}
		if mode == "broker" {
			for n := 0; n < 50; n++ {
				if _, e = os.Stat(sock); e == nil {
					break
				}
				time.Sleep(100 * time.Millisecond)
			}
		}
	}
	for n := 0; n < 50; n++ {
		if status, _ := nativeStatus(ctx, a); status == "active" {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	return fmt.Errorf("原生應用啟動後未通過健康檢查")
}
func installNative(ctx context.Context, s State, v Verified, r Request, progress func(string)) error {
	nativeTransitions.Store(v.Manifest.ID, true)
	defer nativeTransitions.Delete(v.Manifest.ID)
	if !v.Official {
		return fmt.Errorf("原生應用須官方簽章")
	}
	apps, e := LoadApps()
	if e != nil {
		return e
	}
	old, updating := apps.Apps[v.Manifest.ID]
	recoverPreparation := func(cause error) error {
		if updating && old.Enabled {
			if err := startNative(ctx, s, old); err != nil {
				return fmt.Errorf("%v；舊版服務重啟失敗: %w", cause, err)
			}
		}
		return cause
	}
	a := Deployment{ID: v.Manifest.ID, Package: v, Dataset: s.Dataset + "/apps/" + v.Manifest.ID, Enabled: true, Status: "active"}
	if updating {
		a.Dataset = old.Dataset
		a.Enabled = old.Enabled
		if !old.Package.Manifest.IsNative() {
			return fmt.Errorf("不能變更應用執行類型")
		}
		if e = stopNative(ctx, old); e != nil {
			return e
		}
	}
	if _, e = Run(ctx, "zfs", "list", "-H", s.Dataset+"/apps"); e != nil {
		if _, e = Run(ctx, "zfs", "create", "-o", "mountpoint="+Root(s)+"/apps", s.Dataset+"/apps"); e != nil {
			return recoverPreparation(e)
		}
	}
	if !updating {
		if _, e = Run(ctx, "zfs", "list", "-H", a.Dataset); e == nil {
			b, err := Run(ctx, "zfs", "get", "-H", "-o", "value", "org.just-nas:app", a.Dataset)
			if !r.ReuseData || err != nil || strings.TrimSpace(string(b)) != a.ID {
				return fmt.Errorf("已有保留資料；需確認使用此應用資料")
			}
		} else if _, e = Run(ctx, "zfs", "create", "-o", "mountpoint="+appRoot(s, a), "-o", "org.just-nas:app="+a.ID, a.Dataset); e != nil {
			return e
		}
	}
	snapshot := ""
	var stateBackup json.RawMessage
	if updating {
		stateBackup, e = nativeStateBackup(a.ID)
		if e != nil {
			return recoverPreparation(e)
		}
		// All guests are now stopped. Preserve the entire owned VM dataset tree,
		// including per-guest disks/UEFI variables, before starting a new binary.
		for _, ds := range NativeReferences(a.ID) {
			marker, err := Run(ctx, "zfs", "get", "-H", "-o", "value", "org.just-nas:vm-store", ds)
			if err != nil || strings.TrimSpace(string(marker)) != a.ID {
				return recoverPreparation(fmt.Errorf("應用資料識別失效，停止更新"))
			}
			if _, err = Run(ctx, "zfs", "snapshot", "-r", ds+"@nas-app-update-"+fmt.Sprint(time.Now().UnixNano())); err != nil {
				return recoverPreparation(err)
			}
		}
		snapshot = a.Dataset + "@nas-app-update-" + fmt.Sprint(time.Now().UnixNano())
		if _, e = Run(ctx, "zfs", "snapshot", snapshot); e != nil {
			return recoverPreparation(e)
		}
	}
	rollback := func(cause error) error {
		if err := stopNative(ctx, a); err != nil {
			return fmt.Errorf("%v；新版服務未安全停止，保留資料等待處理: %w", cause, err)
		}
		if updating {
			if len(stateBackup) > 0 {
				if err := SaveNativeState(a.ID, stateBackup); err != nil {
					return fmt.Errorf("%v；設定回復失敗: %w", cause, err)
				}
			}
			failed := a.Dataset + "-failed-" + fmt.Sprint(time.Now().UnixNano())
			_, e = Run(ctx, "zfs", "rename", a.Dataset, failed)
			if e == nil {
				_, e = Run(ctx, "zfs", "set", "mountpoint=none", failed)
			}
			if e == nil {
				_, e = Run(ctx, "zfs", "clone", "-o", "mountpoint="+appRoot(s, a), "-o", "org.just-nas:app="+a.ID, failed+"@"+strings.Split(snapshot, "@")[1], a.Dataset)
			}
			if e != nil {
				return fmt.Errorf("%v；舊版資料回復失敗: %w", cause, e)
			}
			if old.Enabled {
				if e = startNative(ctx, s, old); e != nil {
					return fmt.Errorf("%v；舊版服務回復失敗: %w", cause, e)
				}
			}
		}
		return cause
	}
	progress("啟動受管理原生應用")
	if a.Enabled {
		if e = startNative(ctx, s, a); e != nil {
			return rollback(e)
		}
	} else {
		a.Status = "stopped"
	}
	apps.Apps[a.ID] = a
	if e = persist("apps.json", apps); e != nil {
		return rollback(e)
	}
	return nil
}
func ApplicationReferences(s State, a Deployment, pool string) bool {
	if strings.HasPrefix(s.Dataset, pool+"/") || strings.HasPrefix(a.Dataset, pool+"/") || strings.HasPrefix(a.MountDataset, pool+"/") {
		return true
	}
	if a.Package.Manifest.IsNative() {
		for _, ds := range NativeReferences(a.ID) {
			if strings.HasPrefix(ds, pool+"/") {
				return true
			}
		}
		b, e := os.ReadFile(filepath.Join(appRoot(s, a), "data-references.json"))
		if e == nil && len(b) < 8192 {
			var ds []string
			if json.Unmarshal(b, &ds) == nil {
				for _, d := range ds {
					if strings.HasPrefix(d, pool+"/") {
						return true
					}
				}
			}
		}
	}
	return false
}
