const BACKEND = "http://localhost:8080";
const tabSources = new Map();
const VIDEO_EXTENSIONS = /\.(mp4|webm|m3u8|mpd|ts|mkv|avi|mov)(\?|$)/i;
const VIDEO_MIMES = /video\/|application\/x-mpegURL|application\/dash\+xml/i;

chrome.webRequest.onCompleted.addListener(
  (details) => {
    if (details.tabId < 0) return;
    const urlMatch = VIDEO_EXTENSIONS.test(details.url);
    const mimeMatch = details.responseHeaders && details.responseHeaders.some((h) => h.name.toLowerCase() === "content-type" && VIDEO_MIMES.test(h.value));
    if (!urlMatch && !mimeMatch) return;
    const lower = details.url.toLowerCase().split("?")[0];
    let vidType = "unknown";
    if (lower.endsWith(".m3u8")) vidType = "hls";
    else if (lower.endsWith(".mpd")) vidType = "dash";
    else if (lower.endsWith(".mp4") || lower.endsWith(".webm")) vidType = "mp4";
    if (lower.endsWith(".ts")) return;
    let contentType = "";
    let size = 0;
    if (details.responseHeaders) {
      for (const h of details.responseHeaders) {
        const name = h.name.toLowerCase();
        if (name === "content-type") contentType = h.value || "";
        if (name === "content-length") size = parseInt(h.value, 10) || 0;
      }
    }
    if (!tabSources.has(details.tabId)) tabSources.set(details.tabId, []);
    const sources = tabSources.get(details.tabId);
    if (!sources.some((s) => s.url === details.url)) {
      sources.push({ url: details.url, type: vidType, contentType, size, timestamp: Date.now() });
    }
  },
  { urls: ["<all_urls>"] },
  ["responseHeaders", "extraHeaders"]
);

chrome.tabs.onRemoved.addListener((tabId) => tabSources.delete(tabId));
chrome.tabs.onUpdated.addListener((tabId, changeInfo) => { if (changeInfo.url) tabSources.delete(tabId); });

chrome.runtime.onInstalled.addListener(() => {
  chrome.contextMenus.create({ id: "download-video", title: "Download Video", contexts: ["video", "page", "link"] });
  console.log("[Video Downloader] Extension installed.");
});

chrome.contextMenus.onClicked.addListener(async (info, tab) => {
  if (info.menuItemId !== "download-video") return;
  const clickedSrc = info.srcUrl || null;
  try {
    const response = await chrome.tabs.sendMessage(tab.id, { action: "getVideoInfo", clickedSrc });
    const videoUrl = response?.url || "";
    const title = response?.title || "";
    let bestUrl = videoUrl;
    if (!bestUrl) {
      const sources = tabSources.get(tab.id) || [];
      const hls = sources.find((s) => s.type === "hls");
      const dash = sources.find((s) => s.type === "dash");
      const direct = sources.sort((a, b) => (b.size || 0) - (a.size || 0))[0];
      bestUrl = hls?.url || dash?.url || direct?.url || "";
    }
    const cookies = await getCookiesForUrl(tab.url);
    const settings = await chrome.storage.local.get(["defaultQuality", "defaultAudioOnly"]);
    const payload = {
      url: bestUrl, pageUrl: tab.url, title: title, cookies: cookies,
      headers: { "User-Agent": navigator.userAgent, Referer: tab.url },
      quality: settings.defaultQuality || "best", audioOnly: settings.defaultAudioOnly || false,
    };
    console.log("[Video Downloader] Sending download request:", payload);
    const res = await fetch(`${BACKEND}/download`, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(payload) });
    const result = await res.json();
    if (res.ok) console.log(`[Video Downloader] Job created: ${result.jobId}`);
    else console.error(`[Video Downloader] Error: ${result.error}`);
  } catch (err) { console.error("[Video Downloader] Failed:", err.message); }
});

chrome.runtime.onMessage.addListener((message, sender, sendResponse) => {
  if (message.action === "getSources") {
    chrome.tabs.query({ active: true, currentWindow: true }, (tabs) => {
      const tabId = tabs[0]?.id;
      const sources = tabId ? tabSources.get(tabId) || [] : [];
      sendResponse({ sources });
    });
    return true;
  }
  if (message.action === "download") {
    fetch(`${BACKEND}/download`, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(message.payload) })
      .then((res) => res.json()).then((data) => sendResponse(data)).catch((err) => sendResponse({ error: err.message }));
    return true;
  }
});

async function getCookiesForUrl(url) {
  try { const cookies = await chrome.cookies.getAll({ url }); return cookies.map((c) => `${c.name}=${c.value}`).join("; "); }
  catch { return ""; }
}
