package platform

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"just-nas/internal/config"
	"math/big"
	"net"
	"os"
	"time"
)

type TLSSettings struct {
	Enabled     bool   `json:"enabled"`
	Certificate string `json:"certificate"`
	PrivateKey  string `json:"private_key,omitempty"`
}

func TLSConfig() (TLSSettings, error) {
	var s TLSSettings
	e := config.ReadJSON("tls.json", &s)
	return s, e
}
func EnsureCertificate() (TLSSettings, error) {
	s, e := TLSConfig()
	if e == nil {
		return s, nil
	}
	if !os.IsNotExist(e) {
		return s, e
	}
	key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		return s, e
	}
	serial, e := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if e != nil {
		return s, e
	}
	t := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "just-nas"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().AddDate(2, 0, 0), KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, DNSNames: []string{"just-nas", "just-nas.local", "localhost"}, IsCA: true, BasicConstraintsValid: true}
	addrs, e := net.InterfaceAddrs()
	if e != nil {
		return s, e
	}
	for _, a := range addrs {
		ip, _, e := net.ParseCIDR(a.String())
		if e == nil {
			t.IPAddresses = append(t.IPAddresses, ip)
		}
	}
	der, e := x509.CreateCertificate(rand.Reader, t, t, &key.PublicKey, key)
	if e != nil {
		return s, e
	}
	k, e := x509.MarshalPKCS8PrivateKey(key)
	if e != nil {
		return s, e
	}
	s = TLSSettings{Enabled: false, Certificate: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})), PrivateKey: string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: k}))}
	if e = config.WriteJSON("tls.json", s); e != nil {
		return s, e
	}
	if config.Persistent() {
		e = config.NewPersistenceManager().SaveConfigToZfs()
	}
	return s, e
}
func SetTLS(s TLSSettings) error {
	old, e := EnsureCertificate()
	if e != nil {
		return e
	}
	if s.Certificate == "" {
		s.Certificate = old.Certificate
		s.PrivateKey = old.PrivateKey
	}
	pair, e := tls.X509KeyPair([]byte(s.Certificate), []byte(s.PrivateKey))
	if e != nil {
		return fmt.Errorf("憑證與私鑰不符: %w", e)
	}
	cert, e := x509.ParseCertificate(pair.Certificate[0])
	if e != nil || time.Now().After(cert.NotAfter) {
		return fmt.Errorf("憑證無效或已過期")
	}
	if e = config.WriteJSON("tls.json", s); e != nil {
		return e
	}
	if e = config.NewPersistenceManager().SaveConfigToZfs(); e != nil {
		_ = config.WriteJSON("tls.json", old)
		return e
	}
	return nil
}
func ServerTLS() *tls.Config {
	return &tls.Config{MinVersion: tls.VersionTLS12, GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
		s, e := TLSConfig()
		if e != nil {
			return nil, e
		}
		pair, e := tls.X509KeyPair([]byte(s.Certificate), []byte(s.PrivateKey))
		return &pair, e
	}}
}
