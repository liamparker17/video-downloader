package main

import (
	"fmt"
	"sync/atomic"
)

// activeDownloads tracks the number of currently running downloads.
var activeDownloads atomic.Int32

// RegisterDownload increments the active download count. Call before starting a download.
func RegisterDownload() { activeDownloads.Add(1) }

// UnregisterDownload decrements the active download count. Call when a download finishes.
func UnregisterDownload() { activeDownloads.Add(-1) }

// estimateTotalBandwidth sums the current speed (bytes/sec) of all active downloads.
func estimateTotalBandwidth() float64 {
	var total float64
	store.jobs.Range(func(_, val any) bool {
		job := val.(*Job)
		if job.Status == "downloading" && job.SpeedBPS > 0 {
			total += job.SpeedBPS
		}
		return true
	})
	return total
}

// fairShareBPS returns the per-download bandwidth limit in bytes/sec.
// Returns 0 when no limiting is needed (single download or no speed data yet).
func fairShareBPS() float64 {
	active := int(activeDownloads.Load())
	if active <= 1 {
		return 0
	}
	total := estimateTotalBandwidth()
	if total <= 0 {
		return 0
	}
	return total / float64(active)
}

// ytdlpRateFlag returns the --limit-rate value for yt-dlp, or "" if no limit is needed.
func ytdlpRateFlag() string {
	share := fairShareBPS()
	if share <= 0 {
		return ""
	}
	kbps := int64(share / 1024)
	if kbps < 100 {
		return "" // don't throttle below 100 KB/s
	}
	return fmt.Sprintf("%dK", kbps)
}
