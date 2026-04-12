# Video Downloader - Updater
# Run this in PowerShell:
#   irm https://raw.githubusercontent.com/liamparker17/video-downloader/master/update.ps1 | iex

$ErrorActionPreference = "Stop"
$installDir = "$env:USERPROFILE\video-downloader"
$repo = "liamparker17/video-downloader"
$binaryName = "video-downloader.exe"

Write-Host "`n============================================" -ForegroundColor Cyan
Write-Host "  Video Downloader - Updater" -ForegroundColor Cyan
Write-Host "============================================`n" -ForegroundColor Cyan

# --- Check install exists ---

if (-not (Test-Path $installDir)) {
    Write-Host "[ERROR] Video Downloader is not installed at $installDir" -ForegroundColor Red
    Write-Host "        Run the installer first:" -ForegroundColor Red
    Write-Host '        irm https://raw.githubusercontent.com/liamparker17/video-downloader/master/install-remote.ps1 | iex' -ForegroundColor Yellow
    Read-Host "Press Enter to exit"
    exit 1
}

# --- Stop running backend ---

$running = Get-Process -Name "video-downloader" -ErrorAction SilentlyContinue
if ($running) {
    Write-Host "[STOPPING] Shutting down running backend..." -ForegroundColor Yellow
    $running | Stop-Process -Force
    Start-Sleep -Seconds 2
    Write-Host "[OK] Backend stopped" -ForegroundColor Green
} else {
    Write-Host "[OK] Backend is not running" -ForegroundColor Green
}

# --- Get latest release info ---

Write-Host "[CHECKING] Fetching latest release from GitHub..." -ForegroundColor Cyan

try {
    $release = Invoke-RestMethod -Uri "https://api.github.com/repos/$repo/releases/latest" -Headers @{ "User-Agent" = "video-downloader-updater" }
    $latestTag = $release.tag_name
    $asset = $release.assets | Where-Object { $_.name -eq $binaryName } | Select-Object -First 1
} catch {
    Write-Host "[ERROR] Could not reach GitHub. Check your internet connection." -ForegroundColor Red
    Read-Host "Press Enter to exit"
    exit 1
}

if (-not $asset) {
    Write-Host "[ERROR] No binary found in release $latestTag" -ForegroundColor Red
    Read-Host "Press Enter to exit"
    exit 1
}

Write-Host "[OK] Latest release: $latestTag" -ForegroundColor Green

# --- Clean up old files ---

Write-Host "[CLEANING] Removing old build artifacts..." -ForegroundColor Yellow

$oldFiles = @(
    "$installDir\$binaryName",
    "$installDir\video-downloader.exe~",
    "$installDir\*.tmp",
    "$installDir\*.test"
)

foreach ($pattern in $oldFiles) {
    Get-Item $pattern -ErrorAction SilentlyContinue | ForEach-Object {
        Remove-Item $_.FullName -Force
        Write-Host "  Removed: $($_.Name)" -ForegroundColor Gray
    }
}

# --- Download new binary ---

$downloadUrl = $asset.browser_download_url
$outPath = "$installDir\$binaryName"

Write-Host "[DOWNLOADING] $binaryName ($([math]::Round($asset.size / 1MB, 1)) MB)..." -ForegroundColor Cyan

try {
    Invoke-WebRequest -Uri $downloadUrl -OutFile $outPath -UseBasicParsing
} catch {
    Write-Host "[ERROR] Download failed: $_" -ForegroundColor Red
    Read-Host "Press Enter to exit"
    exit 1
}

Write-Host "[OK] Downloaded $binaryName" -ForegroundColor Green

# --- Pull latest source (for extension updates) ---

Write-Host "[UPDATING] Pulling latest source for extension..." -ForegroundColor Cyan

Push-Location $installDir
if (Test-Path "$installDir\.git") {
    $env:GIT_REDIRECT_STDERR = '2>&1'
    git pull origin master 2>&1 | Out-Null
    Remove-Item Env:\GIT_REDIRECT_STDERR -ErrorAction SilentlyContinue
    Write-Host "[OK] Source updated" -ForegroundColor Green
} else {
    Write-Host "[SKIP] Not a git repo — extension not updated" -ForegroundColor Yellow
}
Pop-Location

# --- Done ---

Write-Host ""
Write-Host "============================================" -ForegroundColor Cyan
Write-Host "  UPDATE COMPLETE!  ($latestTag)" -ForegroundColor Green
Write-Host "============================================" -ForegroundColor Cyan
Write-Host ""
Write-Host "  What was updated:" -ForegroundColor White
Write-Host "  - Backend binary (video-downloader.exe)" -ForegroundColor White
Write-Host "  - Extension source files" -ForegroundColor White
Write-Host "  - Old build artifacts cleaned up" -ForegroundColor White
Write-Host ""
Write-Host "  Next steps:" -ForegroundColor White
Write-Host "  1. Double-click 'Video Downloader' on your Desktop" -ForegroundColor White
Write-Host "     to restart the backend" -ForegroundColor White
Write-Host "  2. Go to chrome://extensions and click the refresh" -ForegroundColor White
Write-Host "     icon on the Video Downloader extension" -ForegroundColor Yellow
Write-Host ""
Write-Host "============================================" -ForegroundColor Cyan
Write-Host ""

Read-Host "Press Enter to finish"
