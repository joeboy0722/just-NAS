package command

import (
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestDetachedHelperDoesNotBlockServiceCommand(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux service execution")
	}
	start := time.Now()
	out, err := Execute("", "sh", "-c", "sleep 8 & echo ready")
	if err != nil || !strings.Contains(string(out), "ready") {
		t.Fatalf("%s %v", out, err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("waited for detached service helper")
	}
}
