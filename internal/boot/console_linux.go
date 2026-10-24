package boot

import (
	"os"
	"syscall"
)

func openConsole(path string) *os.File {
	f, _ := os.OpenFile(path, os.O_WRONLY|syscall.O_NOCTTY|syscall.O_NONBLOCK, 0)
	return f
}
