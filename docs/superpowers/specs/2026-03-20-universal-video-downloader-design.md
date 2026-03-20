# Universal Video Downloader — Design Spec

## Overview

Expand the existing video downloader from a basic direct/HLS-only tool into a universal video extraction system. A Chrome MV3 extension detects video sources via DOM scanning and network interception, and a Go backend downloads them through a multi-stage extraction pipeline with yt-dlp as a fallback for proprietary platforms (YouTube, Twitter, Instagram, etc.).

## Requirements

- Download videos from nearly any website, including YouTube, Twitter, Instagram, TikTok, Vimeo, Reddit, Facebook
- Support direct downloads (.mp4, .webm, .mkv, .avi, .mov), HLS (.m3u8), DASH (.mpd), and proprietary streams via yt-dlp
- Intercept network requests in the browser to catch video URLs that don't appear in the DOM
- Provide a toolbar popup UI with source picker, quality selector, audio-only toggle, and live download progress
- Right-click context menu for instant one-click downloads (auto-picks best source)
- Audio-only extraction (output .mp3/.m4a)
- Quality selection (Best / 1080p / 720p / 480p)

## Architecture

```
Chrome Extension (MV3)
  ├── background.js — network interception, context menu, job dispatch
  ├── content.js — DOM video detection
  ├── popup.html/js/css — source picker, downloads panel, settings
  │
  └── HTTP ──→ Go Backend (localhost:8080)
                      ├── POST /download — create job (async, returns jobId)
                      ├── GET /jobs/{id} — poll job progress
                      ├── GET /jobs — list all jobs
                      ├── DELETE /jobs/{id} — cancel/remove job
                      └── GET /health — external tool availability
                      │
                      └── Extraction Pipeline
                            1. Direct Download (.mp4, .webm, etc.)
                            2. HLS Extractor (.m3u8 + master playlist support)
                            3. DASH Extractor (.mpd — XML manifest)
                            4. Raw Stream (network-intercepted URLs)
                            5. yt-dlp Fallback (page URL → yt-dlp)
                            │
                            └── ffmpeg post-processing
                                  - HLS: concat .ts → .mp4
                                  - DASH: mux video + audio → .mp4
                                  - Audio-only: extract → .mp3
```

## Go Backend

### API Endpoints

| Endpoint | Method | Purpose |
|----------|--------|---------|
| `POST /download` | POST | Create a download job. Returns `{ "jobId": "abc123" }` **immediately** (non-blocking). The download runs in a background goroutine. |
| `GET /jobs/{id}` | GET | Poll job progress. Returns `{ status, progress, speed, filename, error }` |
| `GET /jobs` | GET | List all jobs (active + completed + failed) |
| `DELETE /jobs/{id}` | DELETE | Cancel active job or remove completed one |
| `GET /health` | GET | External tool availability (ffmpeg, yt-dlp versions or "not found") |

**Note:** Route parameters use Go 1.22+ `{id}` syntax for `http.ServeMux`. CORS middleware must allow `GET, POST, DELETE, OPTIONS` methods.

### POST /download Request Body

```json
{
  "url": "string",
  "pageUrl": "string",
  "title": "string",
  "cookies": "string",
  "headers": {
    "User-Agent": "string",
    "Referer": "string"
  },
  "quality": "best|1080|720|480",
  "audioOnly": false
}
```

- `url` — the detected video source URL (may be empty if only page URL is available)
- `pageUrl` — the browser page URL (always sent; used for yt-dlp fallback and Referer)
- `title` — page title from `document.title` (used for filename; fallback to URL path or timestamp if empty)
- `quality` — ignored for direct downloads (single source, no variants to select)

### Job State

```go
type Job struct {
    ID        string    // unique job ID (UUID or timestamp-based)
    URL       string    // video source URL
    PageURL   string    // the page the video was on (for yt-dlp)
    Status    string    // pending | downloading | processing | completed | failed
    Progress  float64   // 0.0 - 100.0
    Speed     string    // e.g. "2.4 MB/s"
    Filename  string    // output filename
    Error     string    // error message if failed
    AudioOnly bool
    Quality   string    // "best", "1080", "720", "480"
    CreatedAt time.Time
}
```

Job lifecycle: `pending → downloading → processing → completed` (or `→ failed` at any stage).

Jobs stored in `sync.Map` — no database needed. Completed and failed jobs are evicted after 1 hour via a background cleanup goroutine (runs every 10 minutes). This prevents unbounded memory growth.

