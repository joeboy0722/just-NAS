package backup

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"just-nas/internal/storage"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
)

var restoreName = regexp.MustCompile(`^restore-[0-9]+$`)

func restoreRoot(ds string) (string, error) {
	if !storage.ValidDataset(ds) {
		return "", fmt.Errorf("恢復資料集無效")
	}
	root := "/mnt/" + ds
	if e := storage.MountedDataset(root); e != nil {
		return "", e
	}
	children, e := storage.ChildMounts(root)
	if e != nil {
		return "", e
	}
	if len(children) > 0 {
		return "", fmt.Errorf("恢復目錄包含其他掛載，拒絕跨裝置匯出")
	}
	return root, nil
}
func RestoreFolders(ds string) ([]string, error) {
	root, e := restoreRoot(ds)
	if e != nil {
		return nil, e
	}
	f, e := os.Open(root)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	result := []string{}
	for {
		batch, e := f.ReadDir(128)
		for _, d := range batch {
			if d.IsDir() && restoreName.MatchString(d.Name()) {
				result = append(result, d.Name())
				if len(result) >= 1000 {
					sort.Strings(result)
					return result, nil
				}
			}
		}
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, e
		}
	}
	sort.Strings(result)
	return result, nil
}
func ValidateRestoreDownload(ds, name string) (string, error) {
	if !restoreName.MatchString(name) {
		return "", fmt.Errorf("恢復目錄名稱無效")
	}
	root, e := restoreRoot(ds)
	if e != nil {
		return "", e
	}
	st, e := os.Lstat(filepath.Join(root, name))
	if e != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("恢復目錄不存在或不是一般目錄")
	}
	return root, nil
}

// StreamRestore exports only a validated restored directory, never an arbitrary host path.
func StreamRestore(ctx context.Context, root, name string, out io.Writer) error {
	cmd := exec.CommandContext(ctx, "tar", "-C", root, "-cf", "-", "--", name)
	cmd.Stdout = out
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if e := cmd.Run(); e != nil {
		return fmt.Errorf("恢復下載失敗: %s: %w", stderr.String(), e)
	}
	return nil
}
