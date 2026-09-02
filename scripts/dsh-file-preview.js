// dsh-file-preview.js — 文件预览注入（md/docx/xlsx/pptx）
// 三个入口：① 左侧浮动"📁"文件浏览面板（ListWorkspaceFiles）② 预览弹层（PreviewFile）
// ③ 对话区内点击本地文件路径/链接 → 自动预览。
// 自包含浮动 UI（无前端锚点依赖）；全桥调用走 wails 绑定，透传原则。
(function () {
  if (window.__DSH_FP_INJECTED) return;
  window.__DSH_FP_INJECTED = true;
  var NS = "dsh-fp";

  var css = [
    "." + NS + "__fab{position:fixed;left:14px;bottom:70px;z-index:99990;width:40px;height:40px;border-radius:12px;border:1px solid rgba(128,128,128,.35);background:rgba(28,29,33,.92);color:#f4f4f3;font-size:18px;cursor:pointer;box-shadow:0 4px 14px rgba(0,0,0,.45);display:flex;align-items:center;justify-content:center;transition:transform .15s}",
    "." + NS + "__fab:hover{transform:scale(1.08)}",
    "." + NS + "__panel{position:fixed;left:14px;bottom:118px;z-index:99991;width:340px;max-height:60vh;display:flex;flex-direction:column;background:rgba(24,25,29,.97);color:#f4f4f3;border:1px solid rgba(128,128,128,.4);border-radius:12px;box-shadow:0 8px 32px rgba(0,0,0,.6);font:13px Inter,system-ui,sans-serif;overflow:hidden}",
    "." + NS + "__panel-head{display:flex;align-items:center;gap:6px;padding:8px 10px;border-bottom:1px solid rgba(128,128,128,.25);font-weight:600;font-size:12px}",
    "." + NS + "__panel-crumb{flex:1;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;direction:rtl;text-align:left;color:#9ca3af}",
    "." + NS + "__panel-x{cursor:pointer;color:#9ca3af;font-size:14px;padding:0 4px}",
    "." + NS + "__panel-x:hover{color:#fff}",
    "." + NS + "__panel-path{display:flex;gap:6px;padding:6px 8px;border-bottom:1px solid rgba(128,128,128,.2)}",
    "." + NS + "__panel-path input{flex:1;background:#1c1d21;color:#f4f4f3;border:1px solid rgba(128,128,128,.35);border-radius:6px;padding:5px 8px;font-size:12px;outline:none}",
    "." + NS + "__panel-go{cursor:pointer;background:#3b3f46;color:#fff;border:0;border-radius:6px;padding:0 10px;font-size:12px}",
    "." + NS + "__panel-go:hover{background:#4b5563}",
    "." + NS + "__panel-list{flex:1;overflow-y:auto;padding:4px}",
    "." + NS + "__panel-item{display:flex;align-items:center;gap:8px;padding:5px 8px;border-radius:6px;cursor:pointer;font-size:12px}",
    "." + NS + "__panel-item:hover{background:rgba(255,255,255,.07)}",
    "." + NS + "__panel-item--dir{color:#7dd3fc;font-weight:500}",
    "." + NS + "__panel-item--ext{color:#9ca3af;font-size:10px;margin-left:auto}",
    "." + NS + "__panel-item--size{color:#6b7280;font-size:10px}",
    "." + NS + "__panel-empty{color:#6b7280;text-align:center;padding:16px;font-size:12px}",
    "." + NS + "__ovl{position:fixed;inset:0;z-index:99992;background:rgba(0,0,0,.45);display:flex;align-items:center;justify-content:center;padding:40px}",
    "." + NS + "__modal{background:rgba(24,25,29,.99);color:#f4f4f3;border:1px solid rgba(128,128,128,.45);border-radius:14px;width:min(860px,94vw);max-height:84vh;display:flex;flex-direction:column;box-shadow:0 12px 48px rgba(0,0,0,.7);font:13px Inter,system-ui,sans-serif;overflow:hidden}",
    "." + NS + "__modal-head{display:flex;align-items:center;gap:8px;padding:10px 14px;border-bottom:1px solid rgba(128,128,128,.25)}",
    "." + NS + "__modal-title{font-weight:600;font-size:13px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}",
    "." + NS + "__modal-meta{color:#9ca3af;font-size:11px;white-space:nowrap}",
    "." + NS + "__modal-x{margin-left:auto;cursor:pointer;color:#9ca3af;font-size:15px;padding:0 4px}",
    "." + NS + "__modal-x:hover{color:#fff}",
    "." + NS + "__modal-body{overflow:auto;padding:14px;line-height:1.65;font-size:13px}",
    "." + NS + "__modal-body pre{background:#15161a;border:1px solid rgba(128,128,128,.25);border-radius:8px;padding:10px;overflow-x:auto;font:12px/1.6 Consolas,monospace}",
    "." + NS + "__modal-body table{border-collapse:collapse;margin:6px 0;width:100%}",
    "." + NS + "__modal-body th,." + NS + "__modal-body td{border:1px solid rgba(128,128,128,.3);padding:4px 8px;text-align:left;font-size:12px}",
    "." + NS + "__modal-body th{background:rgba(255,255,255,.06)}",
    "." + NS + "__modal-body blockquote{border-left:3px solid #4b5563;margin:6px 0;padding-left:10px;color:#9ca3af}",
    "." + NS + "__modal-body img{max-width:100%;border-radius:6px}",
    "." + NS + "__slide-card{border:1px solid rgba(128,128,128,.3);border-radius:10px;padding:12px;margin:8px 0;background:#15161a}",
    "." + NS + "__slide-n{color:#7dd3fc;font-weight:600;font-size:11px;margin-bottom:6px}",
    "." + NS + "__slide-t{white-space:pre-wrap;font-size:12.5px}",
    "." + NS + "__modal-body h1,." + NS + "__modal-body h2,." + NS + "__modal-body h3{margin:10px 0 6px;line-height:1.3}",
    "." + NS + "__modal-body h1{font-size:19px;border-bottom:1px solid rgba(128,128,128,.25);padding-bottom:4px}",
    "." + NS + "__modal-body h2{font-size:16px}",
    "." + NS + "__modal-body h3{font-size:14px}",
    "." + NS + "__modal-body ul,." + NS + "__modal-body ol{margin:6px 0;padding-left:22px}",
    "." + NS + "__modal-body a{color:#7dd3fc;text-decoration:none}",
    "." + NS + "__modal-body code{background:#15161a;border-radius:4px;padding:1px 4px;font-size:12px}",
    "." + NS + "__modal-body hr{border:0;border-top:1px solid rgba(128,128,128,.25);margin:10px 0}"
  ].join("\n");
  var styleEl = document.createElement("style");
  styleEl.id = NS + "-css";
  styleEl.textContent = css;
  document.head.appendChild(styleEl);

  function bridge() {
    try { return window.go && window.go.main && window.go.main.App ? window.go.main.App : null; }
    catch (e) { return null; }
  }
  function esc(s) {
    return String(s == null ? "" : s).replace(/[&<>"']/g, function (c) {
      return { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c];
    });
  }
  function toast(msg) {
    if (window.__dshPresetToast) { window.__dshPresetToast(msg); return; }
    var el = document.createElement("div");
    el.style.cssText = "position:fixed;bottom:24px;left:50%;transform:translateX(-50%);z-index:99999;background:#1c1d21;color:#f4f4f3;border:1px solid #333;border-radius:8px;padding:8px 16px;font:13px Inter,sans-serif;box-shadow:0 4px 16px rgba(0,0,0,.5);pointer-events:none;opacity:0;transition:opacity .2s";
    el.textContent = msg;
    document.body.appendChild(el);
    requestAnimationFrame(function () { el.style.opacity = "1"; });
    setTimeout(function () { el.style.opacity = "0"; setTimeout(function () { el.remove(); }, 250); }, 2400);
  }

  // ===== 简易 markdown 渲染（标题/粗斜/代码块/列表/表格/引用/链接）=====
  function renderMd(md) {
    var lines = String(md || "").replace(/\r\n/g, "\n").split("\n");
    var html = "";
    var inCode = false, codeBuf = [], inTable = false, tableBuf = [];
    function inline(s) {
      return esc(s)
        .replace(/`([^`]+)`/g, "<code>$1</code>")
        .replace(/\*\*([^*]+)\*\*/g, "<strong>$1</strong>")
        .replace(/\*([^*]+)\*/g, "<em>$1</em>")
        .replace(/\[([^\]]+)\]\(([^)\s]+)\)/g, '<a href="$2" target="_blank">$1</a>');
    }
    function flushTable() {
      if (!tableBuf.length) return;
      var out = "<table>";
      tableBuf.forEach(function (row, i) {
        out += "<tr>";
        row.forEach(function (c) {
          out += (i === 0 ? "<th>" : "<td>") + inline(c.trim()) + (i === 0 ? "</th>" : "</td>");
        });
        out += "</tr>";
      });
      out += "</table>";
      html += out;
      tableBuf = [];
    }
    for (var i = 0; i < lines.length; i++) {
      var ln = lines[i];
      if (/^```/.test(ln)) {
        if (inCode) { html += "<pre>" + esc(codeBuf.join("\n")) + "</pre>"; codeBuf = []; inCode = false; }
        else inCode = true;
        continue;
      }
      if (inCode) { codeBuf.push(ln); continue; }
      if (/^\|/.test(ln)) {
        if (!inTable) { inTable = true; tableBuf = []; }
        var cells = ln.replace(/^\||\|$/g, "").split("|");
        if (cells.every(function (c) { return /^[\s\-:]+$/.test(c); })) continue; // 分隔行
        tableBuf.push(cells);
        continue;
      }
      if (inTable) { flushTable(); inTable = false; }
      if (/^#{1,6}\s/.test(ln)) {
        var m = ln.match(/^(#{1,6})\s+(.*)$/);
        html += "<h" + m[1].length + ">" + inline(m[2]) + "</h" + m[1].length + ">";
      } else if (/^\s*[-*]\s+/.test(ln)) {
        html += "<li>" + inline(ln.replace(/^\s*[-*]\s+/, "")) + "</li>";
      } else if (/^\s*\d+\.\s+/.test(ln)) {
        html += "<li>" + inline(ln.replace(/^\s*\d+\.\s+/, "")) + "</li>";
      } else if (/^>\s?/.test(ln)) {
        html += "<blockquote>" + inline(ln.replace(/^>\s?/, "")) + "</blockquote>";
      } else if (/^(-{3,}|\*{3,})$/.test(ln)) {
        html += "<hr>";
      } else if (ln.trim() === "") {
        html += "<br>";
      } else {
        html += "<p>" + inline(ln) + "</p>";
      }
    }
    if (inCode) html += "<pre>" + esc(codeBuf.join("\n")) + "</pre>";
    if (inTable) flushTable();
    return html;
  }

  // ===== 预览弹层 =====
  function showPreview(path) {
    var app = bridge();
    if (!app || !app.PreviewFile) { toast("预览桥不可用"); return; }
    var ovl = document.createElement("div");
    ovl.className = NS + "__ovl";
    ovl.innerHTML = '<div class="' + NS + '__modal"><div class="' + NS + '__modal-head"><span class="' + NS + '__modal-title">加载中…</span><span class="' + NS + '__modal-x">✕</span></div><div class="' + NS + '__modal-body"><div style="color:#9ca3af;padding:20px;text-align:center">正在读取 ' + esc(path) + ' …</div></div></div>';
    ovl.addEventListener("click", function (e) { if (e.target === ovl) ovl.remove(); });
    ovl.querySelector("." + NS + "__modal-x").addEventListener("click", function () { ovl.remove(); });
    document.body.appendChild(ovl);
    app.PreviewFile(path).then(function (r) {
      if (!r || !r.ok) {
        ovl.querySelector("." + NS + "__modal-title").textContent = "无法预览";
        ovl.querySelector("." + NS + "__modal-body").innerHTML = '<div style="color:#f87171;padding:20px">' + esc((r && r.error) || "未知错误") + "</div>";
        return;
      }
      ovl.querySelector("." + NS + "__modal-title").textContent = (r.name || "") + "  ·  " + (r.type || "");
      ovl.querySelector("." + NS + "__modal-meta") && false;
      var meta = r.size ? " · " + fmtSize(r.size) : "";
      if (r.modified) meta += " · " + r.modified;
      var head = ovl.querySelector("." + NS + "__modal-head");
      var metaEl = document.createElement("span");
      metaEl.className = NS + "__modal-meta";
      metaEl.textContent = meta;
      head.insertBefore(metaEl, head.querySelector("." + NS + "__modal-x"));
      var body = ovl.querySelector("." + NS + "__modal-body");
      if (r.type === "text") {
        body.innerHTML = renderMd(r.content || "");
      } else if (r.type === "table") {
        var h = "<table>";
        (r.sheets || []).forEach(function (s) {
          h += "<tr><th colspan='99'>" + esc(s.name || "Sheet") + "</th></tr>";
          (s.rows || []).forEach(function (row, i) {
            h += "<tr>" + row.map(function (c) {
              return (i === 0 ? "<th>" : "<td>") + esc(c == null ? "" : String(c)) + (i === 0 ? "</th>" : "</td>");
            }).join("") + "</tr>";
          });
        });
        h += "</table>";
        body.innerHTML = h;
      } else if (r.type === "slides") {
        var sh = "";
        (r.slides || []).forEach(function (s) {
          sh += '<div class="' + NS + '__slide-card"><div class="' + NS + '__slide-n">第 ' + s.n + ' 页</div><div class="' + NS + '__slide-t">' + esc(s.text || "（无文本）") + "</div></div>";
        });
        body.innerHTML = sh || '<div style="color:#9ca3af;padding:20px">无内容</div>';
      } else {
        body.innerHTML = '<div style="color:#9ca3af;padding:20px">' + esc((r.hint || "二进制文件") + "（" + fmtSize(r.size || 0) + "）") + "</div>";
      }
    }).catch(function (e) {
      ovl.querySelector("." + NS + "__modal-title").textContent = "预览失败";
      ovl.querySelector("." + NS + "__modal-body").innerHTML = '<div style="color:#f87171;padding:20px">' + esc(String(e)) + "</div>";
    });
  }

  function fmtSize(n) {
    if (n >= 1048576) return (n / 1048576).toFixed(1) + " MB";
    if (n >= 1024) return (n / 1024).toFixed(1) + " KB";
    return n + " B";
  }

  // ===== 文件浏览面板 =====
  var panel = null;
  function openBrowser(dir) {
    if (panel) { panel.remove(); panel = null; return; }
    panel = document.createElement("div");
    panel.className = NS + "__panel";
    panel.innerHTML =
      '<div class="' + NS + '__panel-head"><span>📁 文件浏览</span><span class="' + NS + '__panel-crumb"></span><span class="' + NS + '__panel-x">✕</span></div>' +
      '<div class="' + NS + '__panel-path"><input placeholder="输入目录路径…" /><button class="' + NS + '__panel-go">前往</button></div>' +
      '<div class="' + NS + '__panel-list"><div class="' + NS + '__panel-empty">加载中…</div></div>';
    panel.querySelector("." + NS + "__panel-x").addEventListener("click", function () { panel.remove(); panel = null; });
    var input = panel.querySelector("input");
    var goBtn = panel.querySelector("." + NS + "__panel-go");
    function load(d) {
      var app = bridge();
      if (!app || !app.ListWorkspaceFiles) return;
      panel.querySelector("." + NS + "__panel-crumb").textContent = d || "";
      panel.querySelector("." + NS + "__panel-list").innerHTML = '<div class="' + NS + '__panel-empty">加载中…</div>';
      app.ListWorkspaceFiles(d || "~", 1).then(function (r) {
        var list = panel.querySelector("." + NS + "__panel-list");
        if (!r || !r.ok) { list.innerHTML = '<div class="' + NS + '__panel-empty">' + esc((r && r.error) || "无法读取") + "</div>"; return; }
        if (r.dir) panel.querySelector("." + NS + "__panel-crumb").textContent = r.dir;
        var html = "";
        (r.entries || []).forEach(function (e) {
          if (e.isDir) {
            html += '<div class="' + NS + '__panel-item ' + NS + '__panel-item--dir" data-path="' + esc(e.path) + '">📂 ' + esc(e.name) + "</div>";
          } else {
            html += '<div class="' + NS + '__panel-item" data-path="' + esc(e.path) + '">📄 ' + esc(e.name) +
              '<span class="' + NS + '__panel-item--size">' + fmtSize(e.size || 0) + "</span>" +
              '<span class="' + NS + '__panel-item--ext">' + esc(e.ext || "") + "</span></div>";
          }
        });
        if (!html) html = '<div class="' + NS + '__panel-empty">空目录</div>';
        list.innerHTML = html;
        list.querySelectorAll("." + NS + "__panel-item").forEach(function (it) {
          it.addEventListener("click", function () {
            var p = it.getAttribute("data-path");
            if (it.classList.contains(NS + "__panel-item--dir")) load(p);
            else showPreview(p);
          });
        });
      }).catch(function () {
        panel.querySelector("." + NS + "__panel-list").innerHTML = '<div class="' + NS + '__panel-empty">读取失败</div>';
      });
    }
    goBtn.addEventListener("click", function () { load(input.value.trim()); });
    input.addEventListener("keydown", function (e) { if (e.key === "Enter") load(input.value.trim()); });
    document.body.appendChild(panel);
    if (dir) input.value = dir;
    load(dir || "");
  }

  // ===== 浮动按钮 =====
  var fab = document.createElement("button");
  fab.className = NS + "__fab";
  fab.textContent = "📁";
  fab.title = "文件浏览 / 预览";
  fab.addEventListener("click", function () { openBrowser(currentCwd()); });
  document.body.appendChild(fab);

  function currentCwd() {
    // 尝试从会话 meta 拿 cwd（window.dsh 透传或前端状态）
    try {
      var app = bridge();
      if (app && app.MetaForTab) {
        var meta = app.MetaForTab("");
        if (meta && meta.cwd) return meta.cwd;
      }
    } catch (e) { /* ignore */ }
    return "";
  }

  // ===== 对话区内点击文件路径 → 预览 =====
  document.addEventListener("click", function (e) {
    var t = e.target;
    if (!t || !t.closest) return;
    var msg = t.closest("[data-role='message'],[data-message],.message,.msg");
    if (!msg) return;
    var text = "";
    if (t.getAttribute && t.getAttribute("href")) text = t.getAttribute("href");
    else if (t.textContent) text = t.textContent;
    text = (text || "").trim();
    var winPath = text.match(/^([A-Za-z]:\\[^\s<>"|?*]+)/);
    var unixPath = text.match(/^(\/(?:[^\s<>"|?*]+\/)*[^\s<>"|?*]+)/);
    var p = winPath ? winPath[1] : unixPath ? unixPath[1] : "";
    if (p && /\.(md|markdown|txt|docx|xlsx|pptx|log|json|yaml|yml|csv|go|ts|tsx|js|jsx|py|sh|ps1|bat|html|css)$/i.test(p)) {
      showPreview(p);
    }
  }, true);

  // 暴露给其他注入脚本
  window.__DSH_FILE_PREVIEW = { showPreview: showPreview, openBrowser: openBrowser };
})();
