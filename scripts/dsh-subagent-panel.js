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

  // 样式对齐原生：数值照抄 Reasonix v1.31.4 源码 styles.css 的真实规则
  // （.workbench-dock__tab / .workspace-files__tab / .workspace-tree__row /
  //   .workspace-change / .workspace-iconbtn / .workspace-files__tools / .workspace-empty /
  //   .workspace-recent-menu__title），颜色统一用原生 CSS 变量，自动跟随主题。
  var css = [
    // tab 按钮：对齐 .workbench-dock__tab（height 24 / padding 0 8 / radius 6 / 11.5px / weight 560）
    "." + NS + "-tab{all:unset;box-sizing:border-box;--wails-draggable:no-drag;display:inline-flex;align-items:center;",
    "justify-content:center;gap:5px;height:24px;padding:0 8px;border-radius:6px;background:transparent;",
    "color:var(--fg-faint);font:inherit;font-size:11.5px;font-weight:560;white-space:nowrap;cursor:pointer}",
    "." + NS + "-tab:hover{background:var(--tree-row-hover-bg,var(--workspace-files-hover,var(--bg-soft)));color:var(--fg)}",
    "." + NS + "-tab--on{background:var(--tree-row-current-bg,var(--bg-elev-2));color:var(--fg)}",
    "." + NS + "-tab-text{font-size:11.5px}",
    "." + NS + "-badge{min-width:15px;height:15px;padding:0 4px;border-radius:8px;background:var(--accent);",
    "color:var(--accent-fg);font-size:10px;font-weight:620;display:inline-flex;align-items:center;justify-content:center}",
    // 面板容器：用原生底色/前景
    "." + NS + "-panel{position:absolute;inset:0;z-index:6;display:flex;flex-direction:column;",
    "background:var(--bg);color:var(--fg);font-size:12.5px;overflow:hidden}",
    // 头部：对齐 .workspace-files__tools（height 42 / padding 7px 8px 3px / gap 4），不另加分隔线
    "." + NS + "-head{flex:0 0 auto;height:42px;display:flex;align-items:center;gap:4px;padding:7px 8px 3px}",
    "." + NS + "-title{font-size:12.5px;font-weight:560;color:var(--fg)}",
    "." + NS + "-sub{color:var(--fg-faint);font-size:11px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;flex:1 1 auto}",
    // 图标按钮：对齐 .workspace-iconbtn（28×28 / radius 7 / transition）
    "." + NS + "-iconbtn{all:unset;box-sizing:border-box;--wails-draggable:no-drag;display:inline-flex;align-items:center;",
    "justify-content:center;width:28px;height:28px;border-radius:7px;background:transparent;color:var(--fg-faint);",
    "cursor:pointer;transition:color .12s,background .12s;flex:0 0 auto}",
    "." + NS + "-iconbtn:hover{background:var(--tree-row-hover-bg,var(--workspace-files-hover,var(--bg-soft)));color:var(--fg)}",
    "." + NS + "-iconbtn:focus-visible{outline:1px solid var(--accent);outline-offset:1px}",
    "." + NS + "-iconbtn--busy{opacity:.55}",
    // 内容区：滚动但不显示滚动条（原生全局 ::-webkit-scrollbar{display:none}）
    "." + NS + "-body{flex:1 1 auto;overflow:auto;padding:2px 0 10px}",
    "." + NS + "-body::-webkit-scrollbar{display:none}",
    "." + NS + "-sect{margin-bottom:10px}",
    // 分组标题：对齐 .workspace-recent-menu__title
    "." + NS + "-secthead{padding:6px 8px 4px;color:var(--fg-faint);font-size:11px;font-weight:620}",
    // 列表行：对齐 .workspace-tree__row / .workspace-change（无边框、圆角、hover 底色）
    "." + NS + "-card{box-sizing:border-box;display:flex;flex-direction:column;gap:2px;width:100%;",
    "min-height:var(--tree-row-height,30px);padding:5px 8px;border:0;border-radius:var(--tree-row-radius,7px);",
    "background:transparent;color:var(--fg-dim)}",
    "." + NS + "-card:hover{background:var(--tree-row-hover-bg,var(--workspace-files-hover,var(--bg-soft)));color:var(--fg)}",
    "." + NS + "-card--active{background:var(--tree-row-current-bg,var(--bg-elev-2));color:var(--tree-row-current-fg,var(--fg))}",
    "." + NS + "-row1{display:flex;align-items:center;gap:6px;min-width:0}",
    "." + NS + "-label{font-size:12.5px;font-weight:560;color:inherit;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;flex:1 1 auto}",
    "." + NS + "-dot{width:6px;height:6px;border-radius:50%;background:var(--fg-faint);flex:0 0 auto}",
    "." + NS + "-dot--on{background:var(--accent)}",
    "." + NS + "-meta{color:var(--fg-faint);font-size:11px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}",
    "." + NS + "-tag{display:inline-block;padding:0 5px;border-radius:5px;background:var(--bg-elev-2);",
    "color:var(--fg-dim);font-size:10.5px;font-weight:560;flex:0 0 auto}",
    // 状态徽标：延续行内的胶囊样式，颜色取原生语义变量
    "." + NS + "-status{display:inline-block;padding:0 6px;border-radius:5px;font-size:10.5px;font-weight:620;",
    "white-space:nowrap;flex:0 0 auto;background:var(--bg-elev-2);color:var(--fg-dim)}",
    "." + NS + "-status--running{background:color-mix(in srgb,var(--accent) 18%,transparent);color:var(--accent)}",
    "." + NS + "-status--done{background:color-mix(in srgb,var(--ok,#1f9d57) 16%,transparent);color:var(--ok,#1f9d57)}",
    "." + NS + "-status--failed{background:color-mix(in srgb,var(--danger,#d9534f) 18%,transparent);color:var(--danger,#d9534f)}",
    "." + NS + "-status--cancelled{background:var(--bg-elev-2);color:var(--fg-faint)}",
    "." + NS + "-status--idle{background:var(--bg-soft);color:var(--fg-faint)}",
    "." + NS + "-jobcmd{font-family:ui-monospace,Consolas,monospace;font-size:11px;color:var(--fg-faint);",
    "overflow:hidden;text-overflow:ellipsis;white-space:nowrap}",
    // 系统进程：单行列表（对齐 .workspace-tree__row 的行高与 hover），不再用表格
    "." + NS + "-procrow{box-sizing:border-box;display:flex;align-items:center;gap:8px;width:100%;",
    "height:var(--tree-row-height,30px);padding:0 8px;border-radius:var(--tree-row-radius,7px);color:var(--fg-dim)}",
    "." + NS + "-procrow:hover{background:var(--tree-row-hover-bg,var(--workspace-files-hover,var(--bg-soft)));color:var(--fg)}",
    "." + NS + "-procname{flex:1 1 auto;min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;font-size:12.5px}",
    "." + NS + "-procid{font-family:ui-monospace,Consolas,monospace;font-size:11px;color:var(--fg-faint);flex:0 0 auto}",
    "." + NS + "-procmem{font-size:11px;color:var(--fg-faint);flex:0 0 auto;min-width:56px;text-align:right}",
    // 区块标题行 + 详情开关（开关是小号图标按钮，融入原生 iconbtn 风格）
    "." + NS + "-sectrow{display:flex;align-items:center;justify-content:space-between;gap:4px;padding:6px 8px 4px}",
    "." + NS + "-sectrow>" + "." + NS + "-secthead{padding:0}",
    "." + NS + "-secttoggle{width:20px;height:20px;border-radius:5px}",
    // 收起态：只显示汇总（不列具体进程）
    "." + NS + "-procsummary{padding:2px 8px 6px;color:var(--fg-faint);font-size:11.5px;line-height:1.5}",
    "." + NS + "-mono{font-family:ui-monospace,Consolas,monospace;font-size:11px}",
    // 空态：对齐 .workspace-empty
    "." + NS + "-empty{padding:18px 8px;color:var(--fg-faint);font-size:13px;line-height:1.6}",
    "." + NS + "-err{color:var(--danger,#d9534f);padding:12px 8px;font-size:12.5px;white-space:pre-wrap}",
    "." + NS + "-note{padding:8px 8px 0;color:var(--fg-faint);font-size:10.5px;line-height:1.6}",
    "." + NS + "-skeleton{animation:" + NS + "-pulse 1.2s ease-in-out infinite}",
    "@keyframes " + NS + "-pulse{0%,100%{opacity:.45}50%{opacity:.95}}"
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

  var SVG_NS = "http://www.w3.org/2000/svg";
  // 与原生一致的 lucide 线条图标（13~14px、currentColor、stroke 2）
  var ICON_PATHS = {
    // 多人协作（子智能体/团队语义）
    users: [
      ["path", { d: "M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2" }],
      ["circle", { cx: "9", cy: "7", r: "4" }],
      ["path", { d: "M22 21v-2a4 4 0 0 0-3-3.87" }],
      ["path", { d: "M16 3.13a4 4 0 0 1 0 7.75" }]
    ],
    // lucide refresh-cw
    refresh: [
      ["path", { d: "M3 12a9 9 0 0 1 9-9 9.75 9.75 0 0 1 6.74 2.74L21 8" }],
      ["path", { d: "M21 3v5h-5" }],
      ["path", { d: "M21 12a9 9 0 0 1-9 9 9.75 9.75 0 0 1-6.74-2.74L3 16" }],
      ["path", { d: "M8 16H3v5" }]
    ],
    // lucide x
    close: [
      ["path", { d: "M18 6 6 18" }],
      ["path", { d: "m6 6 12 12" }]
    ],
    // lucide chevron-right / chevron-down（详情展开/收起）
    "chevron-right": [
      ["path", { d: "m9 18 6-6-6-6" }]
    ],
    "chevron-down": [
      ["path", { d: "m6 9 6 6 6-6" }]
    ]
  };

  function svgIcon(name, size) {
    var px = String(size || 13);
    var svg = document.createElementNS(SVG_NS, "svg");
    svg.setAttribute("width", px);
    svg.setAttribute("height", px);
    svg.setAttribute("viewBox", "0 0 24 24");
    svg.setAttribute("fill", "none");
    svg.setAttribute("stroke", "currentColor");
    svg.setAttribute("stroke-width", "2");
    svg.setAttribute("stroke-linecap", "round");
    svg.setAttribute("stroke-linejoin", "round");
    svg.setAttribute("aria-hidden", "true");
    (ICON_PATHS[name] || []).forEach(function (spec) {
      var n = document.createElementNS(SVG_NS, spec[0]);
      Object.keys(spec[1]).forEach(function (k) { n.setAttribute(k, spec[1][k]); });
      svg.appendChild(n);
    });
    return svg;
  }

  // tab 图标：与原生 workbench-dock__tab 同风格
  function tabIcon() { return svgIcon("users", 13); }

  // 纯图标按钮（同原生工具栏按钮：只有符号，文字仅作 title/aria-label 提示）
  function iconButton(iconName, title) {
    var b = el("button", NS + "-iconbtn");
    b.type = "button";
    b.title = title;
    b.setAttribute("aria-label", title);
    b.appendChild(svgIcon(iconName, 14));
    return b;
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

  // 后台任务状态徽标：运行中 / 已完成 / 失败 / 已取消 / 未启动（桥侧已归一 statusLabel）
  function statusChip(j) {
    var label = j.statusLabel || jobStatusFallback(j.status);
    var cls = NS + "-status --cls--";
    var kind = "idle";
    if (label === "运行中") kind = "running";
    else if (label === "已完成") kind = "done";
    else if (label === "失败") kind = "failed";
    else if (label === "已取消") kind = "cancelled";
    return el("span", NS + "-status " + NS + "-status--" + kind, label);
  }

  // 前端兜底映射（桥未提供 statusLabel 时用）
  function jobStatusFallback(status) {
    var s = String(status || "").toLowerCase();
    if (s === "completed" || s === "done" || s === "success" || s === "succeeded") return "已完成";
    if (s === "running" || s === "active" || s === "in_progress" || s === "started") return "运行中";
    if (s === "failed" || s === "error") return "失败";
    if (s === "cancelled" || s === "canceled" || s === "killed") return "已取消";
    if (s === "queued" || s === "pending" || s === "created") return "未启动";
    return status ? String(status) : "未知";
  }

  // 毫秒 → 人类可读（1.2s / 45s / 2m10s）
  function humanMs(ms) {
    if (ms == null || ms < 0) return "";
    if (ms < 1000) return ms + "ms";
    var s = ms / 1000;
    if (s < 60) return (s < 10 ? s.toFixed(1) : Math.round(s)) + "s";
    var m = Math.floor(s / 60), rs = Math.round(s % 60);
    return m + "m" + (rs ? rs + "s" : "");
  }

  // 后台进程汇总（收起态显示）：按类别计数 + 内存合计，不暴露具体进程
  function procSummary(procs) {
    var byCat = {}, mem = 0;
    (procs || []).forEach(function (p) {
      var c = p.category || "其他";
      byCat[c] = (byCat[c] || 0) + 1;
      if (p.memMb) mem += p.memMb;
    });
    var parts = Object.keys(byCat).map(function (k) { return k + " " + byCat[k]; });
    var tail = mem > 0 ? "，合计约 " + Math.round(mem) + " MB" : "";
    return parts.join(" · ") + tail + "（点右侧箭头展开具体进程）";
  }

  // ===== 状态 =====
  // procDetail：「后台进程」区块的详情开关（展开显示完整命令行 / 父 PID）。默认收起，
  // 用户选择记在 localStorage（注入脚本里访问 storage 一律 try/catch，隐私模式不报错）。
  var PROC_DETAIL_KEY = "dsh-sp-proc-detail";
  function loadProcDetail() {
    try { return window.localStorage && window.localStorage.getItem(PROC_DETAIL_KEY) === "1"; }
    catch (e) { return false; }
  }
  function saveProcDetail(v) {
    try { if (window.localStorage) window.localStorage.setItem(PROC_DETAIL_KEY, v ? "1" : "0"); }
    catch (e) { /* 存不了就只在本次会话生效 */ }
  }

  var state = { open: false, timer: null, loading: false, last: null, error: "", diag: {}, procDetail: loadProcDetail() };

  // 诊断上报（无 devtools 环境下的可观测性；桥侧写入 %TEMP%\resume-debug.log）
  var diagSent = {};
  function diag(tag, extra) {
    try {
      var app = A();
      if (!app || !app.LogFromFrontend) return;
      var key = tag + "|" + (extra || "");
      if (diagSent[key]) return;      // 同类诊断只报一次，避免刷日志
      diagSent[key] = true;
      app.LogFromFrontend("[subagent-panel] " + tag + (extra ? " " + extra : ""));
    } catch (e) { /* 上报失败绝不影响功能 */ }
  }

  // 右栏视图 tab 栏（概览/远程/终端…）——真实类名来自 Reasonix 前端 index chunk：
  //   workbench-dock__tabs / workbench-dock__tab / workbench-dock__body
  // 兜底候选是"工作区视图"里的 文件/改动 子 tab 栏（旧版布局）。
  var TAB_BAR_SELECTORS = [".workbench-dock__tabs", ".workspace-files__tabs"];
  var PANEL_HOST_SELECTORS = [".workbench-dock__body", ".workspace-files", ".workspace-panel"];
  // 原生 tab 的类名（用于样式融入；仅主容器命中时使用）
  var NATIVE_TAB_CLASS = "workbench-dock__tab";

  function firstMatch(selectors) {
    for (var i = 0; i < selectors.length; i++) {
      var n = document.querySelector(selectors[i]);
      if (n) return { node: n, selector: selectors[i] };
    }
    return null;
  }
  function findTabBar() { return firstMatch(TAB_BAR_SELECTORS); }
  function findPanelHost() { return firstMatch(PANEL_HOST_SELECTORS); }
  function findFilePanel() { var m = findPanelHost(); return m ? m.node : null; }

  // ===== 面板渲染（overlay，绝不动 React 节点）=====
  // 防止无谓重建：内容签名不变时直接返回（scan 会周期性调用本函数）。
  function panelSignature() {
    if (!state.open) return "closed";
    if (state.error) return "err:" + state.error;
    if (!state.last) return "loading";
    var subs = (state.last.subagents || []).map(function (s) {
      return (s.id || "") + ":" + (s.activity || "") + ":" + (s.running ? 1 : 0);
    }).join(",");
    var procs = (state.last.processes || []).map(function (p) { return p.pid; }).join(",");
    var jobs = (state.last.jobs || []).map(function (j) {
      return (j.id || "") + ":" + (j.status || "") + ":" + (j.durationMs >= 0 ? Math.round(j.durationMs / 1000) : -1);
    }).join(",");
    return [state.loading ? "L" : "", state.last.sessionId || "", subs, jobs, procs,
      state.procDetail ? "D1" : "D0",
      JSON.stringify(state.last.counts || {})].join("|");
  }

  function renderPanel() {
    var host = findFilePanel();
    if (!state.open) {
      var old = document.getElementById(NS + "-panel");
      if (old && old.parentNode) old.parentNode.removeChild(old);
      state.renderSig = "closed";
      return;
    }
    if (!host) return;
    ensureStyle();
    var panel = document.getElementById(NS + "-panel");
    var sig = panelSignature();
    // 已挂载且内容未变 → 一个 DOM 都不动（避免 MutationObserver/轮询引发的反复重建）
    if (panel && panel.parentNode === host && state.renderSig === sig) return;
    state.renderSig = sig;

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
    // 纯符号按钮（与原生工具栏一致，文字仅作 tooltip / 无障碍标签）
    var btn = iconButton("refresh", state.loading ? "刷新中…" : "刷新");
    if (state.loading) btn.className = NS + "-iconbtn " + NS + "-iconbtn--busy";
    btn.onclick = function () { refresh(true); };
    head.appendChild(btn);
    var close = iconButton("close", "关闭");
    close.onclick = function () { setOpen(false); };
    head.appendChild(close);
    panel.appendChild(head);

    // 内容
    var body = el("div", NS + "-body");
    if (state.error) {
      body.appendChild(el("div", NS + "-err", state.error));
    }
    if (!data) {
      // 骨架：面板一打开就有结构（三个区块标题 + 读取提示），不让人对着空面板等
      [
        ["子智能体", "读取中…"],
        ["后台任务", "读取中…"],
        ["后台进程", "读取中…"]
      ].forEach(function (sec) {
        var box = el("div", NS + "-sect");
        box.appendChild(el("div", NS + "-secthead", sec[0]));
        box.appendChild(el("div", NS + "-empty " + NS + "-skeleton", sec[1]));
        body.appendChild(box);
      });
      if (!state.loading) {
        body.appendChild(el("div", NS + "-empty", "暂无数据"));
      }
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
        var card = el("div", NS + "-card " + NS + "-card--sub" + (active ? " " + NS + "-card--active" : ""));
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

    // 2) 后台任务（DSH session/jobs）：含状态——运行中 / 已完成 / 失败 / 未启动
    var secJobs = el("div", NS + "-sect");
    var jobs = data.jobs || [];
    var runningJobs = jobs.filter(function (j) { return j.running; }).length;
    secJobs.appendChild(el("div", NS + "-secthead",
      "后台任务（" + jobs.length + (runningJobs ? "，运行中 " + runningJobs : "") + "）"));
    if (!jobs.length) {
      secJobs.appendChild(el("div", NS + "-empty",
        "当前会话没有后台任务。\n由工具启动的后台命令（pwsh / bash 等）会显示在这里，并带运行状态。"));
    } else {
      jobs.forEach(function (j) {
        var card = el("div", NS + "-card " + NS + "-card--job" + (j.running ? " " + NS + "-card--active" : ""));
        var r1 = el("div", NS + "-row1");
        r1.appendChild(statusChip(j));
        r1.appendChild(el("span", NS + "-label", j.id || ""));
        r1.appendChild(el("span", NS + "-tag", j.kind || ""));
        if (j.durationMs >= 0) {
          r1.appendChild(el("span", NS + "-meta", humanMs(j.durationMs)));
        }
        card.appendChild(r1);
        if (j.label) {
          var cmd = el("div", NS + "-jobcmd", j.label);
          cmd.title = j.label;
          card.appendChild(cmd);
        }
        if (j.detail) {
          card.appendChild(el("div", NS + "-meta", j.detail));
        }
        secJobs.appendChild(card);
      });
    }
    body.appendChild(secJobs);

    // 3) 后台进程：标题 + 详情开关；**收起时只显示汇总，展开后才逐条列出具体进程**
    var sec2 = el("div", NS + "-sect");
    var procs = data.processes || [];
    var head2 = el("div", NS + "-sectrow");
    head2.appendChild(el("div", NS + "-secthead", "后台进程（" + procs.length + "）"));
    if (procs.length) {
      var toggle = iconButton(state.procDetail ? "chevron-down" : "chevron-right",
        state.procDetail ? "收起（只看汇总）" : "展开（列出具体进程）");
      toggle.className = NS + "-iconbtn " + NS + "-secttoggle";
      toggle.setAttribute("aria-pressed", state.procDetail ? "true" : "false");
      toggle.onclick = function (ev) {
        if (ev && ev.stopPropagation) ev.stopPropagation();
        state.procDetail = !state.procDetail;
        saveProcDetail(state.procDetail);
        diag("proc-detail", state.procDetail ? "on" : "off");
        state.renderSig = null;   // 强制重绘（签名含 D0/D1）
        renderPanel();
      };
      head2.appendChild(toggle);
    }
    sec2.appendChild(head2);
    if (!procs.length) {
      sec2.appendChild(el("div", NS + "-empty", "未发现与本项目/DSH 相关的进程。"));
    } else if (state.procDetail) {
      // 展开：逐条列出具体后台进程
      procs.forEach(function (p) {
        var row = el("div", NS + "-procrow");
        row.appendChild(el("span", NS + "-procname", p.name || ""));
        row.appendChild(el("span", NS + "-procid", "PID " + p.pid));
        row.appendChild(el("span", NS + "-procmem", p.memMb ? Math.round(p.memMb) + " MB" : ""));
        row.appendChild(el("span", NS + "-tag", p.category || ""));
        if (p.cmd) row.title = p.cmd;   // 悬停可看完整命令行
        sec2.appendChild(row);
      });
    } else {
      // 收起：只报汇总（分类计数 + 内存合计），不列具体进程
      sec2.appendChild(el("div", NS + "-procsummary", procSummary(procs)));
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
    if (tab) tab.className = tabClassName();
    if (state.open) {
      diag("panel-open");
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
    var text = String(n);
    // 只在数值真的变化时才写 DOM（减少无谓改动）
    if (badge.textContent !== text) badge.textContent = text;
    var disp = n > 0 ? "" : "none";
    if (badge.style.display !== disp) badge.style.display = disp;
  }

  // ===== tab 按钮（插入到 React 的 tab 栏里；被 React 重排删除时由 scan 补回）=====
  function tabClassName() {
    var cls = NS + "-tab";
    if (state.isDock) cls += " " + NATIVE_TAB_CLASS;   // 融入右栏原生视图 tab 样式
    if (state.open) cls += " " + NS + "-tab--on";
    return cls;
  }

  function ensureTab() {
    var m = findTabBar();
    if (!m) { diag("no-tab-bar"); return; }
    diag("tab-bar-found", m.selector);
    var bar = m.node;
    // 事件委托：点击"我们的按钮以外"的原生视图 tab（概览/远程/终端…）→ 收起我们的面板。
    // 用捕获阶段，保证在任何 React 处理器之前响应；只读判断，不改动 React 节点。
    if (!bar.getAttribute(NS + "-delegated")) {
      bar.setAttribute(NS + "-delegated", "1");
      bar.addEventListener("click", function (ev) {
        var t = ev.target, mine = false;
        while (t && t !== bar) {
          if (t.id === NS + "-tab") { mine = true; break; }
          t = t.parentNode;
        }
        if (!mine && state.open) setOpen(false);
      }, true);
    }
    if (document.getElementById(NS + "-tab")) {
      var ex = document.getElementById(NS + "-tab");
      if (ex.className !== tabClassName()) ex.className = tabClassName();
      return;
    }
    ensureStyle();
    state.isDock = m.selector.indexOf("workbench-dock") >= 0;
    var btn = el("button", tabClassName());
    btn.id = NS + "-tab";
    btn.type = "button";
    btn.setAttribute("role", "tab");
    // 与原生 tab 相同结构：图标 + workbench-dock__tab-label 文本
    btn.appendChild(tabIcon());
    var labelCls = state.isDock ? "workbench-dock__tab-label " + NS + "-tab-text" : NS + "-tab-text";
    btn.appendChild(el("span", labelCls, TAB_LABEL));
    var badge = el("span", NS + "-badge");
    badge.id = NS + "-badge";
    badge.style.display = "none";
    btn.appendChild(badge);
    btn.onclick = function (ev) {
      if (ev && ev.stopPropagation) ev.stopPropagation();
      setOpen(!state.open);
    };
    bar.appendChild(btn);
    diag("tab-injected", m.selector);
    updateBadge();
  }

  function teardownIfDetached() {
    // 面板宿主消失（切到别的视图/布局重排）时移除 overlay，并同步把状态置为关闭，
    // 否则会出现"状态说开着、DOM 已被移除"的不一致（下次轮询又会重建 → 闪烁）。
    var panel = document.getElementById(NS + "-panel");
    if (panel && state.open && !findFilePanel()) {
      if (panel.parentNode) panel.parentNode.removeChild(panel);
      state.open = false;
      state.renderSig = "closed";
      if (state.timer) { window.clearInterval(state.timer); state.timer = null; }
      var tab = document.getElementById(NS + "-tab");
      if (tab) tab.className = tabClassName();
    }
  }

  var scanTimer = null;
  // scan 只做"轻量维持"：确保 tab 在、宿主失联时收掉 overlay。
  // ⚠️ 这里绝不重建面板 DOM —— 早期版本在 scan 里重建 + MutationObserver 观察整个文档，
  // 造成"改 DOM → 观察者回调 → 再改 DOM"的自激循环，把渲染主线程打死（界面冻结）。
  // 现在：面板内容只在 打开/数据到达/手动刷新 时按"内容签名"决定是否重建，且不再使用
  // MutationObserver（纯轮询足以在被 React 重排后补回 tab，且不会自激）。
  function scan() {
    try {
      ensureTab();
      updateBadge();          // 值未变时不写 DOM（updateBadge 内部有比对）
      // 恢复"补挂载"：面板该开着却没挂上（首次挂载撞上 React 重建宿主、或宿主被换掉）时重试。
      // 安全性：renderPanel 现在有内容签名比对——内容未变时一个 DOM 都不动，因此周期调用不会
      // 重蹈 MutationObserver 自激的覆辙（那次的根因是"观察者回调 → 重建 DOM → 再回调"）。
      renderPanel();
      teardownIfDetached();
    } catch (e) {
      diag("scan-error", (e && e.message) ? e.message : String(e));
    }
  }

  function boot() {
    diag("boot", "readyState=" + document.readyState);
    scan();
    scanTimer = window.setInterval(scan, 2000);
    document.addEventListener("visibilitychange", function () {
      // 从后台切回来时补一次（避免长时间不可见导致的陈旧状态）
      if (!document.hidden) scan();
    });
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
