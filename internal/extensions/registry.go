package extensions

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const RegistryRepository = "joeboy0722/just-nas-extensions"
const registryRoot = "https://ghcr.io/v2/" + RegistryRepository
const CatalogURL = registryRoot + "/manifests/catalog-v1"
const catalogMediaType = "application/vnd.just-nas.catalog.v1+json"
const ociManifestType = "application/vnd.oci.image.manifest.v1+json"

func registryClient(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout, CheckRedirect: func(q *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return fmt.Errorf("套件下載重新導向過多")
		}
		if q.URL.Scheme != "https" {
			return fmt.Errorf("套件下載必須使用 HTTPS")
		}
		if q.URL.Host != "ghcr.io" {
			q.Header.Del("Authorization")
		}
		return nil
	}}
}

func validRegistryURL(raw string, manifest bool) bool {
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "https" || u.Host != "ghcr.io" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.RawPath != "" {
		return false
	}
	if manifest {
		return raw == CatalogURL
	}
	prefix := "/v2/" + RegistryRepository + "/blobs/sha256:"
	return strings.HasPrefix(u.Path, prefix) && digestID.MatchString(strings.TrimPrefix(u.Path, prefix))
}

// Public GHCR artifacts need a short-lived anonymous bearer token, never a user PAT.
// The token service and repository scope are compiled in, not supplied by a challenge.
func registryGet(ctx context.Context, client *http.Client, target, accept string) (*http.Response, error) {
	if !validRegistryURL(target, target == CatalogURL) {
		return nil, fmt.Errorf("下載來源不受支援")
	}
	get := func(token string) (*http.Response, error) {
		q, e := http.NewRequestWithContext(ctx, "GET", target, nil)
		if e != nil {
			return nil, e
		}
		if accept != "" {
			q.Header.Set("Accept", accept)
		}
		if token != "" {
			q.Header.Set("Authorization", "Bearer "+token)
		}
		return client.Do(q)
	}
	res, e := get("")
	if e != nil || res.StatusCode != http.StatusUnauthorized {
		return res, e
	}
	res.Body.Close()
	q, e := http.NewRequestWithContext(ctx, "GET", "https://ghcr.io/token?service=ghcr.io&scope="+url.QueryEscape("repository:"+RegistryRepository+":pull"), nil)
	if e != nil {
		return nil, e
	}
	auth, e := client.Do(q)
	if e != nil {
		return nil, e
	}
	defer auth.Body.Close()
	if auth.StatusCode != 200 {
		return nil, fmt.Errorf("無法匿名取得官方套件（HTTP %d）；套件必須公開", auth.StatusCode)
	}
	var token struct {
		Token string `json:"token"`
	}
	if e = json.NewDecoder(io.LimitReader(auth.Body, 64<<10)).Decode(&token); e != nil {
		return nil, fmt.Errorf("套件下載授權回應無效")
	}
	if len(token.Token) == 0 || len(token.Token) > 32768 {
		return nil, fmt.Errorf("套件下載授權回應無效")
	}
	return get(token.Token)
}

type registryDescriptor struct {
	MediaType string `json:"mediaType"`
	Digest    string `json:"digest"`
	Size      int64  `json:"size"`
}

func catalogDescriptor(raw []byte) (registryDescriptor, error) {
	var m struct {
		SchemaVersion int                  `json:"schemaVersion"`
		MediaType     string               `json:"mediaType"`
		Layers        []registryDescriptor `json:"layers"`
	}
	if e := json.Unmarshal(raw, &m); e != nil {
		return registryDescriptor{}, e
	}
	if m.SchemaVersion != 2 || m.MediaType != ociManifestType || len(m.Layers) != 1 {
		return registryDescriptor{}, fmt.Errorf("官方目錄 OCI 格式無效")
	}
	d := m.Layers[0]
	if d.MediaType != catalogMediaType || !strings.HasPrefix(d.Digest, "sha256:") || !digestID.MatchString(strings.TrimPrefix(d.Digest, "sha256:")) || d.Size <= 0 || d.Size > 2<<20 {
		return registryDescriptor{}, fmt.Errorf("官方目錄 OCI 內容無效")
	}
	return d, nil
}
func verifyRegistryBlob(raw []byte, d registryDescriptor) error {
	h := sha256.Sum256(raw)
	if int64(len(raw)) != d.Size || "sha256:"+hex.EncodeToString(h[:]) != d.Digest {
		return fmt.Errorf("官方目錄 OCI 校驗失敗")
	}
	return nil
}
