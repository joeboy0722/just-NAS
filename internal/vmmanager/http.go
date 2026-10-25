package vmmanager

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/gorilla/websocket"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/http/httputil"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

//go:embed web
var webFiles embed.FS

func response(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
func failure(w http.ResponseWriter, e error) { response(w, 409, map[string]string{"error": e.Error()}) }
func decode(q *http.Request, v any) error {
	d := json.NewDecoder(io.LimitReader(q.Body, 65537))
	d.DisallowUnknownFields()
	return d.Decode(v)
}
func (b *Broker) ServeHTTP(w http.ResponseWriter, q *http.Request) {
	if q.Method != "GET" && q.URL.Path != "/control/prepare-stop" && b.stopping.Load() {
		failure(w, fmt.Errorf("管理器正在停止"))
		return
	}
	ctx := q.Context()
	p := strings.TrimPrefix(q.URL.Path, "/api")
	s := b.snapshot()
	if p == "/health" {
		response(w, 200, map[string]bool{"ready": true})
		return
	}
	if q.URL.Path == "/control/prepare-stop" && q.Method == "POST" {
		if e := b.prepareStop(ctx); e != nil {
			failure(w, e)
		} else {
			response(w, 200, map[string]bool{"stopped": true})
		}
		return
	}
	if p == "/capabilities" && q.Method == "GET" {
		ok, reason := probeKVM()
		total, available := memoryInfo()
		raw, e := command(ctx, "zpool", "list", "-H", "-o", "name")
		pools := []string{}
		if e == nil {
			pools = strings.Fields(string(raw))
		}
		addrs, _ := net.InterfaceAddrs()
		ips := []string{}
		for _, a := range addrs {
			ip, _, _ := net.ParseCIDR(a.String())
			if ip != nil && ip.To4() != nil && ip.IsPrivate() && !ip.IsLoopback() {
				ips = append(ips, ip.String())
			}
		}
		sort.Strings(ips)
		availableBytes := int64(0)
		storageError := ""
		if s.Dataset != "" {
			availableBytes, e = freeBytes(ctx, s)
			if e != nil {
				storageError = e.Error()
			}
		}
		response(w, 200, map[string]any{"kvm": ok, "kvm_reason": reason, "cpus": hostCPUs(), "memory_mib": total, "available_mib": available, "pools": pools, "ips": ips, "dataset": s.Dataset, "available_bytes": availableBytes, "storage_error": storageError})
		return
	}
	if p == "/storage" && q.Method == "POST" {
		var v struct {
			Pool string `json:"pool"`
		}
		if e := decode(q, &v); e != nil {
			failure(w, e)
			return
		}
		if e := b.configure(ctx, v.Pool); e != nil {
			failure(w, e)
		} else {
			response(w, 200, map[string]bool{"saved": true})
		}
		return
	}
	if p == "/vms" && q.Method == "GET" {
		views := []View{}
		for _, m := range s.Machines {
			views = append(views, b.view(ctx, s, m))
		}
		sort.Slice(views, func(i, j int) bool { return views[i].Created.Before(views[j].Created) })
		retained := []Machine{}
		for _, m := range s.Retained {
			retained = append(retained, m)
		}
		sort.Slice(retained, func(i, j int) bool { return retained[i].Name < retained[j].Name })
		response(w, 200, map[string]any{"vms": views, "retained": retained})
		return
	}
	if p == "/vms" && q.Method == "POST" {
		var v Machine
		if e := decode(q, &v); e != nil {
			failure(w, e)
			return
		}
		if e := validate(v); e != nil {
			failure(w, e)
			return
		}
		job := b.newJob("建立虛擬機", func(ctx context.Context) error { return b.create(ctx, v) })
		response(w, 202, map[string]string{"task_id": job})
		return
	}
	if p == "/isos" && q.Method == "GET" {
		items := []ISO{}
		for _, i := range s.ISOs {
			items = append(items, i)
		}
		sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
		response(w, 200, items)
		return
	}
	if p == "/isos" && q.Method == "POST" {
		b.uploadISO(w, q)
		return
	}
	if strings.HasPrefix(p, "/isos/") && q.Method == "DELETE" {
		id := strings.TrimPrefix(p, "/isos/")
		if !idPattern.MatchString(id) {
			http.NotFound(w, q)
			return
		}
		if e := b.removeISO(ctx, id); e != nil {
			failure(w, e)
		} else {
			response(w, 200, map[string]bool{"removed": true})
		}
		return
	}
	if p == "/tasks" && q.Method == "GET" {
		b.jobsMu.Lock()
		jobs := []Job{}
		for _, j := range b.jobs {
			jobs = append(jobs, j)
		}
		b.jobsMu.Unlock()
		sort.Slice(jobs, func(i, j int) bool { return jobs[i].Created.After(jobs[j].Created) })
		response(w, 200, jobs)
		return
	}
	if strings.HasPrefix(p, "/tasks/") && q.Method == "GET" {
		b.jobsMu.Lock()
		j, ok := b.jobs[strings.TrimPrefix(p, "/tasks/")]
		b.jobsMu.Unlock()
		if !ok {
			http.NotFound(w, q)
		} else {
			response(w, 200, j)
		}
		return
	}
	if strings.HasPrefix(p, "/vms/") {
		parts := strings.Split(strings.TrimPrefix(p, "/vms/"), "/")
		id := parts[0]
		if !idPattern.MatchString(id) {
			http.NotFound(w, q)
			return
		}
		if len(parts) == 2 && parts[1] == "restore" && q.Method == "POST" {
			job := b.newJob("重新登錄虛擬機", func(ctx context.Context) error { return b.restore(ctx, id) })
			response(w, 202, map[string]string{"task_id": job})
			return
		}
		m, ok := s.Machines[id]
		if !ok {
			http.NotFound(w, q)
			return
		}
		if len(parts) == 1 && q.Method == "PATCH" {
			var v Machine
			if e := decode(q, &v); e != nil {
				failure(w, e)
				return
			}
			job := b.newJob("修改虛擬機", func(ctx context.Context) error { return b.edit(ctx, id, v) })
			response(w, 202, map[string]string{"task_id": job})
			return
		}
		if len(parts) == 1 && q.Method == "DELETE" {
			var v struct {
				Purge   bool   `json:"purge"`
				Confirm string `json:"confirm"`
			}
			if e := decode(q, &v); e != nil {
				failure(w, e)
				return
			}
			job := b.newJob("移除虛擬機", func(ctx context.Context) error { return b.remove(ctx, id, v.Confirm, v.Purge) })
			response(w, 202, map[string]string{"task_id": job})
			return
		}
		if len(parts) == 2 && parts[1] == "actions" && q.Method == "POST" {
			var v struct {
				Action  string `json:"action"`
				Confirm string `json:"confirm"`
			}
			if e := decode(q, &v); e != nil {
				failure(w, e)
				return
			}
			job := b.newJob("虛擬機 "+v.Action, func(ctx context.Context) error { return b.action(ctx, id, v.Action, v.Confirm) })
			response(w, 202, map[string]string{"task_id": job})
			return
		}
		if len(parts) == 2 && parts[1] == "console" && q.Method == "GET" {
			b.console(w, q, s, m)
			return
		}
		if len(parts) == 2 && parts[1] == "logs" && q.Method == "GET" {
			raw, _ := os.ReadFile(machineRoot(s, id) + "/qemu.log")
			if len(raw) > 65536 {
				raw = raw[len(raw)-65536:]
			}
			response(w, 200, map[string]string{"logs": string(raw)})
			return
		}
		if len(parts) == 2 && parts[1] == "screenshot" && q.Method == "POST" {
			_, e := qmp(machineRoot(s, id)+"/run/qmp.sock", "screendump", map[string]string{"filename": "/vm/screen.ppm"})
			if e != nil {
				failure(w, e)
				return
			}
			f, e := os.Open(machineRoot(s, id) + "/screen.ppm")
			if e != nil {
				failure(w, e)
				return
			}
			defer f.Close()
			w.Header().Set("Content-Type", "image/x-portable-pixmap")
			io.Copy(w, io.LimitReader(f, 64<<20))
			return
		}
	}
	http.NotFound(w, q)
}
func (b *Broker) uploadISO(w http.ResponseWriter, q *http.Request) {
	b.operationGate.Lock()
	if b.stopping.Load() {
		b.operationGate.Unlock()
		failure(w, fmt.Errorf("管理器正在停止"))
		return
	}
	b.uploads.Add(1)
	b.operationGate.Unlock()
	defer b.uploads.Add(-1)
	s := b.snapshot()
	if e := mounted(q.Context(), s); e != nil {
		failure(w, e)
		return
	}
	name := q.URL.Query().Get("name")
	if !strings.HasSuffix(strings.ToLower(name), ".iso") || len(name) > 255 || strings.ContainsAny(name, "/\\\x00\r\n") {
		failure(w, fmt.Errorf("請選擇 ISO 檔案"))
		return
	}
	for _, v := range s.ISOs {
		if strings.EqualFold(v.Name, name) {
			failure(w, fmt.Errorf("同名 ISO 已存在，請先重新命名來源檔案"))
			return
		}
	}
	if q.ContentLength > MaxISO {
		failure(w, fmt.Errorf("ISO 最大 16 GiB"))
		return
	}
	available, e := freeBytes(q.Context(), s)
	if e != nil {
		failure(w, e)
		return
	}
	required := q.ContentLength
	if required < 0 {
		required = 0
	}
	if available < required+(512<<20) {
		failure(w, fmt.Errorf("ISO 儲存空間不足"))
		return
	}
	id := identifier()
	tmp := filepath.Join(storeRoot(s), "iso", ".upload-"+id)
	file, e := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0644)
	if e != nil {
		failure(w, e)
		return
	}
	defer os.Remove(tmp)
	hash := sha256.New()
	reader := io.LimitReader(q.Body, MaxISO+1)
	n, e := io.CopyBuffer(io.MultiWriter(file, hash), reader, make([]byte, 1<<20))
	if e == nil {
		e = file.Sync()
	}
	file.Close()
	if e != nil || n > MaxISO || q.ContentLength >= 0 && n != q.ContentLength {
		if e == nil {
			e = fmt.Errorf("上傳長度不符或超過 16 GiB")
		}
		failure(w, e)
		return
	}
	check, e := os.Open(tmp)
	if e != nil {
		failure(w, e)
		return
	}
	valid := false
	buf := make([]byte, 5)
	for sector := int64(16); sector < 32; sector++ {
		if _, e = check.ReadAt(buf, sector*2048+1); e == nil && (string(buf) == "CD001" || string(buf) == "BEA01") {
			valid = true
			break
		}
	}
	check.Close()
	if !valid {
		failure(w, fmt.Errorf("不是可辨識的 ISO／UDF 安裝媒體"))
		return
	}
	if e = mounted(q.Context(), s); e != nil {
		failure(w, e)
		return
	}
	final := isoPath(s, id)
	if e = os.Rename(tmp, final); e != nil {
		failure(w, e)
		return
	}
	v := ISO{ID: id, Name: name, Bytes: n, SHA256: hex.EncodeToString(hash.Sum(nil)), Created: time.Now()}
	b.mu.Lock()
	for _, other := range b.state.ISOs {
		if strings.EqualFold(other.Name, name) {
			b.mu.Unlock()
			os.Remove(final)
			failure(w, fmt.Errorf("同名 ISO 已存在"))
			return
		}
	}
	b.state.ISOs[id] = v
	e = b.saveLocked()
	if e != nil {
		delete(b.state.ISOs, id)
		os.Remove(final)
	}
	b.mu.Unlock()
	if e != nil {
		failure(w, e)
		return
	}
	response(w, 201, v)
}
func (b *Broker) removeISO(ctx context.Context, id string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	s := b.state
	if e := mounted(ctx, s); e != nil {
		return e
	}
	if _, ok := s.ISOs[id]; !ok {
		return fmt.Errorf("找不到 ISO")
	}
	for _, m := range s.Machines {
		if m.ISO == id {
			return fmt.Errorf("ISO 仍被 VM 掛載，請先卸載")
		}
	}
	for _, m := range s.Retained {
		if m.ISO == id {
			return fmt.Errorf("保留的 VM 設定仍參照此 ISO")
		}
	}
	old := b.state.ISOs[id]
	delete(b.state.ISOs, id)
	if e := b.saveLocked(); e != nil {
		b.state.ISOs[id] = old
		return e
	}
	return os.Remove(isoPath(s, id))
}
func (b *Broker) console(w http.ResponseWriter, q *http.Request, s State, m Machine) {
	if e := mounted(q.Context(), s); e != nil {
		failure(w, e)
		return
	}
	status, _ := processStatus(s, m)
	if status != "running" && status != "paused" {
		failure(w, fmt.Errorf("VM 未執行"))
		return
	}
	conn, e := net.DialTimeout("unix", machineRoot(s, m.ID)+"/run/vnc.sock", 3*time.Second)
	if e != nil {
		failure(w, e)
		return
	}
	defer conn.Close()
	ws, e := (&websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}).Upgrade(w, q, nil)
	if e != nil {
		return
	}
	defer ws.Close()
	ws.SetReadLimit(1 << 20)
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 65536)
		for {
			n, e := conn.Read(buf)
			if n > 0 {
				if e = ws.WriteMessage(websocket.BinaryMessage, buf[:n]); e != nil {
					return
				}
			}
			if e != nil {
				return
			}
		}
	}()
	for {
		typ, raw, e := ws.ReadMessage()
		if e != nil {
			return
		}
		if typ != websocket.BinaryMessage {
			return
		}
		if _, e = conn.Write(raw); e != nil {
			return
		}
		select {
		case <-done:
			return
		default:
		}
	}
}
func ServeWeb(socket, broker string) error {
	_ = os.Remove(socket)
	l, e := net.Listen("unix", socket)
	if e != nil {
		return e
	}
	if e = os.Chmod(socket, 0600); e != nil {
		return e
	}
	assets, _ := fs.Sub(webFiles, "web")
	files := http.FileServer(http.FS(assets))
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "unix", broker)
	}}
	proxy := &httputil.ReverseProxy{Transport: transport, FlushInterval: -1, Director: func(q *http.Request) { q.URL.Scheme = "http"; q.URL.Host = "broker"; q.Host = "broker" }, ErrorHandler: func(w http.ResponseWriter, q *http.Request, e error) {
		response(w, 503, map[string]string{"error": "VM 本機管理代理未回應"})
	}}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, q *http.Request) {
		if strings.HasPrefix(q.URL.Path, "/api/") || q.URL.Path == "/health" {
			proxy.ServeHTTP(w, q)
			return
		}
		if q.Method != "GET" {
			w.WriteHeader(405)
			return
		}
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self' data:; frame-ancestors 'self'")
		files.ServeHTTP(w, q)
	})
	return (&http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second}).Serve(l)
}
func ServeBroker(root, socket string) error {
	b, e := NewBroker(root)
	if e != nil {
		return e
	}
	_ = os.Remove(socket)
	l, e := net.Listen("unix", socket)
	if e != nil {
		return e
	}
	if e = os.Chown(socket, 0, WorkerUID); e != nil {
		return e
	}
	if e = os.Chmod(socket, 0660); e != nil {
		return e
	}
	go b.AutoStart()
	go b.Watchdog()
	return (&http.Server{Handler: b, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second}).Serve(l)
}
