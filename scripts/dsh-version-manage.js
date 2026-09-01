/* __DSH_VERSION_MANAGE v2 (DSH 核心版本管理 - 挂到 设置-更新页, 本地注入) */
// dsh-version-manage.js — 在设置面板「更新」页（updates-control 区块后）追加
// 「DSH 核心版本」管理区：当前版本 / 最新版本 / npm 版本列表选择 /
// GitHub Releases（含 win-x64 安装包直链下载）。
// 独立普通脚本（非 module）；通过 window.go.main.App 桥调用 Go。
// 样式自带（Reasonix 是 CSS-in-JS，CSS 文件无对应类）。

(function () {
  "use strict";
  if (window.__DSH_VERSION_MANAGE__) return;
  window.__DSH_VERSION_MANAGE__ = true;

  var NS = "dsh-version-manage";
  var injectTimer = null;
  var css = [
    "." + NS + "{margin:18px 0;padding:14px 16px;border:1px solid var(--border,#333);border-radius:10px;background:var(--surface,#17181b)}",
    "." + NS + "__head{display:flex;align-items:center;justify-content:space-between;gap:8px;margin-bottom:10px}",
    "." + NS + "__title{font-weight:600;font-size:13px}",
    "." + NS + "__badge{font-size:11px;padding:2px 8px;border-radius:999px;background:rgba(1,83,229,.2);color:#7ab2ff;white-space:nowrap}",
    "." + NS + "__badge--ok{background:rgba(34,197,94,.15);color:#4ade80}",
    "." + NS + "__row{display:flex;align-items:center;gap:8px;font-size:12px;margin:5px 0;flex-wrap:wrap}",
    "." + NS + "__row b{font-weight:600}",
    "." + NS + "__row select{padding:4px 6px;border-radius:6px;border:1px solid #333;background:#222;color:inherit;font-size:12px;max-width:220px}",
    "." + NS + "__btn{padding:5px 12px;border-radius:6px;border:1px solid #333;background:rgba(255,255,255,.06);color:inherit;font-size:12px;cursor:pointer}",
    "." + NS + "__btn:hover{background:rgba(255,255,255,.12)}",
    "." + NS + "__btn--primary{border-color:#2563eb;background:rgba(1,83,229,.25);color:#7ab2ff}",
    "." + NS + "__btn:disabled{opacity:.5;cursor:not-allowed}",
    "." + NS + "__rel{border-top:1px dashed #333;margin-top:10px;padding-top:8px;font-size:11px}",
    "." + NS + "__rel-item{display:flex;align-items:center;gap:8px;margin:5px 0;flex-wrap:wrap}",
    "." + NS + "__tag{font-weight:600;color:#7ab2ff}",
    "." + NS + "__date{opacity:.6}",
    "." + NS + "__hint{opacity:.55;font-size:11px;margin-top:6px}",
    "." + NS + "__status{font-size:11px;opacity:.8;margin-top:6px;min-height:14px;word-break:break-all}",
    "." + NS + "__err{color:#f87171;font-size:11px;margin-top:4px}"
  ].join("\n");
  var styleEl = document.createElement("style");
  styleEl.id = "dsh-version-manage-css";
  styleEl.textContent = css;
  document.head.appendChild(styleEl);

  function bridge() {
    try { return window.go && window.go.main && window.go.main.App ? window.go.main.App : null; }
    catch (e) { return null; }
  }
  function toast(msg) {
    if (window.__dshPresetToast) { window.__dshPresetToast(msg); return; }
    var el = document.createElement("div");
    el.style.cssText = "position:fixed;bottom:24px;left:50%;transform:translateX(-50%);z-index:99999;background:#1c1d21;color:#f4f4f3;border:1px solid #333;border-radius:8px;padding:8px 16px;font:13px Inter,sans-serif;box-shadow:0 4px 16px rgba(0,0,0,.5);pointer-events:none;opacity:0;transition:opacity .2s";
    el.textContent = msg;
    document.body.appendChild(el);
    requestAnimationFrame(function () { el.style.opacity = "1"; });
    setTimeout(function () { el.style.opacity = "0"; setTimeout(function () { el.remove(); }, 250); }, 2200);
  }
  function esc(s) {
    return String(s == null ? "" : s).replace(/[&<>"']/g, function (c) {
      return { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c];
    });
  }
  function fmtSize(n) {
    if (!n) return "";
    if (n > 1048576) return (n / 1048576).toFixed(1) + " MB";
    return Math.round(n / 1024) + " KB";
  }

  // 渲染「DSH 核心版本」区块
  function renderBlock(container, data) {
    var cur = data.current || "unknown";
    var latest = data.latest || cur;
    var badge = data.available ? '<span class="' + NS + '__badge">有新版本</span>'
      : '<span class="' + NS + '__badge ' + NS + '__badge--ok">已是最新</span>';

    var wrap = document.createElement("div");
    wrap.className = NS;
    wrap.innerHTML =
      '<div class="' + NS + '__head"><span class="' + NS + '__title">DSH 核心版本</span>' + badge + "</div>" +
      '<div class="' + NS + '__row"><span>当前：<b>' + esc(cur) + "</b></span>" +
      '<span>最新：<b>' + esc(latest) + "</b></span>" +
      '<button class="' + NS + '__btn" data-act="refresh">重新检测</button></div>' +
      '<div class="' + NS + '__row"><span>npm 版本选择：</span><select data-role="ver"></select>' +
      '<button class="' + NS + '__btn ' + NS + '__btn--primary" data-act="npm-dl">下载所选版本 (npm tarball)</button></div>' +
      '<div class="' + NS + '__row"><span>桌面端 Release：</span>' +
      '<a class="' + NS + '__btn" data-act="gh-open" href="#" target="_blank">打开 Releases 页</a></div>' +
      '<div class="' + NS + '__rel" data-role="rels"></div>' +
      '<div class="' + NS + '__status" data-role="status"></div>' +
      '<div class="' + NS + '__err" data-role="err"></div>' +
      '<div class="' + NS + '__hint">下载保存到 ~/.reasonix/downloads/。桌面端安装包 = DeepSeek Harness Desktop 完整安装包（内含 DSH 核心），下载后运行安装即可更新。</div>';

    var sel = wrap.querySelector('[data-role="ver"]');
    (data.versions || []).forEach(function (v) {
      var o = document.createElement("option");
      o.value = v;
      o.textContent = v;
      if (v === latest) o.textContent = v + " (latest)";
      if (v === cur) o.selected = true;
      sel.appendChild(o);
    });
    if (!sel.options.length) {
      var none = document.createElement("option");
      none.value = "";
      none.textContent = "无可用版本列表";
      sel.appendChild(none);
    }

    var relBox = wrap.querySelector('[data-role="rels"]');
    (data.releases || []).forEach(function (r) {
      var item = document.createElement("div");
      item.className = NS + "__rel-item";
      var tag = document.createElement("span");
      tag.className = "dsh-version-manage__tag";
      tag.textContent = r.tag + (r.prerelease ? " (pre)" : "");
      var date = document.createElement("span");
      date.className = NS + "__date";
      date.textContent = String(r.date || "").slice(0, 10);
      item.appendChild(tag);
      item.appendChild(date);
      if (r.winUrl) {
        var btn = document.createElement("button");
        btn.className = NS + "__btn " + NS + "__btn--primary";
        btn.textContent = "下载安装包";
        btn.dataset.url = r.winUrl;
        btn.dataset.name = (r.tag || "dsh-desktop") + "-win-x64.exe";
        btn.addEventListener("click", function () { doDownload(btn.dataset.url, btn.dataset.name, btn); });
        item.appendChild(btn);
      }
      var link = document.createElement("a");
      link.className = NS + "__btn";
      link.href = r.url || "#";
      link.target = "_blank";
      link.rel = "noopener";
      link.textContent = "详情";
      item.appendChild(link);
      relBox.appendChild(item);
    });

    wrap.querySelector('[data-act="refresh"]').addEventListener("click", function () {
      var status = wrap.querySelector('[data-role="status"]');
      status.textContent = "检测中…";
      loadData(function (d2) { renderBlock(container, d2); });
    });
    wrap.querySelector('[data-act="npm-dl"]').addEventListener("click", function () {
      var v = sel.value;
      if (!v) { toast("请先选择 npm 版本"); return; }
      doDownload("https://registry.npmjs.org/@deepseek-ai/dsh/-/dsh-" + encodeURIComponent(v) + ".tgz", "dsh-" + v + ".tgz", wrap.querySelector('[data-act="npm-dl"]'));
    });
    var gh = wrap.querySelector('[data-act="gh-open"]');
    gh.href = data.updateUrl || "https://github.com/sdkwork-ai/deepseek-harness-desktop/releases";
    gh.addEventListener("click", function () { if (!data.releases || !data.releases.length) { toast("打开 GitHub Releases 页面…"); } });

    container.appendChild(wrap);
  }

  function doDownload(url, name, btn) {
    var app = bridge();
    var status = document.querySelector('[data-role="status"]');
    if (!app || !app.DshDownloadVersion) { toast("桥方法不可用"); return; }
    if (btn) { btn.disabled = true; btn.textContent = "下载中…"; }
    if (status) status.textContent = "下载中：" + name + "（大文件请耐心等待）…";
    app.DshDownloadVersion(url, name).then(function (r) {
      if (btn) { btn.disabled = false; btn.textContent = r && r.ok ? "已下载" : "重新下载"; }
      if (status) {
        status.textContent = r && r.ok
          ? "✅ 已下载：" + (r.path || "") + "（" + fmtSize(r.size) + "）"
          : "❌ " + ((r && r.error) || "下载失败");
      }
      if (!r || !r.ok) toast("下载失败：" + ((r && r.error) || "未知错误"));
      else toast("下载完成：" + (r.fileName || name));
    }).catch(function (e) {
      if (btn) { btn.disabled = false; btn.textContent = "重试"; }
      if (status) status.textContent = "❌ 下载调用异常";
      toast("下载调用失败");
    });
  }

  function loadData(cb) {
    var app = bridge();
    if (!app || !app.DshVersionManage) { cb({ current: "unknown", latest: "", versions: [], releases: [] }); return; }
    app.DshVersionManage().then(function (d) { cb(d || {}); }).catch(function () { cb({ current: "unknown", latest: "", versions: [], releases: [] }); });
  }

  var injectedKey = "dsh-vm-injected";
  function inject() {
    // 节流：mutation 高频触发时合并为每 300ms 一次，防止设置面板渲染风暴下主线程卡死
    if (injectTimer) return;
    injectTimer = setTimeout(function () {
      injectTimer = null;
      doInject();
    }, 300);
  }
  function doInject() {
    // 锚点：设置-更新页的更新控制区块（updates-control），在其后插入 DSH 版本管理。
    // 页签切换会重建该区块，故用元素级标记（host.dataset）而非全局 once；
    // 全局注入标志防止任何重建-注入循环。
    if (window.__DSH_VM_INJECTED) return;
    var hosts = document.querySelectorAll(".updates-control");
    for (var h = 0; h < hosts.length; h++) {
      var host = hosts[h];
      if (host.dataset && host.dataset.dshVm === "1") continue;
      if (host.dataset) host.dataset.dshVm = "1";
      window.__DSH_VM_INJECTED = true;
      var container = document.createElement("div");
      container.style.cssText = "margin:14px 0 4px;width:100%";
      host.parentNode.insertBefore(container, host.nextSibling);
      var status = document.createElement("div");
      status.className = NS + "__status";
      status.style.cssText = "margin:10px 0;font-size:12px;opacity:.8";
      status.textContent = "正在读取 DSH 版本信息…";
      container.appendChild(status);
      (function (c, st) {
        loadData(function (d) {
          if (!c.isConnected) return;
          st.remove();
          renderBlock(c, d);
        });
      })(container, status);
    }
  }

  function start() {
    var obs = new MutationObserver(function () { inject(); });
    obs.observe(document.documentElement, { childList: true, subtree: true });
    doInject();
    // 注入成功后 3 秒断开观察（版本管理区已渲染，无需持续监控）
    var guard = setInterval(function () {
      if (window.__DSH_VM_INJECTED) {
        clearInterval(guard);
        obs.disconnect();
      }
    }, 500);
  }
  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", start);
  } else {
    start();
  }
})();
