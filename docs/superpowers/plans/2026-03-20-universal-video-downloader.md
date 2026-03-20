# Universal Video Downloader Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Expand the video downloader from basic direct/HLS into a universal extraction system with async jobs, DASH support, yt-dlp fallback, network interception, and a full popup UI.

**Architecture:** Go monolith backend with job-based async downloads. Extraction pipeline tries Direct → HLS → DASH → Raw → yt-dlp in order. Chrome MV3 extension with network interception, DOM scanning, and a toolbar popup for source selection, quality control, and download progress.

**Tech Stack:** Go 1.22+ (stdlib only), ffmpeg (exec), yt-dlp (exec), Chrome Manifest V3, vanilla HTML/CSS/JS for popup.

**Spec:** `docs/superpowers/specs/2026-03-20-universal-video-downloader-design.md`

---

## File Structure

### Go Backend (create or rewrite)

| File | Responsibility |
|------|---------------|
| `job.go` | Job struct, JobStore (sync.Map), job lifecycle, cleanup goroutine, cancellation |
| `progress.go` | ProgressWriter (wraps io.Writer), speed calculation |
| `ffmpeg.go` | ffmpeg wrapper: convert, mux video+audio, extract audio |
| `retry.go` | Configurable `retryRequest()` replacing old `doWithRetry` |
| `direct.go` | Direct download extractor (extracted from downloader.go) |
| `hls.go` | HLS extractor with master playlist support (extracted + enhanced from downloader.go) |
| `dash.go` | DASH extractor (.mpd XML parser, segment download, mux) |
| `ytdlp.go` | yt-dlp fallback extractor (exec, progress parsing) |
| `downloader.go` | Pipeline router — tries extractors in order; shared helpers (buildRequest, detectType, sanitizeTitle) |
| `main.go` | HTTP server, all routes, CORS, startup checks, health endpoint |

### Chrome Extension (create or rewrite)

| File | Responsibility |
|------|---------------|
| `extension/manifest.json` | MV3 manifest with webRequest + storage permissions |
| `extension/background.js` | Network interception, context menu, messaging hub, job dispatch |
| `extension/content.js` | DOM video detection, title extraction, message handler |
| `extension/popup.html` | Popup markup: sources tab, downloads tab, settings |
| `extension/popup.js` | Popup logic: source list, polling, download trigger, settings |
| `extension/popup.css` | Popup styling |

### Other

| File | Responsibility |
|------|---------------|
| `install.bat` | Dependency check (Go, ffmpeg, yt-dlp), build, setup |
| `go.mod` | Module definition (no changes needed — stdlib only) |

---

## Task 1: Job System (`job.go`)

Foundation for the async job model. Everything else depends on this.

**Files:**
- Create: `job.go`

- [ ] **Step 1: Create `job.go` with Job struct and JobStore**

```go
package main

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
)

// Job represents a download job with its current state.
type Job struct {
	ID        string    `json:"id"`
	URL       string    `json:"url"`
	PageURL   string    `json:"pageUrl"`
	Title     string    `json:"title"`
	Status    string    `json:"status"`
	Progress  float64   `json:"progress"`
	Speed     string    `json:"speed"`
	Filename  string    `json:"filename"`
	Error     string    `json:"error,omitempty"`
	AudioOnly bool      `json:"audioOnly"`
	Quality   string    `json:"quality"`
	CreatedAt time.Time `json:"createdAt"`

	// Internal — not serialized
	ctx    context.Context
	cancel context.CancelFunc
}

// JobStore manages all download jobs in memory.
type JobStore struct {
	jobs sync.Map
	seq  uint64
	mu   sync.Mutex
}

var store = &JobStore{}

// CreateJob creates a new job in "pending" status and returns it.
func (s *JobStore) CreateJob(url, pageURL, title, quality string, audioOnly bool) *Job {
	s.mu.Lock()
	s.seq++
	id := fmt.Sprintf("%d_%d", time.Now().UnixMilli(), s.seq)
	s.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())

	job := &Job{
		ID:        id,
		URL:       url,
		PageURL:   pageURL,
		Title:     title,
		Status:    "pending",
		Quality:   quality,
		AudioOnly: audioOnly,
		CreatedAt: time.Now(),
		ctx:       ctx,
		cancel:    cancel,
	}
	s.jobs.Store(id, job)
	return job
}

// GetJob retrieves a job by ID.
func (s *JobStore) GetJob(id string) (*Job, bool) {
	val, ok := s.jobs.Load(id)
	if !ok {
		return nil, false
	}
	return val.(*Job), true
}

// ListJobs returns all jobs.
func (s *JobStore) ListJobs() []*Job {
	var result []*Job
	s.jobs.Range(func(_, val any) bool {
		result = append(result, val.(*Job))
		return true
	})
	return result
}

// CancelJob cancels a running job.
func (s *JobStore) CancelJob(id string) bool {
	job, ok := s.GetJob(id)
	if !ok {
		return false
	}
	if job.Status == "completed" || job.Status == "failed" {
		s.jobs.Delete(id)
		return true
	}
	job.cancel()
	job.Status = "failed"
	job.Error = "Cancelled by user"
	return true
}

// StartCleanup runs a background goroutine that evicts completed/failed jobs older than 1 hour.
func (s *JobStore) StartCleanup() {
	go func() {
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			now := time.Now()
			s.jobs.Range(func(key, val any) bool {
				job := val.(*Job)
				if (job.Status == "completed" || job.Status == "failed") &&
					now.Sub(job.CreatedAt) > time.Hour {
					s.jobs.Delete(key)
					log.Printf("[CLEANUP] Evicted job %s", job.ID)
				}
				return true
			})
		}
	}()
}
```

- [ ] **Step 2: Verify it compiles**

Run: `cd "C:/Users/liamp/OneDrive/Desktop/Portfolio/Video Downloader" && go build ./...`
Expected: No errors (other files may have issues — that's expected at this stage, we'll fix in later tasks)

- [ ] **Step 3: Commit**

```bash
git add job.go
git commit -m "feat: add job system with async job store, cancellation, and cleanup"
```

---

## Task 2: Progress Tracking (`progress.go`)

**Files:**
- Create: `progress.go`

- [ ] **Step 1: Create `progress.go` with ProgressWriter**

```go
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
```

- [ ] **Step 2: Verify it compiles**

Run: `cd "C:/Users/liamp/OneDrive/Desktop/Portfolio/Video Downloader" && go build ./...`

- [ ] **Step 3: Commit**

```bash
git add progress.go
git commit -m "feat: add ProgressWriter for tracking download speed and percentage"
```

---

## Task 3: Configurable Retry (`retry.go`)

Replaces the existing hardcoded `doWithRetry` with a configurable version.

**Files:**
- Create: `retry.go`

- [ ] **Step 1: Create `retry.go`**

```go
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"
)

// retryRequest performs an HTTP request with configurable retries and backoffs.
// backoffs defines the sleep duration before each retry attempt.
// E.g., []time.Duration{1*time.Second} = 1 retry after 1s.
//       []time.Duration{1*time.Second, 3*time.Second} = 2 retries after 1s and 3s.
func retryRequest(ctx context.Context, client *http.Client, req *http.Request, backoffs []time.Duration) (*http.Response, error) {
	// Attach context to request
	req = req.WithContext(ctx)

	resp, err := client.Do(req)
	if err == nil && resp.StatusCode < 400 {
		return resp, nil
	}
	if resp != nil {
		resp.Body.Close()
	}

	// Retry loop
	for i, backoff := range backoffs {
		log.Printf("[RETRY] Attempt %d/%d for %s", i+1, len(backoffs), req.URL)

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(backoff):
		}

		retryReq := req.Clone(ctx)
		resp, err = client.Do(retryReq)
		if err == nil && resp.StatusCode < 400 {
			return resp, nil
		}
		if resp != nil {
			resp.Body.Close()
		}
	}

	if err != nil {
		return nil, fmt.Errorf("request failed after %d retries: %w", len(backoffs), err)
	}
	return nil, fmt.Errorf("HTTP %d after %d retries", resp.StatusCode, len(backoffs))
}
```

- [ ] **Step 2: Verify it compiles**

Run: `cd "C:/Users/liamp/OneDrive/Desktop/Portfolio/Video Downloader" && go build ./...`

- [ ] **Step 3: Commit**

```bash
git add retry.go
git commit -m "feat: add configurable retry with context cancellation support"
```

---

## Task 4: ffmpeg Wrapper (`ffmpeg.go`)

**Files:**
- Create: `ffmpeg.go`

- [ ] **Step 1: Create `ffmpeg.go`**

