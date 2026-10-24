package extensions

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type registryTransport func(*http.Request) (*http.Response, error)

func (f registryTransport) RoundTrip(q *http.Request) (*http.Response, error) { return f(q) }
func TestRegistryURLsRejectOtherHostsAndRepositories(t *testing.T) {
	good := registryRoot + "/blobs/sha256:" + strings.Repeat("a", 64)
	if !validRegistryURL(good, false) || !validRegistryURL(CatalogURL, true) {
		t.Fatal("official URL rejected")
	}
	for _, u := range []string{"http" + good[5:], strings.Replace(good, "ghcr.io", "ghcr.io.evil.test", 1), strings.Replace(good, RegistryRepository, "someone/else", 1), good + "?token=x", good + "#x", strings.Replace(good, "/blobs/", "/%62lobs/", 1), strings.Replace(good, "https://", "https://user@", 1)} {
		if validRegistryURL(u, false) {
			t.Fatal("unsafe URL accepted", u)
		}
	}
}
func TestAnonymousRegistryChallengeDoesNotSendPersonalCredentials(t *testing.T) {
	target := registryRoot + "/blobs/sha256:" + strings.Repeat("a", 64)
	calls := 0
	client := &http.Client{Transport: registryTransport(func(q *http.Request) (*http.Response, error) {
		calls++
		status := 200
		body := "payload"
		switch calls {
		case 1:
			if q.URL.String() != target || q.Header.Get("Authorization") != "" {
				t.Fatal("unexpected initial request")
			}
			status = 401
		case 2:
			if q.URL.Host != "ghcr.io" || q.URL.Path != "/token" || q.URL.Query().Get("scope") != "repository:"+RegistryRepository+":pull" || q.Header.Get("Authorization") != "" {
				t.Fatal("unexpected token request")
			}
			body = `{"token":"anonymous-short-lived"}`
		case 3:
			if q.URL.String() != target || q.Header.Get("Authorization") != "Bearer anonymous-short-lived" {
				t.Fatal("wrong bearer target")
			}
		default:
			t.Fatal("unexpected request")
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}
	res, e := registryGet(context.Background(), client, target, "")
	if e != nil {
		t.Fatal(e)
	}
	defer res.Body.Close()
	if calls != 3 || res.StatusCode != 200 {
		t.Fatal("challenge flow failed")
	}
}
func TestCatalogOCIRequiresMatchingLayerAndDigest(t *testing.T) {
	raw := []byte(`{"catalog":{},"signature":"x"}`)
	h := sha256.Sum256(raw)
	d := registryDescriptor{MediaType: catalogMediaType, Digest: "sha256:" + hex.EncodeToString(h[:]), Size: int64(len(raw))}
	m, _ := json.Marshal(map[string]any{"schemaVersion": 2, "mediaType": ociManifestType, "layers": []registryDescriptor{d}})
	parsed, e := catalogDescriptor(m)
	if e != nil || verifyRegistryBlob(raw, parsed) != nil {
		t.Fatal("valid layer rejected", e)
	}
	if verifyRegistryBlob(append(raw, 'x'), parsed) == nil {
		t.Fatal("altered layer accepted")
	}
	d.MediaType = "application/octet-stream"
	m, _ = json.Marshal(map[string]any{"schemaVersion": 2, "mediaType": ociManifestType, "layers": []registryDescriptor{d}})
	if _, e = catalogDescriptor(m); e == nil {
		t.Fatal("wrong media accepted")
	}
}
func TestRegistryRedirectNeverForwardsBearerOutsideRegistry(t *testing.T) {
	client := registryClient(0)
	q, _ := http.NewRequest("GET", "https://example.test/blob", nil)
	q.Header.Set("Authorization", "Bearer secret")
	if e := client.CheckRedirect(q, nil); e != nil || q.Header.Get("Authorization") != "" {
		t.Fatal("bearer leaked")
	}
	q, _ = http.NewRequest("GET", "http://example.test/blob", nil)
	if client.CheckRedirect(q, nil) == nil {
		t.Fatal("HTTPS downgrade accepted")
	}
}
