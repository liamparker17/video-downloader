//go:build !windows

package main

import (
	"syscall"
)

const dscpAF41 = 0x88

// setSocketDSCP marks outgoing packets with DSCP AF41 on Unix systems.
func setSocketDSCP(network, address string, c syscall.RawConn) error {
	return c.Control(func(fd uintptr) {
		syscall.SetsockoptInt(int(fd), syscall.IPPROTO_IP, syscall.IP_TOS, dscpAF41)
	})
}
