// Content script: runs on every page to detect video elements.
// Responds to messages from the background script.

chrome.runtime.onMessage.addListener((message, _sender, sendResponse) => {
  if (message.action !== "getVideoInfo") return;

  const videoUrl = findVideoUrl(message.clickedSrc);

  sendResponse({
    url: videoUrl || null,
  });
});

// Find the best video URL on the page.
// Priority: clickedSrc (from context menu) > <video> src > <source> src > DOM scan
function findVideoUrl(clickedSrc) {
  // 1. If Chrome detected a video element and gave us srcUrl, use it
  if (clickedSrc && isVideoUrl(clickedSrc)) {
    return clickedSrc;
  }

  // 2. Look for HTML5 <video> elements with a src attribute
  const videos = document.querySelectorAll("video");
  for (const video of videos) {
    if (video.src && isVideoUrl(video.src)) {
      return video.src;
    }
    // Check currentSrc (set by the browser after source selection)
    if (video.currentSrc && isVideoUrl(video.currentSrc)) {
      return video.currentSrc;
    }
    // Check nested <source> elements
    const sources = video.querySelectorAll("source");
    for (const source of sources) {
      if (source.src && isVideoUrl(source.src)) {
        return source.src;
      }
    }
  }

  // 3. Fallback: scan all DOM elements for video URLs in src/href attributes
  const allElements = document.querySelectorAll("[src], [href]");
  for (const el of allElements) {
    const attr = el.src || el.href;
    if (attr && isVideoUrl(attr)) {
      return attr;
    }
  }

  return null;
}

// Check if a URL looks like a video resource.
function isVideoUrl(url) {
  if (!url || url.startsWith("blob:") || url.startsWith("data:")) return false;
  const lower = url.toLowerCase().split("?")[0];
  return (
    lower.endsWith(".mp4") ||
    lower.endsWith(".webm") ||
    lower.endsWith(".m3u8") ||
    lower.endsWith(".ts") ||
    lower.endsWith(".mkv") ||
    lower.endsWith(".avi") ||
    lower.endsWith(".mov")
  );
}
