// Package extensions manages declarative, verified NAS packages. No install scripts are accepted.
package extensions

import (
	"archive/tar"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

const FormatVersion = 1
const NASABI = "just-nas-stores-v1"
const MaxBundle = int64(4 << 30)

var identifier = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)
var versionID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9.+_-]{0,79}$`)
var digestID = regexp.MustCompile(`^[a-f0-9]{64}$`)
var imageID = regexp.MustCompile(`^(?:[a-zA-Z0-9][a-zA-Z0-9./:_-]+@)?sha256:[a-f0-9]{64}$`)

type File struct {
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
	Mode   int64  `json:"mode"`
}
type Manifest struct {
	Format         int               `json:"format"`
	ID             string            `json:"id"`
	Name           string            `json:"name"`
	Version        string            `json:"version"`
	Kind           string            `json:"kind"`
	ABI            string            `json:"abi"`
	Arch           string            `json:"arch"`
	Kernel         string            `json:"kernel,omitempty"`
	License        string            `json:"license"`
	Dependencies   map[string]string `json:"dependencies"`
	Files          map[string]File   `json:"files"`
	InstalledBytes int64             `json:"installed_bytes"`
	Permissions    []string          `json:"permissions"`
	Reboot         bool              `json:"reboot"`
	App            *AppSpec          `json:"app,omitempty"`
}
type AppSpec struct {
	Image         string  `json:"image"`
	User          string  `json:"user"`
	ContainerPort int     `json:"container_port"`
	ContentPath   string  `json:"content_path"`
	HealthPath    string  `json:"health_path"`
	MemoryMB      int     `json:"memory_mb"`
	CPUs          float64 `json:"cpus"`
}
type Verified struct {
	Manifest Manifest `json:"manifest"`
	Official bool     `json:"official"`
	Hash     string   `json:"sha256"`
}
type Envelope struct {
	Manifest  json.RawMessage `json:"manifest"`
	Signature string          `json:"signature"`
}

func strict(raw []byte, v any) error {
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		return e
	}
	if e := d.Decode(new(any)); e != io.EOF {
		return fmt.Errorf("多餘 JSON 內容")
	}
	return nil
}
func safePath(s string) bool {
	return s != "" && !strings.ContainsAny(s, "\\\x00:") && !strings.HasPrefix(s, "/") && path.Clean(s) == s && s != "." && s != ".." && !strings.HasPrefix(s, "../")
}
func (m Manifest) Validate() error {
	if m.Format != FormatVersion || !identifier.MatchString(m.ID) || !versionID.MatchString(m.Version) || m.ABI != NASABI || m.Arch != "x86_64" || m.Name == "" || m.License == "" {
		return fmt.Errorf("套件識別或相容格式無效")
	}
	if len(m.Files) == 0 || len(m.Files) > 20000 {
		return fmt.Errorf("套件檔案數無效")
	}
	var total int64
	unique := map[string]bool{}
	for n, f := range m.Files {
		if !safePath(n) || !digestID.MatchString(f.SHA256) || f.Size < 0 || f.Size > MaxBundle || f.Mode != 0644 && f.Mode != 0755 {
			return fmt.Errorf("套件檔案無效: %s", n)
		}
		key := f.SHA256 + fmt.Sprint(f.Mode)
		if !unique[key] {
			total += f.Size
			unique[key] = true
		}
		if total > MaxBundle {
			return fmt.Errorf("套件解壓大小過大")
		}
		switch m.Kind {
		case "driver":
			if m.Kernel == "" || !versionID.MatchString(m.Kernel) || !(strings.HasPrefix(n, "modules/"+m.Kernel+"/") || strings.HasPrefix(n, "modules/firmware/") || strings.HasPrefix(n, "licenses/")) {
				return fmt.Errorf("驅動寫入範圍無效")
			}
		case "runtime", "ssh":
			if !strings.HasPrefix(n, "apks/") && !strings.HasPrefix(n, "licenses/") {
				return fmt.Errorf("系統套件只接受離線 APK 與授權文件")
			}
			if strings.HasPrefix(n, "apks/") && (!strings.HasSuffix(n, ".apk") || strings.Count(n, "/") != 1) {
				return fmt.Errorf("APK 路徑無效")
			}
		case "app":
			if n != "image.tar" && !strings.HasPrefix(n, "licenses/") {
				return fmt.Errorf("應用只接受映像及授權文件")
			}
		default:
			return fmt.Errorf("不支援的套件類型")
		}
	}
	if total != m.InstalledBytes {
		return fmt.Errorf("宣告安裝大小不符")
	}
	for id, v := range m.Dependencies {
		if !identifier.MatchString(id) || !versionID.MatchString(v) || id == m.ID {
			return fmt.Errorf("套件相依無效")
		}
	}
	if m.Kind == "runtime" && m.ID != "container-engine" || m.Kind == "ssh" && m.ID != "ssh-server" {
		return fmt.Errorf("系統能力識別無效")
	}
	if m.Kind == "app" {
		a := m.App
		if a == nil || !imageID.MatchString(a.Image) || !regexp.MustCompile(`^[1-9][0-9]{0,8}:[1-9][0-9]{0,8}$`).MatchString(a.User) || a.ContainerPort < 1024 || a.ContainerPort > 65535 || a.ContentPath != "/srv/content" || a.HealthPath != "/" || a.MemoryMB < 32 || a.MemoryMB > 16384 || a.CPUs < 0.1 || a.CPUs > 16 {
			return fmt.Errorf("應用規格無效")
		}
		if _, ok := m.Files["image.tar"]; !ok {
			return fmt.Errorf("缺少離線應用映像")
		}
		if _, ok := m.Dependencies["container-engine"]; !ok {
			return fmt.Errorf("缺少容器引擎相依")
		}
	} else if m.App != nil {
		return fmt.Errorf("系統套件不能包含應用規格")
	}
	return nil
}

// Verify streams the archive, rejecting links, duplicate paths, unexpected members and size/hash mismatches.
// Only regular files are supported, so a package cannot create a symlink escape during extraction.
func Verify(filename string, keys []ed25519.PublicKey, destination string) (Verified, error) {
	var result Verified
	src, e := os.Open(filename)
	if e != nil {
		return result, e
	}
	defer src.Close()
	st, e := src.Stat()
	if e != nil || st.Size() > MaxBundle {
		return result, fmt.Errorf("套件大小無效")
	}
	gz, e := gzip.NewReader(src)
	if e != nil {
		return result, e
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	h, e := tr.Next()
	if e != nil || h.Name != "package.json" || h.Size > 1<<20 || h.Typeflag != tar.TypeReg {
		return result, fmt.Errorf("缺少首筆套件描述")
	}
	raw, e := io.ReadAll(io.LimitReader(tr, 1<<20))
	if e != nil {
		return result, e
	}
	var env Envelope
	if e = strict(raw, &env); e != nil {
		return result, e
	}
	if e = strict(env.Manifest, &result.Manifest); e != nil {
		return result, e
	}
	if e = result.Manifest.Validate(); e != nil {
		return result, e
	}
	if env.Signature != "" {
		sig, err := base64.StdEncoding.DecodeString(env.Signature)
		if err != nil {
			return result, err
		}
		for _, key := range keys {
			if ed25519.Verify(key, env.Manifest, sig) {
				result.Official = true
				break
			}
		}
		if !result.Official {
			return result, fmt.Errorf("官方簽章無效或簽署者不受信任")
		}
	}
	if !result.Official && (result.Manifest.Kind == "runtime" || result.Manifest.Kind == "ssh") {
		return result, fmt.Errorf("系統 APK 套件須由官方簽署")
	}
	seen := map[string]bool{}
	var count int
	for {
		h, e = tr.Next()
		if errors.Is(e, io.EOF) {
			break
		}
		if e != nil {
			return result, e
		}
		count++
		if count > 20000 || (h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeLink) || !strings.HasPrefix(h.Name, "payload/") {
			return result, fmt.Errorf("套件含連結或不允許的檔案")
		}
		n := strings.TrimPrefix(h.Name, "payload/")
		f, ok := result.Manifest.Files[n]
		if !ok || seen[n] || (h.Typeflag == tar.TypeReg && h.Size != f.Size) {
			return result, fmt.Errorf("非預期或重複套件檔案: %s", n)
		}
		if h.Typeflag == tar.TypeLink {
			target := strings.TrimPrefix(h.Linkname, "payload/")
			previous, exists := result.Manifest.Files[target]
			if !strings.HasPrefix(h.Linkname, "payload/") || !safePath(target) || !seen[target] || !exists || previous != f || h.Size != 0 {
				return result, fmt.Errorf("不安全的硬連結")
			}
			if destination != "" {
				dst := filepath.Join(destination, filepath.FromSlash(n))
				if e = os.MkdirAll(filepath.Dir(dst), 0700); e != nil {
					return result, e
				}
				if e = os.Link(filepath.Join(destination, filepath.FromSlash(target)), dst); e != nil {
					return result, e
				}
			}
			seen[n] = true
			continue
		}
		seen[n] = true
		hash := sha256.New()
		var writer io.Writer = hash
		var out *os.File
		if destination != "" {
			target := filepath.Join(destination, filepath.FromSlash(n))
			if e = os.MkdirAll(filepath.Dir(target), 0700); e != nil {
				return result, e
			}
			out, e = os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, os.FileMode(f.Mode))
			if e != nil {
				return result, e
			}
			writer = io.MultiWriter(hash, out)
		}
		_, e = io.Copy(writer, tr)
		if out != nil {
			if se := out.Sync(); e == nil {
				e = se
			}
			if ce := out.Close(); e == nil {
				e = ce
			}
		}
		if e != nil {
			return result, e
		}
		if hex.EncodeToString(hash.Sum(nil)) != f.SHA256 {
			return result, fmt.Errorf("檔案校驗失敗: %s", n)
		}
	}
	if len(seen) != len(result.Manifest.Files) {
		return result, fmt.Errorf("套件檔案不完整")
	}
	// Drain gzip to verify its CRC, and reject hidden trailing decompressed data.
	tail, e := io.ReadAll(io.LimitReader(gz, 1024))
	if e != nil {
		return result, e
	}
	if len(tail) == 1024 {
		return result, fmt.Errorf("套件尾端過長")
	}
	for _, b := range tail {
		if b != 0 {
			return result, fmt.Errorf("套件尾端內容無效")
		}
	}
	if _, e = src.Seek(0, 0); e != nil {
		return result, e
	}
	hash := sha256.New()
	if _, e = io.Copy(hash, src); e != nil {
		return result, e
	}
	result.Hash = hex.EncodeToString(hash.Sum(nil))
	return result, nil
}
