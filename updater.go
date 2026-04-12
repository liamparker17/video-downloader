package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Version is the current release tag. Set this when cutting a release.
var Version = "v1.1.0"

const (
	githubRepo    = "liamparker17/video-downloader"
	updateTimeout = 60 * time.Second
	binaryName    = "video-downloader.exe"
)

type githubRelease struct {
	TagName string        `json:"tag_name"`
	Assets  []githubAsset `json:"assets"`
}

type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

// autoUpdate checks GitHub Releases for a newer version and self-updates if found.
// Called at startup before the server starts. Non-fatal — logs and returns on any error.
func autoUpdate() {
	// Clean up .old binary from a previous update
	exe, err := os.Executable()
	if err != nil {
		return
	}
	exe, _ = filepath.EvalSymlinks(exe)
	dir := filepath.Dir(exe)
	oldPath := filepath.Join(dir, binaryName+".old")
	os.Remove(oldPath)

	log.Printf("[UPDATE] Current version: %s — checking for updates...", Version)

	client := &http.Client{Timeout: updateTimeout}

	req, err := http.NewRequest("GET",
		fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", githubRepo), nil)
	if err != nil {
		log.Printf("[UPDATE] Failed to create request: %v", err)
		return
	}
	req.Header.Set("User-Agent", "video-downloader-updater")

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[UPDATE] Could not reach GitHub: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		log.Printf("[UPDATE] GitHub API returned %d", resp.StatusCode)
		return
	}

	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		log.Printf("[UPDATE] Failed to parse release info: %v", err)
		return
	}

	if !isNewer(release.TagName, Version) {
		log.Printf("[UPDATE] Already up to date (%s)", Version)
		return
	}

	// Find the binary asset
	var asset *githubAsset
	for i := range release.Assets {
		if release.Assets[i].Name == binaryName {
			asset = &release.Assets[i]
			break
		}
	}
	if asset == nil {
		log.Printf("[UPDATE] Release %s has no %s asset", release.TagName, binaryName)
		return
	}

	log.Printf("[UPDATE] New version available: %s -> %s (%.1f MB)",
		Version, release.TagName, float64(asset.Size)/(1024*1024))

	// Download new binary to temp file
	tmpPath := filepath.Join(dir, binaryName+".new")
	if err := downloadFile(client, asset.BrowserDownloadURL, tmpPath); err != nil {
		log.Printf("[UPDATE] Download failed: %v", err)
		os.Remove(tmpPath)
		return
	}

	// Swap: current -> .old, new -> current
	currentPath := filepath.Join(dir, binaryName)
	if err := os.Rename(currentPath, oldPath); err != nil {
		log.Printf("[UPDATE] Could not rename current binary: %v", err)
		os.Remove(tmpPath)
		return
	}
	if err := os.Rename(tmpPath, currentPath); err != nil {
		// Rollback
		log.Printf("[UPDATE] Could not place new binary: %v", err)
		os.Rename(oldPath, currentPath)
		return
	}

	log.Printf("[UPDATE] Updated to %s! Restart the application to use the new version.", release.TagName)
	log.Println("[UPDATE] Restarting now...")

	// Re-launch the new binary and exit this one
	restart(currentPath)
}

func downloadFile(client *http.Client, url, destPath string) error {
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}

	out, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("create %s: %w", destPath, err)
	}
	defer out.Close()

	if _, err := io.Copy(out, resp.Body); err != nil {
		return fmt.Errorf("writing %s: %w", destPath, err)
	}
	return nil
}

// isNewer returns true if remote is a higher semver than local.
// Compares tags like "v1.2.0" by stripping the "v" prefix and comparing segments.
func isNewer(remote, local string) bool {
	remote = strings.TrimPrefix(remote, "v")
	local = strings.TrimPrefix(local, "v")

	rParts := strings.Split(remote, ".")
	lParts := strings.Split(local, ".")

	for i := 0; i < len(rParts) && i < len(lParts); i++ {
		r, l := 0, 0
		fmt.Sscanf(rParts[i], "%d", &r)
		fmt.Sscanf(lParts[i], "%d", &l)
		if r > l {
			return true
		}
		if r < l {
			return false
		}
	}
	return len(rParts) > len(lParts)
}
