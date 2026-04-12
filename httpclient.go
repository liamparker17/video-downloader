package main

import (
	"net"
	"net/http"
	"time"
)

// downloadClient returns an HTTP client optimized for high-throughput downloads.
// Sets DSCP AF41 on all sockets so QoS-aware routers prioritize download traffic.
func downloadClient() *http.Client {
	dialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
		Control:   setSocketDSCP,
	}

	return &http.Client{
		Transport: &http.Transport{
			DialContext:           dialer.DialContext,
			MaxIdleConns:          100,
			MaxIdleConnsPerHost:   10,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 30 * time.Second,
			WriteBufferSize:       64 * 1024, // 64 KB
			ReadBufferSize:        64 * 1024, // 64 KB
		},
	}
}
