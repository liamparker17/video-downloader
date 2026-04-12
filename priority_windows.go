//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

const highPriorityClass = 0x00000080

// setHighPriority configures a command to run at HIGH_PRIORITY_CLASS on Windows.
// This gives the process more CPU time to process network data, which helps
// saturate the network link before other lower-priority processes.
func setHighPriority(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags = highPriorityClass
}
