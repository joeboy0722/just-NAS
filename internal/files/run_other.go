//go:build !linux

package files

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

func Run(ctx context.Context, q Request, uid, gid uint32, in io.Reader, out io.Writer) error {
	return fmt.Errorf("檔案服務需要 Linux 身分隔離")
}

// Non-Linux workers are test-only: Run refuses to start a production worker here.
func renameBetween(from *os.Root, source string, to *os.Root, target string) error {
	return os.Rename(filepath.Join(from.Name(), source), filepath.Join(to.Name(), target))
}

func DeletionTime(st os.FileInfo) time.Time { return st.ModTime() }
