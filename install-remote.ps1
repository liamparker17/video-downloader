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

# --- Final instructions ---

Write-Host "`n============================================" -ForegroundColor Cyan
Write-Host "  Installation complete!" -ForegroundColor Green
Write-Host "============================================" -ForegroundColor Cyan
Write-Host ""
Write-Host "  Two steps left:" -ForegroundColor White
Write-Host ""
Write-Host "  1. Double-click 'Video Downloader' on your desktop" -ForegroundColor White
Write-Host "     (or run: $installDir\video-downloader.exe)" -ForegroundColor Gray
Write-Host ""
Write-Host "  2. Load the Chrome extension:" -ForegroundColor White
Write-Host "     - Chrome is about to open to the extensions page" -ForegroundColor Gray
Write-Host "     - Enable 'Developer mode' (top-right toggle)" -ForegroundColor Gray
Write-Host "     - Click 'Load unpacked'" -ForegroundColor Gray
Write-Host "     - Select: $installDir\extension" -ForegroundColor Yellow
Write-Host ""

# Open Chrome extensions page — find Chrome executable and pass URL as argument
$chromePaths = @(
    "${env:ProgramFiles}\Google\Chrome\Application\chrome.exe",
    "${env:ProgramFiles(x86)}\Google\Chrome\Application\chrome.exe",
    "$env:LOCALAPPDATA\Google\Chrome\Application\chrome.exe"
)
$chromeExe = $chromePaths | Where-Object { Test-Path $_ } | Select-Object -First 1
if ($chromeExe) {
    Start-Process $chromeExe "chrome://extensions"
} else {
    Write-Host "  Could not find Chrome. Open chrome://extensions manually." -ForegroundColor Yellow
}

Write-Host "  Then right-click any video and click 'Download Video'!" -ForegroundColor Green
Write-Host ""
Read-Host "Press Enter to finish"
