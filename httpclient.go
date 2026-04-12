package main

import (
	"net"
	"net/http"
	"time"
)

// downloadClient returns an HTTP client optimized for high-throughput downloads.
// Larger TCP buffers and more idle connections help saturate the network link.
func downloadClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			DialContext: (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
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
