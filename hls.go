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
const manifestTimeout = 30 * time.Second
const hlsOverallTimeout = 30 * time.Minute

var segmentBackoffs = []time.Duration{1 * time.Second, 3 * time.Second}

func downloadHLS(ctx context.Context, req DownloadRequest, job *Job, outPath string) error {
	ctx, cancel := context.WithTimeout(ctx, hlsOverallTimeout)
	defer cancel()

	client := &http.Client{}

	manifestCtx, manifestCancel := context.WithTimeout(ctx, manifestTimeout)
	defer manifestCancel()

	httpReq, err := buildRequest(manifestCtx, "GET", req.URL, req)
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

	if strings.Contains(content, "#EXT-X-STREAM-INF") {
		variantURL, err := selectHLSVariant(content, req.URL, req.Quality)
		if err != nil {
			return err
		}
		log.Printf("[HLS] Selected variant: %s", variantURL)

		req2 := req
		req2.URL = variantURL
		return downloadHLS(ctx, req2, job, outPath)
	}

	segments, err := parseM3U8Segments(strings.NewReader(content), req.URL)
	if err != nil {
		return fmt.Errorf("parsing m3u8: %w", err)
	}
	log.Printf("[HLS] Found %d segments", len(segments))

	if len(segments) == 0 {
		return fmt.Errorf("no segments found in m3u8 playlist")
	}

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

	job.Status = "processing"
	if err := ffmpegConvert(ctx, tsPath, outPath); err != nil {
		return err
	}
	os.Remove(tsPath)
	return nil
}

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
		best := variants[0]
		for _, v := range variants {
			if v.height <= targetHeight && v.height > best.height {
				best = v
			}
		}
		return best.url, nil
	}

	best := variants[0]
	for _, v := range variants {
		if v.bandwidth > best.bandwidth {
			best = v
		}
	}
	return best.url, nil
}

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
