package api

import (
	"context"
	"encoding/json"
	"fmt"
	"just-nas/internal/extensions"
	"net"
	"net/http"
	"strings"
)

func (r *Router) registerStores() {
	r.jobs.Register("store-install", extensions.InstallTask)
	r.jobs.Register("store-action", func(ctx context.Context, resource string, p func(string)) error {
		v := strings.SplitN(resource, "|", 2)
		if len(v) != 2 {
			return fmt.Errorf("商店任務無效")
		}
		var a struct {
			Kind, ID, Action string
			DeleteData       bool
		}
		if e := json.Unmarshal([]byte(v[1]), &a); e != nil {
			return e
		}
		p("套用 " + a.Action)
		if a.Kind == "app" {
			return extensions.AppAction(ctx, a.ID, a.Action, a.DeleteData)
		}
		if a.Kind != "extension" || (a.Action != "remove" && a.Action != "enable" && a.Action != "disable") {
			return fmt.Errorf("不支援的系統擴充操作")
		}
		if a.Action == "remove" {
			return extensions.Remove(ctx, a.ID)
		}
		return extensions.SetEnabled(ctx, a.ID, a.Action == "enable")
	})
	r.jobs.Register("store-download", func(ctx context.Context, resource string, p func(string)) error {
		v := strings.SplitN(resource, "|", 2)
		if len(v) != 2 {
			return fmt.Errorf("下載任務無效")
		}
		c, e := extensions.FetchCatalog(ctx)
		if e != nil {
			return e
		}
		for _, entry := range c.Entries {
			if entry.ID == v[1] {
				p("下載 " + entry.Name)
				id, _, e := extensions.Download(ctx, entry)
				if e != nil {
					return e
				}
				p("已下載；請在待安裝套件選擇：" + id)
				return nil
			}
		}
		return fmt.Errorf("目錄找不到套件")
	})
}
func (r *Router) storeAPI(w http.ResponseWriter, q *http.Request) {
	base := "/api/extensions"
	kind := "extension"
	if strings.HasPrefix(q.URL.Path, "/api/apps") {
		base = "/api/apps"
		kind = "app"
	}
	suffix := strings.TrimPrefix(q.URL.Path, base)
	if q.Method != "GET" && extensions.Restoring() {
		jsonResponse(w, 503, map[string]string{"error": "系統擴充正在恢復，請稍後重試"})
		return
	}
	var result any
	var e error
	switch {
	case q.Method == "GET" && suffix == "/launcher" && kind == "app":
		s, err := extensions.Load()
		if err != nil {
			e = err
			break
		}
		a, err := extensions.LoadApps()
		if err != nil {
			e = err
			break
		}
		mounted := s.Dataset != "" && extensions.Mounted(q.Context(), s) == nil
		a = extensions.AppStatusSnapshot(q.Context(), a, mounted)
		for _, job := range r.jobs.Jobs() {
			if job.State != "running" && job.State != "queued" {
				continue
			}
			parts := strings.SplitN(job.Resource, "|", 2)
			if len(parts) != 2 {
				continue
			}
			if job.Kind == "store-action" {
				var op struct{ Kind, ID, Action string }
				if json.Unmarshal([]byte(parts[1]), &op) != nil || op.Kind != "app" {
					continue
				}
				if v, ok := a.Apps[op.ID]; ok {
					if op.Action == "start" {
						v.Status = "starting"
					} else {
						v.Status = "stopping"
					}
					v.Error = job.Progress
					a.Apps[op.ID] = v
				}
			}
		}
		result = map[string]any{"apps": extensions.LauncherEntries(a, mounted), "mounted": mounted, "restoring": extensions.Restoring(), "storage": s.Dataset}
	case q.Method == "GET" && strings.HasSuffix(suffix, "/icon") && kind == "app":
		id := strings.TrimSuffix(strings.TrimPrefix(suffix, "/"), "/icon")
		b, mime, err := extensions.AppIcon(q.Context(), id)
		if err != nil {
			http.NotFound(w, q)
			return
		}
		w.Header().Set("Content-Type", mime)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "private, no-cache")
		w.Write(b)
		return
	case q.Method == "GET" && suffix == "":
		s, err := extensions.Load()
		if err != nil {
			e = err
			break
		}
		apps, err := extensions.LoadApps()
		if err != nil {
			e = err
			break
		}
		ips := []string{}
		addrs, _ := net.InterfaceAddrs()
		for _, a := range addrs {
			ip, _, _ := net.ParseCIDR(a.String())
			if ip != nil && ip.To4() != nil && ip.IsPrivate() {
				ips = append(ips, ip.String())
			}
		}
		imports := []map[string]any{}
		mounted := s.Dataset != "" && extensions.Mounted(q.Context(), s) == nil
		if mounted {
			imports, e = extensions.Pending(q.Context(), kind == "app")
			if e != nil {
				break
			}
		}

		hostKey, fingerprint := "", ""
		if mounted {
			hostKey, fingerprint = extensions.SSHHostKey(q.Context(), s)
		}
		s, apps = extensions.StatusSnapshot(q.Context(), s, apps, mounted)
		// Keep UI polling small: complete file manifests remain in the verified archives and persistent state.
		for id, v := range s.Installed {
			v.Package.Manifest.Files = nil
			s.Installed[id] = v
		}
		for id, v := range apps.Apps {
			v.Package.Manifest.Files = nil
			apps.Apps[id] = v
		}
		for _, item := range imports {
			v := item["package"].(extensions.Verified)
			v.Manifest.Files = nil
			item["package"] = v
		}
		retained := []string{}
		retainedError := ""
		if kind == "app" && mounted {
			var err error
			retained, err = extensions.RetainedApps(q.Context(), s, apps)
			if err != nil {
				retainedError = "無法查詢保留資料"
			}
		}
		result = map[string]any{"retained_data": retained, "retained_error": retainedError, "ssh_host_key": hostKey, "ssh_fingerprint": fingerprint, "state": s, "apps": apps, "ips": ips, "mounted": mounted, "imports": imports, "restoring": extensions.Restoring()}
	case q.Method == "GET" && suffix == "/catalog":
		result, e = extensions.FetchCatalog(q.Context())
	case q.Method == "POST" && suffix == "/storage":
		var v struct {
			Pool string `json:"pool"`
		}
		e = json.NewDecoder(http.MaxBytesReader(w, q.Body, 4096)).Decode(&v)
		if e == nil {
			e = extensions.Configure(q.Context(), v.Pool)
		}
		result = map[string]bool{"saved": e == nil}
	case q.Method == "POST" && suffix == "/import":
		q.Body = http.MaxBytesReader(w, q.Body, extensions.MaxBundle)
		var id string
		var v extensions.Verified
		id, v, e = extensions.Import(q.Context(), q.Body)
		if e == nil && (kind == "app") != (v.Manifest.Kind == "app") {
			_ = extensions.Discard(q.Context(), id)
			e = fmt.Errorf("套件類型不同，請切換至對應商店匯入")
		}
		result = map[string]any{"token": id, "package": v}
	case q.Method == "POST" && suffix == "/discard":
		var v struct {
			Token string `json:"token"`
		}
		e = json.NewDecoder(http.MaxBytesReader(w, q.Body, 4096)).Decode(&v)
		if e == nil {
			e = extensions.Discard(q.Context(), v.Token)
		}
		result = map[string]bool{"removed": e == nil}
	case q.Method == "POST" && suffix == "/install":
		var v extensions.Request
		e = json.NewDecoder(http.MaxBytesReader(w, q.Body, 16384)).Decode(&v)
		if e == nil {
			var pkg extensions.Verified
			pkg, e = extensions.Lookup(q.Context(), v.Token)
			if e == nil && (kind == "app") != (pkg.Manifest.Kind == "app") {
				e = fmt.Errorf("請從對應商店安裝此套件")
			}
			var resource, id string
			if e == nil {
				resource, e = extensions.Prepare(q.Context(), v)
			}
			if e == nil {
				id, e = r.jobs.Start("store-install", resource)
			}
			result = map[string]string{"task_id": id}
		}
	case q.Method == "POST" && suffix == "/download":
		var v struct {
			ID string `json:"id"`
		}
		e = json.NewDecoder(http.MaxBytesReader(w, q.Body, 4096)).Decode(&v)
		if e == nil {
			s, err := extensions.Load()
			e = err
			if e == nil {
				e = extensions.Mounted(q.Context(), s)
			}
			if e == nil {
				var id string
				id, e = r.jobs.Start("store-download", s.Dataset+"|"+v.ID)
				result = map[string]string{"task_id": id}
			}
		}
	case q.Method == "POST" && suffix == "/ssh-key":
		var v struct {
			PublicKey string `json:"public_key"`
		}
		e = json.NewDecoder(http.MaxBytesReader(w, q.Body, 16384)).Decode(&v)
		if e == nil {
			e = extensions.SetSSHKey(q.Context(), v.PublicKey)
		}
		result = map[string]bool{"saved": e == nil}

	case q.Method == "GET" && strings.HasSuffix(suffix, "/logs") && kind == "app":
		id := strings.TrimSuffix(strings.TrimPrefix(suffix, "/"), "/logs")
		var logs string
		logs, e = extensions.AppLogs(q.Context(), id)
		result = map[string]string{"logs": logs}
	case q.Method == "POST" && suffix == "/action":
		var v struct {
			ID         string `json:"id"`
			Action     string `json:"action"`
			DeleteData bool   `json:"delete_data"`
			Confirm    string `json:"confirm"`
		}
		e = json.NewDecoder(http.MaxBytesReader(w, q.Body, 4096)).Decode(&v)
		allowed := v.Action == "start" || v.Action == "stop" || v.Action == "remove"
		if kind != "app" {
			allowed = v.Action == "enable" || v.Action == "disable" || v.Action == "remove"
		}
		if !allowed {
			e = fmt.Errorf("不支援的操作")
		}
		if v.DeleteData && (v.Action != "remove" || v.Confirm != v.ID) {
			e = fmt.Errorf("刪除應用資料須確認應用名稱")
		}
		if e == nil {
			s, err := extensions.Load()
			e = err
			if e == nil {
				e = extensions.Mounted(q.Context(), s)
			}
			if e == nil {
				raw, _ := json.Marshal(map[string]any{"Kind": kind, "ID": v.ID, "Action": v.Action, "DeleteData": v.DeleteData})
				var id string
				id, e = r.jobs.Start("store-action", s.Dataset+"|"+string(raw))
				result = map[string]string{"task_id": id}
			}
		}
	default:
		w.WriteHeader(405)
		return
	}
	if e != nil {
		jsonResponse(w, 409, map[string]string{"error": e.Error()})
		return
	}
	status := 200
	if q.Method == "POST" && (suffix == "/install" || suffix == "/action" || suffix == "/download") {
		status = 202
	}
	jsonResponse(w, status, result)
}
