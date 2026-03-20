@echo off
echo ============================================
echo  Video Downloader - Dependency Installer
echo ============================================
echo.

REM Check Go is installed
where go >nul 2>&1
if %ERRORLEVEL% neq 0 (
    echo [ERROR] Go is not installed or not on PATH.
    echo         Download from: https://go.dev/dl/
    echo.
    pause
    exit /b 1
)
echo [OK] Go found:
go version
echo.

REM Check ffmpeg is installed
where ffmpeg >nul 2>&1
if %ERRORLEVEL% neq 0 (
    echo [ERROR] ffmpeg is not installed or not on PATH.
    echo         Download from: https://ffmpeg.org/download.html
    echo         Or install via: winget install Gyan.FFmpeg
    echo.
    pause
    exit /b 1
)
echo [OK] ffmpeg found:
ffmpeg -version 2>&1 | findstr /R "^ffmpeg"
echo.

REM Check yt-dlp (optional but recommended)
where yt-dlp >nul 2>&1
if %ERRORLEVEL% neq 0 (
    echo [WARN] yt-dlp not found. Attempting install via winget...
    winget install yt-dlp 2>nul
    where yt-dlp >nul 2>&1
    if %ERRORLEVEL% neq 0 (
        echo [WARN] yt-dlp could not be installed automatically.
        echo         Download from: https://github.com/yt-dlp/yt-dlp/releases
        echo         Without yt-dlp, YouTube and social media downloads will not work.
        echo.
    ) else (
        echo [OK] yt-dlp installed successfully.
    )
) else (
    echo [OK] yt-dlp found:
    yt-dlp --version
)
echo.

REM Download Go module dependencies
echo [INSTALLING] Go module dependencies...
go mod tidy
if %ERRORLEVEL% neq 0 (
    echo [ERROR] Failed to install Go dependencies.
    pause
    exit /b 1
)
echo [OK] Go dependencies installed.
echo.

REM Build the binary
echo [BUILDING] Compiling Go backend...
go build -o video-downloader.exe .
if %ERRORLEVEL% neq 0 (
    echo [ERROR] Build failed.
    pause
    exit /b 1
)
echo [OK] Built video-downloader.exe
echo.

REM Create downloads directory
if not exist downloads mkdir downloads
echo [OK] downloads/ directory ready.
echo.

echo ============================================
echo  Installation complete!
echo.
echo  To start the backend:
echo    video-downloader.exe
echo.
echo  Then load the extension in Chrome:
echo    1. Go to chrome://extensions
echo    2. Enable Developer mode
echo    3. Click "Load unpacked"
echo    4. Select the "extension" folder
echo ============================================
pause
