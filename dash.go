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

type mpd struct {
	XMLName xml.Name    `xml:"MPD"`
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
	ID        string      `xml:"id,attr"`
	Bandwidth int         `xml:"bandwidth,attr"`
	Width     int         `xml:"width,attr"`
	Height    int         `xml:"height,attr"`
	BaseURL   string      `xml:"BaseURL"`
	SegList   *mpdSegList `xml:"SegmentList"`
	SegBase   *mpdSegBase `xml:"SegmentBase"`
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

const dashOverallTimeout = 30 * time.Minute

func downloadDASH(ctx context.Context, req DownloadRequest, job *Job, outPath string) error {
	ctx, cancel := context.WithTimeout(ctx, dashOverallTimeout)
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
	_ = manifest.BaseURL

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

	videoTmp := outPath + ".video.tmp"
	videoSegs := getSegmentURLs(videoRep, videoAS, baseURL)
	if err := downloadSegments(ctx, client, req, job, videoSegs, videoTmp, "video"); err != nil {
		return err
	}

	if audioRep != nil {
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
		job.Status = "processing"
		if err := ffmpegConvert(ctx, videoTmp, outPath); err != nil {
			os.Remove(videoTmp)
			return err
		}
		os.Remove(videoTmp)
	}

	return nil
}

func getSegmentURLs(rep *mpdRepresentation, as *mpdAdaptationSet, manifestURL string) []string {
	base, _ := url.Parse(manifestURL)
	base.Path = path.Dir(base.Path) + "/"

	var urls []string

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

	if rep.BaseURL != "" {
		resolved, _ := base.Parse(rep.BaseURL)
		urls = append(urls, resolved.String())
		return urls
	}

	if as != nil && as.BaseURL != "" {
		resolved, _ := base.Parse(as.BaseURL)
		urls = append(urls, resolved.String())
	}

	return urls
}

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