```go
package main

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"time"
)

const ffmpegTimeout = 10 * time.Minute

// ffmpegConvert converts a media file (e.g., .ts -> .mp4) using stream copy.
func ffmpegConvert(ctx context.Context, input, output string) error {
	ctx, cancel := context.WithTimeout(ctx, ffmpegTimeout)
	defer cancel()

	log.Printf("[FFMPEG] Converting %s -> %s", input, output)
	cmd := exec.CommandContext(ctx, "ffmpeg", "-y", "-i", input, "-c", "copy", output)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		snippet := stderr.String()
		if len(snippet) > 200 {
			snippet = snippet[len(snippet)-200:]
		}
		return fmt.Errorf("ffmpeg failed: %w — %s", err, strings.TrimSpace(snippet))
	}
	return nil
}

// ffmpegMux combines separate video and audio files into a single mp4.
func ffmpegMux(ctx context.Context, videoPath, audioPath, output string) error {
	ctx, cancel := context.WithTimeout(ctx, ffmpegTimeout)
	defer cancel()

	log.Printf("[FFMPEG] Muxing %s + %s -> %s", videoPath, audioPath, output)
	cmd := exec.CommandContext(ctx, "ffmpeg", "-y",
		"-i", videoPath,
		"-i", audioPath,
		"-c", "copy",
		output,
	)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		snippet := stderr.String()
		if len(snippet) > 200 {
			snippet = snippet[len(snippet)-200:]
		}
		return fmt.Errorf("ffmpeg mux failed: %w — %s", err, strings.TrimSpace(snippet))
	}
	return nil
}

// ffmpegExtractAudio extracts audio from a video file and saves as mp3.
func ffmpegExtractAudio(ctx context.Context, input, output string) error {
	ctx, cancel := context.WithTimeout(ctx, ffmpegTimeout)
	defer cancel()

	log.Printf("[FFMPEG] Extracting audio %s -> %s", input, output)
	cmd := exec.CommandContext(ctx, "ffmpeg", "-y",
		"-i", input,
		"-vn",
		"-acodec", "libmp3lame",
		"-q:a", "2",
		output,
	)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		snippet := stderr.String()
		if len(snippet) > 200 {
			snippet = snippet[len(snippet)-200:]
		}
		return fmt.Errorf("ffmpeg audio extraction failed: %w — %s", err, strings.TrimSpace(snippet))
	}
	return nil
}

// checkTool checks if a command-line tool is available and returns its version string.
func checkTool(name string, versionArgs ...string) (bool, string) {
	args := versionArgs
	if len(args) == 0 {
		args = []string{"-version"}
	}
	cmd := exec.Command(name, args...)
	out, err := cmd.Output()
	if err != nil {
		return false, ""
	}
	// Return first line of output
	line := strings.SplitN(string(out), "\n", 2)[0]
	return true, strings.TrimSpace(line)
}
```

- [ ] **Step 2: Verify it compiles**

Run: `cd "C:/Users/liamp/OneDrive/Desktop/Portfolio/Video Downloader" && go build ./...`

- [ ] **Step 3: Commit**

```bash
git add ffmpeg.go
git commit -m "feat: add ffmpeg wrapper for convert, mux, and audio extraction"
```

---

## Task 5: Rewrite All Go Backend Files (Atomic)

Replace ALL Go files at once to avoid incremental compilation failures — these files are interdependent (`main.go` references types in `downloader.go`, which calls functions in `direct.go`, `hls.go`, `dash.go`, `ytdlp.go`).

**Files:**
- Rewrite: `main.go`, `downloader.go`
- Create: `direct.go`, `hls.go`, `dash.go`, `ytdlp.go`

- [ ] **Step 1: Rewrite `downloader.go`**

```go
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// DownloadRequest represents the JSON payload from the browser extension.
type DownloadRequest struct {
	URL       string            `json:"url"`
	PageURL   string            `json:"pageUrl"`
	Title     string            `json:"title"`
	Cookies   string            `json:"cookies"`
	Headers   map[string]string `json:"headers"`
	Quality   string            `json:"quality"`
	AudioOnly bool              `json:"audioOnly"`
}

type videoType int

const (
	videoTypeDirect videoType = iota
	videoTypeHLS
	videoTypeDASH
)

// detectType determines the video type from the URL.
func detectType(rawURL string) videoType {
	lower := strings.ToLower(rawURL)
	if idx := strings.Index(lower, "?"); idx != -1 {
		lower = lower[:idx]
	}
	switch {
	case strings.HasSuffix(lower, ".m3u8"):
		return videoTypeHLS
	case strings.HasSuffix(lower, ".mpd"):
		return videoTypeDASH
	default:
		return videoTypeDirect
	}
}

// isDirectVideoURL checks if the URL points to a directly downloadable video file.
func isDirectVideoURL(rawURL string) bool {
	lower := strings.ToLower(rawURL)
	if idx := strings.Index(lower, "?"); idx != -1 {
		lower = lower[:idx]
	}
	exts := []string{".mp4", ".webm", ".mkv", ".avi", ".mov"}
	for _, ext := range exts {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

// runPipeline executes the extraction pipeline for a job.
// Tries extractors in order: Direct → HLS → DASH → Raw → yt-dlp.
func runPipeline(job *Job, req DownloadRequest) {
	job.Status = "downloading"

	ext := ".mp4"
	if req.AudioOnly {
		ext = ".mp3"
	}
	outPath := generateFilename(req.Title, ext)
	job.Filename = filepath.Base(outPath)

	ctx := job.ctx
	var err error

	if req.URL != "" {
		switch detectType(req.URL) {
		case videoTypeHLS:
			log.Printf("[PIPELINE] Trying HLS for job %s", job.ID)
			err = downloadHLS(ctx, req, job, outPath)
		case videoTypeDASH:
			log.Printf("[PIPELINE] Trying DASH for job %s", job.ID)
			err = downloadDASH(ctx, req, job, outPath)
		case videoTypeDirect:
			if isDirectVideoURL(req.URL) {
				log.Printf("[PIPELINE] Trying direct download for job %s", job.ID)
				err = downloadDirect(ctx, req, job, outPath)
			} else {
				// Raw stream — treat as direct
				log.Printf("[PIPELINE] Trying raw stream download for job %s", job.ID)
				err = downloadDirect(ctx, req, job, outPath)
			}
		}
	} else {
		// No video URL detected — go straight to yt-dlp
		err = fmt.Errorf("no video URL")
	}

	// If Go extractors failed, fall back to yt-dlp using the page URL
	if err != nil && req.PageURL != "" {
		log.Printf("[PIPELINE] Go extractors failed (%v), trying yt-dlp for job %s", err, job.ID)
		err = downloadYtdlp(ctx, req, job, outPath)
	}

	if err != nil {
		job.Status = "failed"
		job.Error = err.Error()
		log.Printf("[PIPELINE] Job %s failed: %v", job.ID, err)
		// Clean up temp files
		cleanupTempFiles(outPath)
		return
	}

	// Post-processing: extract audio if requested and extractor produced video
	if req.AudioOnly && strings.HasSuffix(outPath, ".mp4") {
		job.Status = "processing"
		audioPath := strings.TrimSuffix(outPath, ".mp4") + ".mp3"
		if err := ffmpegExtractAudio(ctx, outPath, audioPath); err != nil {
			job.Status = "failed"
			job.Error = err.Error()
			return
		}
		os.Remove(outPath)
		outPath = audioPath
		job.Filename = filepath.Base(audioPath)
	}

	job.Status = "completed"
	job.Progress = 100
	log.Printf("[PIPELINE] Job %s completed: %s", job.ID, outPath)
}

// generateFilename creates a unique filename with timestamp and sanitized title.
func generateFilename(title, ext string) string {
	timestamp := time.Now().Format("20060102_150405_000")
	sanitized := sanitizeTitle(title)
	if sanitized != "" {
		return fmt.Sprintf("downloads/%s_%s%s", timestamp, sanitized, ext)
	}
	return fmt.Sprintf("downloads/%s%s", timestamp, ext)
}

var unsafeChars = regexp.MustCompile(`[^a-zA-Z0-9_\-\s]`)
var multiSpace = regexp.MustCompile(`\s+`)

// sanitizeTitle strips special characters and caps length for use in filenames.
func sanitizeTitle(title string) string {
	if title == "" {
		return ""
	}
	s := unsafeChars.ReplaceAllString(title, "")
	s = multiSpace.ReplaceAllString(s, "_")
	s = strings.Trim(s, "_")
	if len(s) > 80 {
		s = s[:80]
	}
	return s
}

// buildRequest creates an HTTP request with the headers and cookies from the extension.
func buildRequest(ctx context.Context, method, rawURL string, req DownloadRequest) (*http.Request, error) {
	httpReq, err := http.NewRequestWithContext(ctx, method, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("invalid URL %q: %w", rawURL, err)
	}
	for k, v := range req.Headers {
		httpReq.Header.Set(k, v)
	}
	if req.Cookies != "" {
		httpReq.Header.Set("Cookie", req.Cookies)
	}
	return httpReq, nil
}

// cleanupTempFiles removes temp files matching common intermediate patterns.
func cleanupTempFiles(basePath string) {
	patterns := []string{
		basePath + ".tmp",
		basePath + ".ts",
		basePath + ".video.tmp",
		basePath + ".audio.tmp",
	}
	for _, p := range patterns {
		os.Remove(p)
	}
}
```

