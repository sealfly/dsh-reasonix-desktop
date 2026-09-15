// dsh-subagent-panel.js — 右侧栏「子代理」页：显示当前会话的子智能体 + 相关后台进程。
//
// 机制（与 dsh-inline-editor.js 同款，PRINCIPLES P2 的"作者明确要求改前端"例外路径）：
//   1) 探测右侧栏 tab 栏（.workspace-files__tabs），在其后插入一个「子代理」按钮；
//   2) 点击后在文件面板容器（.workspace-files）上叠加一个绝对定位 overlay（不删改 React 节点），
//      退出时只移除自己创建的节点 —— React 卸载其节点时 DOM 依然完好；
//   3) 数据来自 Go 桥 window.go.main.App.SubagentPanel(sessionId)（sessionId 传空 = 桥侧自动选
//      最近更新的父会话）；子智能体来自 DSH subagent.list，后台进程来自桥侧 Win32_Process 枚举。
//
// ⚠️ React 安全铁律：绝不移除/清空 React 渲染的节点，只 append 自己的节点 + 覆盖式 overlay。
;
(function () {
  "use strict";
  if (window.__DSH_SUBAGENT_PANEL__) return;
  window.__DSH_SUBAGENT_PANEL__ = true;

  var NS = "dsh-sp";
  var REFRESH_MS = 5000;
  var TAB_LABEL = "子代理";

  // Go 桥入口（Wails 注入；未就绪时安全跳过，后续 scan 会补上）
  function A() {
    try { return window.go && window.go.main && window.go.main.App ? window.go.main.App : null; }
    catch (e) { return null; }
  }

  var css = [
    "." + NS + "-tab{all:unset;box-sizing:border-box;display:inline-flex;align-items:center;gap:5px;padding:4px 9px;",
    "border-radius:6px;font:inherit;font-size:12px;line-height:1.4;cursor:pointer;color:var(--fg-dim,#9aa0a6);white-space:nowrap}",
    "." + NS + "-tab:hover{background:var(--bg-soft,rgba(255,255,255,.06));color:var(--fg,#e8eaed)}",
    "." + NS + "-tab--on{background:var(--bg-elev-2,rgba(255,255,255,.1));color:var(--fg,#e8eaed);font-weight:600}",
    "." + NS + "-badge{min-width:16px;height:16px;padding:0 4px;border-radius:8px;background:var(--accent,#ff5a2c);",
    "color:var(--accent-fg,#fff);font-size:10px;font-weight:700;display:inline-flex;align-items:center;justify-content:center}",
    "." + NS + "-panel{position:absolute;inset:0;z-index:6;display:flex;flex-direction:column;background:var(--bg,#111214);",
    "color:var(--fg,#e8eaed);font-size:12px;overflow:hidden}",
    "." + NS + "-head{display:flex;align-items:center;gap:8px;padding:8px 10px;border-bottom:1px solid var(--border-soft,rgba(255,255,255,.08));flex:0 0 auto}",
    "." + NS + "-title{font-weight:600;font-size:12.5px}",
    "." + NS + "-sub{color:var(--fg-faint,#85888f);font-size:11px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;flex:1 1 auto}",
    "." + NS + "-btn{all:unset;cursor:pointer;padding:3px 8px;border-radius:6px;border:1px solid var(--border,rgba(255,255,255,.14));font-size:11px}",
    "." + NS + "-btn:hover{background:var(--bg-soft,rgba(255,255,255,.06))}",
    "." + NS + "-body{flex:1 1 auto;overflow:auto;padding:8px 10px 14px}",
    "." + NS + "-sect{margin-bottom:14px}",
    "." + NS + "-secthead{display:flex;align-items:center;gap:6px;margin:2px 0 6px;color:var(--fg-dim,#9aa0a6);",
    "font-size:11px;font-weight:600;letter-spacing:.02em;text-transform:none}",
    "." + NS + "-card{border:1px solid var(--border-soft,rgba(255,255,255,.09));border-radius:8px;padding:7px 9px;margin-bottom:6px;",
    "background:var(--bg-elev,rgba(255,255,255,.03))}",
    "." + NS + "-card--active{border-color:var(--accent,#ff5a2c)}",
    "." + NS + "-row1{display:flex;align-items:center;gap:6px}",
    "." + NS + "-label{font-weight:600;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;flex:1 1 auto}",
    "." + NS + "-dot{width:7px;height:7px;border-radius:50%;background:var(--fg-faint,#85888f);flex:0 0 auto}",
    "." + NS + "-dot--on{background:var(--accent,#ff5a2c);box-shadow:0 0 0 3px rgba(255,90,44,.18)}",
    "." + NS + "-meta{color:var(--fg-faint,#85888f);font-size:11px;margin-top:3px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}",
    "." + NS + "-tag{display:inline-block;padding:0 5px;border-radius:4px;background:var(--bg-elev-2,rgba(255,255,255,.08));",
    "color:var(--fg-dim,#9aa0a6);font-size:10px;margin-right:4px}",
    "." + NS + "-table{width:100%;border-collapse:collapse}",
    "." + NS + "-table th{text-align:left;font-weight:600;color:var(--fg-faint,#85888f);font-size:10.5px;padding:3px 4px;",
    "border-bottom:1px solid var(--border-soft,rgba(255,255,255,.08))}",
    "." + NS + "-table td{padding:3px 4px;border-bottom:1px solid var(--border-soft,rgba(255,255,255,.05));vertical-align:top}",
    "." + NS + "-mono{font-family:ui-monospace,Consolas,monospace;font-size:11px}",
    "." + NS + "-empty{color:var(--fg-faint,#85888f);padding:10px 2px;font-size:11.5px;line-height:1.6}",
    "." + NS + "-err{color:#ff8a65;padding:8px 2px;font-size:11.5px;white-space:pre-wrap}",
    "." + NS + "-note{color:var(--fg-faint,#85888f);font-size:10.5px;line-height:1.6;margin-top:10px;padding-top:8px;",
    "border-top:1px solid var(--border-soft,rgba(255,255,255,.07))}"
  ].join("");

  function ensureStyle() {
    if (document.getElementById(NS + "-style")) return;
    var s = document.createElement("style");
    s.id = NS + "-style";
    s.textContent = css;
    (document.head || document.documentElement).appendChild(s);
  }

  function el(tag, cls, text) {
    var n = document.createElement(tag);
    if (cls) n.className = cls;
    if (text != null) n.textContent = text;
    return n;
  }

  function shortId(id) {
    if (!id) return "";
    return String(id).replace(/^session-/, "").slice(0, 8);
  }

  function ts(t) {
    if (!t) return "";
    try {
      var d = new Date(t);
      var p = function (n) { return (n < 10 ? "0" : "") + n; };
      return p(d.getMonth() + 1) + "-" + p(d.getDate()) + " " + p(d.getHours()) + ":" + p(d.getMinutes());
    } catch (e) { return ""; }
  }

  function baseName(p) {
    if (!p) return "";
    var s = String(p).replace(/[\\/]+$/, "");
    var i = Math.max(s.lastIndexOf("\\"), s.lastIndexOf("/"));
    return i >= 0 ? s.slice(i + 1) : s;
  }

  // ===== 状态 =====
  var state = { open: false, timer: null, loading: false, last: null, error: "" };

  function findTabBar() {
    return document.querySelector(".workspace-files__tabs") ||
           document.querySelector(".workspace-files__tools .workspace-files__tabs");
  }
  function findFilePanel() {
    return document.querySelector(".workspace-files") || document.querySelector(".workspace-panel");
  }

  // ===== 面板渲染（overlay，绝不动 React 节点）=====
  function renderPanel() {
    var host = findFilePanel();
    if (!host) return;
    ensureStyle();
    var panel = document.getElementById(NS + "-panel");
    if (!state.open) {
      if (panel && panel.parentNode) panel.parentNode.removeChild(panel);
      return;
    }
    if (!panel) {
      panel = el("div", NS + "-panel");
      panel.id = NS + "-panel";
    }
    // 定位加固：overlay 用 absolute 覆盖宿主；若宿主不是定位上下文（position:static），
    // 临时给它加 inline position:relative（只加我们自己的 inline 样式，不动 React 的 class）。
    if (panel.parentNode !== host) {
      try {
        if (getComputedStyle(host).position === "static") {
          if (!host.getAttribute(NS + "-host-pos")) {
            host.setAttribute(NS + "-host-pos", host.style.position || "");
            host.style.position = "relative";
          }
        }
      } catch (e) { /* 拿不到样式就按原样挂载 */ }
      host.appendChild(panel);
    }

    panel.textContent = "";
    var data = state.last;

    // 头部
    var head = el("div", NS + "-head");
    head.appendChild(el("span", NS + "-title", TAB_LABEL));
    var sub = el("span", NS + "-sub");
    if (data && data.sessionId) {
      var c = data.counts || {};
      sub.textContent = "会话 " + shortId(data.sessionId) +
        (data.parentTitle ? " · " + String(data.parentTitle).slice(0, 28) : "") +
        " · 子智能体 " + (c.subagents || 0) + (c.active ? "（活跃 " + c.active + "）" : "") +
        " · 进程 " + (c.processes || 0);
    } else {
      sub.textContent = state.loading ? "读取中…" : "等待桥数据";
    }
    head.appendChild(sub);
    var btn = el("button", NS + "-btn", state.loading ? "刷新中" : "刷新");
    btn.onclick = function () { refresh(true); };
    head.appendChild(btn);
    var close = el("button", NS + "-btn", "关闭");
    close.onclick = function () { setOpen(false); };
    head.appendChild(close);
    panel.appendChild(head);

    // 内容
    var body = el("div", NS + "-body");
    if (state.error) {
      body.appendChild(el("div", NS + "-err", state.error));
    }
    if (!data) {
      body.appendChild(el("div", NS + "-empty", state.loading ? "正在读取子智能体与后台进程…" : "暂无数据"));
      panel.appendChild(body);
      return;
    }

    // 1) 子智能体
    var sec1 = el("div", NS + "-sect");
    var subs = data.subagents || [];
    sec1.appendChild(el("div", NS + "-secthead", "子智能体（" + subs.length + "）"));
    if (!subs.length) {
      sec1.appendChild(el("div", NS + "-empty",
        "当前会话没有子智能体。\n由 dsh-agent-teams 等插件派生、或工具发起的子任务会显示在这里。"));
    } else {
      subs.forEach(function (s) {
        var active = s.activity === "active" || s.running;
        var card = el("div", NS + "-card" + (active ? " " + NS + "-card--active" : ""));
        var r1 = el("div", NS + "-row1");
        r1.appendChild(el("span", NS + "-dot" + (active ? " " + NS + "-dot--on" : "")));
        r1.appendChild(el("span", NS + "-label", s.label || s.title || shortId(s.id)));
        r1.appendChild(el("span", NS + "-tag", s.mode || s.kind || ""));
        card.appendChild(r1);
        var meta = el("div", NS + "-meta");
        var bits = [];
        bits.push(active ? "活跃" : "已结束");
        if (s.agentPreset) bits.push(s.agentPreset);
        if (s.cwd) bits.push(baseName(s.cwd));
        if (s.updatedAt) bits.push(ts(s.updatedAt));
        if (s.hasChildren) bits.push("含子级");
        bits.push(shortId(s.id));
        meta.textContent = bits.join(" · ");
        meta.title = [s.title, s.sessionId, s.cwd].filter(Boolean).join("\n");
        card.appendChild(meta);
        sec1.appendChild(card);
      });
    }
    body.appendChild(sec1);

    // 2) 后台进程
    var sec2 = el("div", NS + "-sect");
    var procs = data.processes || [];
    sec2.appendChild(el("div", NS + "-secthead", "后台进程（" + procs.length + "）"));
    if (!procs.length) {
      sec2.appendChild(el("div", NS + "-empty", "未发现与本项目/DSH 相关的进程。"));
    } else {
      var t = el("table", NS + "-table");
      var thead = el("thead");
      var htr = el("tr");
      ["PID", "进程", "内存", "类别"].forEach(function (h) { htr.appendChild(el("th", null, h)); });
      thead.appendChild(htr);
      t.appendChild(thead);
      var tb = el("tbody");
      procs.forEach(function (p) {
        var tr = el("tr");
        tr.appendChild(el("td", NS + "-mono", String(p.pid)));
        tr.appendChild(el("td", null, p.name || ""));
        tr.appendChild(el("td", NS + "-mono", (p.memMb ? Math.round(p.memMb) + " MB" : "")));
        tr.appendChild(el("td", null, p.category || ""));
        if (p.cmd) tr.title = p.cmd;
        tb.appendChild(tr);
      });
      t.appendChild(tb);
      sec2.appendChild(t);
    }
    body.appendChild(sec2);

    // 3) 说明（来源与边界，如实告知）
    if (data.notes && data.notes.length) {
      var note = el("div", NS + "-note");
      data.notes.forEach(function (n, i) {
        note.appendChild(el("div", null, "· " + n));
      });
      body.appendChild(note);
    }
    panel.appendChild(body);
  }

  // ===== 数据 =====
  function refresh(manual) {
    var app = A();
    if (!app || !app.SubagentPanel) {
      state.error = "";
      if (manual) state.error = "桥未就绪（window.go.main.App.SubagentPanel 不存在）";
      renderPanel();
      return;
    }
    state.loading = true;
    renderPanel();
    Promise.resolve(app.SubagentPanel(""))
      .then(function (res) {
        state.loading = false;
        state.error = "";
        if (res && res.ok === false) {
          state.error = "桥返回失败：" + ((res.notes && res.notes.join("；")) || "未知原因");
        }
        state.last = res || null;
        renderPanel();
        updateBadge();
      })
      .catch(function (err) {
        state.loading = false;
        state.error = "读取失败：" + (err && err.message ? err.message : String(err));
        renderPanel();
      });
  }

  function setOpen(open) {
    state.open = !!open;
    var tab = document.getElementById(NS + "-tab");
    if (tab) tab.className = NS + "-tab" + (state.open ? " " + NS + "-tab--on" : "");
    if (state.open) {
      refresh(false);
      if (!state.timer) state.timer = window.setInterval(function () { if (state.open) refresh(false); }, REFRESH_MS);
    } else if (state.timer) {
      window.clearInterval(state.timer);
      state.timer = null;
    }
    renderPanel();
  }

  function updateBadge() {
    var badge = document.getElementById(NS + "-badge");
    if (!badge || !state.last) return;
    var c = state.last.counts || {};
    var n = c.active || 0;
    badge.textContent = String(n);
    badge.style.display = n > 0 ? "" : "none";
  }

  // ===== tab 按钮（插入到 React 的 tab 栏里；被 React 重排删除时由 scan 补回）=====
  function ensureTab() {
    var bar = findTabBar();
    if (!bar) return;
    if (document.getElementById(NS + "-tab")) return;
    ensureStyle();
    var btn = el("button", NS + "-tab" + (state.open ? " " + NS + "-tab--on" : ""));
    btn.id = NS + "-tab";
    btn.type = "button";
    btn.appendChild(document.createTextNode(TAB_LABEL));
    var badge = el("span", NS + "-badge");
    badge.id = NS + "-badge";
    badge.style.display = "none";
    btn.appendChild(badge);
    btn.onclick = function () { setOpen(!state.open); };
    bar.appendChild(btn);
    updateBadge();
  }

  function teardownIfDetached() {
    // 面板宿主消失（切到别的视图）时移除 overlay；tab 被 React 移除时由 scan 重插
    var panel = document.getElementById(NS + "-panel");
    if (panel && state.open && !findFilePanel()) {
      if (panel.parentNode) panel.parentNode.removeChild(panel);
    }
  }

  var scanTimer = null;
  function scan() {
    try {
      ensureTab();
      renderPanel();
      teardownIfDetached();
    } catch (e) { /* 失败留痕但不打断前端 */ }
  }

  function boot() {
    scan();
    scanTimer = window.setInterval(scan, 1500);
    if (window.MutationObserver) {
      try {
        var obs = new MutationObserver(function () { scan(); });
        obs.observe(document.documentElement, { childList: true, subtree: true });
      } catch (e) { /* 轮询已兜底 */ }
    }
    window.addEventListener("beforeunload", function () {
      if (scanTimer) window.clearInterval(scanTimer);
      if (state.timer) window.clearInterval(state.timer);
    });
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", boot);
  } else {
    boot();
  }
})();
