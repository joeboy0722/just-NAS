package backup

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"just-nas/internal/command"
	"just-nas/internal/config"
	"just-nas/internal/storage"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

type Settings struct {
	Mount        string   `json:"mount"`
	UUID         string   `json:"uuid"`
	RepositoryID string   `json:"repository_id"`
	Password     string   `json:"password,omitempty"`
	Datasets     []string `json:"datasets"`
}

var mu sync.Mutex

func Get() (Settings, error)    { var s Settings; e := config.ReadJSON("backup.json", &s); return s, e }
func Public() (Settings, error) { s, e := Get(); s.Password = ""; return s, e }
func mountIdentity(path string) (string, error) {
	b, e := command.Run("", "findmnt", "-J", "-T", path, "-o", "TARGET,SOURCE,UUID,FSTYPE")
	if e != nil {
		return "", e
	}
	var x struct {
		Filesystems []struct{ Target, Source, UUID, Fstype string }
	}
	if json.Unmarshal(b, &x) != nil || len(x.Filesystems) != 1 {
		return "", fmt.Errorf("無法辨識備份掛載")
	}
	f := x.Filesystems[0]
	if f.Target != path || f.UUID == "" || f.Fstype != "ext4" || !strings.HasPrefix(f.Source, "/dev/") {
		return "", fmt.Errorf("請選擇已掛載的獨立 ext4 裝置根目錄，系統不會自動格式化")
	}
	// The backup disk cannot be a ZFS member or share an ancestor block device.
	b, e = command.Run("", "lsblk", "-s", "-nr", "-o", "MAJ:MIN", f.Source)
	if e != nil {
		return "", e
	}
	anc := strings.Fields(string(b))
	z, e := command.Run("", "zpool", "status", "-P")
	if e != nil {
		return "", e
	}
	for _, line := range strings.Split(string(z), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && strings.HasPrefix(fields[0], "/dev/") {
			c, e := command.Run("", "lsblk", "-s", "-nr", "-o", "MAJ:MIN", fields[0])
			if e != nil {
				return "", e
			}
			for _, a := range anc {
				for _, b := range strings.Fields(string(c)) {
					if a == b {
						return "", fmt.Errorf("備份目的地必須與資料池使用不同實體裝置")
					}
				}
			}
		}
	}
	return f.UUID, nil
}
func run(ctx context.Context, s Settings, out io.Writer, args ...string) error {
	uuid, e := mountIdentity(s.Mount)
	if e != nil || uuid != s.UUID {
		return fmt.Errorf("備份裝置未掛載或 UUID 不符；已停止")
	}
	f, e := os.CreateTemp(config.Dir, "restic-password-")
	if e != nil {
		return e
	}
	name := f.Name()
	defer os.Remove(name)
	if e = f.Chmod(0600); e == nil {
		_, e = f.WriteString(s.Password)
	}
	ce := f.Close()
	if e != nil {
		return e
	}
	if ce != nil {
		return ce
	}
	argv := []string{"-r", filepath.Join(s.Mount, "just-nas-restic"), "--password-file", name, "--cache-dir", filepath.Join(config.Dir, "restic-cache")}
	argv = append(argv, args...)
	return command.Stream(ctx, nil, out, "restic", argv...)
}
func verify(ctx context.Context, s Settings) error {
	var out bytes.Buffer
	if e := run(ctx, s, &out, "cat", "config"); e != nil {
		return e
	}
	var c struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(out.Bytes(), &c) != nil || c.ID == "" || c.ID != s.RepositoryID {
		return fmt.Errorf("備份儲存庫識別不符")
	}
	return nil
}
func Configure(s Settings) error {
	mu.Lock()
	defer mu.Unlock()
	if !filepath.IsAbs(s.Mount) || filepath.Clean(s.Mount) != s.Mount || len(s.Password) < 12 || len(s.Password) > 256 || len(s.Datasets) == 0 {
		return fmt.Errorf("備份設定不完整；密碼至少 12 字元")
	}
	uuid, e := mountIdentity(s.Mount)
	if e != nil {
		return e
	}
	s.UUID = uuid
	for _, d := range s.Datasets {
		if !storage.ValidDataset(d) || storage.MountedDataset("/mnt/"+d) != nil {
			return fmt.Errorf("備份資料集無效")
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	repo := filepath.Join(s.Mount, "just-nas-restic")
	if _, e = os.Stat(filepath.Join(repo, "config")); os.IsNotExist(e) {
		var out bytes.Buffer
		if e = run(ctx, s, &out, "init"); e != nil {
			return fmt.Errorf("建立備份儲存庫失敗: %w", e)
		}
	}
	var out bytes.Buffer
	if e = run(ctx, s, &out, "cat", "config"); e != nil {
		return e
	}
	var c struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(out.Bytes(), &c) != nil || c.ID == "" {
		return fmt.Errorf("無法讀取儲存庫識別")
	}
	s.RepositoryID = c.ID
	if e = config.WriteJSON("backup.json", s); e != nil {
		return e
	}
	return config.NewPersistenceManager().SaveConfigToZfs()
}

type progressWriter struct {
	report func(string)
	last   time.Time
}

func (p *progressWriter) Write(b []byte) (int, error) {
	if time.Since(p.last) > time.Second {
		p.report(string(b))
		p.last = time.Now()
	}
	return len(b), nil
}
func Execute(ctx context.Context, kind string, report func(string)) error {
	mu.Lock()
	defer mu.Unlock()
	s, e := Get()
	if e != nil {
		return e
	}
	if e = verify(ctx, s); e != nil {
		return e
	}
	out := &progressWriter{report: report}
	switch kind {
	case "backup":
		tag := "nas-backup-" + fmt.Sprint(time.Now().UnixNano())
		args := []string{"backup", "--json", "--host", "just-nas", "--tag", "just-nas"}
		for _, ds := range s.Datasets {
			if !storage.ValidDataset(ds) || storage.MountedDataset("/mnt/"+ds) != nil {
				return fmt.Errorf("來源資料集未掛載")
			}
			snap := ds + "@" + tag
			if _, e = command.Run("", "zfs", "snapshot", snap); e != nil {
				return e
			}
			defer command.Run("", "zfs", "destroy", snap)
			args = append(args, "/mnt/"+ds+"/.zfs/snapshot/"+tag)
		}
		state, e := config.ExportFiles()
		if e != nil {
			return e
		}
		stage, e := os.MkdirTemp(config.Dir, "backup-state-")
		if e != nil {
			return e
		}
		defer os.RemoveAll(stage)
		b, _ := json.Marshal(state)
		if e = os.WriteFile(filepath.Join(stage, "nas-settings.json"), b, 0600); e != nil {
			return e
		}
		args = append(args, stage)
		if e = run(ctx, s, out, args...); e != nil {
			return e
		}
		return run(ctx, s, out, "forget", "--host", "just-nas", "--tag", "just-nas", "--group-by", "host", "--keep-daily", "7", "--keep-weekly", "4", "--keep-monthly", "6", "--prune")
	case "backup-check":
		return run(ctx, s, out, "check", "--read-data")
	default:
		return fmt.Errorf("unsupported backup task")
	}
}
func Snapshots(ctx context.Context) (json.RawMessage, error) {
	s, e := Get()
	if e != nil {
		return nil, e
	}
	if e = verify(ctx, s); e != nil {
		return nil, e
	}
	var out bytes.Buffer
	e = run(ctx, s, &out, "snapshots", "--json")
	return json.RawMessage(out.Bytes()), e
}
func Restore(ctx context.Context, snapshot, targetDataset string, report func(string)) error {
	mu.Lock()
	defer mu.Unlock()
	if !regexp.MustCompile(`^[a-f0-9]{8,64}$`).MatchString(snapshot) || !storage.ValidDataset(targetDataset) {
		return fmt.Errorf("指定完整備份 ID 與恢復用資料集")
	}
	root := "/mnt/" + targetDataset
	if e := storage.MountedDataset(root); e != nil {
		return e
	}
	s, e := Get()
	if e != nil {
		return e
	}
	if e = verify(ctx, s); e != nil {
		return e
	}
	target := filepath.Join(root, "restore-"+fmt.Sprint(time.Now().UnixNano()))
	if e = os.Mkdir(target, 0700); e != nil {
		return e
	}
	report("Restoring to " + target)
	return run(ctx, s, &progressWriter{report: report}, "restore", snapshot, "--target", target, "--verify")
}
