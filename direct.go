package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"
)

const directTimeout = 10 * time.Minute

func downloadDirect(ctx context.Context, req DownloadRequest, job *Job, outPath string) error {
	ctx, cancel := context.WithTimeout(ctx, directTimeout)
	defer cancel()

	client := &http.Client{}

	httpReq, err := buildRequest(ctx, "GET", req.URL, req)
	if err != nil {
		return err
	}

	resp, err := retryRequest(ctx, client, httpReq, []time.Duration{1 * time.Second})
	if err != nil {
		return fmt.Errorf("Video URL not reachable")
	}
	defer resp.Body.Close()

	tmpPath := outPath + ".tmp"
	out, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	defer out.Close()

	pw := NewProgressWriter(out, job, resp.ContentLength)

	written, err := io.Copy(pw, resp.Body)
	if err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("writing file: %w", err)
	}

	out.Close()
	if err := os.Rename(tmpPath, outPath); err != nil {
		return fmt.Errorf("rename tmp file: %w", err)
	}

	log.Printf("[DIRECT] Downloaded %d bytes -> %s", written, outPath)
	return nil
}
