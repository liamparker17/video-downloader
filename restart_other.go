//go:build !windows

package main

import (
	"log"
	"os"
	"syscall"
)

// restart replaces the current process with the new binary (Unix exec).
func restart(exePath string) {
	if err := syscall.Exec(exePath, os.Args, os.Environ()); err != nil {
		log.Printf("[UPDATE] Failed to restart: %v", err)
	}
}
