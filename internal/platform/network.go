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
	Interface string    `json:"interface"`
	DHCP      bool      `json:"dhcp"`
	Address   string    `json:"address"`
	Gateway   string    `json:"gateway"`
	DNS       []string  `json:"dns"`
	Pending   bool      `json:"pending"`
	Deadline  time.Time `json:"deadline"`
	Previous  string    `json:"previous,omitempty"`
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
	if e != nil || !p.Addr().Is4() {
		return "", fmt.Errorf("請輸入 IPv4 CIDR，例如 192.168.1.10/24")
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
	if len(s.DNS) > 0 {
		base += " dns-nameservers " + strings.Join(s.DNS, " ") + "\n"
	}
	return base, nil
}
func ConfigureNetwork(s NetworkSettings) error {
	networkMu.Lock()
	defer networkMu.Unlock()
	text, e := networkText(s)
	if e != nil {
		return e
	}
	old, e := os.ReadFile("/etc/network/interfaces")
	if e != nil {
		return e
	}
	s.Previous = string(old)
	s.Pending = true
	s.Deadline = time.Now().Add(90 * time.Second)
	if e = config.WriteJSON("network.json", s); e != nil {
		return e
	}
	if e = config.NewPersistenceManager().SaveConfigToZfs(); e != nil {
		return e
	}
	if e = config.AtomicWrite("/etc/network/interfaces", []byte(text), 0644); e != nil {
		return e
	}
	go func() {
		time.Sleep(2 * time.Second)
		_, _ = command.Run("", "ifdown", s.Interface)
		_, _ = command.Run("", "ifup", s.Interface)
		time.Sleep(time.Until(s.Deadline))
		var current NetworkSettings
		if config.ReadJSON("network.json", &current) == nil && current.Pending {
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
	s.Pending = false
	s.Previous = ""
	if e := config.WriteJSON("network.json", s); e != nil {
		return e
	}
	return config.NewPersistenceManager().SaveConfigToZfs()
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
	if s.Pending {
		text = s.Previous
		s.Pending = false
		s.Interface = ""
	}
	if e = config.AtomicWrite("/etc/network/interfaces", []byte(text), 0644); e != nil {
		return e
	}
	if s.Interface != "" {
		_, _ = command.Run("", "ifdown", s.Interface)
		_, e = command.Run("", "ifup", s.Interface)
	} else {
		_, _ = command.Run("", "rc-service", "networking", "restart")
	}
	if e != nil {
		return e
	}
	if e = config.WriteJSON("network.json", s); e != nil {
		return e
	}
	return config.NewPersistenceManager().SaveConfigToZfs()
}
