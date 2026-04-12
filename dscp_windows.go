//go:build windows

package main

import (
	"syscall"
)

// DSCP Assured Forwarding 41 (high-priority data) shifted into the ToS byte.
// Routers with QoS/SQM that inspect DSCP will prioritize these packets.
const dscpAF41 = 0x88 // AF41 = 100010 in DSCP field = 0x88 in full ToS byte

// setSocketDSCP marks all outgoing packets with DSCP AF41 so QoS-aware routers
// prioritize download traffic over normal best-effort traffic.
func setSocketDSCP(network, address string, c syscall.RawConn) error {
	return c.Control(func(fd uintptr) {
		h := syscall.Handle(fd)
		syscall.SetsockoptInt(h, syscall.IPPROTO_IP, syscall.IP_TOS, dscpAF41)
		syscall.SetsockoptInt(h, syscall.IPPROTO_IPV6, 39, dscpAF41) // 39 = IPV6_TCLASS
	})
}
