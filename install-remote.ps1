# Video Downloader - One-Click Installer
# Run this in PowerShell:
#   irm https://raw.githubusercontent.com/liamparker17/video-downloader/master/install-remote.ps1 | iex

$ErrorActionPreference = "Stop"
$installDir = "$env:USERPROFILE\video-downloader"

Write-Host "`n============================================" -ForegroundColor Cyan
Write-Host "  Video Downloader - Installer" -ForegroundColor Cyan
Write-Host "============================================`n" -ForegroundColor Cyan

# --- Check prerequisites ---

# ffmpeg
if (-not (Get-Command ffmpeg -ErrorAction SilentlyContinue)) {
    Write-Host "[INSTALLING] ffmpeg via winget..." -ForegroundColor Yellow
    winget install Gyan.FFmpeg --accept-source-agreements --accept-package-agreements 2>$null
    $env:PATH = [System.Environment]::GetEnvironmentVariable("PATH", "Machine") + ";" + [System.Environment]::GetEnvironmentVariable("PATH", "User")
    if (-not (Get-Command ffmpeg -ErrorAction SilentlyContinue)) {
        Write-Host "[ERROR] ffmpeg install failed. Install manually: winget install Gyan.FFmpeg" -ForegroundColor Red
        Read-Host "Press Enter to exit"
        exit 1
    }
}
Write-Host "[OK] ffmpeg found" -ForegroundColor Green

# yt-dlp (optional but recommended)
if (-not (Get-Command yt-dlp -ErrorAction SilentlyContinue)) {
    Write-Host "[INSTALLING] yt-dlp via winget..." -ForegroundColor Yellow
    winget install yt-dlp.yt-dlp --accept-source-agreements --accept-package-agreements 2>$null
    $env:PATH = [System.Environment]::GetEnvironmentVariable("PATH", "Machine") + ";" + [System.Environment]::GetEnvironmentVariable("PATH", "User")
    if (-not (Get-Command yt-dlp -ErrorAction SilentlyContinue)) {
        Write-Host "[WARN] yt-dlp not installed. YouTube downloads won't work." -ForegroundColor Yellow
        Write-Host "       Install later: winget install yt-dlp.yt-dlp`n" -ForegroundColor Yellow
    } else {
        Write-Host "[OK] yt-dlp installed" -ForegroundColor Green
    }
} else {
    Write-Host "[OK] yt-dlp found" -ForegroundColor Green
}

# --- Download pre-built binary from GitHub Releases ---

$repo = "liamparker17/video-downloader"

if (-not (Test-Path $installDir)) {
    New-Item -ItemType Directory -Path $installDir | Out-Null
}

Write-Host "`n[DOWNLOADING] Fetching latest release..." -ForegroundColor Cyan

try {
    $release = Invoke-RestMethod -Uri "https://api.github.com/repos/$repo/releases/latest" -Headers @{ "User-Agent" = "video-downloader-installer" }
    $asset = $release.assets | Where-Object { $_.name -eq "video-downloader.exe" } | Select-Object -First 1
} catch {
    Write-Host "[ERROR] Could not reach GitHub. Check your internet connection." -ForegroundColor Red
    Read-Host "Press Enter to exit"
    exit 1
}

if (-not $asset) {
    Write-Host "[ERROR] No binary found in release $($release.tag_name)" -ForegroundColor Red
    Read-Host "Press Enter to exit"
    exit 1
}

$exePath = "$installDir\video-downloader.exe"

Write-Host "[DOWNLOADING] video-downloader.exe ($($release.tag_name), $([math]::Round($asset.size / 1MB, 1)) MB)..." -ForegroundColor Cyan
Invoke-WebRequest -Uri $asset.browser_download_url -OutFile $exePath -UseBasicParsing
Write-Host "[OK] Downloaded video-downloader.exe" -ForegroundColor Green

# --- Download extension files from GitHub ---

Write-Host "[DOWNLOADING] Extension files..." -ForegroundColor Cyan

$extensionDir = "$installDir\extension"
if (-not (Test-Path $extensionDir)) {
    New-Item -ItemType Directory -Path $extensionDir | Out-Null
}

$extensionFiles = @("manifest.json", "popup.html", "popup.js", "background.js", "content.js", "popup.css", "icon16.png", "icon48.png", "icon128.png")
$baseRawUrl = "https://raw.githubusercontent.com/$repo/master/extension"

foreach ($file in $extensionFiles) {
    try {
        Invoke-WebRequest -Uri "$baseRawUrl/$file" -OutFile "$extensionDir\$file" -UseBasicParsing 2>$null
    } catch {
        Write-Host "  [WARN] Could not download $file" -ForegroundColor Yellow
    }
}
Write-Host "[OK] Extension files downloaded" -ForegroundColor Green

# --- Create desktop shortcut ---

$desktop = [Environment]::GetFolderPath("Desktop")
$shortcutPath = "$desktop\Video Downloader.lnk"
$shell = New-Object -ComObject WScript.Shell
$shortcut = $shell.CreateShortcut($shortcutPath)
$shortcut.TargetPath = "$installDir\video-downloader.exe"
$shortcut.WorkingDirectory = $installDir
$shortcut.Description = "Start Video Downloader backend"
$shortcut.Save()
Write-Host "[OK] Desktop shortcut created" -ForegroundColor Green

# --- Auto-start on login ---

