package files

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strconv"
	"strings"
	"time"
)

type Request struct {
	Limited                             bool
	MaxBytes                            int64
	Root, Operation, Path, Target, User string
	SourceRoot                          string
	TrashRoot                           string
	Offset, Limit                       int
	Replace                             bool
}
type Entry struct {
	Name      string    `json:"name"`
	Size      int64     `json:"size"`
	Directory bool      `json:"directory"`
	Modified  time.Time `json:"modified"`
}

func safe(p string) bool {
	if strings.ContainsAny(p, "\\\x00") || path.IsAbs(p) {
		return false
	}
	for _, s := range strings.Split(p, "/") {
		if s == ".." || s == ".nas-trash" || s == ".zfs" || strings.HasPrefix(s, ".nas-upload-") {
			return false
		}
	}
	return true
}
func noLinks(r *os.Root, p string) error {
	if p == "" || p == "." {
		return nil
	}
	parts := strings.Split(path.Clean(p), "/")
	for i := range parts {
		st, e := r.Lstat(strings.Join(parts[:i+1], "/"))
		if os.IsNotExist(e) && i == len(parts)-1 {
			return nil
		}
		if e != nil {
			return e
		}
		if st.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("符號連結不可操作")
		}
		if !st.IsDir() && !st.Mode().IsRegular() {
			return fmt.Errorf("只支援一般檔案及資料夾")
		}
	}
	return nil
}