### Job Cancellation

Active jobs use `context.WithCancel`. When `DELETE /jobs/{id}` is called on a running job:
- The job's context is cancelled, which aborts in-flight HTTP requests
- For yt-dlp jobs, the child process is killed via `cmd.Process.Kill()`
- Temp files are cleaned up
- Job status is set to `failed` with error "Cancelled by user"

### Extraction Pipeline

Each extractor is tried in order. First successful match handles the download.

**1. Direct Download**
- Matches: URL path ends in `.mp4`, `.webm`, `.mkv`, `.avi`, `.mov`
- Streams to disk via `io.Copy` through a `ProgressWriter` that tracks bytes vs `Content-Length`
- 1 retry, 10-minute timeout

**2. HLS Extractor**
- Matches: URL path ends in `.m3u8`
- Fetches playlist. If it's a **master playlist** (contains `#EXT-X-STREAM-INF`), selects the variant matching the requested quality (or best available) and re-fetches that variant
- Downloads `.ts` segments sequentially, progress = `completedSegments / totalSegments`
- Concatenates segments into single `.ts` file
- ffmpeg converts `.ts → .mp4` (`-c copy`)
- Segment retries use a configurable `retryRequest(client, req, maxRetries, backoffs)` function (replaces the existing `doWithRetry` which only supports 1 retry)
- 2 retries per segment (1s then 3s backoff), 60s timeout per segment

**3. DASH Extractor**
- Matches: URL path ends in `.mpd`
- Fetches `.mpd` XML manifest, parses `<AdaptationSet>` and `<Representation>` elements
- Selects best video representation (or user-requested quality) + best audio representation
- Downloads video segments and audio segments separately
- ffmpeg muxes video + audio into `.mp4` (`-c copy`)
- 2 retries per segment, 60s timeout per segment

**4. Raw Stream**
- Matches: network-intercepted URL with video MIME type but no recognized extension
- Treated as direct download — stream to file with headers/cookies
- 1 retry, 10-minute timeout

**5. yt-dlp Fallback**
- Matches: when all above extractors fail, or when only a page URL is available
- Shells out: `yt-dlp --progress --newline -o <output> <pageURL>`
- Audio-only: adds `--extract-audio --audio-format mp3`
- Quality: adds `-f "bestvideo[height<=X]+bestaudio/best[height<=X]"`
- Progress parsed from stdout lines (`[download]  45.2% of ~12.3MiB at 2.4MiB/s`)
- 15-minute timeout, 0 retries (yt-dlp has internal retry logic)
- If yt-dlp not installed, job fails with: "yt-dlp not installed — required for this site"

### Post-Processing

If `audioOnly` is true and the extractor produced a video file:
```
ffmpeg -i input.mp4 -vn -acodec libmp3lame -q:a 2 output.mp3
```
10-minute timeout.

### Progress Tracking

`ProgressWriter` wraps `io.Writer`:
- Intercepts `Write()` calls, counts bytes written
- Calculates `progress = (written / total) * 100` and `speed = written / elapsed`
- Updates job's `Progress` and `Speed` fields in place
- For segment-based downloads (HLS/DASH): `progress = (completedSegments / totalSegments) * 100`
- For yt-dlp: stdout parsed line-by-line for progress percentage and speed

### File Naming

```
downloads/{timestamp}_{sanitized_title}.{ext}
```
- `timestamp`: `20260320_143022_481` (with milliseconds to avoid collisions)
- `sanitized_title`: priority order: (1) `title` field from request body (page title sent by extension), (2) yt-dlp metadata output title, (3) filename from URL path. Special characters stripped, capped at 80 chars
- `ext`: `.mp4` for video, `.mp3` for audio-only
- Fallback: timestamp-only if title unavailable

### File Safety

- Intermediate files written with `.tmp` suffix, renamed on completion
- Failed jobs delete temp files — no partial files left behind
- Intermediate concat/mux inputs (`.ts`, separate audio/video tracks) deleted after ffmpeg

### Startup Checks

On startup, the server logs and caches availability of external tools:
```
[STARTUP] ffmpeg: found (v6.1)
[STARTUP] yt-dlp: found (2024.12.06)
```
Exposed via `GET /health` for the extension settings panel.

## Chrome Extension

### Permissions

```json
{
  "permissions": ["contextMenus", "activeTab", "cookies", "webRequest", "storage"],
  "host_permissions": ["<all_urls>"]
}
```

