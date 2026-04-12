package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"
)

const compatTimeout = 20 * time.Minute

// wmpCompatibleCodecs lists codecs that Windows Media Player can decode natively.
var wmpCompatibleVideo = map[string]bool{
	"h264": true, "h265": true, "hevc": true,
	"mpeg4": true, "mpeg2video": true, "msmpeg4v3": true,
	"wmv1": true, "wmv2": true, "wmv3": true, "vc1": true,
}

var wmpCompatibleAudio = map[string]bool{
	"aac": true, "mp3": true, "mp2": true,
	"wmav1": true, "wmav2": true, "wmalossless": true, "wmapro": true,
	"pcm_s16le": true, "pcm_s24le": true, "pcm_f32le": true,
	"flac": true, "alac": true,
}

type probeResult struct {
	Streams []probeStream `json:"streams"`
}

type probeStream struct {
	CodecType string `json:"codec_type"`
	CodecName string `json:"codec_name"`
}

// probeCodecs uses ffprobe to detect the video and audio codecs in a file.
// Returns (videoCodec, audioCodec, error).
func probeCodecs(ctx context.Context, filePath string) (string, string, error) {
	cmd := exec.CommandContext(ctx, "ffprobe",
		"-v", "quiet",
		"-print_format", "json",
		"-show_streams",
		"-select_streams", "v:0,a:0",
		filePath,
	)
	setHighPriority(cmd)

	var out bytes.Buffer
	cmd.Stdout = &out

	if err := cmd.Run(); err != nil {
		return "", "", fmt.Errorf("ffprobe: %w", err)
	}

	var result probeResult
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		return "", "", fmt.Errorf("parsing ffprobe output: %w", err)
	}

	var videoCodec, audioCodec string
	for _, s := range result.Streams {
		switch s.CodecType {
		case "video":
			videoCodec = s.CodecName
		case "audio":
			audioCodec = s.CodecName
		}
	}
	return videoCodec, audioCodec, nil
}

// ensureWMPCompatible checks a downloaded video file and re-encodes any
// incompatible codecs to H.264+AAC so it plays in Windows Media Player.
// Only re-encodes streams that need it — compatible streams are copied as-is.
// Returns the final output path (may differ if re-encoding changed the extension).
func ensureWMPCompatible(ctx context.Context, filePath string, job *Job) (string, error) {
	videoCodec, audioCodec, err := probeCodecs(ctx, filePath)
	if err != nil {
		log.Printf("[COMPAT] Could not probe %s: %v — skipping compatibility check", filePath, err)
		return filePath, nil
	}

	videoOK := videoCodec == "" || wmpCompatibleVideo[strings.ToLower(videoCodec)]
	audioOK := audioCodec == "" || wmpCompatibleAudio[strings.ToLower(audioCodec)]

	if videoOK && audioOK {
		log.Printf("[COMPAT] %s is already WMP-compatible (video=%s, audio=%s)", filePath, videoCodec, audioCodec)
		return filePath, nil
	}

	log.Printf("[COMPAT] Re-encoding for WMP compatibility (video=%s ok=%v, audio=%s ok=%v)",
		videoCodec, videoOK, audioCodec, audioOK)

	job.Status = "processing"
	job.Progress = 0

	tmpPath := filePath + ".compat.mp4"

	args := []string{"-y", "-i", filePath}

	if videoOK {
		args = append(args, "-c:v", "copy")
	} else {
		args = append(args, "-c:v", "libx264", "-crf", "18", "-preset", "fast")
	}

	if audioOK {
		args = append(args, "-c:a", "copy")
	} else {
		args = append(args, "-c:a", "aac", "-b:a", "192k")
	}

	args = append(args, "-movflags", "+faststart", tmpPath)

	ctx, cancel := context.WithTimeout(ctx, compatTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	setHighPriority(cmd)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		snippet := stderr.String()
		if len(snippet) > 300 {
			snippet = snippet[len(snippet)-300:]
		}
		os.Remove(tmpPath)
		return "", fmt.Errorf("compatibility re-encode failed: %w — %s", err, strings.TrimSpace(snippet))
	}

	// Replace original with compatible version
	os.Remove(filePath)
	if err := os.Rename(tmpPath, filePath); err != nil {
		// If rename fails (e.g. cross-device), the tmp file is still valid
		os.Remove(filePath)
		return tmpPath, nil
	}

	log.Printf("[COMPAT] Re-encoded %s successfully", filePath)
	return filePath, nil
}
