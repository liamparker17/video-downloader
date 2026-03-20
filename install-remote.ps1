# Video Downloader - One-Click Installer
# Run this in PowerShell:
#   irm https://raw.githubusercontent.com/liamparker17/video-downloader/master/install-remote.ps1 | iex

$ErrorActionPreference = "Stop"
$installDir = "$env:USERPROFILE\video-downloader"

Write-Host "`n============================================" -ForegroundColor Cyan
Write-Host "  Video Downloader - Installer" -ForegroundColor Cyan
Write-Host "============================================`n" -ForegroundColor Cyan

# --- Check prerequisites ---

# Go
if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    Write-Host "[ERROR] Go is not installed." -ForegroundColor Red
    Write-Host "        Install it: winget install GoLang.Go" -ForegroundColor Yellow
    Write-Host "        Or download: https://go.dev/dl/`n" -ForegroundColor Yellow
    Read-Host "Press Enter to exit"
    exit 1
}
Write-Host "[OK] Go: $(go version)" -ForegroundColor Green

# ffmpeg
if (-not (Get-Command ffmpeg -ErrorAction SilentlyContinue)) {
    Write-Host "[INSTALLING] ffmpeg via winget..." -ForegroundColor Yellow
    winget install Gyan.FFmpeg --accept-source-agreements --accept-package-agreements 2>$null
    # Refresh PATH
    $env:PATH = [System.Environment]::GetEnvironmentVariable("PATH", "Machine") + ";" + [System.Environment]::GetEnvironmentVariable("PATH", "User")
    if (-not (Get-Command ffmpeg -ErrorAction SilentlyContinue)) {
        Write-Host "[ERROR] ffmpeg install failed. Install manually: winget install Gyan.FFmpeg" -ForegroundColor Red
        Read-Host "Press Enter to exit"
        exit 1
    }
}
Write-Host "[OK] ffmpeg found" -ForegroundColor Green

# yt-dlp (optional)
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

# Git
if (-not (Get-Command git -ErrorAction SilentlyContinue)) {
    Write-Host "[ERROR] Git is not installed." -ForegroundColor Red
    Write-Host "        Install it: winget install Git.Git" -ForegroundColor Yellow
    Read-Host "Press Enter to exit"
    exit 1
}

# --- Clone and build ---

Write-Host "`n[CLONING] video-downloader..." -ForegroundColor Cyan
if (Test-Path $installDir) {
    Write-Host "[INFO] Directory exists, pulling latest..." -ForegroundColor Yellow
    Push-Location $installDir
    $env:GIT_REDIRECT_STDERR = '2>&1'
    git pull origin master | Out-Null
    Remove-Item Env:\GIT_REDIRECT_STDERR -ErrorAction SilentlyContinue
    Pop-Location
} else {
    $env:GIT_REDIRECT_STDERR = '2>&1'
    git clone https://github.com/liamparker17/video-downloader.git $installDir | Out-Null
    Remove-Item Env:\GIT_REDIRECT_STDERR -ErrorAction SilentlyContinue
}

Push-Location $installDir

Write-Host "[BUILDING] Compiling Go backend..." -ForegroundColor Cyan
go build -o video-downloader.exe .
if ($LASTEXITCODE -ne 0) {
    Write-Host "[ERROR] Build failed." -ForegroundColor Red
    Pop-Location
    Read-Host "Press Enter to exit"
    exit 1
}
Write-Host "[OK] Built video-downloader.exe" -ForegroundColor Green

# Create downloads dir
New-Item -ItemType Directory -Path "downloads" -Force | Out-Null

# --- Create start script ---

$startScript = @"
@echo off
cd /d "$installDir"
echo Starting Video Downloader backend...
echo Press Ctrl+C to stop.
video-downloader.exe
"@
Set-Content -Path "$installDir\start.bat" -Value $startScript

# --- Create desktop shortcut ---

$desktop = [Environment]::GetFolderPath("Desktop")
$shortcutPath = "$desktop\Video Downloader.lnk"
$shell = New-Object -ComObject WScript.Shell
$shortcut = $shell.CreateShortcut($shortcutPath)
$shortcut.TargetPath = "$installDir\start.bat"
$shortcut.WorkingDirectory = $installDir
$shortcut.Description = "Start Video Downloader backend"
$shortcut.Save()
Write-Host "[OK] Desktop shortcut created" -ForegroundColor Green

Pop-Location

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

# Try Chrome first, then Brave
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
Write-Host "  Your video will be saved to:" -ForegroundColor White
Write-Host "  $installDir\downloads" -ForegroundColor Cyan
Write-Host ""
Write-Host "============================================" -ForegroundColor Cyan
Write-Host ""

if ($browserExe) {
    Start-Process $browserExe "chrome://extensions"
}

Read-Host "Press Enter to finish"
