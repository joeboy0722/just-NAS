// Package boot presents real startup results without owning system configuration.
package boot

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"
)

type State string

const (
	Pending State = "pending"
	Running State = "running"
	OK      State = "ok"
	Warn    State = "warn"
	Fail    State = "fail"
	Skip    State = "skip"
)

type Stage struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	State  State  `json:"state"`
	Detail string `json:"detail,omitempty"`
}
type Address struct {
	Interface          string `json:"interface"`
	IP                 string `json:"ip"`
	URL                string `json:"url"`
	Primary            bool   `json:"primary"`
	CertificateWarning bool   `json:"certificate_warning,omitempty"`
}
type View struct {
	Ready          bool      `json:"ready"`
	Stopped        bool      `json:"stopped"`
	Stages         []Stage   `json:"stages"`
	Addresses      []Address `json:"addresses"`
	PrimaryMissing bool      `json:"primary_missing"`
	Storage        string    `json:"storage"`
	NetworkError   bool      `json:"network_error,omitempty"`
	Page           int       `json:"page"`
	Pages          int       `json:"pages"`
}
type Dashboard struct {
	serialLast  string
	mu          sync.Mutex
	view        View
	out, serial io.Writer
	plain       bool
	path        string
	last        string
	started     time.Time
}

func New(out, serial io.Writer, plain bool, path string) *Dashboard {
	d := &Dashboard{out: out, serial: serial, plain: plain, path: path, started: time.Now()}
	d.view.Storage = "Checking"
	for _, s := range []Stage{{ID: "hardware", Label: "Core hardware drivers checked"}, {ID: "pools", Label: "ZFS pools imported and mounted"}, {ID: "settings", Label: "NAS settings restored"}, {ID: "network", Label: "Management network configured"}, {ID: "shares", Label: "Storage services started"}, {ID: "web", Label: "Web console listening"}, {ID: "extensions", Label: "Optional extensions restored"}} {
		s.State = Pending
		d.view.Stages = append(d.view.Stages, s)
	}
	d.drawLocked()
	return d
}
func Plain(cmdline string) bool {
	for _, s := range strings.Fields(cmdline) {
		if s == "just_nas_boot=plain" {
			return true
		}
	}
	return false
}
func clean(s string, n int) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsControl(r) || r > 126 {
			r = '?'
		}
		if b.Len() >= n {
			break
		}
		b.WriteRune(r)
	}
	return b.String()
}
func (d *Dashboard) Set(id string, state State, detail string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for i := range d.view.Stages {
		if d.view.Stages[i].ID == id {
			s := &d.view.Stages[i]
			if s.State == state && s.Detail == detail {
				return
			}
			s.State = state
			s.Detail = clean(detail, 72)
			if d.serial != nil {
				fmt.Fprintf(d.serial, "[%s] %s: %s\r\n", status(state), s.Label, s.Detail)
			}
			if d.plain && d.out != nil {
				fmt.Fprintf(d.out, "[%s] %s: %s\n", status(state), s.Label, s.Detail)
			}
			d.drawLocked()
			return
		}
	}
}
func (d *Dashboard) Finish(ready bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.view.Ready = ready
	d.view.Stopped = !ready
	d.drawLocked()
}
func (d *Dashboard) Refresh(addresses []Address, missing bool, storage string, networkError bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.view.Addresses = addresses
	d.view.PrimaryMissing = missing
	d.view.Storage = clean(storage, 38)
	d.view.NetworkError = networkError
	_, page, _ := PageAddresses(addresses, int(time.Since(d.started)/(5*time.Second)))
	d.view.Page = page - 1
	d.drawLocked()
}
func (d *Dashboard) Snapshot() View {
	d.mu.Lock()
	defer d.mu.Unlock()
	v := d.view
	v.Stages = append([]Stage(nil), v.Stages...)
	v.Addresses = append([]Address(nil), v.Addresses...)
	return v
}
func status(s State) string {
	switch s {
	case Running:
		return "...."
	case OK:
		return "OK"
	case Fail:
		return "FAIL"
	case Warn:
		return "WARN"
	case Skip:
		return "SKIP"
	}
	return "WAIT"
}
func color(s State, ready bool) string {
	switch s {
	case OK:
		if ready {
			return "32"
		}
		return "31"
	case Fail:
		if ready {
			return "31"
		}
		return "32"
	case Running:
		return "36"
	case Warn:
		return "33"
	case Skip:
		return "90"
	}
	return "37"
}
func PageAddresses(a []Address, page int) ([]Address, int, int) {
	if len(a) <= 4 {
		return a, 1, 1
	}
	pages := (len(a) - 1 + 2) / 3
	page = page % pages
	if page < 0 {
		page = 0
	}
	start := 1 + page*3
	end := start + 3
	if end > len(a) {
		end = len(a)
	}
	out := append([]Address{a[0]}, a[start:end]...)
	return out, page + 1, pages
}
func Render(v View, ansi bool) string {
	var b strings.Builder
	paint := func(s State, text string) string {
		if !ansi {
			return text
		}
		return "\x1b[" + color(s, v.Ready || v.Stopped) + "m" + text + "\x1b[0m"
	}
	if v.Ready {
		b.WriteString("Welcome to Just NAS.\n")
	} else if v.Stopped {
		b.WriteString("Just NAS startup stopped.\n")
	} else {
		b.WriteString("  [ JN ] just-NAS\n  Starting your home storage...\n")
	}
	if !v.Ready && !v.Stopped {
		for _, s := range v.Stages {
			fmt.Fprintf(&b, "[ %-4s ] %s\n", paint(s.State, fmt.Sprintf("%-4s", status(s.State))), s.Label)
			if s.State == Warn || s.State == Fail {
				fmt.Fprintf(&b, "         %s\n", clean(s.Detail, 67))
			}
		}
	} else {
		state := OK
		summary := "Ready"
		if v.Stopped {
			state = Fail
			summary = "Startup failed"
		}
		for _, s := range v.Stages {
			if s.State == Fail {
				state = Fail
				summary = "Degraded"
				break
			}
			if s.State == Warn && state == OK {
				state = Warn
				summary = "Needs attention"
			}
		}
		if v.PrimaryMissing || v.NetworkError {
			if state == OK {
				state = Warn
				summary = "Needs attention"
			}
		}
		if v.Storage != "Healthy" && v.Storage != "First setup" && v.Storage != "Checking" {
			if state == OK {
				state = Warn
				summary = "Needs attention"
			}
		}
		fmt.Fprintf(&b, "System       %s\nStorage      %s\n", paint(state, summary), v.Storage)
		for _, s := range v.Stages {
			if s.State == Fail || s.State == Warn {
				fmt.Fprintf(&b, "%s %s\n", paint(s.State, "["+status(s.State)+"]"), clean(s.Label, 48))
			}
		}
		for _, s := range v.Stages {
			if s.ID == "extensions" && s.State == Running {
				b.WriteString("Extensions   Restoring in background\n")
			}
		}
	}
	if v.PrimaryMissing {
		b.WriteString(paint(Warn, "[WARN] Primary management interface has no usable address") + "\n")
	}
	if v.NetworkError {
		b.WriteString(paint(Warn, "[WARN] Network address check unavailable") + "\n")
	}
	if v.Ready {
		a, page, pages := PageAddresses(v.Addresses, v.Page)
		fmt.Fprintf(&b, "\nManagement addresses (%d/%d)\n", page, pages)
		if len(a) == 0 {
			b.WriteString("  Waiting for a usable network address\n")
		}
		for _, a := range a {
			label := clean(a.Interface, 16)
			if a.Primary {
				label += "  Primary"
			}
			if a.CertificateWarning {
				label += "  [certificate does not cover IP]"
			}
			fmt.Fprintf(&b, "  %s\n    %s\n", label, clean(a.URL, 70))
		}
	}
	b.WriteString("\nAlt+F2: Local console | Logs: /var/log/nas-server.log\n")
	if ansi {
		return "\x1b[0m\x1b[H\x1b[2J" + strings.ReplaceAll(b.String(), "\n", "\r\n") + "\x1b[0m"
	}
	return b.String()
}
func (d *Dashboard) drawLocked() {
	_, _, d.view.Pages = PageAddresses(d.view.Addresses, d.view.Page)
	frame := Render(d.view, !d.plain)
	if frame != d.last && d.out != nil {
		if !d.plain || d.view.Ready || d.view.Stopped {
			_, _ = io.WriteString(d.out, frame)
		}
		d.last = frame
	}
	if d.serial != nil && (d.view.Ready || d.view.Stopped) {
		summary := d.view
		summary.Page = 0
		summary.Pages = 0
		encoded, _ := json.Marshal(summary)
		if string(encoded) != d.serialLast {
			_, _ = io.WriteString(d.serial, Render(summary, false))
			if len(summary.Addresses) > 4 {
				for _, a := range summary.Addresses[4:] {
					fmt.Fprintf(d.serial, "%s %s\r\n", a.Interface, a.URL)
				}
			}
			d.serialLast = string(encoded)
		}
	}
	if d.path != "" {
		b, _ := json.MarshalIndent(d.view, "", "  ")
		_ = os.MkdirAll(filepath.Dir(d.path), 0700)
		tmp := d.path + ".next"
		if os.WriteFile(tmp, b, 0600) == nil {
			_ = os.Rename(tmp, d.path)
		}
	}
}
