package boot

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"time"
)

type Interface struct {
	Name      string
	Index     int
	Up        bool
	Kind      string
	Addresses []string
	Container bool
}

var Query = func(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = 250 * time.Millisecond
	return cmd.Output()
}

func CollectInterfaces(ctx context.Context) ([]Interface, error) {
	ifs, e := net.Interfaces()
	if e != nil {
		return nil, e
	}
	kinds := linkKinds()
	containers := containerInterfaces(ctx)
	out := []Interface{}
	for _, i := range ifs {
		a, e := i.Addrs()
		if e != nil {
			return nil, e
		}
		v := Interface{Name: i.Name, Index: i.Index, Up: i.Flags&net.FlagUp != 0 && i.Flags&net.FlagLoopback == 0, Kind: kinds[i.Name], Container: containers[i.Name]}
		for _, s := range a {
			v.Addresses = append(v.Addresses, s.String())
		}
		out = append(out, v)
	}
	return out, nil
}
func containerInterfaces(ctx context.Context) map[string]bool {
	out := map[string]bool{"docker0": true}
	socket := "/run/just-nas-docker.sock"
	if _, e := os.Stat(socket); e != nil {
		return out
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: time.Second}
	q, _ := http.NewRequestWithContext(ctx, "GET", "http://localhost/networks", nil)
	r, e := client.Do(q)
	if e != nil {
		return out
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		return out
	}
	var networks []struct {
		ID      string `json:"Id"`
		Driver  string
		Options map[string]string
	}
	if json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&networks) != nil {
		return out
	}
	for _, n := range networks {
		if n.Driver != "bridge" {
			continue
		}
		name := n.Options["com.docker.network.bridge.name"]
		if name == "" && len(n.ID) >= 12 {
			name = "br-" + n.ID[:12]
		}
		out[name] = true
	}
	return out
}

var dockerBridge = regexp.MustCompile(`^br-[0-9a-f]{12}$`)

func Addresses(ifs []Interface, primary, scheme, port, certificate string) []Address {
	out := []Address{}
	var cert *x509.Certificate
	if b, _ := pem.Decode([]byte(certificate)); b != nil {
		cert, _ = x509.ParseCertificate(b.Bytes)
	}
	for _, i := range ifs {
		if !i.Up || i.Container || i.Kind == "veth" || strings.HasPrefix(i.Name, "veth") || i.Name == "docker0" || dockerBridge.MatchString(i.Name) {
			continue
		}
		seen := map[string]bool{}
		for _, raw := range i.Addresses {
			ip, _, e := net.ParseCIDR(raw)
			if e != nil {
				ip = net.ParseIP(raw)
			}
			if ip == nil || !ip.IsGlobalUnicast() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
				continue
			}
			s := ip.String()
			if seen[s] {
				continue
			}
			seen[s] = true
			out = append(out, Address{Interface: i.Name, IP: s, URL: scheme + "://" + net.JoinHostPort(s, port), Primary: i.Name == primary, CertificateWarning: scheme == "https" && (cert == nil || cert.VerifyHostname(s) != nil)})
		}
	}
	indexes := map[string]int{}
	for _, i := range ifs {
		indexes[i.Name] = i.Index
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Primary != b.Primary {
			return a.Primary
		}
		if indexes[a.Interface] != indexes[b.Interface] {
			return indexes[a.Interface] < indexes[b.Interface]
		}
		x, y := net.ParseIP(a.IP), net.ParseIP(b.IP)
		if (x.To4() != nil) != (y.To4() != nil) {
			return x.To4() != nil
		}
		return strings.Compare(string(x.To16()), string(y.To16())) < 0
	})
	return out
}
func HardwareStatus(ctx context.Context) (State, string) {
	for _, s := range []string{"hwdrivers", "modloop", "modules"} {
		if _, e := Query(ctx, "rc-service", s, "status"); e != nil {
			return Warn, "Check OpenRC driver services in local console"
		}
	}
	return OK, "Core driver services completed; hardware compatibility is not certified"
}
func StorageStatus(ctx context.Context) string {
	b, e := Query(ctx, "zpool", "list", "-H", "-o", "name,health")
	if e != nil {
		return "Unavailable"
	}
	if strings.TrimSpace(string(b)) == "" {
		return "First setup"
	}
	for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		f := strings.Fields(l)
		if len(f) != 2 {
			return "Unavailable"
		}
		if f[1] != "ONLINE" {
			return "Needs attention: " + f[1]
		}
	}
	return "Healthy"
}
