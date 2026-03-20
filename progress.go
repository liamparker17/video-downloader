package main

import (
	"fmt"
	"io"
	"sync/atomic"
	"time"
)

// ProgressWriter wraps an io.Writer to track download progress and speed.
type ProgressWriter struct {
	writer    io.Writer
	job       *Job
	total     int64 // expected total bytes (0 if unknown)
	written   atomic.Int64
	startTime time.Time
}

// NewProgressWriter creates a ProgressWriter that updates the given job.
func NewProgressWriter(w io.Writer, job *Job, total int64) *ProgressWriter {
	return &ProgressWriter{
		writer:    w,
		job:       job,
		total:     total,
		startTime: time.Now(),
	}
}

func (pw *ProgressWriter) Write(p []byte) (int, error) {
	n, err := pw.writer.Write(p)
	if n > 0 {
		current := pw.written.Add(int64(n))
		elapsed := time.Since(pw.startTime).Seconds()

		if pw.total > 0 {
			pw.job.Progress = float64(current) / float64(pw.total) * 100
		}
		if elapsed > 0 {
			bytesPerSec := float64(current) / elapsed
			pw.job.Speed = formatSpeed(bytesPerSec)
		}
	}
	return n, err
}

// UpdateSegmentProgress updates progress based on completed segments.
func UpdateSegmentProgress(job *Job, completed, total int) {
	if total > 0 {
		job.Progress = float64(completed) / float64(total) * 100
	}
}

func formatSpeed(bytesPerSec float64) string {
	switch {
	case bytesPerSec >= 1024*1024*1024:
		return fmt.Sprintf("%.1f GB/s", bytesPerSec/(1024*1024*1024))
	case bytesPerSec >= 1024*1024:
		return fmt.Sprintf("%.1f MB/s", bytesPerSec/(1024*1024))
	case bytesPerSec >= 1024:
		return fmt.Sprintf("%.1f KB/s", bytesPerSec/1024)
	default:
		return fmt.Sprintf("%.0f B/s", bytesPerSec)
	}
}
