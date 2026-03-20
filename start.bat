@echo off
cd /d "C:\Users\liamp\OneDrive\Desktop\Portfolio\Video Downloader"

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

echo Starting Video Downloader backend...
echo Leave this window open while downloading videos.
echo Press Ctrl+C to stop.
echo.
video-downloader.exe
pause
