package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"strings"
	"time"
)

type videoType int

const (
	videoTypeDirect videoType = iota
	videoTypeHLS
)

const (
	downloadTimeout = 5 * time.Minute
	maxRetries      = 1
)

// detectType determines whether the URL points to an HLS playlist or a direct file.
func detectType(rawURL string) videoType {
	lower := strings.ToLower(rawURL)
	// Strip query params for extension check
	if idx := strings.Index(lower, "?"); idx != -1 {
		lower = lower[:idx]
	}
	if strings.HasSuffix(lower, ".m3u8") {
		return videoTypeHLS
	}
	return videoTypeDirect
}

// buildHTTPClient creates a client with the download timeout.
func buildHTTPClient() *http.Client {
	return &http.Client{Timeout: downloadTimeout}
}

// buildRequest creates an HTTP request with the headers and cookies from the extension.
func buildRequest(method, rawURL string, req DownloadRequest) (*http.Request, error) {
	httpReq, err := http.NewRequest(method, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("invalid URL %q: %w", rawURL, err)
	}

	// Apply forwarded headers (User-Agent, Referer, etc.)
	for k, v := range req.Headers {
		httpReq.Header.Set(k, v)
	}

	// Apply cookies as a raw header
	if req.Cookies != "" {
		httpReq.Header.Set("Cookie", req.Cookies)
	}

	return httpReq, nil
}

// doWithRetry performs an HTTP request with one retry on failure.
func doWithRetry(client *http.Client, httpReq *http.Request) (*http.Response, error) {
	resp, err := client.Do(httpReq)
	if err == nil && resp.StatusCode < 400 {
		return resp, nil
	}
	if resp != nil {
		resp.Body.Close()
	}

	// One retry
	log.Printf("[RETRY] Retrying request to %s", httpReq.URL)
	time.Sleep(1 * time.Second)

	// Rebuild request since body may be consumed
	retryReq := httpReq.Clone(httpReq.Context())
	resp, err = client.Do(retryReq)
	if err != nil {
		return nil, fmt.Errorf("request failed after retry: %w", err)
	}
	if resp.StatusCode >= 400 {
		resp.Body.Close()
		return nil, fmt.Errorf("HTTP %d after retry", resp.StatusCode)
	}
	return resp, nil
}

// downloadDirect streams a video file directly to disk.
func downloadDirect(req DownloadRequest, outPath string) error {
	client := buildHTTPClient()

	httpReq, err := buildRequest("GET", req.URL, req)
	if err != nil {
		return err
	}

	resp, err := doWithRetry(client, httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	out, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	defer out.Close()

	written, err := io.Copy(out, resp.Body)
	if err != nil {
		return fmt.Errorf("writing file: %w", err)
	}

	log.Printf("[PROGRESS] Downloaded %d bytes -> %s", written, outPath)
	return nil
}

// downloadHLS fetches an m3u8 playlist, downloads all .ts segments, and converts to mp4.
func downloadHLS(req DownloadRequest, outPath string) error {
	client := buildHTTPClient()

	// 1. Fetch the m3u8 playlist
	httpReq, err := buildRequest("GET", req.URL, req)
	if err != nil {
		return err
	}

	resp, err := doWithRetry(client, httpReq)
	if err != nil {
		return fmt.Errorf("fetching m3u8: %w", err)
	}
	defer resp.Body.Close()

	// 2. Parse segment URLs from the playlist
	segments, err := parseM3U8(resp.Body, req.URL)
	if err != nil {
		return fmt.Errorf("parsing m3u8: %w", err)
	}
	log.Printf("[HLS] Found %d segments", len(segments))

	if len(segments) == 0 {
		return fmt.Errorf("no segments found in m3u8 playlist")
	}

	// 3. Download all segments into a single concatenated .ts file
	tsPath := outPath + ".ts"
	tsFile, err := os.Create(tsPath)
	if err != nil {
		return fmt.Errorf("create ts file: %w", err)
	}

	for i, segURL := range segments {
		segReq, err := buildRequest("GET", segURL, req)
		if err != nil {
			tsFile.Close()
			return fmt.Errorf("segment %d: %w", i, err)
		}

		segResp, err := doWithRetry(client, segReq)
		if err != nil {
			tsFile.Close()
			return fmt.Errorf("downloading segment %d: %w", i, err)
		}

		written, err := io.Copy(tsFile, segResp.Body)
		segResp.Body.Close()
		if err != nil {
			tsFile.Close()
			return fmt.Errorf("writing segment %d: %w", i, err)
		}

		log.Printf("[HLS] Segment %d/%d: %d bytes", i+1, len(segments), written)
	}
	tsFile.Close()

	// 4. Convert .ts to .mp4 using ffmpeg
	log.Println("[HLS] Converting .ts -> .mp4 via ffmpeg")
	cmd := exec.Command("ffmpeg", "-y", "-i", tsPath, "-c", "copy", outPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg conversion failed: %w", err)
	}

	// Clean up the intermediate .ts file
	os.Remove(tsPath)

	return nil
}

// parseM3U8 reads an m3u8 playlist and returns absolute URLs for each .ts segment.
// It handles both relative and absolute segment URLs.
func parseM3U8(body io.Reader, playlistURL string) ([]string, error) {
	base, err := url.Parse(playlistURL)
	if err != nil {
		return nil, err
	}
	// Use the directory of the playlist as the base for relative URLs
	base.Path = path.Dir(base.Path) + "/"

	var segments []string
	scanner := bufio.NewScanner(body)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments/tags
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Resolve relative URLs against the playlist base
		segURL, err := base.Parse(line)
		if err != nil {
			log.Printf("[WARN] Skipping malformed segment URL: %s", line)
			continue
		}

		segments = append(segments, segURL.String())
	}

	return segments, scanner.Err()
}
