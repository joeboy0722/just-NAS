// Package extensions manages declarative, verified NAS packages. No install scripts are accepted.
package extensions

import (
	"archive/tar"
	"bytes"
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
const NativeABI = "just-nas-stores-v2"
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
	Icon           string            `json:"icon,omitempty"`
	MinNASVersion  string            `json:"min_nas_version,omitempty"`
	Native         *NativeSpec       `json:"native,omitempty"`
}
type NativeSpec struct {
	Service string `json:"service"`
}

func (m Manifest) IsNative() bool { return m.Kind == "app" && m.Native != nil }

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
	if m.Native != nil && !m.IsNative() {
		return fmt.Errorf("原生規格只能用於應用")
	}
	if m.Icon != "" {
		f, ok := m.Files[m.Icon]
		if m.Kind != "app" || (m.Icon != "assets/icon.png" && m.Icon != "assets/icon.webp") || !ok || f.Size <= 0 || f.Size > 256<<10 || f.Mode != 0644 || (m.MinNASVersion != "2.1.0-rc3" && m.MinNASVersion != "2.1.0-rc4") {
			return fmt.Errorf("圖標格式、大小或最低相容版本無效（需要 2.1.0-rc3）")
		}
	}
	if m.MinNASVersion != "" && m.MinNASVersion != "2.1.0-rc3" && m.MinNASVersion != "2.1.0-rc4" {
		return fmt.Errorf("套件需要不支援的 NAS 版本 %s", m.MinNASVersion)
	}
	formatOK := m.Format == 1 && m.ABI == NASABI || m.Format == 2 && m.ABI == NativeABI && m.MinNASVersion == "2.1.0-rc4"
	if !formatOK || !identifier.MatchString(m.ID) || !versionID.MatchString(m.Version) || m.Arch != "x86_64" || m.Name == "" || m.License == "" {
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
			if m.IsNative() {
				if n != "bin/"+m.ID && n != m.Icon && !strings.HasPrefix(n, "web/") && !strings.HasPrefix(n, "licenses/") {
					return fmt.Errorf("原生應用寫入範圍無效")
				}
			} else if n != "image.tar" && n != m.Icon && !strings.HasPrefix(n, "licenses/") {
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
	if m.Kind == "runtime" && m.ID != "container-engine" && !(m.ID == "qemu-runtime" && m.Format == 2) || m.Kind == "ssh" && m.ID != "ssh-server" {
		return fmt.Errorf("系統能力識別無效")
	}
	if m.IsNative() {
		if m.Format != 2 || m.ID != "vm-manager" || m.Native.Service != m.ID || m.App != nil {
			return fmt.Errorf("未核准的原生應用能力")
		}
		f, ok := m.Files["bin/"+m.ID]
		if !ok || f.Mode != 0755 {
			return fmt.Errorf("缺少原生服務執行檔")
		}
		if _, ok := m.Dependencies["qemu-runtime"]; !ok {
			return fmt.Errorf("缺少 QEMU 執行環境相依")
		}
	} else if m.Kind == "app" {
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
// Only regular files and backward hard links to verified members are accepted; symbolic links are forbidden.
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
	if !result.Official && (result.Manifest.Kind == "runtime" || result.Manifest.Kind == "ssh" || result.Manifest.IsNative()) {
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
			if n == result.Manifest.Icon || (result.Manifest.IsNative() && n == "bin/"+result.Manifest.ID) {
				return result, fmt.Errorf("圖標必須為獨立的一般檔案")
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
		var icon bytes.Buffer
		var executable prefixCapture
		if result.Manifest.IsNative() && n == "bin/"+result.Manifest.ID {
			writer = io.MultiWriter(writer, &executable)
		}
		if n == result.Manifest.Icon {
			writer = io.MultiWriter(writer, &icon)
		}
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
			writer = io.MultiWriter(writer, out)
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
		if result.Manifest.IsNative() && n == "bin/"+result.Manifest.ID {
			h := executable.data
			if len(h) < 20 || string(h[:4]) != "\x7fELF" || h[4] != 2 || h[5] != 1 || h[18] != 0x3e || h[19] != 0 {
				return result, fmt.Errorf("原生服務必須為 x86_64 ELF 執行檔")
			}
		}
		if n == result.Manifest.Icon {
			if _, err := validateIcon(icon.Bytes(), n); err != nil {
				return result, err
			}
		}
	}
	if len(seen) != len(result.Manifest.Files) {
		return result, fmt.Errorf("套件檔案不完整")
	}
	// Drain gzip to verify its CRC, and reject hidden trailing decompressed data.
	tail, e := io.ReadAll(io.LimitReader(gz, (64<<10)+1))
	if e != nil {
		return result, e
	}
	if len(tail) > 64<<10 {
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

type prefixCapture struct{ data []byte }

func (p *prefixCapture) Write(b []byte) (int, error) {
	n := len(b)
	if len(p.data) < 64 {
		limit := 64 - len(p.data)
		if len(b) > limit {
			b = b[:limit]
		}
		p.data = append(p.data, b...)
	}
	return n, nil
}
