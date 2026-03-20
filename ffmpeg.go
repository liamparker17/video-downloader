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
