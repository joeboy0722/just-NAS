//go:build linux

package files

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"syscall"
	"time"
)

func Run(ctx context.Context, q Request, uid, gid uint32, in io.Reader, out io.Writer) error {
	exe, e := os.Executable()
	if e != nil {
		return e
	}
	root, e := os.OpenRoot(q.Root)
	if e != nil {
		return e
	}
	defer root.Close()
	rootFD, e := root.Open(".")
	if e != nil {
		return e
	}
	defer rootFD.Close()
	// Pass only the authenticated user's recycle directory, never its private parent.
	trash, e := root.Open(".nas-trash/" + q.User)
	if e != nil {
		return e
	}
	defer trash.Close()
	st, e := trash.Stat()
	if e != nil {
		return e
	}
	meta, ok := st.Sys().(*syscall.Stat_t)
	if !ok || meta.Uid != uid || !st.IsDir() {
		return fmt.Errorf("recycle owner does not match authenticated user")
	}
	q.Root = "/proc/self/fd/3"
	q.TrashRoot = "/proc/self/fd/4"
	cmd := exec.CommandContext(ctx, exe, "--file-worker")
	cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: uid, Gid: gid, Groups: []uint32{gid}}}
	cmd.ExtraFiles = []*os.File{rootFD, trash}
	cmd.WaitDelay = 2 * time.Second
	header, _ := json.Marshal(q)
	cmd.Stdin = io.MultiReader(bytes.NewReader(append(header, '\n')), in)
	cmd.Stdout = out
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if e = cmd.Run(); e != nil {
		return fmt.Errorf("file operation failed: %s", stderr.String())
	}
	return nil
}

func renameBetween(from *os.Root, source string, to *os.Root, target string) error {
	src, e := from.Open(path.Dir(source))
	if e != nil {
		return e
	}
	defer src.Close()
	dst, e := to.Open(path.Dir(target))
	if e != nil {
		return e
	}
	defer dst.Close()
	return syscall.Renameat(int(src.Fd()), path.Base(source), int(dst.Fd()), path.Base(target))
}

func DeletionTime(st os.FileInfo) time.Time {
	t := st.ModTime()
	if v, ok := st.Sys().(*syscall.Stat_t); ok {
		c := time.Unix(v.Ctim.Sec, v.Ctim.Nsec)
		if c.After(t) {
			t = c
		}
	}
	return t
}