- [ ] **Step 2: Create `direct.go`**

```go
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

// downloadDirect streams a video file directly to disk with progress tracking.
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
```

- [ ] **Step 3: Create `hls.go`**

```go
package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"time"
)

const segmentTimeout = 60 * time.Second

var segmentBackoffs = []time.Duration{1 * time.Second, 3 * time.Second}

// downloadHLS fetches an m3u8 playlist, downloads segments, and converts to mp4.
func downloadHLS(ctx context.Context, req DownloadRequest, job *Job, outPath string) error {
	client := &http.Client{}

	// Fetch the playlist
	httpReq, err := buildRequest(ctx, "GET", req.URL, req)
	if err != nil {
		return err
	}

	resp, err := retryRequest(ctx, client, httpReq, []time.Duration{1 * time.Second})
	if err != nil {
		return fmt.Errorf("fetching m3u8: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading m3u8: %w", err)
	}
	content := string(body)

	// Check if this is a master playlist
	if strings.Contains(content, "#EXT-X-STREAM-INF") {
		variantURL, err := selectHLSVariant(content, req.URL, req.Quality)
		if err != nil {
			return err
		}
		log.Printf("[HLS] Selected variant: %s", variantURL)

		// Re-fetch the variant playlist
		req2 := req
		req2.URL = variantURL
		return downloadHLS(ctx, req2, job, outPath)
	}

	// Parse segment URLs from media playlist
	segments, err := parseM3U8Segments(strings.NewReader(content), req.URL)
	if err != nil {
		return fmt.Errorf("parsing m3u8: %w", err)
	}
	log.Printf("[HLS] Found %d segments", len(segments))

	if len(segments) == 0 {
		return fmt.Errorf("no segments found in m3u8 playlist")
	}

	// Download all segments into a single concatenated .ts file
	tsPath := outPath + ".ts"
	tsFile, err := os.Create(tsPath)
	if err != nil {
		return fmt.Errorf("create ts file: %w", err)
	}

	for i, segURL := range segments {
		select {
		case <-ctx.Done():
			tsFile.Close()
			os.Remove(tsPath)
			return ctx.Err()
		default:
		}

		segCtx, segCancel := context.WithTimeout(ctx, segmentTimeout)
		segReq, err := buildRequest(segCtx, "GET", segURL, req)
		if err != nil {
			segCancel()
			tsFile.Close()
			return fmt.Errorf("segment %d: %w", i, err)
		}

		segResp, err := retryRequest(segCtx, client, segReq, segmentBackoffs)
		segCancel()
		if err != nil {
			tsFile.Close()
			return fmt.Errorf("downloading segment %d: %w", i, err)
		}

		_, err = io.Copy(tsFile, segResp.Body)
		segResp.Body.Close()
		if err != nil {
			tsFile.Close()
			return fmt.Errorf("writing segment %d: %w", i, err)
		}

		UpdateSegmentProgress(job, i+1, len(segments))
		log.Printf("[HLS] Segment %d/%d", i+1, len(segments))
	}
	tsFile.Close()

	// Convert .ts to .mp4
	job.Status = "processing"
	if err := ffmpegConvert(ctx, tsPath, outPath); err != nil {
		return err
	}
	os.Remove(tsPath)
	return nil
}

// selectHLSVariant picks the best variant from a master playlist matching the requested quality.
func selectHLSVariant(content, playlistURL, quality string) (string, error) {
	base, err := url.Parse(playlistURL)
	if err != nil {
		return "", err
	}
	base.Path = path.Dir(base.Path) + "/"

	type variant struct {
		bandwidth int
		height    int
		url       string
	}

	var variants []variant
	scanner := bufio.NewScanner(strings.NewReader(content))

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "#EXT-X-STREAM-INF") {
			continue
		}

		// Parse BANDWIDTH and RESOLUTION from the tag
		bw := 0
		h := 0
		if idx := strings.Index(line, "BANDWIDTH="); idx != -1 {
			s := line[idx+len("BANDWIDTH="):]
			if end := strings.IndexAny(s, ",\n"); end != -1 {
				s = s[:end]
			}
			bw, _ = strconv.Atoi(s)
		}
		if idx := strings.Index(line, "RESOLUTION="); idx != -1 {
			s := line[idx+len("RESOLUTION="):]
			if end := strings.IndexAny(s, ",\n"); end != -1 {
				s = s[:end]
			}
			parts := strings.Split(s, "x")
			if len(parts) == 2 {
				h, _ = strconv.Atoi(parts[1])
			}
		}

		// Next non-comment line is the URL
		if scanner.Scan() {
			urlLine := strings.TrimSpace(scanner.Text())
			resolved, err := base.Parse(urlLine)
			if err != nil {
				continue
			}
			variants = append(variants, variant{bandwidth: bw, height: h, url: resolved.String()})
		}
	}

	if len(variants) == 0 {
		return "", fmt.Errorf("no variants found in master playlist")
	}

	// Select variant based on requested quality
	targetHeight := 0
	switch quality {
	case "480":
		targetHeight = 480
	case "720":
		targetHeight = 720
	case "1080":
		targetHeight = 1080
	}

	if targetHeight > 0 {
		// Find closest variant at or below target height
		best := variants[0]
		for _, v := range variants {
			if v.height <= targetHeight && v.height > best.height {
				best = v
			}
		}
		return best.url, nil
	}

	// Default: pick highest bandwidth
	best := variants[0]
	for _, v := range variants {
		if v.bandwidth > best.bandwidth {
			best = v
		}
	}
	return best.url, nil
}

// parseM3U8Segments reads a media playlist and returns absolute segment URLs.
func parseM3U8Segments(body io.Reader, playlistURL string) ([]string, error) {
	base, err := url.Parse(playlistURL)
	if err != nil {
		return nil, err
	}
	base.Path = path.Dir(base.Path) + "/"

	var segments []string
	scanner := bufio.NewScanner(body)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		segURL, err := base.Parse(line)
		if err != nil {
			log.Printf("[WARN] Skipping malformed segment URL: %s", line)
			continue
		}
		segments = append(segments, segURL.String())
	}
	return segments, scanner.Err()
}
```

- [ ] **Step 4: Create `dash.go`**

**Note:** This DASH parser handles `SegmentList` and `BaseURL` modes. `SegmentTemplate` (used by YouTube, Netflix) is NOT supported — those streams fall through to yt-dlp. This is a known limitation.

