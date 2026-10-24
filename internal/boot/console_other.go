//go:build !linux

package boot

import "os"

func openConsole(path string) *os.File { return nil }