**New permissions vs existing:** `webRequest` and `storage` are additions. `declarativeNetRequest` is NOT needed — the extension only observes requests, it does not modify them.

### Network Interception (background.js)

Uses `chrome.webRequest.onCompleted` to passively monitor all network requests per tab.

**MV3 limitation:** In Manifest V3, `chrome.webRequest.onCompleted` does NOT reliably expose response headers (`Content-Type`, `Content-Length`) unless `extraHeaders` opt-in is used via `chrome.webRequest.onCompleted.addListener(callback, filter, ["responseHeaders", "extraHeaders"])`. Even with this, header access may be limited on some platforms. **Primary detection is therefore URL-pattern-based, with MIME type as a secondary signal when available.**

**Capture criteria** — a request is flagged as video if:
- URL path ends in `.mp4`, `.webm`, `.m3u8`, `.mpd`, `.ts` (primary — always works)
- OR response `Content-Type` contains `video/`, `application/x-mpegURL`, `application/dash+xml` (secondary — when headers are available)

**Stored per detection:**
- `url` — the resource URL
- `type` — `mp4 | hls | dash | unknown`
- `contentType` — MIME from response headers (may be empty if headers unavailable)
- `size` — Content-Length if available (may be 0)
- `timestamp` — when detected

**Deduplication:** same URL on same tab stored once. `.ts` segments grouped by base path (keep parent `.m3u8`, not individual segments).

**Cleanup:** entries removed on `chrome.tabs.onRemoved` and `chrome.tabs.onUpdated` (navigation).

### Video Detection Priority (combined)

When a download is triggered, sources are ranked:

1. User-selected source (from popup picker)
2. Right-clicked `<video>` element `srcUrl`
3. Network-intercepted `.m3u8` or `.mpd` (highest quality stream)
4. Network-intercepted `.mp4`/`.webm` (largest Content-Length)
5. DOM `<video>` src / currentSrc
6. DOM `<source>` elements
7. DOM scan for video URLs in `[src]`/`[href]` attributes
8. Page URL (sent to backend for yt-dlp fallback)

Source 8 ensures YouTube, Twitter, Instagram, and any yt-dlp-supported site works even when no direct video URL is detectable.

### Extension Messaging Protocol

Communication between popup, background, and content scripts:

| From | To | Message | Purpose |
|------|----|---------|---------|
| popup.js | background.js | `{ action: "getSources" }` | Request network-intercepted URLs for current tab |
| background.js | popup.js | `{ sources: [...] }` | Return intercepted video URLs |
| popup.js | content.js | `{ action: "getVideoInfo" }` | Request DOM-detected video URLs |
| content.js | popup.js | `{ url, sources: [...] }` | Return DOM video sources |
| popup.js | background.js | `{ action: "download", payload: {...} }` | Trigger download via backend |
| background.js | content.js | `{ action: "getVideoInfo", clickedSrc }` | Get video info on right-click (existing) |

### Request Payload (Extension → Backend)

The extension **must** send all of these fields in the POST `/download` body:
- `url` — best detected video source URL (may be empty)
- `pageUrl` — `tab.url` (always sent)
- `title` — `document.title` from content script (always sent)
- `cookies` — from `chrome.cookies.getAll()`
- `headers` — `{ "User-Agent": navigator.userAgent, "Referer": tab.url }`
- `quality` — from user selection or default setting
- `audioOnly` — from user toggle or default setting

### Context Menu (right-click)

"Download Video" — auto-picks best source using the priority list above, sends to backend with quality=best. No popup interaction needed.

### Toolbar Popup UI

Two-tab layout:

**Sources Tab:**
- Lists all detected videos for the current tab
- Each entry shows: filename/URL snippet, type badge, size estimate
- Radio selection for which source to download
- "Page URL (yt-dlp)" always present as last option
- Quality dropdown: Best / 1080p / 720p / 480p
- Audio-only checkbox
- "Download Selected" button

**Downloads Tab:**
- Lists all jobs from `GET /jobs`
- Each entry: filename, progress bar, percentage, speed, status
- Completed jobs show checkmark and file size
- Failed jobs show error message and "Retry" button
- Polls every 2 seconds while popup is open

**Settings (gear icon):**
- Default quality preference
- Default audio-only toggle
- yt-dlp status indicator (from `/status/health`)
- ffmpeg status indicator

Settings stored via `chrome.storage.local`.

### Content Script Video Detection (content.js)

