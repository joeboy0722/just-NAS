//go:build !linux

package vmmanager

import (
	"context"
	"fmt"
	"runtime"
)

func memoryInfo() (int64, int64)                                  { return 8192, 8192 }
func hostCPUs() int                                               { return runtime.NumCPU() }
func allocatedBytes(string) int64                                 { return 0 }
func probeKVM() (bool, string)                                    { return false, "需要 Linux" }
func chooseAcceleration(context.Context, string) (string, string) { return "tcg", "需要 Linux KVM" }
func processAlive(State, Machine) bool                            { return false }
func signalGuest(State, Machine) error                            { return fmt.Errorf("需要 Linux") }
func launchGuest(context.Context, State, Machine, []string) error { return fmt.Errorf("需要 Linux") }
func GuestRun(string) error                                       { return fmt.Errorf("需要 Linux") }
