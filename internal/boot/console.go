package boot

import (
	"io"
	"os"
)

type ConsoleFile struct {
	path string
	file *os.File
}

func Console(path string) *ConsoleFile {
	f := openConsole(path)
	if f == nil {
		return nil
	}
	return &ConsoleFile{path: path, file: f}
}

// Terminal session hangups invalidate old descriptors; retry a complete frame.
func (c *ConsoleFile) Write(b []byte) (int, error) {
	n := 0
	var e error
	if c.file != nil {
		n, e = c.file.Write(b)
		if e == nil && n == len(b) {
			return n, nil
		}
		_ = c.file.Close()
	}
	c.file = openConsole(c.path)
	if c.file == nil {
		if e == nil {
			e = io.ErrShortWrite
		}
		return n, e
	}
	return c.file.Write(b)
}
