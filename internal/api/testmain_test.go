package api

import (
	"just-nas/internal/config"
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	dir, e := os.MkdirTemp("", "just-nas-api-tests-")
	if e != nil {
		panic(e)
	}
	config.Dir = dir
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}
