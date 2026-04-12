//go:build windows

package main

import (
	"log"
	"os"
	"os/exec"
	"syscall"
)

// restart launches the new binary and exits the current process.
func restart(exePath string) {
	cmd := exec.Command(exePath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
	}

	if err := cmd.Start(); err != nil {
		log.Printf("[UPDATE] Failed to restart: %v", err)
		return
	}

	os.Exit(0)
}
