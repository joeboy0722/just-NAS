package extensions

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Request struct {
	Token             string `json:"token"`
	ConfirmUnofficial bool   `json:"confirm_unofficial"`
	Port              int    `json:"port,omitempty"`
	BindIP            string `json:"bind_ip,omitempty"`
	MountDataset      string `json:"mount_dataset,omitempty"`
	ReadOnly          bool   `json:"read_only"`
	ReuseData         bool   `json:"reuse_data"`
}

func token() string {
	b := make([]byte, 24)
	_, e := rand.Read(b)
	if e != nil {
		panic(e)
	}
	return hex.EncodeToString(b)
}
func Import(ctx context.Context, reader io.Reader) (string, Verified, error) {
	operationMu.Lock()
	defer operationMu.Unlock()
	var v Verified
	s, e := Load()
	if e != nil {
		return "", v, e
	}
	if e = Mounted(ctx, s); e != nil {
		return "", v, e
	}
	id := token()
	name := filepath.Join(Root(s), "imports", id+".jnpkg")
	f, e := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return "", v, e
	}
	n, e := io.Copy(f, io.LimitReader(reader, MaxBundle+1))
	if se := f.Sync(); e == nil {
		e = se
	}
	if ce := f.Close(); e == nil {
		e = ce
	}
	if e != nil || n > MaxBundle {
		os.Remove(name)
		return "", v, fmt.Errorf("套件上傳失敗或過大")
	}
	keys, e := Keys()
	if e == nil {
		v, e = Verify(name, keys, "")
	}
	if e != nil {
		os.Remove(name)
		return "", v, e
	}
	meta, _ := json.Marshal(v)
	if e = os.WriteFile(filepath.Join(Root(s), "imports", id+".json"), meta, 0600); e != nil {
		os.Remove(name)
		return "", v, e
	}
	return id, v, nil
}
func Lookup(ctx context.Context, id string) (Verified, error) {
	var v Verified
	if len(id) != 48 {
		return v, fmt.Errorf("匯入識別無效")
	}
	if _, e := hex.DecodeString(id); e != nil {
		return v, e
	}
	s, e := Load()
	if e != nil {
		return v, e
	}
	if e = Mounted(ctx, s); e != nil {
		return v, e
	}
	keys, e := Keys()
	if e != nil {
		return v, e
	}
	return Verify(filepath.Join(Root(s), "imports", id+".jnpkg"), keys, "")
}
func Prepare(ctx context.Context, r Request) (string, error) {
	operationMu.Lock()
	defer operationMu.Unlock()
	s, e := Load()
	if e != nil {
		return "", e
	}
	v, e := Lookup(ctx, r.Token)
	if e != nil {
		return "", e
	}
	if !v.Official && !r.ConfirmUnofficial {
		return "", fmt.Errorf("請確認非官方套件的權限與來源")
	}
	if e = Compatible(ctx, v.Manifest); e != nil {
		return "", e
	}
	if v.Manifest.Kind == "app" {
		a := Deployment{ID: v.Manifest.ID, Package: v, Dataset: s.Dataset + "/apps/" + v.Manifest.ID, Port: r.Port, BindIP: r.BindIP, MountDataset: r.MountDataset, ReadOnly: r.ReadOnly}
		if e = validateDeployment(a); e != nil {
			return "", e
		}
	}
	id := token()
	b, _ := json.Marshal(r)
	if e = os.WriteFile(filepath.Join(Root(s), "journal", id+".request"), b, 0600); e != nil {
		return "", e
	}
	return s.Dataset + "/" + id, nil
}
func InstallTask(ctx context.Context, resource string, progress func(string)) error {
	operationMu.Lock()
	defer operationMu.Unlock()
	s, e := Load()
	if e != nil {
		return e
	}
	if e = Mounted(ctx, s); e != nil {
		return e
	}
	id := strings.TrimPrefix(resource, s.Dataset+"/")
	if len(id) != 48 || strings.Contains(id, "/") {
		return fmt.Errorf("安裝任務識別無效")
	}
	requestPath := filepath.Join(Root(s), "journal", id+".request")
	b, e := os.ReadFile(requestPath)
	if e != nil {
		return e
	}
	var r Request
	if e = strict(b, &r); e != nil {
		return e
	}
	v, e := Lookup(ctx, r.Token)
	if e != nil {
		return e
	}
	if !v.Official && !r.ConfirmUnofficial {
		return fmt.Errorf("未確認非官方套件")
	}
	if e = Compatible(ctx, v.Manifest); e != nil {
		return e
	}
	// A persistent request remains on crash; only the task engine may resume or report it interrupted.
	progress("檢查可用空間")
	b, e = Run(ctx, "zfs", "get", "-H", "-p", "-o", "value", "available", s.Dataset)
	if e != nil {
		return e
	}
	var available int64
	if _, e = fmt.Sscan(string(b), &available); e != nil {
		return e
	}
	if available < v.Manifest.InstalledBytes*2+(64<<20) {
		return fmt.Errorf("擴充儲存空間不足")
	}
	stage := filepath.Join(Root(s), "staging", id)
	if e = os.Mkdir(stage, 0700); e != nil {
		return e
	}
	defer os.RemoveAll(stage)
	progress("驗證並展開套件")
	keys, e := Keys()
	if e != nil {
		return e
	}
	if _, e = Verify(filepath.Join(Root(s), "imports", r.Token+".jnpkg"), keys, stage); e != nil {
		return e
	}
	final := packagePath(s, v)
	if e = os.MkdirAll(filepath.Dir(final), 0700); e != nil {
		return e
	}
	if _, e = os.Stat(final); os.IsNotExist(e) {
		if e = os.Rename(stage, final); e != nil {
			return e
		}
	} else if e != nil {
		return e
	}
	progress("套用並檢查服務")
	if v.Manifest.Kind == "app" {
		e = installApp(ctx, s, v, r, progress)
	} else {
		old, existed := s.Installed[v.Manifest.ID]
		if existed && old.Enabled && old.Package.Hash != v.Hash && v.Manifest.Kind != "driver" {
			if e = deactivate(ctx, s, old.Package); e != nil {
				return e
			}
		}
		status := "active"
		if v.Manifest.Kind == "driver" {
			status = "pending-reboot"
		} else {
			e = activate(ctx, s, v)
		}
		if e != nil {
			_ = deactivate(ctx, s, v)
			if existed && old.Enabled {
				_ = activate(ctx, s, old.Package)
			}
			return e
		}
		s.Installed[v.Manifest.ID] = Installation{Package: v, Enabled: true, Status: status, InstalledAt: time.Now()}
		if e = persist(StateFile, s); e != nil {
			_ = deactivate(ctx, s, v)
			if existed && old.Enabled {
				_ = activate(ctx, s, old.Package)
			}
			return e
		}
	}
	if e != nil {
		return e
	}
	os.Remove(requestPath)
	_ = Discard(ctx, r.Token)
	progress("已保存安裝結果")
	return nil
}
func installApp(ctx context.Context, s State, v Verified, r Request, progress func(string)) error {
	apps, e := LoadApps()
	if e != nil {
		return e
	}
	old, updating := apps.Apps[v.Manifest.ID]
	a := Deployment{ID: v.Manifest.ID, Package: v, Dataset: s.Dataset + "/apps/" + v.Manifest.ID, Port: r.Port, BindIP: r.BindIP, MountDataset: r.MountDataset, ReadOnly: r.ReadOnly, Enabled: true, Status: "active"}
	if e = validateDeployment(a); e != nil {
		return e
	}
	if updating {
		a.Dataset = old.Dataset
		a.Enabled = old.Enabled
	}
	root := appRoot(s, a)
	if _, e = Run(ctx, "zfs", "list", "-H", s.Dataset+"/apps"); e != nil {
		if _, e = Run(ctx, "zfs", "create", "-o", "mountpoint="+Root(s)+"/apps", s.Dataset+"/apps"); e != nil {
			return e
		}
	}
	reused := false
	if !updating {
		if _, e = Run(ctx, "zfs", "list", "-H", a.Dataset); e == nil {
			if !r.ReuseData {
				return fmt.Errorf("已有保留資料，請明確勾選使用保留資料")
			}
			b, err := Run(ctx, "zfs", "get", "-H", "-o", "value", "org.just-nas:app", a.Dataset)
			if err != nil || strings.TrimSpace(string(b)) != a.ID {
				return fmt.Errorf("資料集不是此應用管理的資料，拒絕接管")
			}
			reused = true
		} else if _, e = Run(ctx, "zfs", "create", "-o", "mountpoint="+root, "-o", "org.just-nas:app="+a.ID, a.Dataset); e != nil {
			return e
		}
	}

	if e = checkDataset(ctx, a.Dataset, root); e != nil {
		return e
	}
	if e = os.MkdirAll(root+"/content", 0755); e != nil {
		return e
	}
	if !updating && !reused {
		parts := strings.Split(v.Manifest.App.User, ":")
		if _, e = Run(ctx, "chown", parts[0]+":"+parts[1], root+"/content"); e != nil {
			return e
		}
		if e = os.WriteFile(root+"/content/index.html", []byte("<!doctype html><html lang=zh-Hant><meta charset=utf-8><title>just-NAS</title><h1>just-NAS 應用已啟動</h1></html>"), 0644); e != nil {
			return e
		}
	}
	snapshot := ""
	if updating {
		if _, e = docker(ctx, s, "stop", "--time", "15", containerName(a.ID)); e != nil {
			return e
		}
		snapshot = a.Dataset + "@nas-app-update-" + fmt.Sprint(time.Now().UnixNano())
		if _, e = Run(ctx, "zfs", "snapshot", snapshot); e != nil {
			return e
		}
	}
	rollback := func(cause error) error {
		_, _ = docker(context.Background(), s, "stop", "--time", "5", containerName(a.ID))
		_, _ = docker(context.Background(), s, "rm", containerName(a.ID))
		if updating {
			failed := a.Dataset + "-failed-" + fmt.Sprint(time.Now().UnixNano())
			// Preserve the failed generation, including its snapshots; clone the pre-update snapshot back into place.
			if _, err := Run(context.Background(), "zfs", "rename", a.Dataset, failed); err != nil {
				return fmt.Errorf("%v；保留失敗版本失敗: %w", cause, err)
			}
			if _, err := Run(context.Background(), "zfs", "set", "mountpoint=none", failed); err != nil {
				return fmt.Errorf("%v；釋放失敗資料掛載失敗: %w", cause, err)
			}
			tag := strings.SplitN(snapshot, "@", 2)[1]
			if _, err := Run(context.Background(), "zfs", "clone", "-o", "mountpoint="+root, "-o", "org.just-nas:app="+a.ID, failed+"@"+tag, a.Dataset); err != nil {
				return fmt.Errorf("%v；舊版資料恢復失敗: %w", cause, err)
			}
			if old.Enabled {
				if err := startApp(context.Background(), s, old); err != nil {
					return fmt.Errorf("%v；舊版啟動失敗: %w", cause, err)
				}
			}
		}
		return cause
	}
	progress("載入離線容器映像")
	if _, e = docker(ctx, s, "load", "--input", filepath.Join(packagePath(s, v), "image.tar")); e != nil {
		return rollback(e)
	}
	if e = startApp(ctx, s, a); e != nil {
		return rollback(e)
	}
	if !a.Enabled {
		if _, e = docker(ctx, s, "stop", "--time", "15", containerName(a.ID)); e != nil {
			return rollback(e)
		}
		a.Status = "stopped"
	}
	apps.Apps[a.ID] = a
	if e = persist("apps.json", apps); e != nil {
		return rollback(e)
	}
	return nil
}
func AppAction(ctx context.Context, id, action string, deleteData bool) error {
	operationMu.Lock()
	defer operationMu.Unlock()
	s, e := Load()
	if e != nil {
		return e
	}
	if e = Mounted(ctx, s); e != nil {
		return e
	}
	a, e := LoadApps()
	if e != nil {
		return e
	}
	v, ok := a.Apps[id]
	if !ok {
		return fmt.Errorf("找不到應用")
	}
	old := v
	switch action {
	case "start":
		if e = startApp(ctx, s, v); e != nil {
			return e
		}
		v.Enabled = true
		v.Status = "active"
		a.Apps[id] = v
	case "stop":
		if _, e = docker(ctx, s, "stop", "--time", "15", containerName(id)); e != nil {
			return e
		}
		v.Enabled = false
		v.Status = "stopped"
		a.Apps[id] = v
	case "remove":
		if _, e = docker(ctx, s, "stop", "--time", "15", containerName(id)); e != nil {
			return e
		}
		if _, e = docker(ctx, s, "rm", containerName(id)); e != nil {
			return e
		}
		delete(a.Apps, id)
	default:
		return fmt.Errorf("不支援的應用操作")
	}
	if e = persist("apps.json", a); e != nil {
		if old.Enabled {
			_ = startApp(ctx, s, old)
		}
		return e
	}
	if action == "remove" && deleteData {
		if e = checkDataset(ctx, v.Dataset, appRoot(s, v)); e != nil {
			return e
		}
		_, e = Run(ctx, "zfs", "destroy", v.Dataset)
		return e
	}
	return nil
}
func AppLogs(ctx context.Context, id string) (string, error) {
	s, e := Load()
	if e != nil {
		return "", e
	}
	a, e := LoadApps()
	if e != nil {
		return "", e
	}
	if _, ok := a.Apps[id]; !ok {
		return "", fmt.Errorf("找不到應用")
	}
	b, e := docker(ctx, s, "logs", "--tail", "150", containerName(id))
	return string(b), e
}
func Boot(ctx context.Context) error {
	booting.Store(true)
	defer booting.Store(false)
	operationMu.Lock()
	defer operationMu.Unlock()
	if b, _ := os.ReadFile("/proc/cmdline"); skipExtensions(string(b)) {
		return fmt.Errorf("救援模式：略過所有擴充")
	}
	s, e := Load()
	if e != nil || s.Dataset == "" {
		return e
	}
	if e = Mounted(ctx, s); e != nil {
		return e
	}
	var failures []string
	hasDrivers := false
	var driverError error
	for id, v := range s.Installed {
		if !v.Enabled {
			v.Status = "disabled"
			s.Installed[id] = v
			continue
		}
		if v.Package.Manifest.Kind == "driver" {
			if e = Compatible(ctx, v.Package.Manifest); e != nil {
				v.Status = "incompatible"
				v.Error = e.Error()
				s.Installed[id] = v
				failures = append(failures, e.Error())
				continue
			}
			hasDrivers = true
		}
	}
	if hasDrivers {
		if e = rebuildLayers(ctx, s, nil, ""); e != nil {
			failures = append(failures, e.Error())
			driverError = e
		}
	}
	for id, v := range s.Installed {
		if !v.Enabled {
			v.Status = "disabled"
			s.Installed[id] = v
			continue
		}
		if v.Package.Manifest.Kind == "driver" {
			if v.Status != "incompatible" {
				v.Status = "active"
				if driverError != nil {
					v.Status = "failed"
					v.Error = driverError.Error()
				}
			}
			s.Installed[id] = v
			continue
		}
		if e = activate(ctx, s, v.Package); e != nil {
			v.Status = "failed"
			v.Error = e.Error()
			failures = append(failures, id+": "+e.Error())
		} else {
			v.Status = "active"
			v.Error = ""
		}
		s.Installed[id] = v
	}
	if e = persist(StateFile, s); e != nil {
		return e
	}
	a, e := LoadApps()
	if e != nil {
		return e
	}
	for id, v := range a.Apps {
		if !v.Enabled {
			v.Status = "disabled"
			a.Apps[id] = v
			continue
		}
		if e = startApp(ctx, s, v); e != nil {
			v.Status = "failed"
			v.Error = e.Error()
			failures = append(failures, id+": "+e.Error())
		} else {
			v.Status = "active"
			v.Error = ""
		}
		a.Apps[id] = v
	}
	if e = persist("apps.json", a); e != nil {
		return e
	}
	if len(failures) > 0 {
		return fmt.Errorf("%s", strings.Join(failures, "；"))
	}
	return nil
}