```go
package main

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"time"
)

// MPD XML structures (minimal, covers common manifests)
type mpd struct {
	XMLName xml.Name  `xml:"MPD"`
	Periods []mpdPeriod `xml:"Period"`
	BaseURL string      `xml:"BaseURL"`
}

type mpdPeriod struct {
	AdaptationSets []mpdAdaptationSet `xml:"AdaptationSet"`
	BaseURL        string             `xml:"BaseURL"`
}

type mpdAdaptationSet struct {
	MimeType        string              `xml:"mimeType,attr"`
	ContentType     string              `xml:"contentType,attr"`
	Representations []mpdRepresentation `xml:"Representation"`
	BaseURL         string              `xml:"BaseURL"`
}

type mpdRepresentation struct {
	ID        string         `xml:"id,attr"`
	Bandwidth int            `xml:"bandwidth,attr"`
	Width     int            `xml:"width,attr"`
	Height    int            `xml:"height,attr"`
	BaseURL   string         `xml:"BaseURL"`
	SegList   *mpdSegList    `xml:"SegmentList"`
	SegBase   *mpdSegBase    `xml:"SegmentBase"`
}

type mpdSegList struct {
	Initialization *mpdURL  `xml:"Initialization"`
	Segments       []mpdURL `xml:"SegmentURL"`
}

type mpdSegBase struct {
	Initialization *mpdURL `xml:"Initialization"`
}

type mpdURL struct {
	SourceURL string `xml:"sourceURL,attr"`
	Media     string `xml:"media,attr"`
}

// downloadDASH fetches an MPD manifest, downloads video+audio segments, and muxes them.
func downloadDASH(ctx context.Context, req DownloadRequest, job *Job, outPath string) error {
	client := &http.Client{}

	// Fetch MPD manifest
	httpReq, err := buildRequest(ctx, "GET", req.URL, req)
	if err != nil {
		return err
	}

	resp, err := retryRequest(ctx, client, httpReq, []time.Duration{1 * time.Second})
	if err != nil {
		return fmt.Errorf("fetching mpd: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading mpd: %w", err)
	}

	var manifest mpd
	if err := xml.Unmarshal(body, &manifest); err != nil {
		return fmt.Errorf("parsing mpd XML: %w", err)
	}

	if len(manifest.Periods) == 0 {
		return fmt.Errorf("no periods in MPD manifest")
	}

	period := manifest.Periods[0]

	// Find best video and audio representations
	var videoRep *mpdRepresentation
	var audioRep *mpdRepresentation
	var videoAS *mpdAdaptationSet
	var audioAS *mpdAdaptationSet

	targetHeight := 0
	switch req.Quality {
	case "480":
		targetHeight = 480
	case "720":
		targetHeight = 720
	case "1080":
		targetHeight = 1080
	}

	for i := range period.AdaptationSets {
		as := &period.AdaptationSets[i]
		isVideo := strings.Contains(as.MimeType, "video") || strings.Contains(as.ContentType, "video")
		isAudio := strings.Contains(as.MimeType, "audio") || strings.Contains(as.ContentType, "audio")

		for j := range as.Representations {
			rep := &as.Representations[j]
			if isVideo {
				if videoRep == nil {
					videoRep = rep
					videoAS = as
				} else if targetHeight > 0 {
					if rep.Height <= targetHeight && rep.Height > videoRep.Height {
						videoRep = rep
						videoAS = as
					}
				} else if rep.Bandwidth > videoRep.Bandwidth {
					videoRep = rep
					videoAS = as
				}
			}
			if isAudio {
				if audioRep == nil || rep.Bandwidth > audioRep.Bandwidth {
					audioRep = rep
					audioAS = as
				}
			}
		}
	}

	if videoRep == nil {
		return fmt.Errorf("no video representation found in MPD")
	}

	log.Printf("[DASH] Video: %dx%d (%d bps)", videoRep.Width, videoRep.Height, videoRep.Bandwidth)
	if audioRep != nil {
		log.Printf("[DASH] Audio: %d bps", audioRep.Bandwidth)
	}

	baseURL := req.URL
	_ = manifest.BaseURL // Could refine base URL resolution further

	// Audio-only mode: download only the audio track if available
	if req.AudioOnly && audioRep != nil {
		audioTmp := outPath + ".audio.tmp"
		audioSegs := getSegmentURLs(audioRep, audioAS, baseURL)
		if err := downloadSegments(ctx, client, req, job, audioSegs, audioTmp, "audio"); err != nil {
			return err
		}
		job.Status = "processing"
		if err := ffmpegConvert(ctx, audioTmp, outPath); err != nil {
			os.Remove(audioTmp)
			return err
		}
		os.Remove(audioTmp)
		return nil
	}

	// Download video segments
	videoTmp := outPath + ".video.tmp"
	videoSegs := getSegmentURLs(videoRep, videoAS, baseURL)
	if err := downloadSegments(ctx, client, req, job, videoSegs, videoTmp, "video"); err != nil {
		return err
	}

	if audioRep != nil {
		// Download audio segments and mux with video
		audioTmp := outPath + ".audio.tmp"
		audioSegs := getSegmentURLs(audioRep, audioAS, baseURL)
		if err := downloadSegments(ctx, client, req, job, audioSegs, audioTmp, "audio"); err != nil {
			os.Remove(videoTmp)
			return err
		}

		job.Status = "processing"
		if err := ffmpegMux(ctx, videoTmp, audioTmp, outPath); err != nil {
			os.Remove(videoTmp)
			os.Remove(audioTmp)
			return err
		}
		os.Remove(videoTmp)
		os.Remove(audioTmp)
	} else {
		// Video only — convert directly
		job.Status = "processing"
		if err := ffmpegConvert(ctx, videoTmp, outPath); err != nil {
			os.Remove(videoTmp)
			return err
		}
		os.Remove(videoTmp)
	}

	return nil
}

// getSegmentURLs extracts segment URLs from a DASH representation.
func getSegmentURLs(rep *mpdRepresentation, as *mpdAdaptationSet, manifestURL string) []string {
	base, _ := url.Parse(manifestURL)
	base.Path = path.Dir(base.Path) + "/"

	var urls []string

	// Check for SegmentList
	if rep.SegList != nil {
		if rep.SegList.Initialization != nil {
			src := rep.SegList.Initialization.SourceURL
			if src == "" {
				src = rep.SegList.Initialization.Media
			}
			if src != "" {
				resolved, _ := base.Parse(src)
				urls = append(urls, resolved.String())
			}
		}
		for _, seg := range rep.SegList.Segments {
			src := seg.Media
			if src == "" {
				src = seg.SourceURL
			}
			if src != "" {
				resolved, _ := base.Parse(src)
				urls = append(urls, resolved.String())
			}
		}
		return urls
	}

	// Check for BaseURL (single-file representation)
	if rep.BaseURL != "" {
		resolved, _ := base.Parse(rep.BaseURL)
		urls = append(urls, resolved.String())
		return urls
	}

	// Check AdaptationSet BaseURL
	if as != nil && as.BaseURL != "" {
		resolved, _ := base.Parse(as.BaseURL)
		urls = append(urls, resolved.String())
	}

	return urls
}

// downloadSegments downloads a list of segment URLs into a single concatenated file.
func downloadSegments(ctx context.Context, client *http.Client, req DownloadRequest, job *Job, segURLs []string, outPath, label string) error {
	out, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("create %s file: %w", label, err)
	}

	for i, segURL := range segURLs {
		select {
		case <-ctx.Done():
			out.Close()
			os.Remove(outPath)
			return ctx.Err()
		default:
		}

		segCtx, segCancel := context.WithTimeout(ctx, segmentTimeout)
		segReq, err := buildRequest(segCtx, "GET", segURL, req)
		if err != nil {
			segCancel()
			out.Close()
			return fmt.Errorf("%s segment %d: %w", label, i, err)
		}

		segResp, err := retryRequest(segCtx, client, segReq, segmentBackoffs)
		segCancel()
		if err != nil {
			out.Close()
			return fmt.Errorf("downloading %s segment %d: %w", label, i, err)
		}

		_, err = io.Copy(out, segResp.Body)
		segResp.Body.Close()
		if err != nil {
			out.Close()
			return fmt.Errorf("writing %s segment %d: %w", label, i, err)
		}

		UpdateSegmentProgress(job, i+1, len(segURLs))
		log.Printf("[DASH] %s segment %d/%d", label, i+1, len(segURLs))
	}

	out.Close()
	return nil
}

```

- [ ] **Step 5: Create `ytdlp.go`**

```go
package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const ytdlpTimeout = 15 * time.Minute

var ytdlpAvailable bool

// ytdlp progress line: [download]  45.2% of ~12.3MiB at 2.4MiB/s ETA 00:05
var ytdlpProgressRe = regexp.MustCompile(`\[download\]\s+([\d.]+)%`)
var ytdlpSpeedRe = regexp.MustCompile(`at\s+([\d.]+\s*\S+/s)`)

// checkYtdlp checks if yt-dlp is available at startup.
func checkYtdlp() (bool, string) {
	return checkTool("yt-dlp", "--version")
}

// downloadYtdlp uses yt-dlp to download a video from a page URL.
func downloadYtdlp(ctx context.Context, req DownloadRequest, job *Job, outPath string) error {
	if !ytdlpAvailable {
		return fmt.Errorf("yt-dlp not installed — required for this site")
	}

	ctx, cancel := context.WithTimeout(ctx, ytdlpTimeout)
	defer cancel()

	args := []string{
		"--progress",
		"--newline",
		"--no-part",
		"-o", outPath,
	}

	// Quality selection
	if req.AudioOnly {
		args = append(args, "--extract-audio", "--audio-format", "mp3")
	} else {
		switch req.Quality {
		case "480":
			args = append(args, "-f", "bestvideo[height<=480]+bestaudio/best[height<=480]")
		case "720":
			args = append(args, "-f", "bestvideo[height<=720]+bestaudio/best[height<=720]")
		case "1080":
			args = append(args, "-f", "bestvideo[height<=1080]+bestaudio/best[height<=1080]")
		default:
			args = append(args, "-f", "bestvideo+bestaudio/best")
		}
	}

	// Forward cookies if present
	if req.Cookies != "" {
		// yt-dlp doesn't accept raw cookie headers, but we can use --add-header
		args = append(args, "--add-header", "Cookie:"+req.Cookies)
	}

	// Use page URL for yt-dlp (it extracts the video itself)
	targetURL := req.PageURL
	if targetURL == "" {
		targetURL = req.URL
	}
	args = append(args, targetURL)

	log.Printf("[YT-DLP] Running: yt-dlp %s", strings.Join(args, " "))

	cmd := exec.CommandContext(ctx, "yt-dlp", args...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("yt-dlp stdout pipe: %w", err)
	}
	cmd.Stderr = cmd.Stdout // Merge stderr into stdout

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("yt-dlp start: %w", err)
	}

	// Parse progress from stdout in real time
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()

		if match := ytdlpProgressRe.FindStringSubmatch(line); len(match) > 1 {
			if pct, err := strconv.ParseFloat(match[1], 64); err == nil {
				job.Progress = pct
			}
		}
		if match := ytdlpSpeedRe.FindStringSubmatch(line); len(match) > 1 {
			job.Speed = match[1]
		}
	}

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("yt-dlp failed: %w", err)
	}

	log.Printf("[YT-DLP] Download completed: %s", outPath)
	return nil
}
```

