const BACKEND = "http://localhost:8080";
let selectedSource = null;
let pollInterval = null;
let statusInterval = null;

// ── Backend Status Indicator ──
async function checkBackendStatus() {
  const dot = document.getElementById("status-dot");
  try {
    const res = await fetch(`${BACKEND}/health`, { signal: AbortSignal.timeout(2000) });
    if (res.ok) {
      dot.className = "status-dot online";
      dot.title = "Backend running";
      return true;
    }
  } catch {}
  dot.className = "status-dot offline";
  dot.title = "Backend offline — double-click 'Video Downloader' on your desktop to start";
  return false;
}
checkBackendStatus();
statusInterval = setInterval(checkBackendStatus, 3000);

document.querySelectorAll("nav .tab").forEach((tab) => {
  tab.addEventListener("click", () => {
    document.querySelectorAll("nav .tab").forEach((t) => t.classList.remove("active"));
    document.querySelectorAll(".panel").forEach((p) => p.classList.remove("active"));
    tab.classList.add("active");
    document.getElementById(tab.dataset.tab).classList.add("active");
    if (tab.dataset.tab === "downloads") startPolling();
    else stopPolling();
  });
});

document.getElementById("settings-btn").addEventListener("click", () => {
  document.querySelectorAll(".panel").forEach((p) => p.classList.remove("active"));
  document.querySelectorAll("nav .tab").forEach((t) => t.classList.remove("active"));
  document.getElementById("settings").classList.add("active");
  loadSettings();
  loadHealth();
});

document.getElementById("settings-back").addEventListener("click", () => {
  document.getElementById("settings").classList.remove("active");
  document.querySelector('[data-tab="sources"]').click();
});

document.getElementById("default-quality").addEventListener("change", (e) => {
  chrome.storage.local.set({ defaultQuality: e.target.value });
});

document.getElementById("default-audio-only").addEventListener("change", (e) => {
  chrome.storage.local.set({ defaultAudioOnly: e.target.checked });
});

async function loadSettings() {
  const s = await chrome.storage.local.get(["defaultQuality", "defaultAudioOnly"]);
  document.getElementById("default-quality").value = s.defaultQuality || "best";
  document.getElementById("default-audio-only").checked = s.defaultAudioOnly || false;
}

async function loadHealth() {
  const el = document.getElementById("tool-status");
  try {
    const res = await fetch(`${BACKEND}/health`);
    const data = await res.json();
    el.innerHTML = `
      <p class="${data.ffmpeg.available ? "tool-ok" : "tool-missing"}">ffmpeg: ${data.ffmpeg.available ? data.ffmpeg.version : "NOT FOUND"}</p>
      <p class="${data.ytdlp.available ? "tool-ok" : "tool-missing"}">yt-dlp: ${data.ytdlp.available ? data.ytdlp.version : "NOT FOUND"}</p>
    `;
  } catch { el.innerHTML = '<p class="tool-missing">Backend not running</p>'; }
}

async function loadSources() {
  const listEl = document.getElementById("source-list");
  const allSources = [];
  try {
    const bg = await chrome.runtime.sendMessage({ action: "getSources" });
    if (bg?.sources) bg.sources.forEach((s) => allSources.push(s));
  } catch {}
  try {
    const [tab] = await chrome.tabs.query({ active: true, currentWindow: true });
    if (tab?.id) {
      const cs = await chrome.tabs.sendMessage(tab.id, { action: "getVideoInfo" });
      if (cs?.sources) {
        cs.sources.forEach((url) => {
          if (!allSources.some((s) => s.url === url)) {
            const lower = url.toLowerCase().split("?")[0];
            let type = "unknown";
            if (lower.endsWith(".m3u8")) type = "hls";
            else if (lower.endsWith(".mpd")) type = "dash";
            else if (lower.endsWith(".mp4") || lower.endsWith(".webm")) type = "mp4";
            allSources.push({ url, type, size: 0, contentType: "" });
          }
        });
      }
    }
  } catch {}
  try {
    const [tab] = await chrome.tabs.query({ active: true, currentWindow: true });
    if (tab?.url && !tab.url.startsWith("chrome://")) {
      allSources.push({ url: tab.url, type: "ytdlp", size: 0, label: "Page URL (yt-dlp)" });
    }
  } catch {}
  if (allSources.length === 0) { listEl.innerHTML = '<p class="muted">No videos detected on this page.</p>'; return; }
  listEl.innerHTML = "";
  allSources.forEach((source, i) => {
    const div = document.createElement("div");
    div.className = "source-item";
    const urlDisplay = source.label || truncateUrl(source.url);
    const sizeStr = source.size ? formatSize(source.size) : "";
    div.innerHTML = `
      <input type="radio" name="source" value="${i}">
      <span class="badge ${source.type}">${source.type}</span>
      <div class="source-info">
        <div class="source-url" title="${source.url}">${urlDisplay}</div>
        <div class="source-meta">${sizeStr}</div>
      </div>
    `;
    div.addEventListener("click", () => {
      div.querySelector("input").checked = true;
      document.querySelectorAll(".source-item").forEach((s) => s.classList.remove("selected"));
      div.classList.add("selected");
      selectedSource = source;
      document.getElementById("download-btn").disabled = false;
    });
    listEl.appendChild(div);
  });
  if (allSources.length > 0) listEl.querySelector(".source-item")?.click();
}

