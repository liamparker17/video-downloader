//go:build !windows

package main

import "os/exec"

// setHighPriority is a no-op on non-Windows platforms.
func setHighPriority(_ *exec.Cmd) {}