$startupPath = [Environment]::GetFolderPath("Startup")
$startupShortcut = $shell.CreateShortcut("$startupPath\Video Downloader.lnk")
$startupShortcut.TargetPath = "$installDir\video-downloader.exe"
$startupShortcut.WorkingDirectory = $installDir
$startupShortcut.WindowStyle = 7
$startupShortcut.Description = "Auto-start Video Downloader backend"
$startupShortcut.Save()
Write-Host "[OK] Auto-start on login enabled" -ForegroundColor Green

# --- Copy extension path to clipboard ---

$extensionPath = "$installDir\extension"
Set-Clipboard -Value $extensionPath
Write-Host "[OK] Extension folder path copied to clipboard" -ForegroundColor Green

# --- Detect browser and open extensions page ---

$chromePaths = @(
    "${env:ProgramFiles}\Google\Chrome\Application\chrome.exe",
    "${env:ProgramFiles(x86)}\Google\Chrome\Application\chrome.exe",
    "$env:LOCALAPPDATA\Google\Chrome\Application\chrome.exe"
)
$bravePaths = @(
    "${env:ProgramFiles}\BraveSoftware\Brave-Browser\Application\brave.exe",
    "${env:ProgramFiles(x86)}\BraveSoftware\Brave-Browser\Application\brave.exe",
    "$env:LOCALAPPDATA\BraveSoftware\Brave-Browser\Application\brave.exe"
)

$browserExe = $null
$browserName = $null

$chromeExe = $chromePaths | Where-Object { Test-Path $_ } | Select-Object -First 1
$braveExe = $bravePaths | Where-Object { Test-Path $_ } | Select-Object -First 1

if ($chromeExe) {
    $browserExe = $chromeExe
    $browserName = "Chrome"
} elseif ($braveExe) {
    $browserExe = $braveExe
    $browserName = "Brave"
}

# --- Final instructions ---

Write-Host ""
Write-Host "============================================" -ForegroundColor Cyan
Write-Host "  INSTALLATION COMPLETE!" -ForegroundColor Green
Write-Host "============================================" -ForegroundColor Cyan
Write-Host ""
Write-Host "  STEP 1: Start the downloader" -ForegroundColor White
Write-Host "  -------" -ForegroundColor Gray
Write-Host "  Double-click the 'Video Downloader' shortcut" -ForegroundColor White
Write-Host "  on your Desktop. A black window will open." -ForegroundColor White
Write-Host "  LEAVE IT OPEN while you download videos." -ForegroundColor Yellow
Write-Host "  (It will also auto-start when you log in)" -ForegroundColor Gray
Write-Host ""
Write-Host "  STEP 2: Add the extension to your browser" -ForegroundColor White
Write-Host "  -------" -ForegroundColor Gray

if ($browserExe) {
    Write-Host "  I'm opening $browserName's extensions page now..." -ForegroundColor Cyan
    Write-Host ""
    Write-Host "  When the page opens, do this:" -ForegroundColor White
} else {
    Write-Host "  Open Chrome or Brave and go to:" -ForegroundColor White
    Write-Host "  chrome://extensions" -ForegroundColor Yellow
    Write-Host ""
    Write-Host "  Then do this:" -ForegroundColor White
}

Write-Host ""
Write-Host "    a) Look at the TOP-RIGHT corner of the page" -ForegroundColor White
Write-Host "       You'll see a toggle switch called" -ForegroundColor White
Write-Host "       'Developer mode' — TURN IT ON" -ForegroundColor Yellow
Write-Host ""
Write-Host "    b) New buttons will appear at the top." -ForegroundColor White
Write-Host "       Click the button that says" -ForegroundColor White
Write-Host "       'Load unpacked'" -ForegroundColor Yellow
Write-Host ""
Write-Host "    c) A folder picker will open." -ForegroundColor White
Write-Host "       The path is already copied to your clipboard!" -ForegroundColor Green
Write-Host "       Just click the address bar at the top," -ForegroundColor White
Write-Host "       press Ctrl+V to paste, then press Enter." -ForegroundColor Yellow
Write-Host ""
Write-Host "       Path: $extensionPath" -ForegroundColor Gray
Write-Host ""
Write-Host "    d) Click 'Select Folder'" -ForegroundColor Yellow
Write-Host ""
Write-Host "  That's it! The extension is now installed." -ForegroundColor Green
Write-Host ""
Write-Host "  STEP 3: Download a video" -ForegroundColor White
Write-Host "  -------" -ForegroundColor Gray
Write-Host "  Go to any website with a video (YouTube, etc.)" -ForegroundColor White
Write-Host "  RIGHT-CLICK anywhere on the page and click" -ForegroundColor White
Write-Host "  'Download Video'" -ForegroundColor Yellow
Write-Host ""
Write-Host "  Videos are saved to your Downloads folder:" -ForegroundColor White
Write-Host "  $env:USERPROFILE\Downloads" -ForegroundColor Cyan
Write-Host ""
Write-Host "  Videos are auto-converted to play in any player" -ForegroundColor Gray
Write-Host "  including Windows Media Player — no VLC needed!" -ForegroundColor Gray
Write-Host ""
Write-Host "============================================" -ForegroundColor Cyan
Write-Host ""

if ($browserExe) {
    Start-Process $browserExe "chrome://extensions"
}

Read-Host "Press Enter to finish"
