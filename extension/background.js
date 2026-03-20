// Create the right-click context menu item when the extension is installed.
chrome.runtime.onInstalled.addListener(() => {
  chrome.contextMenus.create({
    id: "download-video",
    title: "Download Video",
    // Show on all contexts — content script will find the actual video
    contexts: ["video", "page", "link"],
  });
  console.log("[Video Downloader] Context menu registered.");
});

// Handle the context menu click
chrome.contextMenus.onClicked.addListener(async (info, tab) => {
  if (info.menuItemId !== "download-video") return;

  console.log("[Video Downloader] Menu clicked. Asking content script for video info...");

  // If right-clicked directly on a <video> element, Chrome gives us srcUrl
  const clickedSrc = info.srcUrl || null;

  try {
    // Ask the content script to find video sources and page metadata
    const response = await chrome.tabs.sendMessage(tab.id, {
      action: "getVideoInfo",
      clickedSrc,
    });

    if (!response || !response.url) {
      console.error("[Video Downloader] No video URL found on the page.");
      return;
    }

    console.log(`[Video Downloader] Found video: ${response.url}`);

    // Get cookies for the current tab's URL to forward with the download
    const cookies = await getCookiesForUrl(tab.url);

    const payload = {
      url: response.url,
      cookies: cookies,
      headers: {
        "User-Agent": navigator.userAgent,
        Referer: tab.url,
      },
    };

    console.log("[Video Downloader] Sending download request to Go backend...");

    const res = await fetch("http://localhost:8080/download", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
    });

    const result = await res.json();

    if (res.ok) {
      console.log(`[Video Downloader] Success! Saved to: ${result.file}`);
    } else {
      console.error(`[Video Downloader] Backend error: ${result.error}`);
    }
  } catch (err) {
    console.error("[Video Downloader] Failed:", err.message);
  }
});

// Retrieve cookies for a given URL and format them as a header string.
async function getCookiesForUrl(url) {
  try {
    const cookies = await chrome.cookies.getAll({ url });
    return cookies.map((c) => `${c.name}=${c.value}`).join("; ");
  } catch {
    return "";
  }
}