// Official catalogs and downloads are HTTPS only and stay within the compiled distribution host.
const CatalogURL = "https://github.com/joeboy0722/just-NAS/releases/download/stores-v1/catalog.json"

func Download(ctx context.Context, entry CatalogEntry) (string, Verified, error) {
	var v Verified
	u, e := url.Parse(entry.URL)
	if e != nil || u.Scheme != "https" || u.Host != "github.com" || !strings.HasPrefix(u.Path, "/joeboy0722/just-NAS/releases/download/") {
		return "", v, fmt.Errorf("下載來源不受支援")
	}
	client := &http.Client{Timeout: 45 * time.Minute}
	req, e := http.NewRequestWithContext(ctx, "GET", entry.URL, nil)
	if e != nil {
		return "", v, e
	}
	res, e := client.Do(req)
	if e != nil {
		return "", v, e
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return "", v, fmt.Errorf("套件下載失敗: HTTP %d", res.StatusCode)
	}
	id, v, e := Import(ctx, io.LimitReader(res.Body, entry.Bytes+1))
	if e != nil {
		return "", v, e
	}
	if !v.Official || v.Hash != entry.SHA256 || v.Manifest.ID != entry.ID || v.Manifest.Version != entry.Version {
		_ = Discard(ctx, id)
		return "", v, fmt.Errorf("下載套件與官方目錄不符")
	}
	return id, v, nil
}
func Discard(ctx context.Context, id string) error {
	s, e := Load()
	if e != nil {
		return e
	}
	if e = Mounted(ctx, s); e != nil {
		return e
	}
	if len(id) != 48 {
		return fmt.Errorf("無效識別")
	}
	if _, e = hex.DecodeString(id); e != nil {
		return e
	}
	_ = os.Remove(filepath.Join(Root(s), "imports", id+".json"))
	return os.Remove(filepath.Join(Root(s), "imports", id+".jnpkg"))
}