- [ ] **Step 6: Rewrite `main.go`**

```go
package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"
)

func main() {
	// Ensure downloads directory exists
	if err := os.MkdirAll("downloads", 0755); err != nil {
		log.Fatalf("Failed to create downloads directory: %v", err)
	}

	// Startup checks — detect external tools
	ffmpegOk, ffmpegVer := checkTool("ffmpeg")
	if ffmpegOk {
		log.Printf("[STARTUP] ffmpeg: found (%s)", ffmpegVer)
	} else {
		log.Println("[STARTUP] ffmpeg: NOT FOUND — video conversion will not work")
	}

	var ytdlpVer string
	ytdlpAvailable, ytdlpVer = checkYtdlp()
	if ytdlpAvailable {
		log.Printf("[STARTUP] yt-dlp: found (%s)", ytdlpVer)
	} else {
		log.Println("[STARTUP] yt-dlp: NOT FOUND — YouTube/social media downloads will not work")
	}

	// Start job cleanup goroutine
	store.StartCleanup()

	mux := http.NewServeMux()
	mux.HandleFunc("POST /download", handleCreateJob)
	mux.HandleFunc("GET /jobs/{id}", handleGetJob)
	mux.HandleFunc("GET /jobs", handleListJobs)
	mux.HandleFunc("DELETE /jobs/{id}", handleDeleteJob)
	mux.HandleFunc("GET /health", handleHealth(ffmpegOk, ffmpegVer, ytdlpVer))

	server := &http.Server{
		Addr:         ":8080",
		Handler:      corsMiddleware(mux),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	log.Println("Video downloader server starting on http://localhost:8080")
	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

// corsMiddleware adds CORS headers so the browser extension can reach us.
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// handleCreateJob creates a new download job and returns immediately.
func handleCreateJob(w http.ResponseWriter, r *http.Request) {
	var req DownloadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}

	if req.URL == "" && req.PageURL == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "url or pageUrl is required"})
		return
	}

	job := store.CreateJob(req.URL, req.PageURL, req.Title, req.Quality, req.AudioOnly)
	log.Printf("[JOB CREATED] ID: %s, URL: %s, PageURL: %s", job.ID, req.URL, req.PageURL)

	// Run the pipeline in a background goroutine
	go runPipeline(job, req)

	writeJSON(w, http.StatusAccepted, map[string]string{"jobId": job.ID})
}

// handleGetJob returns the status of a single job.
func handleGetJob(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	job, ok := store.GetJob(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "job not found"})
		return
	}
	writeJSON(w, http.StatusOK, job)
}

// handleListJobs returns all jobs.
func handleListJobs(w http.ResponseWriter, _ *http.Request) {
	jobs := store.ListJobs()
	if jobs == nil {
		jobs = []*Job{}
	}
	writeJSON(w, http.StatusOK, jobs)
}

// handleDeleteJob cancels or removes a job.
func handleDeleteJob(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if ok := store.CancelJob(id); !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "job not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleHealth returns external tool availability.
func handleHealth(ffmpegOk bool, ffmpegVer, ytdlpVer string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		health := map[string]any{
			"ffmpeg": map[string]any{
				"available": ffmpegOk,
				"version":   ffmpegVer,
			},
			"ytdlp": map[string]any{
				"available": ytdlpAvailable,
				"version":   ytdlpVer,
			},
		}
		writeJSON(w, http.StatusOK, health)
	}
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
```

- [ ] **Step 7: Verify full Go backend compiles**

Run: `cd "C:/Users/liamp/OneDrive/Desktop/Portfolio/Video Downloader" && go build -o video-downloader.exe .`
Expected: Clean compilation with zero errors.

- [ ] **Step 8: Commit all Go backend files**

```bash
git add main.go downloader.go direct.go hls.go dash.go ytdlp.go
git commit -m "feat: rewrite Go backend with async jobs, pipeline router, DASH, yt-dlp"
```

---

## Task 6: Extension — `manifest.json`, `background.js` & Icons

Rewrite with network interception, updated messaging, popup support, and extension icons.

**Files:**
- Rewrite: `extension/manifest.json`
- Rewrite: `extension/background.js`
- Create: `extension/icons/` (placeholder SVG-based PNGs)

- [ ] **Step 1: Create icon placeholder directory**

Run: `mkdir -p "C:/Users/liamp/OneDrive/Desktop/Portfolio/Video Downloader/extension/icons"`

Generate simple placeholder icons (solid color squares) using ImageMagick or manually create 16x16, 48x48, 128x128 PNG files. If ImageMagick is not available, the extension will work without icons (shows generic puzzle piece) — icons can be added later.

- [ ] **Step 2: Rewrite `extension/manifest.json`**

```json
{
  "manifest_version": 3,
  "name": "Video Downloader",
  "version": "2.0.0",
  "description": "Right-click any video to download it via a local Go backend. Supports YouTube, HLS, DASH, and more.",
  "permissions": [
    "contextMenus",
    "activeTab",
    "cookies",
    "webRequest",
    "storage"
  ],
  "host_permissions": [
    "<all_urls>"
  ],
  "icons": {
    "16": "icons/icon16.png",
    "48": "icons/icon48.png",
    "128": "icons/icon128.png"
  },
  "background": {
    "service_worker": "background.js"
  },
  "content_scripts": [
    {
      "matches": ["<all_urls>"],
      "js": ["content.js"],
      "run_at": "document_idle"
    }
  ],
  "action": {
    "default_popup": "popup.html",
    "default_icon": {
      "16": "icons/icon16.png",
      "48": "icons/icon48.png"
    },
    "default_title": "Video Downloader"
  }
}
```

- [ ] **Step 3: Rewrite `extension/background.js`**

```javascript
const BACKEND = "http://localhost:8080";

// ── Per-tab detected video sources ──
const tabSources = new Map();

// ── Network Interception ──
// Watch completed requests for video MIME types and file extensions.
const VIDEO_EXTENSIONS = /\.(mp4|webm|m3u8|mpd|ts|mkv|avi|mov)(\?|$)/i;
const VIDEO_MIMES = /video\/|application\/x-mpegURL|application\/dash\+xml/i;

chrome.webRequest.onCompleted.addListener(
  (details) => {
    if (details.tabId < 0) return;

    const urlMatch = VIDEO_EXTENSIONS.test(details.url);
    const mimeMatch =
      details.responseHeaders &&
      details.responseHeaders.some(
        (h) => h.name.toLowerCase() === "content-type" && VIDEO_MIMES.test(h.value)
      );

    if (!urlMatch && !mimeMatch) return;

    // Determine type
    const lower = details.url.toLowerCase().split("?")[0];
    let type = "unknown";
    if (lower.endsWith(".m3u8")) type = "hls";
    else if (lower.endsWith(".mpd")) type = "dash";
    else if (lower.endsWith(".mp4") || lower.endsWith(".webm")) type = "mp4";

    // Skip individual .ts segments — we want the .m3u8 parent
    if (lower.endsWith(".ts")) return;

    // Get content type and size from headers if available
    let contentType = "";
    let size = 0;
    if (details.responseHeaders) {
      for (const h of details.responseHeaders) {
        const name = h.name.toLowerCase();
        if (name === "content-type") contentType = h.value || "";
        if (name === "content-length") size = parseInt(h.value, 10) || 0;
      }
    }

    // Store (deduplicate)
    if (!tabSources.has(details.tabId)) {
      tabSources.set(details.tabId, []);
    }
    const sources = tabSources.get(details.tabId);
    if (!sources.some((s) => s.url === details.url)) {
      sources.push({
        url: details.url,
        type,
        contentType,
        size,
        timestamp: Date.now(),
      });
    }
  },
  { urls: ["<all_urls>"] },
  ["responseHeaders", "extraHeaders"]
);

// Clean up on tab close or navigation
chrome.tabs.onRemoved.addListener((tabId) => tabSources.delete(tabId));
chrome.tabs.onUpdated.addListener((tabId, changeInfo) => {
  if (changeInfo.url) tabSources.delete(tabId);
});

// ── Context Menu ──
chrome.runtime.onInstalled.addListener(() => {
  chrome.contextMenus.create({
    id: "download-video",
    title: "Download Video",
    contexts: ["video", "page", "link"],
  });
  console.log("[Video Downloader] Extension installed.");
});

chrome.contextMenus.onClicked.addListener(async (info, tab) => {
  if (info.menuItemId !== "download-video") return;

  const clickedSrc = info.srcUrl || null;

  try {
    // Ask content script for video info and page title
    const response = await chrome.tabs.sendMessage(tab.id, {
      action: "getVideoInfo",
      clickedSrc,
    });

    const videoUrl = response?.url || "";
    const title = response?.title || "";

    // Also check network-intercepted sources
    let bestUrl = videoUrl;
    if (!bestUrl) {
      const sources = tabSources.get(tab.id) || [];
      // Prefer HLS/DASH, then largest direct file
      const hls = sources.find((s) => s.type === "hls");
      const dash = sources.find((s) => s.type === "dash");
      const direct = sources.sort((a, b) => (b.size || 0) - (a.size || 0))[0];
      bestUrl = hls?.url || dash?.url || direct?.url || "";
    }

    const cookies = await getCookiesForUrl(tab.url);
    const settings = await chrome.storage.local.get(["defaultQuality", "defaultAudioOnly"]);

    const payload = {
      url: bestUrl,
      pageUrl: tab.url,
      title: title,
      cookies: cookies,
      headers: {
        "User-Agent": navigator.userAgent,
        Referer: tab.url,
      },
      quality: settings.defaultQuality || "best",
      audioOnly: settings.defaultAudioOnly || false,
    };

    console.log("[Video Downloader] Sending download request:", payload);

    const res = await fetch(`${BACKEND}/download`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
    });
    const result = await res.json();

    if (res.ok) {
      console.log(`[Video Downloader] Job created: ${result.jobId}`);
    } else {
      console.error(`[Video Downloader] Error: ${result.error}`);
    }
  } catch (err) {
    console.error("[Video Downloader] Failed:", err.message);
  }
});

// ── Messaging Hub ──
chrome.runtime.onMessage.addListener((message, sender, sendResponse) => {
  if (message.action === "getSources") {
    // Return network-intercepted sources for the sender's tab
    chrome.tabs.query({ active: true, currentWindow: true }, (tabs) => {
      const tabId = tabs[0]?.id;
      const sources = tabId ? tabSources.get(tabId) || [] : [];
      sendResponse({ sources });
    });
    return true; // async response
  }

  if (message.action === "download") {
    // Forward download request to backend
    fetch(`${BACKEND}/download`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(message.payload),
    })
      .then((res) => res.json())
      .then((data) => sendResponse(data))
      .catch((err) => sendResponse({ error: err.message }));
    return true;
  }
});

async function getCookiesForUrl(url) {
  try {
    const cookies = await chrome.cookies.getAll({ url });
    return cookies.map((c) => `${c.name}=${c.value}`).join("; ");
  } catch {
    return "";
  }
}
```

