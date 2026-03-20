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

var ytdlpProgressRe = regexp.MustCompile(`\[download\]\s+([\d.]+)%`)
var ytdlpSpeedRe = regexp.MustCompile(`at\s+([\d.]+\s*\S+/s)`)

func checkYtdlp() (bool, string) {
	return checkTool("yt-dlp", "--version")
}

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
