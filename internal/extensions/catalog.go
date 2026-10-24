package extensions

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

type CatalogEntry struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Version      string            `json:"version"`
	Kind         string            `json:"kind"`
	URL          string            `json:"url"`
	Bytes        int64             `json:"bytes"`
	SHA256       string            `json:"sha256"`
	License      string            `json:"license"`
	Kernel       string            `json:"kernel,omitempty"`
	Dependencies map[string]string `json:"dependencies"`
	Reboot       bool              `json:"reboot"`
}
type Catalog struct {
	Source  string         `json:"source,omitempty"`
	Version int            `json:"version"`
	ABI     string         `json:"abi"`
	Entries []CatalogEntry `json:"entries"`
}

func VerifyCatalog(raw []byte) (Catalog, error) {
	keys, e := Keys()
	if e != nil {
		return Catalog{}, e
	}
	return verifyCatalog(raw, keys)
}
func verifyCatalog(raw []byte, keys []ed25519.PublicKey) (Catalog, error) {
	var env struct {
		Catalog   json.RawMessage `json:"catalog"`
		Signature string          `json:"signature"`
	}
	var c Catalog
	if e := strict(raw, &env); e != nil {
		return c, e
	}
	sig, e := base64.StdEncoding.DecodeString(env.Signature)
	if e != nil {
		return c, e
	}
	trusted := false
	for _, k := range keys {
		if ed25519.Verify(k, env.Catalog, sig) {
			trusted = true
			break
		}
	}
	if !trusted {
		return c, fmt.Errorf("商店目錄簽章無效")
	}
	if e = strict(env.Catalog, &c); e != nil {
		return c, e
	}
	if c.Version != 1 || c.ABI != NASABI {
		return c, fmt.Errorf("商店格式不相容")
	}
	seen := map[string]bool{}
	for _, v := range c.Entries {
		if !identifier.MatchString(v.ID) || !versionID.MatchString(v.Version) || !digestID.MatchString(v.SHA256) || v.Bytes <= 0 || v.Bytes > MaxBundle || seen[v.ID] {
			return c, fmt.Errorf("商店項目無效")
		}
		seen[v.ID] = true
	}
	return c, nil
}
func FetchCatalog(ctx context.Context) (Catalog, error) {
	c, e := fetchRemoteCatalog(ctx)
	if e == nil {
		c.Source = "network"
		return c, nil
	}
	b, err := os.ReadFile("/usr/share/just-nas/catalog.json")
	if err == nil {
		bundled, err := VerifyCatalog(b)
		if err == nil {
			bundled.Source = "bundled"
			return bundled, nil
		}
	}
	return c, e
}
func fetchRemoteCatalog(ctx context.Context) (Catalog, error) {
	var c Catalog
	client := &http.Client{Timeout: 5 * time.Second}
	r, e := http.NewRequestWithContext(ctx, "GET", CatalogURL, nil)
	if e != nil {
		return c, e
	}
	res, e := client.Do(r)
	if e != nil {
		return c, e
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return c, fmt.Errorf("官方目錄尚未提供或下載失敗（HTTP %d）；可使用離線匯入", res.StatusCode)
	}
	b, e := io.ReadAll(io.LimitReader(res.Body, 2<<20))
	if e != nil {
		return c, e
	}
	return VerifyCatalog(b)
}