- [ ] **Step 4: Commit**

```bash
git add extension/manifest.json extension/background.js extension/icons/
git commit -m "feat: rewrite extension manifest and background with network interception"
```

---

## Task 7: Extension — `content.js`

Updated with `.mpd` detection, title extraction, and multiple source reporting.

**Files:**
- Rewrite: `extension/content.js`

- [ ] **Step 1: Rewrite `extension/content.js`**

```javascript
// Content script: DOM video detection and page metadata.

chrome.runtime.onMessage.addListener((message, _sender, sendResponse) => {
  if (message.action !== "getVideoInfo") return;

  const sources = findAllVideoUrls(message.clickedSrc);
  const bestUrl = sources.length > 0 ? sources[0] : null;

  sendResponse({
    url: bestUrl,
    title: document.title || "",
    sources: sources,
  });
});

// Find all video URLs on the page, ordered by priority.
function findAllVideoUrls(clickedSrc) {
  const found = [];
  const seen = new Set();

  function add(url) {
    if (url && !seen.has(url) && isVideoUrl(url)) {
      seen.add(url);
      found.push(url);
    }
  }

  // 1. Clicked element src (from context menu)
  add(clickedSrc);

  // 2. HTML5 <video> elements
  for (const video of document.querySelectorAll("video")) {
    add(video.src);
    add(video.currentSrc);
    for (const source of video.querySelectorAll("source")) {
      add(source.src);
    }
  }

  // 3. Fallback: scan DOM for video URLs in src/href attributes
  for (const el of document.querySelectorAll("[src], [href]")) {
    add(el.src || el.href);
  }

  return found;
}

// Check if a URL looks like a video resource.
function isVideoUrl(url) {
  if (!url || url.startsWith("blob:") || url.startsWith("data:")) return false;
  const lower = url.toLowerCase().split("?")[0];
  return (
    lower.endsWith(".mp4") ||
    lower.endsWith(".webm") ||
    lower.endsWith(".m3u8") ||
    lower.endsWith(".mpd") ||
    lower.endsWith(".ts") ||
    lower.endsWith(".mkv") ||
    lower.endsWith(".avi") ||
    lower.endsWith(".mov")
  );
}
```

- [ ] **Step 2: Commit**

```bash
git add extension/content.js
git commit -m "feat: update content.js with .mpd detection, title extraction, multi-source"
```

---

## Task 8: Extension — Popup UI (`popup.html`, `popup.css`, `popup.js`)

The full toolbar popup with Sources tab, Downloads tab, and Settings.

**Files:**
- Create: `extension/popup.html`
- Create: `extension/popup.css`
- Create: `extension/popup.js`

- [ ] **Step 1: Create `extension/popup.html`**

```html
<!DOCTYPE html>
<html>
<head>
  <meta charset="UTF-8">
  <link rel="stylesheet" href="popup.css">
</head>
<body>
  <header>
    <h1>Video Downloader</h1>
    <button id="settings-btn" title="Settings">&#9881;</button>
  </header>

  <nav>
    <button class="tab active" data-tab="sources">Sources</button>
    <button class="tab" data-tab="downloads">Downloads</button>
  </nav>

  <!-- Sources Tab -->
  <section id="sources" class="panel active">
    <div id="source-list"><p class="muted">Scanning for videos...</p></div>

    <div class="controls">
      <label>
        Quality:
        <select id="quality">
          <option value="best">Best</option>
          <option value="1080">1080p</option>
          <option value="720">720p</option>
          <option value="480">480p</option>
        </select>
      </label>
      <label>
        <input type="checkbox" id="audio-only"> Audio only
      </label>
    </div>

    <button id="download-btn" class="primary" disabled>Download Selected</button>
  </section>

  <!-- Downloads Tab -->
  <section id="downloads" class="panel">
    <div id="job-list"><p class="muted">No downloads yet.</p></div>
  </section>

  <!-- Settings Panel -->
  <section id="settings" class="panel">
    <h2>Settings</h2>
    <label>
      Default Quality:
      <select id="default-quality">
        <option value="best">Best</option>
        <option value="1080">1080p</option>
        <option value="720">720p</option>
        <option value="480">480p</option>
      </select>
    </label>
    <label>
      <input type="checkbox" id="default-audio-only"> Default Audio Only
    </label>
    <div id="tool-status"></div>
    <button id="settings-back" class="secondary">Back</button>
  </section>

  <script src="popup.js"></script>
</body>
</html>
```

- [ ] **Step 2: Create `extension/popup.css`**