// Worker is entered only in a separate process after dropping Unix credentials.
func Worker(in io.Reader, out io.Writer) error {
	b := bufio.NewReaderSize(in, 64*1024)
	header, e := b.ReadString('\n')
	if e != nil || len(header) > 32768 {
		return fmt.Errorf("invalid worker request")
	}
	var q Request
	if json.Unmarshal([]byte(header), &q) != nil {
		return fmt.Errorf("invalid worker JSON")
	}
	if !safe(q.Path) || !safe(q.Target) {
		return fmt.Errorf("invalid relative path")
	}
	r, e := os.OpenRoot(q.Root)
	if e != nil {
		return e
	}
	defer r.Close()
	if q.Path == "" {
		q.Path = "."
	}
	if q.Operation != "restore" && q.Operation != "trash-list" && q.Operation != "purge" && q.Operation != "recover-version" && q.Operation != "trash-stat" && q.Operation != "trash-download" {
		if e = noLinks(r, q.Path); e != nil {
			return e
		}
	}
	if q.Target != "" {
		if e = noLinks(r, q.Target); e != nil {
			return e
		}
	}
	switch q.Operation {
	case "recover-version":
		source, e := os.OpenRoot(q.SourceRoot)
		if e != nil {
			return e
		}
		defer source.Close()
		if e = noLinks(source, q.Path); e != nil {
			return e
		}
		f, e := source.Open(q.Path)
		if e != nil {
			return e
		}
		defer f.Close()
		st, e := f.Stat()
		if e != nil || !st.Mode().IsRegular() {
			return fmt.Errorf("snapshot source is not a regular file")
		}
		if q.Target == "" {
			return fmt.Errorf("recovery destination required")
		}
		dst, e := r.OpenFile(q.Target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0660)
		if e != nil {
			return e
		}
		_, e = copyBudget(dst, f, q)
		ce := dst.Close()
		if e != nil {
			_ = r.Remove(q.Target)
			return e
		}
		return ce
	case "trash-stat", "trash-download":
		tr, e := openTrash(q, r)
		if e != nil {
			return e
		}
		defer tr.Close()
		if e = noLinks(tr, q.Path); e != nil {
			return e
		}
		f, e := tr.Open(q.Path)
		if e != nil {
			return e
		}
		defer f.Close()
		st, e := f.Stat()
		if e != nil || !st.Mode().IsRegular() {
			return fmt.Errorf("回收內容不是一般檔案")
		}
		if q.Operation == "trash-stat" {
			return json.NewEncoder(out).Encode(Entry{path.Base(q.Path), st.Size(), false, DeletionTime(st)})
		}
		_, e = io.Copy(out, f)
		return e
	case "stat":
		st, e := r.Stat(q.Path)
		if e != nil {
			return e
		}
		if !st.Mode().IsRegular() {
			return fmt.Errorf("not a regular file")
		}
		return json.NewEncoder(out).Encode(Entry{path.Base(q.Path), st.Size(), false, st.ModTime()})
	case "list":
		f, e := r.Open(q.Path)
		if e != nil {
			return e
		}
		defer f.Close()
		if q.Offset < 0 || q.Offset > 1000000 {
			return fmt.Errorf("offset out of range")
		}
		if q.Limit < 1 || q.Limit > 200 {
			q.Limit = 100
		}
		entries := []Entry{}
		seen := 0
		more := false
		for {
			batch, e := f.ReadDir(128)
			for _, v := range batch {
				if !safe(v.Name()) || v.Type()&os.ModeSymlink != 0 {
					continue
				}
				if seen < q.Offset {
					seen++
					continue
				}
				if len(entries) == q.Limit {
					more = true
					break
				}
				st, err := v.Info()
				if err != nil {
					continue
				}
				if !st.IsDir() && !st.Mode().IsRegular() {
					continue
				}
				entries = append(entries, Entry{v.Name(), st.Size(), st.IsDir(), st.ModTime()})
				seen++
			}
			if more || e == io.EOF {
				break
			}
			if e != nil {
				return e
			}
		}
		return json.NewEncoder(out).Encode(map[string]any{"entries": entries, "more": more, "next_offset": q.Offset + len(entries)})
	case "download":
		f, e := r.Open(q.Path)
		if e != nil {
			return e
		}
		defer f.Close()
		st, e := f.Stat()
		if e != nil || !st.Mode().IsRegular() {
			return fmt.Errorf("not a regular file")
		}
		_, e = io.Copy(out, f)
		return e
	case "upload":
		if q.Path == "." {
			return fmt.Errorf("filename required")
		}
		tmp := path.Join(path.Dir(q.Path), ".nas-upload-"+strconv.FormatInt(time.Now().UnixNano(), 10))
		f, e := r.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0660)
		if e != nil {
			return e
		}
		defer r.Remove(tmp)
		_, e = copyBudget(f, b, q)
		if e == nil {
			e = f.Sync()
		}
		ce := f.Close()
		if e != nil {
			return e
		}
		if ce != nil {
			return ce
		}
		if !q.Replace {
			if e = r.Link(tmp, q.Path); e != nil {
				return e
			}
			return r.Remove(tmp)
		}
		if st, e := r.Lstat(q.Path); e == nil && !st.Mode().IsRegular() {
			return fmt.Errorf("target is not a regular file")
		}
		return r.Rename(tmp, q.Path)
	case "mkdir":
		return r.Mkdir(q.Path, 0770)
	case "move":
		if q.Path == "." || q.Target == "" {
			return fmt.Errorf("invalid move")
		}
		if _, e := r.Lstat(q.Target); !os.IsNotExist(e) {
			return fmt.Errorf("destination exists or unavailable")
		}
		return r.Rename(q.Path, q.Target)
	case "trash":
		if q.Path == "." {
			return fmt.Errorf("cannot delete root")
		}
		trash, e := openTrash(q, r)
		if e != nil {
			return e
		}
		defer trash.Close()
		target := q.Path
		if _, e := trash.Lstat(target); e == nil {
			target = path.Join(path.Dir(q.Path), path.Base(q.Path)+".deleted-"+strconv.FormatInt(time.Now().UnixNano(), 10))
		}
		if e = trash.MkdirAll(path.Dir(target), 0700); e != nil {
			return e
		}
		if e = renameBetween(r, q.Path, trash, target); e != nil {
			return e
		}
		_ = trash.Chtimes(target, time.Now(), time.Now())
		return nil
	case "purge":
		if q.Path == "." {
			return fmt.Errorf("不能清除整個回收根目錄")
		}
		trash, e := openTrash(q, r)
		if e != nil {
			return e
		}
		defer trash.Close()
		if e = noLinks(trash, q.Path); e != nil {
			return e
		}
		return trash.RemoveAll(q.Path)
	case "trash-list":
		trash, e := openTrash(q, r)
		if e != nil {
			return e
		}
		defer trash.Close()
		if e = noLinks(trash, q.Path); e != nil {
			return e
		}
		f, e := trash.Open(q.Path)
		if e != nil {
			return e
		}
		defer f.Close()
		if q.Offset < 0 || q.Offset > 1000000 {
			return fmt.Errorf("offset out of range")
		}
		if q.Limit < 1 || q.Limit > 200 {
			q.Limit = 100
		}
		entries := []Entry{}
		seen, more := 0, false
		for {
			batch, err := f.ReadDir(128)
			for _, x := range batch {
				if !safe(x.Name()) || x.Type()&os.ModeSymlink != 0 {
					continue
				}
				st, e := x.Info()
				if e != nil || (!st.IsDir() && !st.Mode().IsRegular()) {
					continue
				}
				if seen < q.Offset {
					seen++
					continue
				}
				if len(entries) == q.Limit {
					more = true
					break
				}
				entries = append(entries, Entry{x.Name(), st.Size(), st.IsDir(), DeletionTime(st)})
				seen++
			}
			if more || err == io.EOF {
				break
			}
			if err != nil {
				return err
			}
		}
		return json.NewEncoder(out).Encode(map[string]any{"entries": entries, "more": more, "next_offset": q.Offset + len(entries)})

	case "restore":
		if q.Path == "." || q.Target == "" {
			return fmt.Errorf("restore path required")
		}
		trash, e := openTrash(q, r)
		if e != nil {
			return e
		}
		defer trash.Close()
		src := q.Path
		if e = noLinks(trash, src); e != nil {
			return e
		}
		if _, e := r.Lstat(q.Target); !errors.Is(e, os.ErrNotExist) {
			return fmt.Errorf("destination exists or unavailable")
		}
		return renameBetween(trash, src, r, q.Target)
	default:
		return fmt.Errorf("unsupported operation")
	}
}

func openTrash(q Request, r *os.Root) (*os.Root, error) {
	if q.TrashRoot != "" {
		return os.OpenRoot(q.TrashRoot)
	}
	return r.OpenRoot(path.Join(".nas-trash", q.User))
}

// copyBudget streams only the remaining budget, then probes one byte without writing it.
func copyBudget(dst io.Writer, src io.Reader, q Request) (int64, error) {
	if !q.Limited {
		return io.Copy(dst, src)
	}
	if q.MaxBytes < 0 {
		return 0, fmt.Errorf("invalid quota budget")
	}
	n, e := io.Copy(dst, io.LimitReader(src, q.MaxBytes))
	if e != nil {
		return n, e
	}
	var probe [1]byte
	count, e := io.ReadFull(src, probe[:])
	if count > 0 {
		return n, fmt.Errorf("上傳超過剩餘配額；暫存檔已移除")
	}
	if e == io.EOF {
		return n, nil
	}
	return n, e
}