document.getElementById("download-btn").addEventListener("click", async () => {
  if (!selectedSource) return;
  const quality = document.getElementById("quality").value;
  const audioOnly = document.getElementById("audio-only").checked;
  const [tab] = await chrome.tabs.query({ active: true, currentWindow: true });
  const cookies = await getCookies(tab.url);
  const isYtdlp = selectedSource.type === "ytdlp";
  const payload = {
    url: isYtdlp ? "" : selectedSource.url, pageUrl: tab.url, title: tab.title || "",
    cookies: cookies, headers: { "User-Agent": navigator.userAgent, Referer: tab.url },
    quality, audioOnly,
  };
  try {
    const response = await chrome.runtime.sendMessage({ action: "download", payload });
    if (response?.jobId) document.querySelector('[data-tab="downloads"]').click();
    else if (response?.error) alert("Download failed: " + response.error);
  } catch (err) { alert("Backend not running. Start video-downloader.exe"); }
});

function startPolling() { loadJobs(); pollInterval = setInterval(loadJobs, 2000); }
function stopPolling() { if (pollInterval) { clearInterval(pollInterval); pollInterval = null; } }

async function loadJobs() {
  const listEl = document.getElementById("job-list");
  try {
    const res = await fetch(`${BACKEND}/jobs`);
    const jobs = await res.json();
    if (!jobs || jobs.length === 0) { listEl.innerHTML = '<p class="muted">No downloads yet.</p>'; return; }
    jobs.sort((a, b) => {
      const order = { downloading: 0, processing: 1, pending: 2, failed: 3, completed: 4 };
      const diff = (order[a.status] ?? 5) - (order[b.status] ?? 5);
      if (diff !== 0) return diff;
      return new Date(b.createdAt) - new Date(a.createdAt);
    });
    listEl.innerHTML = "";
    jobs.forEach((job) => {
      const div = document.createElement("div");
      div.className = "job-item";
      let statusIcon = "";
      if (job.status === "completed") statusIcon = "&#10003;";
      if (job.status === "failed") statusIcon = "&#10007;";
      let progressHtml = "";
      if (job.status === "downloading" || job.status === "processing") {
        progressHtml = `<div class="progress-bar"><div class="progress-fill" style="width: ${job.progress.toFixed(1)}%"></div></div>
          <div class="job-meta">${job.progress.toFixed(1)}% &middot; ${job.speed || "..."} &middot; ${job.status}</div>`;
      }
      let errorHtml = "";
      if (job.status === "failed" && job.error) {
        errorHtml = `<div class="job-error">${job.error} <button class="retry-btn" data-job-url="${job.url || ""}" data-job-page="${job.pageUrl || ""}">Retry</button></div>`;
      }
      div.innerHTML = `<div class="job-header"><span class="job-filename">${job.filename || job.id}</span><span class="job-status ${job.status}">${statusIcon} ${job.status}</span></div>${progressHtml}${errorHtml}`;
      const retryBtn = div.querySelector(".retry-btn");
      if (retryBtn) {
        retryBtn.addEventListener("click", async () => {
          const payload = { url: retryBtn.dataset.jobUrl, pageUrl: retryBtn.dataset.jobPage, title: job.title || "", cookies: "", headers: { Referer: retryBtn.dataset.jobPage }, quality: job.quality || "best", audioOnly: job.audioOnly || false };
          await chrome.runtime.sendMessage({ action: "download", payload });
        });
      }
      listEl.appendChild(div);
    });
  } catch { listEl.innerHTML = '<div class="error-banner">Backend not running. Start video-downloader.exe</div>'; }
}

async function getCookies(url) {
  try { const cookies = await chrome.cookies.getAll({ url }); return cookies.map((c) => `${c.name}=${c.value}`).join("; "); }
  catch { return ""; }
}

function truncateUrl(url) {
  try { const u = new URL(url); const p = u.pathname.split("/").pop() || u.hostname; return p.length > 40 ? p.substring(0, 40) + "..." : p; }
  catch { return url.substring(0, 40) + "..."; }
}

function formatSize(bytes) {
  if (bytes >= 1024 * 1024) return (bytes / (1024 * 1024)).toFixed(1) + " MB";
  if (bytes >= 1024) return (bytes / 1024).toFixed(1) + " KB";
  return bytes + " B";
}

loadSources();
chrome.storage.local.get(["defaultQuality", "defaultAudioOnly"], (s) => {
  if (s.defaultQuality) document.getElementById("quality").value = s.defaultQuality;
  if (s.defaultAudioOnly) document.getElementById("audio-only").checked = true;
});