```css
* { box-sizing: border-box; margin: 0; padding: 0; }

body {
  width: 360px;
  min-height: 400px;
  font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
  font-size: 13px;
  color: #e0e0e0;
  background: #1a1a2e;
}

header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 12px 16px;
  background: #16213e;
  border-bottom: 1px solid #0f3460;
}

header h1 {
  font-size: 15px;
  font-weight: 600;
  color: #e94560;
}

#settings-btn {
  background: none;
  border: none;
  color: #a0a0b0;
  font-size: 18px;
  cursor: pointer;
}
#settings-btn:hover { color: #e94560; }

nav {
  display: flex;
  background: #16213e;
}

nav .tab {
  flex: 1;
  padding: 8px;
  border: none;
  background: none;
  color: #a0a0b0;
  cursor: pointer;
  font-size: 13px;
  border-bottom: 2px solid transparent;
}

nav .tab.active {
  color: #e94560;
  border-bottom-color: #e94560;
}

.panel { display: none; padding: 12px 16px; }
.panel.active { display: block; }

.muted { color: #666; font-style: italic; padding: 20px 0; text-align: center; }

/* Source list */
.source-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px;
  margin: 4px 0;
  background: #16213e;
  border-radius: 6px;
  cursor: pointer;
}

.source-item:hover { background: #1a2744; }
.source-item.selected { border: 1px solid #e94560; }

.source-item input[type="radio"] { accent-color: #e94560; }

.source-info { flex: 1; overflow: hidden; }
.source-url { font-size: 12px; color: #a0a0b0; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.source-meta { font-size: 11px; color: #666; margin-top: 2px; }

.badge {
  font-size: 10px;
  padding: 2px 6px;
  border-radius: 3px;
  font-weight: 600;
  text-transform: uppercase;
}
.badge.mp4 { background: #0f3460; color: #53a8ff; }
.badge.hls { background: #1a3a1a; color: #6bcf6b; }
.badge.dash { background: #3a2a1a; color: #cf9f6b; }
.badge.ytdlp { background: #3a1a2a; color: #cf6b9f; }
.badge.unknown { background: #2a2a2a; color: #aaa; }

/* Controls */
.controls {
  display: flex;
  gap: 12px;
  align-items: center;
  margin: 12px 0;
}

.controls label { display: flex; align-items: center; gap: 4px; color: #a0a0b0; }

select {
  background: #16213e;
  color: #e0e0e0;
  border: 1px solid #0f3460;
  padding: 4px 8px;
  border-radius: 4px;
}

input[type="checkbox"] { accent-color: #e94560; }

/* Buttons */
.primary {
  width: 100%;
  padding: 10px;
  background: #e94560;
  color: white;
  border: none;
  border-radius: 6px;
  font-size: 13px;
  font-weight: 600;
  cursor: pointer;
}
.primary:hover { background: #d63851; }
.primary:disabled { background: #444; color: #888; cursor: not-allowed; }

.secondary {
  padding: 6px 16px;
  background: #16213e;
  color: #a0a0b0;
  border: 1px solid #0f3460;
  border-radius: 4px;
  cursor: pointer;
  margin-top: 12px;
}

/* Job list */
.job-item {
  padding: 10px;
  margin: 4px 0;
  background: #16213e;
  border-radius: 6px;
}

.job-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 6px;
}

.job-filename { font-weight: 500; font-size: 12px; }
.job-status { font-size: 11px; }
.job-status.completed { color: #6bcf6b; }
.job-status.failed { color: #e94560; }
.job-status.downloading { color: #53a8ff; }
.job-status.processing { color: #cf9f6b; }

.progress-bar {
  width: 100%;
  height: 4px;
  background: #0f3460;
  border-radius: 2px;
  overflow: hidden;
}

.progress-fill {
  height: 100%;
  background: #e94560;
  border-radius: 2px;
  transition: width 0.3s ease;
}

.job-meta { font-size: 11px; color: #666; margin-top: 4px; }

.job-error {
  font-size: 11px;
  color: #e94560;
  margin-top: 4px;
}

.retry-btn {
  background: none;
  border: 1px solid #e94560;
  color: #e94560;
  padding: 2px 8px;
  border-radius: 3px;
  font-size: 11px;
  cursor: pointer;
  margin-left: 8px;
}

#tool-status {
  margin: 12px 0;
  font-size: 12px;
}

.tool-ok { color: #6bcf6b; }
.tool-missing { color: #e94560; }

h2 { font-size: 14px; margin-bottom: 12px; color: #e0e0e0; }

#settings label {
  display: block;
  margin: 8px 0;
  color: #a0a0b0;
}

.error-banner {
  background: #3a1a1a;
  color: #e94560;
  padding: 10px;
  text-align: center;
  font-size: 12px;
}
```

- [ ] **Step 3: Create `extension/popup.js`**

```javascript
const BACKEND = "http://localhost:8080";
let selectedSource = null;
let pollInterval = null;

// ── Tab Navigation ──
document.querySelectorAll("nav .tab").forEach((tab) => {
  tab.addEventListener("click", () => {
    document.querySelectorAll("nav .tab").forEach((t) => t.classList.remove("active"));
    document.querySelectorAll(".panel").forEach((p) => p.classList.remove("active"));
    tab.classList.add("active");
    document.getElementById(tab.dataset.tab).classList.add("active");

    if (tab.dataset.tab === "downloads") startPolling();
    else stopPolling();
  });
});

// ── Settings ──
document.getElementById("settings-btn").addEventListener("click", () => {
  document.querySelectorAll(".panel").forEach((p) => p.classList.remove("active"));
  document.querySelectorAll("nav .tab").forEach((t) => t.classList.remove("active"));
  document.getElementById("settings").classList.add("active");
  loadSettings();
  loadHealth();
});

document.getElementById("settings-back").addEventListener("click", () => {
  document.getElementById("settings").classList.remove("active");
  document.querySelector('[data-tab="sources"]').click();
});

document.getElementById("default-quality").addEventListener("change", (e) => {
  chrome.storage.local.set({ defaultQuality: e.target.value });
});

document.getElementById("default-audio-only").addEventListener("change", (e) => {
  chrome.storage.local.set({ defaultAudioOnly: e.target.checked });
});

async function loadSettings() {
  const s = await chrome.storage.local.get(["defaultQuality", "defaultAudioOnly"]);
  document.getElementById("default-quality").value = s.defaultQuality || "best";
  document.getElementById("default-audio-only").checked = s.defaultAudioOnly || false;
}

async function loadHealth() {
  const el = document.getElementById("tool-status");
  try {
    const res = await fetch(`${BACKEND}/health`);
    const data = await res.json();
    el.innerHTML = `
      <p class="${data.ffmpeg.available ? "tool-ok" : "tool-missing"}">
        ffmpeg: ${data.ffmpeg.available ? data.ffmpeg.version : "NOT FOUND"}
      </p>
      <p class="${data.ytdlp.available ? "tool-ok" : "tool-missing"}">
        yt-dlp: ${data.ytdlp.available ? data.ytdlp.version : "NOT FOUND"}
      </p>
    `;
  } catch {
    el.innerHTML = '<p class="tool-missing">Backend not running</p>';
  }
}

// ── Sources Tab ──
async function loadSources() {
  const listEl = document.getElementById("source-list");
  const allSources = [];

  // Get network-intercepted sources from background
  try {
    const bg = await chrome.runtime.sendMessage({ action: "getSources" });
    if (bg?.sources) {
      bg.sources.forEach((s) => allSources.push(s));
    }
  } catch {}

  // Get DOM-detected sources from content script
  try {
    const [tab] = await chrome.tabs.query({ active: true, currentWindow: true });
    if (tab?.id) {
      const cs = await chrome.tabs.sendMessage(tab.id, { action: "getVideoInfo" });
      if (cs?.sources) {
        cs.sources.forEach((url) => {
          if (!allSources.some((s) => s.url === url)) {
            const lower = url.toLowerCase().split("?")[0];
            let type = "unknown";
            if (lower.endsWith(".m3u8")) type = "hls";
            else if (lower.endsWith(".mpd")) type = "dash";
            else if (lower.endsWith(".mp4") || lower.endsWith(".webm")) type = "mp4";
            allSources.push({ url, type, size: 0, contentType: "" });
          }
        });
      }
    }
  } catch {}

  // Always add page URL as yt-dlp fallback
  try {
    const [tab] = await chrome.tabs.query({ active: true, currentWindow: true });
    if (tab?.url && !tab.url.startsWith("chrome://")) {
      allSources.push({ url: tab.url, type: "ytdlp", size: 0, label: "Page URL (yt-dlp)" });
    }
  } catch {}

  if (allSources.length === 0) {
    listEl.innerHTML = '<p class="muted">No videos detected on this page.</p>';
    return;
  }

  listEl.innerHTML = "";
  allSources.forEach((source, i) => {
    const div = document.createElement("div");
    div.className = "source-item";
    const urlDisplay = source.label || truncateUrl(source.url);
    const sizeStr = source.size ? formatSize(source.size) : "";

    div.innerHTML = `
      <input type="radio" name="source" value="${i}">
      <span class="badge ${source.type}">${source.type}</span>
      <div class="source-info">
        <div class="source-url" title="${source.url}">${urlDisplay}</div>
        <div class="source-meta">${sizeStr}</div>
      </div>
    `;

    div.addEventListener("click", () => {
      div.querySelector("input").checked = true;
      document.querySelectorAll(".source-item").forEach((s) => s.classList.remove("selected"));
      div.classList.add("selected");
      selectedSource = source;
      document.getElementById("download-btn").disabled = false;
    });

    listEl.appendChild(div);
  });

  // Auto-select first source
  if (allSources.length > 0) {
    listEl.querySelector(".source-item")?.click();
  }
}

// ── Download Button ──
document.getElementById("download-btn").addEventListener("click", async () => {
  if (!selectedSource) return;

  const quality = document.getElementById("quality").value;
  const audioOnly = document.getElementById("audio-only").checked;

  const [tab] = await chrome.tabs.query({ active: true, currentWindow: true });
  const cookies = await getCookies(tab.url);

  // For yt-dlp sources, send page URL as pageUrl and leave url empty
  const isYtdlp = selectedSource.type === "ytdlp";

  const payload = {
    url: isYtdlp ? "" : selectedSource.url,
    pageUrl: tab.url,
    title: tab.title || "",
    cookies: cookies,
    headers: {
      "User-Agent": navigator.userAgent,
      Referer: tab.url,
    },
    quality,
    audioOnly,
  };

  try {
    const response = await chrome.runtime.sendMessage({
      action: "download",
      payload,
    });

    if (response?.jobId) {
      // Switch to downloads tab
      document.querySelector('[data-tab="downloads"]').click();
    } else if (response?.error) {
      alert("Download failed: " + response.error);
    }
  } catch (err) {
    alert("Backend not running. Start video-downloader.exe");
  }
});

// ── Downloads Tab / Polling ──
function startPolling() {
  loadJobs();
  pollInterval = setInterval(loadJobs, 2000);
}

function stopPolling() {
  if (pollInterval) {
    clearInterval(pollInterval);
    pollInterval = null;
  }
}

async function loadJobs() {
  const listEl = document.getElementById("job-list");
  try {
    const res = await fetch(`${BACKEND}/jobs`);
    const jobs = await res.json();

    if (!jobs || jobs.length === 0) {
      listEl.innerHTML = '<p class="muted">No downloads yet.</p>';
      return;
    }

    // Sort: active first, then by creation time descending
    jobs.sort((a, b) => {
      const order = { downloading: 0, processing: 1, pending: 2, failed: 3, completed: 4 };
      const diff = (order[a.status] ?? 5) - (order[b.status] ?? 5);
      if (diff !== 0) return diff;
      return new Date(b.createdAt) - new Date(a.createdAt);
    });

    listEl.innerHTML = "";
    jobs.forEach((job) => {
      const div = document.createElement("div");
      div.className = "job-item";

      let statusIcon = "";
      if (job.status === "completed") statusIcon = "&#10003;";
      if (job.status === "failed") statusIcon = "&#10007;";

      let progressHtml = "";
      if (job.status === "downloading" || job.status === "processing") {
        progressHtml = `
          <div class="progress-bar">
            <div class="progress-fill" style="width: ${job.progress.toFixed(1)}%"></div>
          </div>
          <div class="job-meta">${job.progress.toFixed(1)}% &middot; ${job.speed || "..."} &middot; ${job.status}</div>
        `;
      }

      let errorHtml = "";
      if (job.status === "failed" && job.error) {
        errorHtml = `<div class="job-error">${job.error} <button class="retry-btn" data-job-url="${job.url || ""}" data-job-page="${job.pageUrl || ""}">Retry</button></div>`;
      }

      div.innerHTML = `
        <div class="job-header">
          <span class="job-filename">${job.filename || job.id}</span>
          <span class="job-status ${job.status}">${statusIcon} ${job.status}</span>
        </div>
        ${progressHtml}
        ${errorHtml}
      `;

      // Wire retry button
      const retryBtn = div.querySelector(".retry-btn");
      if (retryBtn) {
        retryBtn.addEventListener("click", async () => {
          const payload = {
            url: retryBtn.dataset.jobUrl,
            pageUrl: retryBtn.dataset.jobPage,
            title: job.title || "",
            cookies: "",
            headers: { Referer: retryBtn.dataset.jobPage },
            quality: job.quality || "best",
            audioOnly: job.audioOnly || false,
          };
          await chrome.runtime.sendMessage({ action: "download", payload });
        });
      }

      listEl.appendChild(div);
    });
  } catch {
    listEl.innerHTML = '<div class="error-banner">Backend not running. Start video-downloader.exe</div>';
  }
}

// ── Helpers ──
async function getCookies(url) {
  try {
    const cookies = await chrome.cookies.getAll({ url });
    return cookies.map((c) => `${c.name}=${c.value}`).join("; ");
  } catch {
    return "";
  }
}

function truncateUrl(url) {
  try {
    const u = new URL(url);
    const path = u.pathname.split("/").pop() || u.hostname;
    return path.length > 40 ? path.substring(0, 40) + "..." : path;
  } catch {
    return url.substring(0, 40) + "...";
  }
}

function formatSize(bytes) {
  if (bytes >= 1024 * 1024) return (bytes / (1024 * 1024)).toFixed(1) + " MB";
  if (bytes >= 1024) return (bytes / 1024).toFixed(1) + " KB";
  return bytes + " B";
}

// ── Init ──
loadSources();
// Load user settings into quality selector
chrome.storage.local.get(["defaultQuality", "defaultAudioOnly"], (s) => {
  if (s.defaultQuality) document.getElementById("quality").value = s.defaultQuality;
  if (s.defaultAudioOnly) document.getElementById("audio-only").checked = true;
});
```

