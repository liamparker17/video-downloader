chrome.runtime.onMessage.addListener((message, _sender, sendResponse) => {
  if (message.action !== "getVideoInfo") return;
  const sources = findAllVideoUrls(message.clickedSrc);
  const bestUrl = sources.length > 0 ? sources[0] : null;
  sendResponse({ url: bestUrl, title: document.title || "", sources: sources });
});

function findAllVideoUrls(clickedSrc) {
  const found = [];
  const seen = new Set();
  function add(url) {
    if (url && !seen.has(url) && isVideoUrl(url)) { seen.add(url); found.push(url); }
  }
  add(clickedSrc);
  for (const video of document.querySelectorAll("video")) {
    add(video.src);
    add(video.currentSrc);
    for (const source of video.querySelectorAll("source")) { add(source.src); }
  }
  for (const el of document.querySelectorAll("[src], [href]")) { add(el.src || el.href); }
  return found;
}

function isVideoUrl(url) {
  if (!url || url.startsWith("blob:") || url.startsWith("data:")) return false;
  const lower = url.toLowerCase().split("?")[0];
  return (
    lower.endsWith(".mp4") || lower.endsWith(".webm") || lower.endsWith(".m3u8") ||
    lower.endsWith(".mpd") || lower.endsWith(".ts") || lower.endsWith(".mkv") ||
    lower.endsWith(".avi") || lower.endsWith(".mov")
  );
}
