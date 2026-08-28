;(function () {
  /* __DSH_UPDATE_BANNER v1 — DSH 新版提醒横幅（仿 Reasonix 更新提醒）。
     启动后调 A.DshUpdateCheck()，available=true 时显示可关闭横幅，
     点击打开更新渠道（GitHub Releases）。无副作用、失败静默。 */
  if (window.__DSH_UPDATE_BANNER__) return;
  window.__DSH_UPDATE_BANNER__ = true;

  var SHOWN_KEY = "dsh-update-banner-dismissed-v1";

  function openExternal(url) {
    try {
      var rt = window.runtime;
      if (rt && rt.OpenExternal) { rt.OpenExternal(url); return; }
    } catch (e) {}
    try { window.open(url, "_blank"); } catch (e) {}
  }

  function showBanner(info) {
    var old = document.getElementById("dsh-update-banner");
    if (old) old.remove();
    var wrap = document.createElement("div");
    wrap.id = "dsh-update-banner";
    wrap.setAttribute("role", "status");
    wrap.style.cssText = [
      "position:fixed", "right:16px", "bottom:16px", "z-index:2147483000",
      "display:flex", "align-items:center", "gap:10px",
      "padding:10px 14px", "border-radius:10px",
      "background:#1b1e24", "border:1px solid #333a45",
      "box-shadow:0 6px 24px rgba(0,0,0,.45)",
      "font:13px/1.4 -apple-system,'Segoe UI',system-ui,sans-serif",
      "color:#e6e8eb", "max-width:460px"
    ].join(";");
    var text = document.createElement("span");
    text.style.cssText = "flex:1;min-width:0";
    text.textContent = "DSH 有新版可用：" + info.current + " → " + info.latest;
    var dl = document.createElement("button");
    dl.type = "button";
    dl.textContent = "下载更新";
    dl.style.cssText = [
      "border:0", "border-radius:6px", "padding:5px 12px", "cursor:pointer",
      "background:#0153e5", "color:#fff", "font:600 12px/1 -apple-system,'Segoe UI',system-ui,sans-serif"
    ].join(";");
    dl.onclick = function () {
      try { localStorage.setItem(SHOWN_KEY, String(Date.now())); } catch (e) {}
      openExternal(info.downloadUrl || info.releaseUrl || info.updateUrl ||
        "https://github.com/sdkwork-ai/deepseek-harness-desktop/releases");
      wrap.remove();
    };
    var detail = document.createElement("button");
    detail.type = "button";
    detail.textContent = "详情";
    detail.style.cssText = [
      "border:1px solid #4a5260", "border-radius:6px", "padding:5px 10px", "cursor:pointer",
      "background:none", "color:#c9d0d9", "font:600 12px/1 -apple-system,'Segoe UI',system-ui,sans-serif"
    ].join(";");
    detail.onclick = function () {
      try { localStorage.setItem(SHOWN_KEY, String(Date.now())); } catch (e) {}
      openExternal(info.releaseUrl || info.updateUrl ||
        "https://github.com/sdkwork-ai/deepseek-harness-desktop/releases");
      wrap.remove();
    };
    var close = document.createElement("button");
    close.type = "button";
    close.textContent = "×";
    close.setAttribute("aria-label", "关闭");
    close.style.cssText = "border:0;background:none;color:#9aa3af;font-size:16px;cursor:pointer;padding:0 2px";
    close.onclick = function () {
      try { localStorage.setItem(SHOWN_KEY, String(Date.now())); } catch (e) {}
      wrap.remove();
    };
    wrap.appendChild(text);
    wrap.appendChild(dl);
    wrap.appendChild(detail);
    wrap.appendChild(close);
    (document.body || document.documentElement).appendChild(wrap);
  }

  function maybeCheck() {
    try {
      var A = window.go && window.go.main && window.go.main.App;
      if (!A || !A.DshUpdateCheck) return;
      A.DshUpdateCheck().then(function (info) {
        if (!info || !info.available) return;
        try { if (localStorage.getItem(SHOWN_KEY)) return; } catch (e) {}
        setTimeout(function () { showBanner(info); }, 800);
      }).catch(function () {});
    } catch (e) {}
  }

  function whenReady(fn) {
    if (document.readyState === "complete" || document.readyState === "interactive") {
      setTimeout(fn, 1);
    } else {
      window.addEventListener("DOMContentLoaded", fn);
    }
  }
  whenReady(function () { setTimeout(maybeCheck, 6000); });
})();