- [ ] **Step 4: Commit**

```bash
git add extension/popup.html extension/popup.css extension/popup.js
git commit -m "feat: add popup UI with source picker, downloads panel, and settings"
```

---

## Task 9: Update `install.bat`

Add yt-dlp check/install and update instructions.

**Files:**
- Rewrite: `install.bat`

- [ ] **Step 1: Rewrite `install.bat`**

```batch
@echo off
echo ============================================
echo  Video Downloader - Dependency Installer
echo ============================================
echo.

REM Check Go is installed
where go >nul 2>&1
if %ERRORLEVEL% neq 0 (
    echo [ERROR] Go is not installed or not on PATH.
    echo         Download from: https://go.dev/dl/
    echo.
    pause
    exit /b 1
)
echo [OK] Go found:
go version
echo.

REM Check ffmpeg is installed
where ffmpeg >nul 2>&1
if %ERRORLEVEL% neq 0 (
    echo [ERROR] ffmpeg is not installed or not on PATH.
    echo         Download from: https://ffmpeg.org/download.html
    echo         Or install via: winget install Gyan.FFmpeg
    echo.
    pause
    exit /b 1
)
echo [OK] ffmpeg found:
ffmpeg -version 2>&1 | findstr /R "^ffmpeg"
echo.

REM Check yt-dlp (optional but recommended)
where yt-dlp >nul 2>&1
if %ERRORLEVEL% neq 0 (
    echo [WARN] yt-dlp not found. Attempting install via winget...
    winget install yt-dlp 2>nul
    where yt-dlp >nul 2>&1
    if %ERRORLEVEL% neq 0 (
        echo [WARN] yt-dlp could not be installed automatically.
        echo         Download from: https://github.com/yt-dlp/yt-dlp/releases
        echo         Without yt-dlp, YouTube and social media downloads will not work.
        echo.
    ) else (
        echo [OK] yt-dlp installed successfully.
    )
) else (
    echo [OK] yt-dlp found:
    yt-dlp --version
)
echo.

REM Download Go module dependencies
echo [INSTALLING] Go module dependencies...
go mod tidy
if %ERRORLEVEL% neq 0 (
    echo [ERROR] Failed to install Go dependencies.
    pause
    exit /b 1
)
echo [OK] Go dependencies installed.
echo.

REM Build the binary
echo [BUILDING] Compiling Go backend...
go build -o video-downloader.exe .
if %ERRORLEVEL% neq 0 (
    echo [ERROR] Build failed.
    pause
    exit /b 1
)
echo [OK] Built video-downloader.exe
echo.

REM Create downloads directory
if not exist downloads mkdir downloads
echo [OK] downloads/ directory ready.
echo.

echo ============================================
echo  Installation complete!
echo.
echo  To start the backend:
echo    video-downloader.exe
echo.
echo  Then load the extension in Chrome:
echo    1. Go to chrome://extensions
echo    2. Enable Developer mode
echo    3. Click "Load unpacked"
echo    4. Select the "extension" folder
echo ============================================
pause
```

- [ ] **Step 2: Commit**

```bash
git add install.bat
git commit -m "feat: update install.bat with yt-dlp detection and auto-install"
```

---

## Task 10: Compile, Smoke Test, Final Commit

**Files:**
- All files from previous tasks

- [ ] **Step 1: Verify full Go project compiles**

Run: `cd "C:/Users/liamp/OneDrive/Desktop/Portfolio/Video Downloader" && go build -o video-downloader.exe .`
Expected: Clean compilation, no errors.

- [ ] **Step 2: Run the server and test health endpoint**

Run: `cd "C:/Users/liamp/OneDrive/Desktop/Portfolio/Video Downloader" && ./video-downloader.exe &`
Then: `curl http://localhost:8080/health`
Expected: JSON with ffmpeg and yt-dlp status.

- [ ] **Step 3: Test POST /download with a public video URL**

Run: `curl -X POST http://localhost:8080/download -H "Content-Type: application/json" -d '{"url":"https://www.w3schools.com/html/mov_bbb.mp4","pageUrl":"https://www.w3schools.com/html/html5_video.asp","title":"test","quality":"best","audioOnly":false}'`
Expected: Returns `{"jobId":"..."}` immediately. This tests the direct download path.

- [ ] **Step 4: Poll job status**

Run: `curl http://localhost:8080/jobs`
Expected: Shows job with progress/status.

- [ ] **Step 5: Kill server and commit all remaining changes**

```bash
git add -A
git commit -m "feat: universal video downloader v2 — full implementation"
```

- [ ] **Step 6: Update README.md**

Update `README.md` to reflect the new features: popup UI, yt-dlp support, DASH, network interception, quality selection, audio extraction. Replace the existing content with updated architecture, setup instructions, and usage guide.
