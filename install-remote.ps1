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
    Write-Host "[INSTALLING] Go via winget..." -ForegroundColor Yellow
    winget install GoLang.Go --accept-source-agreements --accept-package-agreements 2>$null
    $env:PATH = [System.Environment]::GetEnvironmentVariable("PATH", "Machine") + ";" + [System.Environment]::GetEnvironmentVariable("PATH", "User")
    if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
        Write-Host "[ERROR] Go install failed. Install manually: winget install GoLang.Go" -ForegroundColor Red
        Read-Host "Press Enter to exit"
        exit 1
    }
}
Write-Host "[OK] Go: $(go version)" -ForegroundColor Green

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

# deno (needed by yt-dlp for YouTube)
if (-not (Get-Command deno -ErrorAction SilentlyContinue)) {
    Write-Host "[INSTALLING] deno (needed by yt-dlp for YouTube)..." -ForegroundColor Yellow
    winget install DenoLand.Deno --accept-source-agreements --accept-package-agreements 2>$null
    $env:PATH = [System.Environment]::GetEnvironmentVariable("PATH", "Machine") + ";" + [System.Environment]::GetEnvironmentVariable("PATH", "User")
    if (-not (Get-Command deno -ErrorAction SilentlyContinue)) {
        Write-Host "[WARN] deno not installed. Some YouTube formats may not work." -ForegroundColor Yellow
    } else {
        Write-Host "[OK] deno installed" -ForegroundColor Green
    }
} else {
    Write-Host "[OK] deno found" -ForegroundColor Green
}

# Git
if (-not (Get-Command git -ErrorAction SilentlyContinue)) {
    Write-Host "[INSTALLING] Git via winget..." -ForegroundColor Yellow
    winget install Git.Git --accept-source-agreements --accept-package-agreements 2>$null
    $env:PATH = [System.Environment]::GetEnvironmentVariable("PATH", "Machine") + ";" + [System.Environment]::GetEnvironmentVariable("PATH", "User")
    if (-not (Get-Command git -ErrorAction SilentlyContinue)) {
        Write-Host "[ERROR] Git install failed. Install manually: winget install Git.Git" -ForegroundColor Red
        Read-Host "Press Enter to exit"
        exit 1
    }
}
Write-Host "[OK] Git found" -ForegroundColor Green

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

# --- Create start script with PATH refresh ---

$startScript = @"
@echo off
cd /d "$installDir"

REM Refresh PATH so yt-dlp, deno, ffmpeg are found
for /f "tokens=2*" %%A in ('reg query "HKCU\Environment" /v Path 2^>nul') do set "USER_PATH=%%B"
for /f "tokens=2*" %%A in ('reg query "HKLM\SYSTEM\CurrentControlSet\Control\Session Manager\Environment" /v Path 2^>nul') do set "SYS_PATH=%%B"
set "PATH=%SYS_PATH%;%USER_PATH%"

if not exist video-downloader.exe (
    echo First run — building video-downloader.exe...
    go build -o video-downloader.exe .
    if %ERRORLEVEL% neq 0 (
        echo Build failed. Make sure Go is installed.
        pause
        exit /b 1
    )
)

echo ============================================
echo  Video Downloader Backend
echo ============================================
echo.
echo  Leave this window open while downloading.
echo  Press Ctrl+C to stop.
echo.
video-downloader.exe
pause
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

# --- Auto-start on login ---

$startupPath = [Environment]::GetFolderPath("Startup")
$startupShortcut = $shell.CreateShortcut("$startupPath\Video Downloader.lnk")
$startupShortcut.TargetPath = "$installDir\start.bat"
$startupShortcut.WorkingDirectory = $installDir
$startupShortcut.WindowStyle = 7
$startupShortcut.Description = "Auto-start Video Downloader backend"
$startupShortcut.Save()
Write-Host "[OK] Auto-start on login enabled" -ForegroundColor Green

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
Write-Host "  TIP: Open downloaded videos with VLC for best" -ForegroundColor Gray
Write-Host "  compatibility (some players can't handle all formats)" -ForegroundColor Gray
Write-Host ""
Write-Host "============================================" -ForegroundColor Cyan
Write-Host ""

if ($browserExe) {
    Start-Process $browserExe "chrome://extensions"
}

Read-Host "Press Enter to finish"
