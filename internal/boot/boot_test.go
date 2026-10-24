package boot

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"regexp"
	"strings"
	"testing"
	"time"
)

func certificate(t *testing.T, ips ...string) string {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	c := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "just-nas"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	for _, s := range ips {
		c.IPAddresses = append(c.IPAddresses, net.ParseIP(s))
	}
	b, e := x509.CreateCertificate(rand.Reader, c, c, &key.PublicKey, key)
	if e != nil {
		t.Fatal(e)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: b}))
}
func TestColorsReturnToNormalWithoutChangingFailure(t *testing.T) {
	v := View{Stages: []Stage{{ID: "pools", Label: "Pools", State: OK}, {ID: "shares", Label: "Shares", State: Fail}}}
	s := Render(v, true)
	if !strings.Contains(s, "\x1b[31mOK") || !strings.Contains(s, "\x1b[32mFAIL") {
		t.Fatal("startup colors incorrect")
	}
	v.Ready = true
	s = Render(v, true)
	if !strings.Contains(s, "Welcome to Just NAS.") || !strings.Contains(s, "\x1b[31m[FAIL]") || strings.Contains(s, "System       \x1b[32mReady") {
		t.Fatal("failure hidden after welcome")
	}
	plain := Render(v, false)
	if strings.Contains(plain, "\x1b") || !strings.Contains(plain, "[FAIL] Shares") {
		t.Fatal("plain output lost failure")
	}
	v.Ready = false
	v.Stopped = true
	if strings.Contains(Render(v, false), "https://") {
		t.Fatal("unavailable listener advertised")
	}
}
func TestMultipleAddressesFiltersAndStableOrdering(t *testing.T) {
	ifs := []Interface{{Name: "eth1", Index: 2, Up: true, Addresses: []string{"192.168.10.2/24"}}, {Name: "br-lan", Index: 4, Up: true, Kind: "bridge", Addresses: []string{"10.2.0.1/24"}}, {Name: "bond0.20", Index: 5, Up: true, Kind: "vlan", Addresses: []string{"10.3.0.1/24"}}, {Name: "eth0", Index: 1, Up: true, Addresses: []string{"fd00::2/64", "192.168.1.22/24", "192.168.1.2/24", "192.168.1.2/24", "fe80::1/64", "169.254.1.1/16", "127.0.0.1/8", "0.0.0.0/0"}}, {Name: "custom-container", Index: 9, Up: true, Container: true, Addresses: []string{"172.19.0.1/16"}}, {Name: "custom-pair", Index: 10, Up: true, Kind: "veth", Addresses: []string{"172.20.0.2/16"}}, {Name: "eth2", Index: 11, Up: false, Addresses: []string{"10.4.0.1/24"}}}
	a := Addresses(ifs, "eth0", "https", "8443", certificate(t, "192.168.1.2"))
	if len(a) != 6 {
		t.Fatalf("addresses: %+v", a)
	}
	if a[0].IP != "192.168.1.2" || a[1].IP != "192.168.1.22" || a[2].URL != "https://[fd00::2]:8443" || a[3].Interface != "eth1" || a[4].Interface != "br-lan" {
		t.Fatal("incorrect primary, family or interface order", a)
	}
	if a[0].CertificateWarning || !a[1].CertificateWarning {
		t.Fatal("certificate coverage not distinguished")
	}
	if !a[0].Primary || a[3].Primary {
		t.Fatal("incorrect primary label")
	}
}
func TestPagesKeepPrimaryAndFitTerminal(t *testing.T) {
	addresses := []Address{}
	for i := 0; i < 11; i++ {
		addresses = append(addresses, Address{Interface: "bond0.123456789", URL: "https://[ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff]:8443", CertificateWarning: true})
	}
	seen := map[int]bool{}
	for p := 0; p < 4; p++ {
		a, page, pages := PageAddresses(addresses, p)
		if len(a) > 4 || page != p+1 || pages != 4 || a[0] != addresses[0] {
			t.Fatal("bad page", page, pages)
		}
		for j := 1 + p*3; j < len(addresses) && j < 1+(p+1)*3; j++ {
			seen[j] = true
		}
	}
	if len(seen) != 10 {
		t.Fatal("pagination lost addresses")
	}
	v := View{Ready: true, Storage: "Unavailable", PrimaryMissing: true, NetworkError: true, Addresses: addresses}
	for i := 0; i < 7; i++ {
		v.Stages = append(v.Stages, Stage{Label: "Service", State: Fail})
	}
	plain := regexp.MustCompile(`[[0-9;]*[A-Za-z]`).ReplaceAllString(Render(v, true), "")
	lines := strings.Split(strings.TrimRight(plain, "\r\n"), "\n")
	if len(lines) > 25 {
		t.Fatal("frame exceeds 25 rows", len(lines))
	}
	for _, line := range lines {
		if len(strings.TrimSuffix(line, "\r")) > 80 {
			t.Fatal("line exceeds 80 columns", line)
		}
	}
}
func TestPlainEventsAndControlSanitization(t *testing.T) {
	var out, serial bytes.Buffer
	d := New(&out, &serial, true, "")
	d.Set("pools", Running, "Loading")
	d.Set("pools", Fail, "bad\x1b[31m\nline")
	d.Finish(true)
	if strings.Contains(out.String(), "\x1b") || !strings.Contains(out.String(), "[FAIL]") || !strings.Contains(serial.String(), "[FAIL]") {
		t.Fatal("plain or serial output incorrect")
	}
	if !Plain("quiet just_nas_boot=plain\n") || Plain("xjust_nas_boot=plain") {
		t.Fatal("flag parsing")
	}
}
func TestHardwareTimeoutNeverReportsSuccess(t *testing.T) {
	old := Query
	defer func() { Query = old }()
	Query = func(context.Context, string, ...string) ([]byte, error) { return nil, context.DeadlineExceeded }
	state, _ := HardwareStatus(context.Background())
	if state == OK {
		t.Fatal("timeout reported as OK")
	}
	Query = func(context.Context, string, ...string) ([]byte, error) { return nil, errors.New("service failed") }
	state, _ = HardwareStatus(context.Background())
	if state == OK {
		t.Fatal("failure reported as OK")
	}
}

type interruptedWriter struct {
	fail   bool
	output bytes.Buffer
}

func (w *interruptedWriter) Write(b []byte) (int, error) {
	if w.fail {
		return 0, errors.New("terminal hangup")
	}
	return w.output.Write(b)
}
func TestFailedFrameIsRetriedInsteadOfCached(t *testing.T) {
	w := &interruptedWriter{fail: true}
	d := New(w, nil, false, "")
	d.Set("hardware", OK, "Checked")
	w.fail = false
	d.Refresh(nil, true, "First setup", false)
	if !strings.Contains(w.output.String(), "Core hardware drivers checked") {
		t.Fatal("failed frame was incorrectly cached")
	}
}
