package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"just-nas/internal/config"
	"just-nas/internal/extensions"
	"net"
	"net/http"
	"net/http/httputil"
	"strings"
	"time"
)

func (r *Router) StartNativeServices() error {
	err := extensions.StartNativeStateService(func(id string, data json.RawMessage) error {
		r.nativeStateMu.Lock()
		defer r.nativeStateMu.Unlock()
		if config.PoolDeletionPending() {
			return fmt.Errorf("刪池清理進行中，暫停原生應用設定保存")
		}
		return extensions.SaveNativeState(id, data)
	})
	if err != nil {
		return err
	}
	r.jobs.Register("native-task", func(ctx context.Context, resource string, progress func(string)) error {
		parts := strings.SplitN(resource, "|", 3)
		if len(parts) != 3 {
			return fmt.Errorf("原生任務參照無效")
		}
		for {
			jobs, e := extensions.NativeJobs(ctx, parts[1], parts[2])
			if e != nil {
				return e
			}
			j := jobs[0]
			progress(j.Progress)
			if j.State != "queued" && j.State != "running" {
				if j.State != "succeeded" {
					return fmt.Errorf("%s", j.Error)
				}
				return nil
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Second):
			}
		}
	})
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			apps, e := extensions.LoadApps()
			if e != nil {
				continue
			}
			known := map[string]bool{}
			for _, j := range r.jobs.Jobs() {
				if j.Kind == "native-task" {
					known[j.Resource] = true
				}
			}
			for id, a := range apps.Apps {
				if !a.Package.Manifest.IsNative() || !a.Enabled {
					continue
				}
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				jobs, e := extensions.NativeJobs(ctx, id, "")
				cancel()
				if e != nil {
					continue
				}
				ds := a.Dataset
				if refs := extensions.NativeReferences(id); len(refs) > 0 {
					ds = refs[0]
				}
				for _, j := range jobs {
					resource := ds + "|" + id + "|" + j.ID
					if len(j.ID) == 32 && !known[resource] {
						_, _ = r.jobs.Start("native-task", resource)
						known[resource] = true
					}
				}
			}
		}
	}()
	return nil
}

func (a *auth) active(req *http.Request) bool {
	c, e := req.Cookie("nas_session")
	if e != nil {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	s, ok := a.Sessions[c.Value]
	return ok && time.Now().Before(s.Expires)
}
func (r *Router) nativeProxy(w http.ResponseWriter, q *http.Request) {
	if !sameOrigin(q) {
		jsonResponse(w, 403, map[string]string{"error": "請求來源不允許"})
		return
	}
	prefix := "/api/app-services/"
	if strings.HasPrefix(q.URL.Path, "/apps/") {
		prefix = "/apps/"
	}
	parts := strings.SplitN(strings.TrimPrefix(q.URL.Path, prefix), "/", 2)
	id := parts[0]
	apps, e := extensions.LoadApps()
	if e != nil {
		jsonResponse(w, 409, map[string]string{"error": e.Error()})
		return
	}
	app, ok := apps.Apps[id]
	if !ok || !app.Package.Manifest.IsNative() || !app.Enabled {
		http.NotFound(w, q)
		return
	}
	if extensions.NativeTransitionBusy(id) {
		jsonResponse(w, 503, map[string]string{"error": "應用正在更新，請稍後再試"})
		return
	}
	state, e := extensions.Load()
	if e != nil || extensions.Mounted(q.Context(), state) != nil {
		jsonResponse(w, 503, map[string]string{"error": "應用儲存空間不可用"})
		return
	}
	target := "/"
	if len(parts) == 2 {
		target += parts[1]
	}
	if strings.Contains(target, "..") || strings.ContainsAny(target, "\\\x00") {
		http.NotFound(w, q)
		return
	}
	if prefix == "/api/app-services/" {
		target = "/api" + target
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "unix", extensions.NativeSocket(id))
	}}
	defer transport.CloseIdleConnections()
	proxy := httputil.ReverseProxy{Transport: transport, FlushInterval: -1, Director: func(req *http.Request) {
		req.URL.Scheme = "http"
		req.URL.Host = "native"
		req.URL.Path = target
		req.Host = "native"
		req.Header.Del("Cookie")
		req.Header.Del("X-CSRF-Token")
		req.Header.Del("Authorization")
	}, ModifyResponse: func(res *http.Response) error {
		if res.StatusCode == 101 {
			conn, ok := res.Body.(io.ReadWriteCloser)
			if !ok {
				return fmt.Errorf("主控台連線無效")
			}
			go func() {
				t := time.NewTicker(time.Second)
				defer t.Stop()
				for {
					select {
					case <-q.Context().Done():
						return
					case <-t.C:
						if !r.adminAuth.active(q) {
							conn.Close()
							return
						}
					}
				}
			}()
		}
		return nil
	}, ErrorHandler: func(w http.ResponseWriter, _ *http.Request, e error) {
		jsonResponse(w, 503, map[string]string{"error": "應用服務未回應，請查看應用狀態及日誌"})
	}}
	proxy.ServeHTTP(w, q)
}
