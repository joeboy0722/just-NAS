package command

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

type Runner func(input string, name string, args ...string) ([]byte, error)

var Run Runner = Execute

func Execute(input, name string, args ...string) ([]byte, error) {
	if runtime.GOOS != "linux" {
		return nil, fmt.Errorf("此功能需要 Linux NAS 環境")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	// OpenRC may leave background helpers holding stdout after its own exit.
	// Bound that drain time without reporting a successful service start as failed.
	cmd.WaitDelay = 2 * time.Second
	cmd.Stdin = strings.NewReader(input)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if errors.Is(err, exec.ErrWaitDelay) {
		err = nil
	}
	if ctx.Err() != nil {
		return stdout.Bytes(), fmt.Errorf("%s 執行逾時", name)
	}
	if err != nil {
		return stdout.Bytes(), fmt.Errorf("%s: %s (%w)", name, strings.TrimSpace(stderr.String()), err)
	}
	return stdout.Bytes(), nil
}
