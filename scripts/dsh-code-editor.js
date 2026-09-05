// dsh-code-editor.js — 代码编辑器（本地文件实时编辑 + 保存）。
// 右侧文件栏原生只读预览；此注入提供内嵌编辑器：文件树 → 点文件 →
// 载入内容到等宽 textarea → 编辑（Tab 缩进 / Ctrl+S 保存）→ WriteFileForTab 写回。
// 自包含：不依赖原生 DOM 状态；从 Tabs() 活动会话取工作区根，ListDirForTab 列目录，
// ReadFileForTab 读文件，WriteFileForTab 保存（Go 端已实现，防穿越 + 二进制拒绝）。
;
(function () {
  "use strict";
  if (window.__DSH_CODE_EDITOR__) return;
  window.__DSH_CODE_EDITOR__ = true;
  var NS = "dsh-ce";

  function A() {
    try { return window.go && window.go.main && window.go.main.App ? window.go.main.App : null; }
    catch (e) { return null; }
  }

  var css = [
    "." + NS + "-fab{position:fixed;right:16px;top:64px;z-index:2147483001;display:inline-flex;align-items:center;gap:6px;padding:7px 12px;border-radius:9px;border:1px solid rgba(128,128,128,.4);background:rgba(24,25,29,.95);color:#f4f4f3;font:600 12px Inter,system-ui,sans-serif;cursor:pointer;box-shadow:0 4px 16px rgba(0,0,0,.4)}",
    "." + NS + "-fab:hover{background:#3b3f46}",
    "." + NS + "-ovl{position:fixed;inset:0;z-index:2147483002;background:rgba(0,0,0,.55);display:flex;align-items:center;justify-content:center;padding:28px}",
    "." + NS + "-box{background:rgba(23,24,28,.99);color:#e8eaed;border:1px solid rgba(128,128,128,.45);border-radius:14px;width:min(1100px,96vw);height:min(88vh,820px);display:flex;flex-direction:column;box-shadow:0 16px 64px rgba(0,0,0,.7);font:13px Inter,system-ui,sans-serif;overflow:hidden}",
    "." + NS + "-head{display:flex;align-items:center;gap:8px;padding:9px 12px;border-bottom:1px solid rgba(128,128,128,.25);background:rgba(255,255,255,.02)}",
    "." + NS + "-title{font-weight:600;font-size:13px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}",
    "." + NS + "-crumb{color:#8b949e;font-size:11px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;direction:rtl;text-align:left;flex:1}",
    "." + NS + "-x{cursor:pointer;color:#9aa3af;font-size:15px;padding:0 5px;font-style:normal}",
    "." + NS + "-x:hover{color:#fff}",
    "." + NS + "-body{flex:1;display:flex;min-height:0}",
    "." + NS + "-tree{width:230px;min-width:160px;overflow:auto;border-right:1px solid rgba(128,128,128,.2);padding:6px;background:rgba(0,0,0,.15)}",
    "." + NS + "-tree-item{display:flex;align-items:center;gap:6px;padding:3px 6px;border-radius:5px;cursor:pointer;font-size:12px;color:#d1d5db;white-space:nowrap}",
    "." + NS + "-tree-item:hover{background:rgba(255,255,255,.07)}",
    "." + NS + "-tree-item.sel{background:rgba(1,83,229,.35)}",
    "." + NS + "-tree-dir{color:#7dd3fc}",
    "." + NS + "-tree-back{color:#9aa3af;font-style:italic}",
    "." + NS + "-edit{flex:1;display:flex;flex-direction:column;min-width:0}",
    "." + NS + "-ta{flex:1;width:100%;resize:none;border:0;outline:none;background:#15161a;color:#dbe2ea;padding:12px 14px;font:13px/1.6 Consolas,'Courier New',monospace;white-space:pre;overflow:auto;tab-size:2}",
    "." + NS + "-empty{flex:1;display:flex;align-items:center;justify-content:center;color:#6b7280;font-size:13px}",
    "." + NS + "-foot{display:flex;align-items:center;gap:10px;padding:7px 12px;border-top:1px solid rgba(128,128,128,.2);font-size:11.5px;color:#9aa3af}",
    "." + NS + "-status{flex:1;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}",
    "." + NS + "-save{cursor:pointer;background:#0153e5;color:#fff;border:0;border-radius:6px;padding:5px 16px;font:600 12px Inter,sans-serif}",
    "." + NS + "-save:hover{background:#1a63f0}",
    "." + NS + "-save:disabled{opacity:.5;cursor:default}",
  ].join("\n");
  var styleEl = document.createElement("style");
  styleEl.id = NS + "-css";
  styleEl.textContent = css;
  document.head.appendChild(styleEl);

  var state = { tabId: "", root: "", cur: "", rel: "", dirty: false, editing: false };

  function toast(msg) {
    var el = document.createElement("div");
    el.style.cssText = "position:fixed;bottom:24px;left:50%;transform:translateX(-50%);z-index:2147483005;background:#1c1d21;color:#f4f4f3;border:1px solid #3d4450;border-radius:8px;padding:8px 16px;font:13px Inter,sans-serif;box-shadow:0 4px 16px rgba(0,0,0,.5);opacity:0;transition:opacity .2s";
    el.textContent = msg;
    document.body.appendChild(el);
    requestAnimationFrame(function () { el.style.opacity = "1"; });
    setTimeout(function () { el.style.opacity = "0"; setTimeout(function () { el.remove(); }, 220); }, 2000);
  }

  // 从 Tabs() 找活动 tab 的工作区根
  function resolveRoot(cb) {
    var app = A();
    if (!app || !app.Tabs) return cb("");
    app.Tabs().then(function (tabs) {
      var arr = Array.isArray(tabs) ? tabs : (tabs && tabs.items) ? tabs.items : [];
      var act = null;
      for (var i = 0; i < arr.length; i++) {
        var t = arr[i] || {};
        if (t.active) { act = t; break; }
      }
      if (!act && arr[0]) act = arr[0];
      cb(act ? (act.workspaceRoot || act.cwd || "") : "");
    }).catch(function () { cb(""); });
  }

  var box = null, treeEl = null, ta = null, statusEl = null, saveBtn = null;

  function openEditor() {
    if (document.getElementById(NS + "-ovl")) return;
    resolveRoot(function (root) {
      state.root = root || "";
      buildUI();
    });
  }

  function buildUI() {
    var app = A();
    var ovl = document.createElement("div");
    ovl.id = NS + "-ovl";
    ovl.className = NS + "-ovl";
    box = document.createElement("div");
    box.className = NS + "-box";
    var head = document.createElement("div");
    head.className = NS + "-head";
    var title = document.createElement("span");
    title.className = NS + "-title";
    title.textContent = "代码编辑器" + (state.root ? " · " + state.root : "");
    var crumb = document.createElement("span");
    crumb.className = NS + "-crumb";
    crumb.id = NS + "-crumb";
    crumb.textContent = "← 点左侧文件编辑";
    var x = document.createElement("span");
    x.className = NS + "-x";
    x.textContent = "✕";
    x.onclick = closeEditor;
    head.appendChild(title);
    head.appendChild(crumb);
    head.appendChild(x);
    var body = document.createElement("div");
    body.className = NS + "-body";
    treeEl = document.createElement("div");
    treeEl.className = NS + "-tree";
    var editPane = document.createElement("div");
    editPane.className = NS + "-edit";
    var empty = document.createElement("div");
    empty.className = NS + "-empty";
    empty.id = NS + "-empty";
    empty.textContent = state.root ? "从左侧选择文件开始编辑" : "未能定位工作区根（Tabs 无活动会话）";
    ta = document.createElement("textarea");
    ta.className = NS + "-ta";
    ta.id = NS + "-ta";
    ta.spellcheck = false;
    ta.style.display = "none";
    ta.addEventListener("input", function () { state.dirty = true; syncFoot(); });
    ta.addEventListener("keydown", function (e) {
      // Tab 插入两空格缩进
      if (e.key === "Tab") {
        e.preventDefault();
        var s = ta.selectionStart, en = ta.selectionEnd;
        ta.value = ta.value.slice(0, s) + "  " + ta.value.slice(en);
        ta.selectionStart = ta.selectionEnd = s + 2;
        state.dirty = true;
        syncFoot();
      }
      // Ctrl/Cmd+S 保存
      if (e.key === "s" && (e.ctrlKey || e.metaKey)) {
        e.preventDefault();
        saveFile();
      }
    });
    editPane.appendChild(empty);
    editPane.appendChild(ta);
    body.appendChild(treeEl);
    body.appendChild(editPane);
    var foot = document.createElement("div");
    foot.className = NS + "-foot";
    statusEl = document.createElement("span");
    statusEl.className = NS + "-status";
    saveBtn = document.createElement("button");
    saveBtn.className = NS + "-save";
    saveBtn.textContent = "保存 (Ctrl+S)";
    saveBtn.disabled = true;
    saveBtn.onclick = saveFile;
    foot.appendChild(statusEl);
    foot.appendChild(saveBtn);
    box.appendChild(head);
    box.appendChild(body);
    box.appendChild(foot);
    ovl.appendChild(box);
    document.body.appendChild(ovl);
    ovl.addEventListener("mousedown", function (e) { if (e.target === ovl) closeEditor(); });
    loadDir("");
  }

  function closeEditor() {
    if (state.dirty) {
      if (!window.confirm("有未保存的修改，确定关闭编辑器？")) return;
    }
    var ovl = document.getElementById(NS + "-ovl");
    if (ovl) ovl.remove();
    box = treeEl = ta = statusEl = saveBtn = null;
    state.cur = state.rel = ""; state.dirty = false; state.editing = false;
  }

  function syncFoot() {
    if (!statusEl) return;
    if (!state.cur) { statusEl.textContent = "就绪"; saveBtn.disabled = true; return; }
    statusEl.textContent = (state.dirty ? "● 已修改" : "○") + "  " + state.cur;
    saveBtn.disabled = !state.dirty || !state.editing;
  }

  // 列目录（相对 root）
  function loadDir(rel) {
    var app = A();
    if (!app || !app.ListDirForTab) {
      treeEl.textContent = "ListDirForTab 不可用";
      return;
    }
    if (!state.root) { treeEl.textContent = "工作区根为空"; return; }
    treeEl.innerHTML = "";
    var back = document.createElement("div");
    back.className = NS + "-tree-item " + NS + "-tree-back";
    back.textContent = "← 上级";
    back.onclick = function () {
      var p = rel.split("/"); p.pop();
      loadDir(p.join("/"));
    };
    if (rel) treeEl.appendChild(back);
    statusEl.textContent = "加载 " + (rel || "（根）") + " …";
    app.ListDirForTab(state.tabId, rel).then(function (entries) {
      treeEl.innerHTML = "";
      if (rel) {
        var b2 = document.createElement("div");
        b2.className = NS + "-tree-item " + NS + "-tree-back";
        b2.textContent = "← 上级";
        b2.onclick = function () { var p = rel.split("/"); p.pop(); loadDir(p.join("/")); };
        treeEl.appendChild(b2);
      }
      var arr = Array.isArray(entries) ? entries : [];
      arr.sort(function (x, y) {
        var xd = x.isDir ? 0 : 1, yd = y.isDir ? 0 : 1;
        return xd === yd ? (x.name < y.name ? -1 : 1) : xd - yd;
      });
      arr.forEach(function (it) {
        var row = document.createElement("div");
        row.className = NS + "-tree-item" + (it.isDir ? " " + NS + "-tree-dir" : "");
        row.textContent = (it.isDir ? "📁 " : "📄 ") + it.name;
        row.onclick = function () {
          var path = rel ? rel + "/" + it.name : it.name;
          if (it.isDir) loadDir(path);
          else openFile(path);
        };
        treeEl.appendChild(row);
      });
      if (!arr.length) {
        var e = document.createElement("div");
        e.className = NS + "-tree-item";
        e.textContent = "（空）";
        e.style.color = "#6b7280";
        treeEl.appendChild(e);
      }
      syncFoot();
    }).catch(function (err) {
      treeEl.innerHTML = "";
      var e = document.createElement("div");
      e.className = NS + "-tree-item";
      e.textContent = "加载失败: " + String(err && err.message || err);
      e.style.color = "#f87171";
      treeEl.appendChild(e);
    });
  }

  // 打开文件（相对 root）→ 载入内容
  function openFile(rel) {
    var app = A();
    if (!app || !app.ReadFileForTab) return;
    if (state.dirty) {
      if (!window.confirm("当前文件有未保存修改，切换将丢失，继续？")) return;
    }
    state.rel = rel;
    statusEl.textContent = "读取 " + rel + " …";
    app.ReadFileForTab(state.tabId, rel).then(function (pv) {
      if (pv && pv.err) {
        toast("读取失败: " + pv.err);
        return;
      }
      if (pv && pv.binary) {
        toast("二进制文件不能文本编辑（用外部程序打开）");
        state.cur = state.rel = "";
        state.editing = false;
        syncFoot();
        return;
      }
      if (pv && pv.truncated) {
        toast("文件过大已截断预览，保存将覆盖为截断内容");
      }
      state.cur = rel;
      state.editing = true;
      state.dirty = false;
      ta.value = (pv && pv.body) || "";
      ta.style.display = "block";
      var empty = document.getElementById(NS + "-empty");
      if (empty) empty.style.display = "none";
      var crumb = document.getElementById(NS + "-crumb");
      if (crumb) crumb.textContent = rel;
      ta.focus();
      syncFoot();
    }).catch(function (err) {
      toast("读取失败: " + String(err && err.message || err));
    });
  }

  function saveFile() {
    if (!state.editing || !state.cur || !state.dirty) return;
    var app = A();
    if (!app || !app.WriteFileForTab) {
      toast("WriteFileForTab 不可用（需重新构建）");
      return;
    }
    saveBtn.disabled = true;
    statusEl.textContent = "保存中 …";
    app.WriteFileForTab(state.tabId, state.rel, ta.value).then(function (r) {
      if (r && r.ok) {
        state.dirty = false;
        statusEl.textContent = "✓ 已保存 " + state.cur + (r.size != null ? "（" + r.size + " 字节）" : "");
        toast("✓ 已保存 " + state.cur);
      } else {
        statusEl.textContent = "保存失败: " + ((r && r.error) || "未知错误");
        toast("保存失败: " + ((r && r.error) || "未知错误"));
      }
      saveBtn.disabled = !state.dirty;
    }).catch(function (err) {
      statusEl.textContent = "保存失败: " + String(err && err.message || err);
      toast("保存失败");
      saveBtn.disabled = false;
    });
  }

  // 浮动入口按钮
  function addFab() {
    var fab = document.createElement("button");
    fab.className = NS + "-fab";
    fab.id = NS + "-fab";
    fab.textContent = "✏️ 编辑代码";
    fab.title = "代码编辑器：浏览/编辑/保存工作区文件（Ctrl+S 保存）";
    fab.onclick = openEditor;
    document.body.appendChild(fab);
  }

  function boot() {
    addFab();
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", boot);
  } else {
    boot();
  }
})();
