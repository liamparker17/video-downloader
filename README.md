# Video Downloader

A browser-based video downloader with a Chrome extension frontend and Go backend. Right-click any video to download it locally.

## Architecture

```
Browser Extension (Chrome MV3)
  → HTTP POST to localhost:8080/download
    → Go Backend
      → Direct download (.mp4, .webm)
      → HLS download (.m3u8 → .ts segments → ffmpeg → .mp4)
```

## Prerequisites

- **Go 1.22+** — [https://go.dev/dl/](https://go.dev/dl/)
- **ffmpeg** — required for HLS stream conversion. Must be on your PATH.
- **Google Chrome** or Chromium-based browser

## Setup

### 1. Start the Go Backend

```bash
cd "Video Downloader"
go run .
```

The server starts on `http://localhost:8080`. Downloaded videos are saved to the `downloads/` folder.

### 2. Load the Chrome Extension

1. Open Chrome and go to `chrome://extensions`
2. Enable **Developer mode** (top-right toggle)
3. Click **Load unpacked**
4. Select the `extension/` folder from this project

### 3. Use It

1. Navigate to any page with a video
2. Right-click on the video (or anywhere on the page)
3. Click **"Download Video"**
4. Check the Go server logs — the video will be saved to `downloads/`

## Supported Formats

| Format | Method |
|--------|--------|
| `.mp4`, `.webm` | Direct HTTP download (streamed to disk) |
| `.m3u8` (HLS) | Segments downloaded sequentially, concatenated, converted via ffmpeg |

## Limitations

- Does not bypass DRM or download encrypted streams
- Does not handle DASH (`.mpd`) streams
- `blob:` URLs are not supported (these are typically DRM-protected)
- Requires the Go backend to be running locally

## Project Structure

```
├── main.go            # HTTP server and request handler
├── downloader.go      # Download logic (direct + HLS)
├── go.mod             # Go module definition
├── downloads/         # Downloaded videos saved here
├── extension/
│   ├── manifest.json  # Chrome MV3 extension manifest
│   ├── background.js  # Service worker: context menu + backend communication
│   └── content.js     # Content script: video detection on pages
└── README.md
```