// Pending metadata is for display only; installation always re-verifies the complete archive.
func Pending(ctx context.Context, apps bool) ([]map[string]any, error) {
	s, e := Load()
	if e != nil {
		return nil, e
	}
	if e = Mounted(ctx, s); e != nil {
		return nil, e
	}
	result := []map[string]any{}
	files, e := filepath.Glob(filepath.Join(Root(s), "imports", "*.json"))
	if e != nil {
		return nil, e
	}
	for _, name := range files {
		id := strings.TrimSuffix(filepath.Base(name), ".json")
		if len(id) != 48 {
			continue
		}
		if _, e = os.Stat(filepath.Join(Root(s), "imports", id+".jnpkg")); e != nil {
			continue
		}
		f, err := os.Open(name)
		if err != nil {
			continue
		}
		raw, err := io.ReadAll(io.LimitReader(f, 1<<20))
		f.Close()
		if err != nil {
			continue
		}
		var v Verified
		if strict(raw, &v) != nil || v.Manifest.Validate() != nil {
			continue
		}
		if apps == (v.Manifest.Kind == "app") {
			result = append(result, map[string]any{"token": id, "package": v})
		}
	}
	return result, nil
}

// RestoreServices is used after an aborted maintenance operation, not as a second boot.
// In particular it must never mark a pending driver disable complete while its layer is still mounted.
func RestoreServices(ctx context.Context) error {
	operationMu.Lock()
	defer operationMu.Unlock()
	s, e := Load()
	if e != nil || s.Dataset == "" {
		return e
	}
	b, e := Run(ctx, "zfs", "get", "-H", "-o", "value", "guid", s.Dataset)
	if e != nil || strings.TrimSpace(string(b)) != s.GUID {
		return fmt.Errorf("擴充資料集識別不符，停止服務恢復")
	}
	if _, e = Run(ctx, "zfs", "mount", "-a"); e != nil {
		return e
	}
	if e = Mounted(ctx, s); e != nil {
		return e
	}
	failures := []string{}
	for id, v := range s.Installed {
		if !v.Enabled || v.Package.Manifest.Kind == "driver" {
			continue
		}
		if e = activate(ctx, s, v.Package); e != nil {
			v.Status = "failed"
			v.Error = e.Error()
			failures = append(failures, id+": "+e.Error())
		} else {
			v.Status = "active"
			v.Error = ""
		}
		s.Installed[id] = v
	}
	if e = persist(StateFile, s); e != nil {
		return e
	}
	a, e := LoadApps()
	if e != nil {
		return e
	}
	for id, v := range a.Apps {
		if !v.Enabled {
			continue
		}
		if e = startApp(ctx, s, v); e != nil {
			v.Status = "failed"
			v.Error = e.Error()
			failures = append(failures, id+": "+e.Error())
		} else {
			v.Status = "active"
			v.Error = ""
		}
		a.Apps[id] = v
	}
	if e = persist("apps.json", a); e != nil {
		return e
	}
	if len(failures) > 0 {
		return fmt.Errorf("%s", strings.Join(failures, "；"))
	}
	return nil
}

func skipExtensions(cmdline string) bool {
	for _, field := range strings.Fields(cmdline) {
		if field == "just_nas_no_extensions=1" {
			return true
		}
	}
	return false
}
