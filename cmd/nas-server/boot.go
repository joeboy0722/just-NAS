package main

import (
	"context"
	"crypto/tls"
	"io"
	"just-nas/internal/boot"
	"just-nas/internal/config"
	"just-nas/internal/platform"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"
)

type bootDisplay struct {
	d                                     *boot.Dashboard
	httpPort                              string
	networkDone, networkFailed, webFailed atomic.Bool
	httpBound, httpsBound                 atomic.Bool
}

func startBootDisplay(port string) *bootDisplay {
	tty := boot.Console("/dev/tty1")
	serial := boot.Console("/dev/ttyS0")
	var out io.Writer = tty
	if tty == nil {
		out = os.Stdout
	}
	var mirror io.Writer
	if serial != nil {
		mirror = serial
	}
	cmdline, _ := os.ReadFile("/proc/cmdline")
	d := boot.New(out, mirror, boot.Plain(string(cmdline)), "/run/just-nas/boot.json")
	b := &bootDisplay{d: d, httpPort: port}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	state, detail := boot.HardwareStatus(ctx)
	cancel()
	d.Set("hardware", state, detail)
	go func() {
		for {
			b.refresh()
			time.Sleep(2 * time.Second)
		}
	}()
	return b
}
func (b *bootDisplay) refresh() {
	var network platform.NetworkSettings
	_ = config.ReadJSON("network.json", &network)
	primary := network.Interface
	if primary == "" {
		primary = "eth0"
	}
	settings, e := platform.TLSConfig()
	scheme, port := "http", b.httpPort
	if e == nil && settings.Enabled {
		scheme, port = "https", "8443"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	ifs, err := boot.CollectInterfaces(ctx)
	cancel()
	a := boot.Addresses(ifs, primary, scheme, port, settings.Certificate)
	missing := true
	for _, v := range a {
		if v.Primary {
			missing = false
		}
	}
	ctx, cancel = context.WithTimeout(context.Background(), 1500*time.Millisecond)
	storage := boot.StorageStatus(ctx)
	cancel()
	b.d.Refresh(a, missing, storage, err != nil)
	if b.networkDone.Load() && !b.networkFailed.Load() {
		if err != nil || missing {
			b.d.Set("network", boot.Warn, "Primary management interface has no usable address")
		} else {
			b.d.Set("network", boot.OK, "Current management interface has a usable address")
		}
	}
	if !b.d.Snapshot().Ready && !b.webFailed.Load() && ((scheme == "https" && b.httpsBound.Load()) || (scheme == "http" && b.httpBound.Load())) {
		// Loopback-only health probe validates a live handler and TLS handshake, not certificate trust.
		client := &http.Client{Timeout: 750 * time.Millisecond, Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}, CheckRedirect: func(q *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}
		r, e := client.Get(scheme + "://127.0.0.1:" + port + "/api/auth/status")
		if e == nil {
			r.Body.Close()
			if r.StatusCode == 200 {
				b.d.Set("web", boot.OK, "Management service is accepting requests")
				b.d.Finish(true)
			}
		}
		client.CloseIdleConnections()
	}
}
func (b *bootDisplay) failedWeb(detail string) {
	b.webFailed.Store(true)
	b.d.Set("web", boot.Fail, detail)
	b.d.Finish(false)
}
func (b *bootDisplay) networkResult(e error) {
	if e != nil {
		b.networkFailed.Store(true)
		b.d.Set("network", boot.Fail, "Management network restore failed; see logs")
	}
	b.networkDone.Store(true)
}
func rescueExtensions() bool {
	raw, _ := os.ReadFile("/proc/cmdline")
	for _, v := range strings.Fields(string(raw)) {
		if v == "just_nas_no_extensions=1" {
			return true
		}
	}
	return false
}
