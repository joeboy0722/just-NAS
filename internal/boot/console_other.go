//go:build !linux

package boot

import "os"

func Console(path string) *os.File { return nil }
