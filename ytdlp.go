package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const ytdlpTimeout = 15 * time.Minute

var ytdlpAvailable bool

var ytdlpProgressRe = regexp.MustCompile(`\[download\]\s+([\d.]+)%`)
var ytdlpSpeedUnitRe = regexp.MustCompile(`([\d.]+)\s*(B|KiB|MiB|GiB)/s`)

func checkYtdlp() (bool, string) {
	return checkTool("yt-dlp", "--version")
}

func downloadYtdlp(ctx context.Context, req DownloadRequest, job *Job, outPath string) error {
	if !ytdlpAvailable {
		return fmt.Errorf("yt-dlp not installed — required for this site")
	}

	ctx, cancel := context.WithTimeout(ctx, ytdlpTimeout)
	defer cancel()

	// Find ffmpeg so yt-dlp can merge video+audio streams
	ffmpegPath, _ := exec.LookPath("ffmpeg")

	// Use multiple concurrent fragment downloads to saturate the connection.
	// More TCP connections = larger share of available bandwidth at the network level.
	// 8 fragments for a single download is aggressive but maximizes throughput.
	concurrentFrags := 8
	active := int(activeDownloads.Load())
	if active > 1 {
		// Scale down fragments when multiple downloads are active to stay fair
		concurrentFrags = max(2, 8/active)
	}

	args := []string{
		"--progress",
		"--newline",
		"--no-part",
		"--concurrent-fragments", fmt.Sprintf("%d", concurrentFrags),
		"-o", outPath,
	}

	// Fair-share bandwidth limiting when multiple downloads are active
	if rateLimit := ytdlpRateFlag(); rateLimit != "" {
		args = append(args, "--limit-rate", rateLimit)
		log.Printf("[YT-DLP] Rate-limited to %s (concurrent downloads active)", rateLimit)
	}

	if ffmpegPath != "" {
		args = append(args, "--ffmpeg-location", filepath.Dir(ffmpegPath))
	}

	if req.AudioOnly {
		args = append(args, "--extract-audio", "--audio-format", "mp3")
	} else {
		// Merge into mp4 container; prefer H.264+AAC for Windows Media Player compatibility
		args = append(args, "--merge-output-format", "mp4")
		switch req.Quality {
		case "480":
			args = append(args, "-f", "bestvideo[vcodec^=avc1][height<=480]+bestaudio[acodec^=mp4a]/bestvideo[height<=480]+bestaudio/best[height<=480]/best")
		case "720":
			args = append(args, "-f", "bestvideo[vcodec^=avc1][height<=720]+bestaudio[acodec^=mp4a]/bestvideo[height<=720]+bestaudio/best[height<=720]/best")
		case "1080":
			args = append(args, "-f", "bestvideo[vcodec^=avc1][height<=1080]+bestaudio[acodec^=mp4a]/bestvideo[height<=1080]+bestaudio/best[height<=1080]/best")
		default:
			args = append(args, "-f", "bestvideo[vcodec^=avc1]+bestaudio[acodec^=mp4a]/bestvideo+bestaudio/best")
		}
		// Re-encode audio to AAC during merge for Windows Media Player compatibility
		// (copies video as-is, only audio gets re-encoded — fast)
		args = append(args, "--ppa", "Merger:-c:v copy -c:a aac -b:a 192k")
	}

	if req.Cookies != "" {
		args = append(args, "--add-header", "Cookie:"+req.Cookies)
	}

	targetURL := req.PageURL
	if targetURL == "" {
		targetURL = req.URL
	}
	args = append(args, targetURL)

	log.Printf("[YT-DLP] Running: yt-dlp %s", strings.Join(args, " "))

	cmd := exec.CommandContext(ctx, "yt-dlp", args...)
	setHighPriority(cmd)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("yt-dlp stdout pipe: %w", err)
	}
	cmd.Stderr = cmd.Stdout

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("yt-dlp start: %w", err)
	}

	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()

		if match := ytdlpProgressRe.FindStringSubmatch(line); len(match) > 1 {
			if pct, err := strconv.ParseFloat(match[1], 64); err == nil {
				job.Progress = pct
			}
		}
		if match := ytdlpSpeedUnitRe.FindStringSubmatch(line); len(match) > 2 {
			if val, err := strconv.ParseFloat(match[1], 64); err == nil {
				var bps float64
				switch match[2] {
				case "GiB":
					bps = val * 1024 * 1024 * 1024
				case "MiB":
					bps = val * 1024 * 1024
				case "KiB":
					bps = val * 1024
				default:
					bps = val
				}
				job.SpeedBPS = bps
				job.Speed = formatSpeed(bps)
			}
		}
	}

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("yt-dlp failed: %w", err)
	}

	log.Printf("[YT-DLP] Download completed: %s", outPath)
	return nil
}
