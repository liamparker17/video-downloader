@echo off
cd /d "C:\Users\liamp\OneDrive\Desktop\Portfolio\Video Downloader"

REM Refresh PATH so it picks up yt-dlp, deno, ffmpeg from winget
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
    echo Build complete.
    echo.
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
