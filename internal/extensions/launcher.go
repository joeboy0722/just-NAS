package extensions

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	_ "golang.org/x/image/webp"
	"image"
	_ "image/png"
	"io"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Images are decoded only after the bounded header has passed size checks.
func validateIcon(b []byte, name string) (string, error) {
	if len(b) == 0 || len(b) > 256<<10 {
		return "", fmt.Errorf("圖標超過大小限制")
	}
	c, format, e := image.DecodeConfig(bytes.NewReader(b))
	if e != nil || c.Width < 1 || c.Height < 1 || c.Width > 512 || c.Height > 512 || (format != "png" && format != "webp") || filepath.Ext(name) != "."+format {
		return "", fmt.Errorf("圖標必須為有效的 PNG／WebP，最大 512×512")
	}
	if _, _, e = image.Decode(bytes.NewReader(b)); e != nil {
		return "", fmt.Errorf("圖標內容損壞")
	}
	return "image/" + format, nil
}

// Inspect all owned containers once, including those whose desired state is stopped.
func AppStatusSnapshot(parent context.Context, apps AppState, mounted bool) AppState {
	out := AppState{Version: apps.Version, Apps: map[string]Deployment{}}
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	type container struct{ Names, State, Labels string }
	containers := map[string]container{}
	var queryErr error
	if mounted && !Restoring() && len(apps.Apps) > 0 {
		var b []byte
		b, queryErr = Run(ctx, "docker", "--host", "unix:///run/just-nas-docker.sock", "ps", "--all", "--no-trunc", "--filter", "label=org.just-nas.app", "--format", "{{json .}}")
		if queryErr == nil {
			for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
				if line == "" {
					continue
				}
				var c container
				if e := json.Unmarshal([]byte(line), &c); e != nil {
					queryErr = e
					break
				}
				containers[c.Names] = c
			}
		}
	}
	for id, v := range apps.Apps {
		v.Error = ""
		switch {
		case !mounted:
			v.Status = "unavailable"
			v.Error = "應用儲存空間尚未掛載"
		case Restoring():
			v.Status = "restoring"
			v.Error = "系統正在恢復應用"
		case queryErr != nil:
			v.Status = "unknown"
			v.Error = "無法確認容器狀態，請檢查容器引擎"
		default:
			c, exists := containers[containerName(id)]
			owned := false
			for _, label := range strings.Split(c.Labels, ",") {
				if label == "org.just-nas.app="+id {
					owned = true
				}
			}
			if !exists || !owned {
				v.Status = "failed"
				v.Error = "找不到此應用的受管理容器"
			} else {
				switch c.State {
				case "running":
					v.Status = "active"
				case "restarting", "created":
					v.Status = "starting"
				case "removing":
					v.Status = "stopping"
				case "exited":
					if v.Enabled {
						v.Status = "failed"
						v.Error = "應用非預期停止，請查看日誌"
					} else {
						v.Status = "stopped"
					}
				case "paused":
					v.Status = "failed"
					v.Error = "容器已暫停，請檢查容器引擎"
				default:
					v.Status = "failed"
					v.Error = "容器未正常執行：" + c.State
				}
			}
		}
		out.Apps[id] = v
	}
	return out
}

type LauncherEntry struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Version  string   `json:"version"`
	Icon     string   `json:"icon,omitempty"`
	Status   string   `json:"status"`
	Reason   string   `json:"reason,omitempty"`
	OpenMode string   `json:"open_mode"`
	URL      string   `json:"url,omitempty"`
	Actions  []string `json:"actions"`
}

func LauncherEntries(apps AppState, mounted bool) []LauncherEntry {
	entries := []LauncherEntry{}
	for id, a := range apps.Apps {
		m := a.Package.Manifest
		v := LauncherEntry{ID: id, Name: m.Name, Version: m.Version, Status: a.Status, Reason: a.Error, OpenMode: "none", Actions: []string{"details", "update"}}
		if m.Icon != "" {
			v.Icon = "/api/apps/" + id + "/icon?v=" + a.Package.Hash
		}
		if mounted && !Restoring() && a.Status != "starting" && a.Status != "stopping" {
			v.Actions = append(v.Actions, "logs", "remove")
			if a.Status == "active" {
				v.Actions = append(v.Actions, "stop")
			} else if a.Status != "unknown" && a.Status != "unavailable" {
				v.Actions = append(v.Actions, "start")
			}
		}
		ip := net.ParseIP(a.BindIP)
		if a.Status == "active" && ip != nil && !ip.IsUnspecified() && !ip.IsMulticast() && a.Port >= 1024 && a.Port <= 65535 {
			v.OpenMode = "external"
			v.URL = "http://" + net.JoinHostPort(ip.String(), fmt.Sprint(a.Port)) + "/"
			v.Actions = append(v.Actions, "open")
		}
		entries = append(entries, v)
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Name == entries[j].Name {
			return entries[i].ID < entries[j].ID
		}
		return entries[i].Name < entries[j].Name
	})
	return entries
}

func AppIcon(ctx context.Context, id string) ([]byte, string, error) {
	s, e := Load()
	if e != nil {
		return nil, "", e
	}
	if e = Mounted(ctx, s); e != nil {
		return nil, "", e
	}
	a, e := LoadApps()
	if e != nil {
		return nil, "", e
	}
	v, ok := a.Apps[id]
	if !ok || v.Package.Manifest.Icon == "" {
		return nil, "", os.ErrNotExist
	}
	root, e := os.OpenRoot(packagePath(s, v.Package))
	if e != nil {
		return nil, "", e
	}
	defer root.Close()
	f, e := root.Open(v.Package.Manifest.Icon)
	if e != nil {
		return nil, "", e
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, (256<<10)+1))
	if e != nil {
		return nil, "", e
	}
	h := sha256.Sum256(b)
	if hex.EncodeToString(h[:]) != v.Package.Manifest.Files[v.Package.Manifest.Icon].SHA256 {
		return nil, "", fmt.Errorf("圖標校驗失敗")
	}
	mime, e := validateIcon(b, v.Package.Manifest.Icon)
	return b, mime, e
}

// Only immediate child datasets left by managed applications are listed.
func RetainedApps(ctx context.Context, s State, a AppState) ([]string, error) {
	out := []string{}
	if s.Dataset == "" {
		return out, nil
	}
	b, e := Run(ctx, "zfs", "list", "-H", "-r", "-o", "name", s.Dataset)
	if e != nil {
		return nil, e
	}
	prefix := s.Dataset + "/apps/"
	for _, n := range strings.Fields(string(b)) {
		if strings.HasPrefix(n, prefix) {
			id := strings.TrimPrefix(n, prefix)
			if identifier.MatchString(id) {
				if _, ok := a.Apps[id]; !ok {
					out = append(out, n)
				}
			}
		}
	}
	sort.Strings(out)
	return out, nil
}