The `isVideoUrl()` function must recognize all supported extensions:
`.mp4`, `.webm`, `.m3u8`, `.mpd`, `.ts`, `.mkv`, `.avi`, `.mov`

This is an update from the existing code which does not include `.mpd`.

### Extension File Structure

```
extension/
├── manifest.json
├── background.js      # Service worker: network intercept, context menu, job dispatch
├── content.js          # DOM video detection
├── popup.html          # Toolbar popup markup
├── popup.js            # Popup logic: source list, downloads panel, settings, polling
├── popup.css           # Popup styling
└── icons/
    ├── icon16.png
    ├── icon48.png
    └── icon128.png
```

## Error Handling & Resilience

### Retry Strategy

| Component | Retries | Backoff |
|-----------|---------|---------|
| Direct download | 1 | 1 second |
| HLS/DASH segment | 2 per segment | 1s, then 3s |
| Manifest fetch (.m3u8/.mpd) | 1 | 1 second |
| yt-dlp process | 0 | yt-dlp has internal retries |

### Timeouts

| Operation | Timeout |
|-----------|---------|
| Manifest/playlist fetch | 30 seconds |
| Individual segment download | 60 seconds |
| Direct file download | 10 minutes |
| yt-dlp process | 15 minutes |
| ffmpeg conversion | 10 minutes |

### User-Facing Error Messages

| Condition | Message |
|-----------|---------|
| HTTP 4xx/5xx after retries | "Video URL not reachable" |
| All extractors fail | "No video found on page" |
| yt-dlp not installed | "yt-dlp not installed — required for this site" |
| ffmpeg fails | "ffmpeg failed: {stderr snippet}" |
| Timeout exceeded | "Download timed out" |
| DRM/encrypted content | "Unsupported format (possibly DRM protected)" |

### Extension Error States

- Backend unreachable → popup shows "Backend not running. Start video-downloader.exe"
- Job fails → downloads panel shows error + "Retry" button
- No videos detected → sources tab shows "No videos found" + one-click "Try with yt-dlp" using page URL

## Go Project File Structure

```
├── main.go              # HTTP server, CORS, routing, startup checks
├── downloader.go        # Extraction pipeline router
├── direct.go            # Direct download extractor
├── hls.go               # HLS extractor (master + variant playlists)
├── dash.go              # DASH extractor (.mpd XML parser)
├── ytdlp.go             # yt-dlp fallback extractor
├── job.go               # Job struct, job store (sync.Map), status helpers
├── progress.go          # ProgressWriter for tracking bytes/speed
├── ffmpeg.go            # ffmpeg wrapper (convert, mux, audio extract)
├── go.mod
├── install.bat           # Dependency checker + builder
├── downloads/            # Output directory
├── extension/            # Chrome extension (see above)
└── docs/
    └── superpowers/specs/
        └── 2026-03-20-universal-video-downloader-design.md
```

## Dependencies

| Dependency | Required | Purpose |
|------------|----------|---------|
| Go 1.22+ | Yes | Backend runtime |
| ffmpeg | Yes | Media conversion, muxing, audio extraction |
| yt-dlp | No (recommended) | Fallback extractor for YouTube, social media, 1000+ sites |
| Chrome/Chromium | Yes | Browser extension host |

`install.bat` checks for Go and ffmpeg (required), attempts to install yt-dlp via `winget install yt-dlp`, and builds the Go binary.

## Install Script (install.bat)

The install script performs these steps in order:

1. **Check Go** — `where go`. If missing, print download URL and exit with error.
2. **Check ffmpeg** — `where ffmpeg`. If missing, print download URL and `winget install Gyan.FFmpeg` suggestion, exit with error.
3. **Check yt-dlp** — `where yt-dlp`. If missing, attempt `winget install yt-dlp`. If `winget` unavailable, print manual download URL. **This is a warning, not an error** — the tool works without yt-dlp but with reduced site coverage.
4. **Run `go mod tidy`** — install Go dependencies.
5. **Run `go build`** — compile `video-downloader.exe`.
6. **Create `downloads/`** directory.
7. **Print success message** with startup instructions.

## Out of Scope

- DRM bypassing or encrypted stream decryption
- Web UI dashboard (popup is the only UI)
- Remote/cloud deployment (localhost only)
- Concurrent download limits or queue management — downloads run concurrently without limits (each job is a goroutine). No queue, no max-concurrent setting. This is acceptable for a local tool.
- Automatic yt-dlp updates
