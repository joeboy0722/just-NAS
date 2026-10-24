package command

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"time"
)

// Stream is reserved for fixed, allowlisted maintenance operations with caller-owned deadlines.
func Stream(ctx context.Context, input io.Reader, out io.Writer, name string, args ...string) error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("maintenance requires Linux")
	}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = input
	cmd.Stdout = out
	cmd.Stderr = out
	cmd.WaitDelay = 2 * time.Second
	return cmd.Run()
}
