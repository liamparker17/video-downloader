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
				log.Printf("[PIPELINE] Trying raw stream download for job %s", job.ID)
				err = downloadDirect(ctx, req, job, outPath)
			}
		}
	} else {
		err = fmt.Errorf("no video URL")
	}

	if err != nil && req.PageURL != "" {
		log.Printf("[PIPELINE] Go extractors failed (%v), trying yt-dlp for job %s", err, job.ID)
		err = downloadYtdlp(ctx, req, job, outPath)
	}

	if err != nil {
		job.Status = "failed"
		job.Error = err.Error()
		log.Printf("[PIPELINE] Job %s failed: %v", job.ID, err)
		cleanupTempFiles(outPath)
		return
	}

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

// getDownloadsDir returns the user's system Downloads folder.
func getDownloadsDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "downloads"
	}
	dir := filepath.Join(home, "Downloads")
	os.MkdirAll(dir, 0755)
	return dir
}

func generateFilename(title, ext string) string {
	timestamp := time.Now().Format("20060102_150405_000")
	sanitized := sanitizeTitle(title)
	dir := getDownloadsDir()
	if sanitized != "" {
		return filepath.Join(dir, fmt.Sprintf("%s_%s%s", timestamp, sanitized, ext))
	}
	return filepath.Join(dir, fmt.Sprintf("%s%s", timestamp, ext))
}

var unsafeChars = regexp.MustCompile(`[^a-zA-Z0-9_\-\s]`)
var multiSpace = regexp.MustCompile(`\s+`)

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
