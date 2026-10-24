package extensions

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

func serviceRunning(pidfile, program string) bool {
	p, e := os.ReadFile(pidfile)
	if e != nil {
		return false
	}
	pid := strings.TrimSpace(string(p))
	if !regexp.MustCompile(`^[1-9][0-9]{0,9}$`).MatchString(pid) {
		return false
	}
	target, e := os.Readlink("/proc/" + pid + "/exe")
	return e == nil && filepath.Base(target) == program
}
func sshRunning() bool { return serviceRunning("/run/just-nas/sshd.pid", "sshd") }

// StatusSnapshot changes only the response copy, never the desired persistent state.
func StatusSnapshot(parent context.Context, s State, a AppState, mounted bool) (State, AppState) {
	installed := map[string]Installation{}
	for id, v := range s.Installed {
		installed[id] = v
	}
	s.Installed = installed
	applications := map[string]Deployment{}
	for id, v := range a.Apps {
		applications[id] = v
	}
	a.Apps = applications
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	for id, v := range s.Installed {
		if !v.Enabled {
			continue
		}
		if Restoring() {
			v.Status = "restoring"
		} else if !mounted {
			v.Status = "unavailable"
			v.Error = "擴充儲存空間尚未掛載"
		} else {
			switch v.Package.Manifest.Kind {
			case "runtime":
				b, e := Run(ctx, "docker", "--host", "unix:///run/just-nas-docker.sock", "info", "--format", "{{.Driver}}")
				if e != nil || strings.TrimSpace(string(b)) != "zfs" {
					v.Status = "failed"
					v.Error = "容器引擎未就緒或儲存後端不符"
				} else {
					v.Status = "active"
					v.Error = ""
				}
			case "ssh":
				if sshRunning() {
					v.Status = "active"
					v.Error = ""
				} else {
					v.Status = "failed"
					v.Error = "SSH 服務未運作"
				}
			}
		}
		s.Installed[id] = v
	}
	for id, v := range a.Apps {
		if !v.Enabled {
			continue
		}
		if Restoring() {
			v.Status = "restoring"
		} else if !mounted {
			v.Status = "unavailable"
			v.Error = "應用儲存空間尚未掛載"
		} else {
			b, e := Run(ctx, "docker", "--host", "unix:///run/just-nas-docker.sock", "inspect", "--format", "{{.State.Running}}", containerName(id))
			if e != nil {
				v.Status = "failed"
				v.Error = "無法查詢應用狀態"
			} else if strings.TrimSpace(string(b)) != "true" {
				v.Status = "failed"
				v.Error = "應用已停止；請查看日誌並重新啟動"
			} else {
				v.Status = "active"
				v.Error = ""
			}
		}
		if ctx.Err() != nil {
			v.Status = "unknown"
			v.Error = fmt.Sprint("狀態查詢逾時")
		}
		a.Apps[id] = v
	}
	return s, a
}
