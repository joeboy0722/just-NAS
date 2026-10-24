package platform

import (
	"fmt"
	"just-nas/internal/command"
	"just-nas/internal/config"
	"net"
	"net/netip"
	"os"
	"strings"
	"sync"
	"time"
)

type NetworkSettings struct {
	Interface        string           `json:"interface"`
	DHCP             bool             `json:"dhcp"`
	Address          string           `json:"address"`
	Gateway          string           `json:"gateway"`
	DNS              []string         `json:"dns"`
	Pending          bool             `json:"pending"`
	Deadline         time.Time        `json:"deadline"`
	Previous         string           `json:"previous,omitempty"`
	PreviousResolver string           `json:"previous_resolver,omitempty"`
	PreviousSettings *NetworkSettings `json:"previous_settings,omitempty"`
}

var networkMu sync.Mutex

func NetworkState() (map[string]any, error) {
	ifs, e := net.Interfaces()
	if e != nil {
		return nil, e
	}
	out := []map[string]any{}
	for _, i := range ifs {
		if i.Flags&net.FlagLoopback != 0 {
			continue
		}
		a, e := i.Addrs()
		if e != nil {
			return nil, e
		}
		ss := []string{}
		for _, v := range a {
			ss = append(ss, v.String())
		}
		out = append(out, map[string]any{"name": i.Name, "addresses": ss, "up": i.Flags&net.FlagUp != 0})
	}
	var s NetworkSettings
	_ = config.ReadJSON("network.json", &s)
	s.Previous = ""
	s.PreviousResolver = ""
	s.PreviousSettings = nil
	return map[string]any{"interfaces": out, "settings": s}, nil
}
func networkText(s NetworkSettings) (string, error) {
	i, e := net.InterfaceByName(s.Interface)
	if e != nil || i.Flags&net.FlagLoopback != 0 || strings.ContainsAny(s.Interface, " \t\r\n") {
		return "", fmt.Errorf("介面無效")
	}
	base := "auto lo\niface lo inet loopback\nauto " + s.Interface + "\niface " + s.Interface + " inet "
	if s.DHCP {
		return base + "dhcp\n", nil
	}
	p, e := netip.ParsePrefix(s.Address)
	if e != nil || !p.Addr().Is4() || p.Addr().IsLoopback() || p.Addr().IsUnspecified() || p.Addr().IsMulticast() {
		return "", fmt.Errorf("請輸入有效的區網 IPv4/CIDR")
	}
	base += "static\n address " + s.Address + "\n"
	if s.Gateway != "" {
		g, e := netip.ParseAddr(s.Gateway)
		if e != nil || !p.Contains(g) {
			return "", fmt.Errorf("閘道必須位於相同子網")
		}
		base += " gateway " + s.Gateway + "\n"
	}
	for _, d := range s.DNS {
		if _, e := netip.ParseAddr(d); e != nil {
			return "", fmt.Errorf("DNS 位址無效")
		}
	}
	return base, nil
}
func resolverText(s NetworkSettings) string {
	var b strings.Builder
	b.WriteString("# managed by just-NAS\n")
	if !s.DHCP {
		for _, d := range s.DNS {
			b.WriteString("nameserver " + d + "\n")
		}
	}
	return b.String()
}
func confirmed(s NetworkSettings) NetworkSettings {
	s.Pending = false
	s.Previous = ""
	s.PreviousResolver = ""
	s.PreviousSettings = nil
	s.Deadline = time.Time{}
	return s
}
func rollbackSettings(s NetworkSettings) NetworkSettings {
	if s.PreviousSettings != nil {
		return confirmed(*s.PreviousSettings)
	}
	return NetworkSettings{Interface: "eth0", DHCP: true}
}
func ConfigureNetwork(s NetworkSettings) error {
	networkMu.Lock()
	defer networkMu.Unlock()
	var existing NetworkSettings
	_ = config.ReadJSON("network.json", &existing)
	if existing.Pending {
		return fmt.Errorf("請先確認或等待上次網路變更回復")
	}
	primary := existing.Interface
	if primary == "" {
		primary = "eth0"
		existing = NetworkSettings{Interface: primary, DHCP: true}
	}
	if s.Interface != primary {
		return fmt.Errorf("第一版僅調整主要管理介面 %s；其他介面提供狀態查看", primary)
	}
	text, e := networkText(s)
	if e != nil {
		return e
	}
	old, e := os.ReadFile("/etc/network/interfaces")
	if e != nil {
		return e
	}
	resolver, _ := os.ReadFile("/etc/resolv.conf")
	previous := confirmed(existing)
	s.Previous = string(old)
	s.PreviousResolver = string(resolver)
	s.PreviousSettings = &previous
	s.Pending = true
	s.Deadline = time.Now().Add(90 * time.Second)
	if e = config.WriteJSON("network.json", s); e != nil {
		return e
	}
	if e = config.NewPersistenceManager().SaveConfigToZfs(); e != nil {
		_ = config.WriteJSON("network.json", existing)
		return e
	}
	if e = config.AtomicWrite("/etc/network/interfaces", []byte(text), 0644); e != nil {
		go RecoverNetwork()
		return e
	}
	if e = config.AtomicWrite("/etc/resolv.conf", []byte(resolverText(s)), 0644); e != nil {
		go RecoverNetwork()
		return e
	}
	go func() {
		time.Sleep(2 * time.Second)
		_, _ = command.Run("", "ifdown", s.Interface)
		_, _ = command.Run("", "ifup", s.Interface)
		time.Sleep(time.Until(s.Deadline))
		var current NetworkSettings
		if config.ReadJSON("network.json", &current) == nil && current.Pending && current.Deadline.Equal(s.Deadline) {
			_ = RecoverNetwork()
		}
	}()
	return nil
}
func ConfirmNetwork() error {
	networkMu.Lock()
	defer networkMu.Unlock()
	var s NetworkSettings
	if e := config.ReadJSON("network.json", &s); e != nil {
		return e
	}
	if !s.Pending || time.Now().After(s.Deadline) {
		return fmt.Errorf("確認期限已過")
	}
	old := s
	s = confirmed(s)
	if e := config.WriteJSON("network.json", s); e != nil {
		return e
	}
	if e := config.NewPersistenceManager().SaveConfigToZfs(); e != nil {
		_ = config.WriteJSON("network.json", old)
		return e
	}
	return nil
}
func RecoverNetwork() error {
	networkMu.Lock()
	defer networkMu.Unlock()
	var s NetworkSettings
	if e := config.ReadJSON("network.json", &s); os.IsNotExist(e) {
		return nil
	} else if e != nil {
		return e
	}
	if s.Interface == "" {
		return nil
	}
	text, e := networkText(s)
	if e != nil {
		return e
	}
	resolver := resolverText(s)
	if s.Pending {
		text = s.Previous
		resolver = s.PreviousResolver
		s = rollbackSettings(s)
	}
	if e = config.AtomicWrite("/etc/network/interfaces", []byte(text), 0644); e != nil {
		return e
	}
	if e = config.AtomicWrite("/etc/resolv.conf", []byte(resolver), 0644); e != nil {
		return e
	}
	_, _ = command.Run("", "ifdown", s.Interface)
	if _, e = command.Run("", "ifup", s.Interface); e != nil {
		return e
	}
	s = confirmed(s)
	if e = config.WriteJSON("network.json", s); e != nil {
		return e
	}
	return config.NewPersistenceManager().SaveConfigToZfs()
}
