# Video Downloader

A universal browser-based video downloader with a Chrome extension frontend and Go backend. Downloads videos from nearly any website — including YouTube, Twitter, Instagram, and 1000+ sites via yt-dlp fallback.

## Architecture

```
Chrome Extension (MV3)
  ├── Network interception (catches video URLs from traffic)
  ├── DOM scanning (finds <video> elements)
  ├── Toolbar popup (source picker, quality, progress)
  └── Right-click context menu (one-click download)
        │
        ▼
  Go Backend (localhost:8080)
  ├── POST /download    → create async download job
  ├── GET  /jobs/{id}   → poll job progress
  ├── GET  /jobs        → list all jobs
  ├── DELETE /jobs/{id} → cancel job
  └── GET  /health      → tool availability
        │
        ▼
  Extraction Pipeline
  1. Direct download (.mp4, .webm, .mkv, .avi, .mov)
  2. HLS (.m3u8 → master playlist selection → segments → ffmpeg)
  3. DASH (.mpd → video+audio segments → ffmpeg mux)
  4. Raw stream (network-intercepted URLs)
  5. yt-dlp fallback (YouTube, Twitter, Instagram, etc.)
```

## Prerequisites

- **Go 1.22+** — [go.dev/dl](https://go.dev/dl/)
- **ffmpeg** — required for media conversion. Must be on your PATH.
- **yt-dlp** (optional, recommended) — enables YouTube and social media downloads. [github.com/yt-dlp/yt-dlp](https://github.com/yt-dlp/yt-dlp/releases)
- **Google Chrome** or Chromium-based browser

## Quick Start

### 1. Install dependencies and build

```bash
# Option A: Use the install script (Windows)
install.bat

# Option B: Manual
go build -o video-downloader.exe .
```

### 2. Start the backend

```bash
./video-downloader.exe
```

The server starts on `http://localhost:8080` and logs tool availability:
```
[STARTUP] ffmpeg: found (ffmpeg version 6.1)
[STARTUP] yt-dlp: found (2024.12.06)
Video downloader server starting on http://localhost:8080
```

### 3. Load the Chrome extension

1. Open Chrome → `chrome://extensions`
2. Enable **Developer mode** (top-right toggle)
3. Click **Load unpacked**
4. Select the `extension/` folder

### 4. Download videos

**Right-click method:** Right-click any video → "Download Video" → auto-downloads best quality.

**Popup method:** Click the extension toolbar icon → browse detected sources → pick quality → click "Download Selected".

## Features

| Feature | Description |
|---------|-------------|
| **Multi-format** | Direct files, HLS, DASH, and 1000+ sites via yt-dlp |
| **Network interception** | Catches video URLs from browser network traffic |
| **Quality selection** | Best / 1080p / 720p / 480p |
| **Audio extraction** | Toggle "Audio only" to save as .mp3 |
| **Live progress** | Progress bars, speed, and status in the popup |
| **Async jobs** | Downloads run in background, poll for status |
| **Auto-retry** | Configurable retries with backoff |
| **HLS master playlist** | Automatically selects best quality variant |
| **DASH muxing** | Downloads separate video+audio, muxes via ffmpeg |

## Supported Sites

- **Direct downloads:** Any site serving .mp4, .webm, .mkv, .avi, .mov files
- **HLS streams:** Sites using .m3u8 playlists (including master playlists with quality variants)
- **DASH streams:** Sites using .mpd manifests (SegmentList and BaseURL modes)
- **yt-dlp fallback:** YouTube, Twitter/X, Instagram, TikTok, Vimeo, Reddit, Facebook, and [1000+ more](https://github.com/yt-dlp/yt-dlp/blob/master/supportedsites.md)

## Limitations

- Does not bypass DRM or download encrypted streams
- `blob:` URLs are not supported (typically DRM-protected)
- DASH `SegmentTemplate` mode falls through to yt-dlp
- Requires the Go backend running locally
- yt-dlp must be installed for YouTube/social media support

## API

| Endpoint | Method | Purpose |
|----------|--------|---------|
| `/download` | POST | Create download job (async). Body: `{url, pageUrl, title, cookies, headers, quality, audioOnly}` |
| `/jobs/{id}` | GET | Poll job status: `{id, status, progress, speed, filename, error}` |
| `/jobs` | GET | List all jobs |
| `/jobs/{id}` | DELETE | Cancel or remove a job |
| `/health` | GET | Check ffmpeg and yt-dlp availability |

## Project Structure

```
├── main.go              # HTTP server, routes, CORS, startup checks
├── downloader.go        # Pipeline router, shared helpers, filename generation
├── direct.go            # Direct download extractor
├── hls.go               # HLS extractor (master + variant playlists)
├── dash.go              # DASH extractor (.mpd XML parser)
├── ytdlp.go             # yt-dlp fallback extractor
├── job.go               # Job struct, store, cancellation, cleanup
├── progress.go          # ProgressWriter for tracking speed/percentage
├── retry.go             # Configurable HTTP retry with backoff
├── ffmpeg.go            # ffmpeg wrapper (convert, mux, audio extract)
├── go.mod               # Go module (stdlib only, zero dependencies)
├── install.bat          # Dependency checker + builder
├── downloads/           # Downloaded videos saved here
└── extension/
    ├── manifest.json    # Chrome MV3 manifest
    ├── background.js    # Network interception, context menu, messaging
    ├── content.js       # DOM video detection
    ├── popup.html       # Toolbar popup markup
    ├── popup.js         # Popup logic (sources, downloads, settings)
    └── popup.css        # Dark theme popup styling
```
