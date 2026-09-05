// dsh-inline-editor.js — 就地代码编辑：在原生文件预览处直接编辑 + 保存。
// 用户点开文件看到预览的地方就能改（不另开面板）。
// 机制：探测原生预览容器(.workspace-preview__body)，注入「✏️ 编辑」按钮；
// 点击后把当前文件(来自 .workspace-tree__row--active 的 data-workspace-path)内容
// 载入 textarea 就地编辑；Ctrl+S 或「保存」→ WriteFileForTab 写回后恢复预览。
//
// ★ React 安全：绝不删除/清空 React 渲染的节点（removeChild 会崩）。
// 编辑时在预览容器上叠加一个绝对定位的覆盖层(overlay)，React 的 CodeViewer 原样保留；
// 退出时只移除自己创建的 overlay 节点。React 卸载其节点时 DOM 完好。
;
(function () {
  "use strict";
  if (window.__DSH_INLINE_EDITOR__) return;
  window.__DSH_INLINE_EDITOR__ = true;
  var NS = "dsh-ie";

  function A() {
    try { return window.go && window.go.main && window.go.main.App ? window.go.main.App : null; }
    catch (e) { return null; }
  }

  var css = [
    "." + NS + "-btn{display:inline-flex;align-items:center;gap:4px;margin-left:6px;padding:3px 9px;border-radius:6px;border:1px solid rgba(128,128,128,.35);background:rgba(28,29,33,.85);color:#e8eaed;font:600 11px Inter,system-ui,sans-serif;cursor:pointer;white-space:nowrap;line-height:1.6}",
    "." + NS + "-btn:hover{background:#3b3f46}",
    "." + NS + "-btn.save{background:#0153e5;border-color:#0153e5;color:#fff}",
    "." + NS + "-btn.save:hover{background:#1a63f0}",
    // 覆盖层：absolute 盖住预览容器(其 position 需为 relative——预览容器通常是，保险起见用 inset:0 由 JS 定位)
    "." + NS + "-ovl{position:absolute;inset:0;z-index:50;display:flex;flex-direction:column;background:#15161a;color:#dbe2ea;font:13px Inter,system-ui,sans-serif}",
    "." + NS + "-bar{display:flex;align-items:center;gap:8px;padding:6px 12px;background:rgba(255,255,255,.04);border-bottom:1px solid rgba(128,128,128,.18);font:11.5px Inter,system-ui,sans-serif;color:#9aa3af;flex:0 0 auto}",
    "." + NS + "-bar .nm{color:#cbd5e1;font-weight:600;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;max-width:60%}",
    "." + NS + "-bar .sp{flex:1}",
    "." + NS + "-bar .ht{color:#6b7280}",
    "." + NS + "-ta{flex:1;width:100%;box-sizing:border-box;resize:none;border:0;outline:none;background:#15161a;color:#dbe2ea;padding:14px 16px;font:13px/1.6 Consolas,monospace;white-space:pre;overflow:auto;tab-size:2;display:block}",
  ].join("\n");
  if (!document.getElementById(NS + "-css")) {
    var s = document.createElement("style");
    s.id = NS + "-css";
    s.textContent = css;
    document.head.appendChild(s);
  }

  var overlay = null;   // 当前编辑覆盖层(自己创建的，可安全移除)
  var relPath = "";
  var tabId = "";

  function toast(m) {
    var el = document.createElement("div");
    el.style.cssText = "position:fixed;bottom:24px;left:50%;transform:translateX(-50%);z-index:2147483005;background:#1c1d21;color:#f4f4f3;border:1px solid #3d4450;border-radius:8px;padding:8px 16px;font:13px Inter,sans-serif;box-shadow:0 4px 16px rgba(0,0,0,.5);opacity:0;transition:opacity .2s;pointer-events:none";
    el.textContent = m;
    document.body.appendChild(el);
    requestAnimationFrame(function () { el.style.opacity = "1"; });
    setTimeout(function () { el.style.opacity = "0"; setTimeout(function () { el.remove(); }, 220); }, 2200);
  }

  function resolveTab(cb) {
    var app = A();
    if (!app || !app.Tabs) return cb("");
    app.Tabs().then(function (tabs) {
      var arr = Array.isArray(tabs) ? tabs : (tabs && tabs.items) ? tabs.items : [];
      for (var i = 0; i < arr.length; i++) { if (arr[i] && arr[i].active) { cb(arr[i].tabId || ""); return; } }
      if (arr[0]) cb(arr[0].tabId || ""); else cb("");
    }).catch(function () { cb(""); });
  }

  function currentRel() {
    var active = document.querySelector(".workspace-tree__row--active[data-workspace-path]");
    return active ? (active.getAttribute("data-workspace-path") || "") : "";
  }

  function previewBody() {
    return document.querySelector(".workspace-preview__body") || null;
  }

  function windowActions() {
    return document.querySelector(".workspace-preview__window-actions") || null;
  }

  function ensureEditBtn() {
    var actions = windowActions();
    if (!actions || actions.querySelector("." + NS + "-btn")) return;
    var b = document.createElement("button");
    b.type = "button";
    b.className = NS + "-btn";
    b.title = "就地编辑此文件（Ctrl+S 保存，Esc 退出）";
    b.textContent = overlay ? "退出编辑" : "✏️ 编辑";
    b.onclick = function () { overlay ? exitEdit() : enterEdit(); };
    actions.appendChild(b);
  }

  function enterEdit() {
    if (overlay) return;
    var rel = currentRel();
    if (!rel) { toast("未识别当前文件，请先在文件树选中文件"); return; }
    var app = A();
    if (!app || !app.ReadFileForTab) return;
    resolveTab(function (tid) {
      tabId = tid;
      relPath = rel;
      app.ReadFileForTab(tid, rel).then(function (pv) {
        if (pv && pv.err) { toast("读取失败: " + pv.err); return; }
        if (pv && pv.binary) { toast("二进制文件不能文本编辑"); return; }
        var body = previewBody();
        if (!body) { toast("预览区不可用"); return; }
        // 确保 body 是定位上下文（非 absolute 时临时置 relative；不碰其子节点）
        if (getComputedStyle(body).position === "static") body.style.position = "relative";
        // 建覆盖层（仅追加，不动 React 子节点）
        var ovl = document.createElement("div");
        ovl.className = NS + "-ovl";
        var bar = document.createElement("div");
        bar.className = NS + "-bar";
        var nm = document.createElement("span");
        nm.className = "nm";
        nm.textContent = rel;
        var sp = document.createElement("span");
        sp.className = "sp";
        var ht = document.createElement("span");
        ht.className = "ht";
        ht.textContent = pv && pv.truncated ? "内容超限已截断，保存会覆盖" : "Tab 缩进 · Ctrl+S 保存 · Esc 退出";
        var save = document.createElement("button");
        save.type = "button";
        save.className = NS + "-btn save";
        save.textContent = "保存";
        save.onclick = doSave;
        bar.appendChild(nm);
        bar.appendChild(sp);
        bar.appendChild(ht);
        bar.appendChild(save);
        var ta = document.createElement("textarea");
        ta.className = NS + "-ta";
        ta.spellcheck = false;
        ta.value = (pv && pv.body) || "";
        ta.addEventListener("keydown", function (e) {
          if (e.key === "Tab") {
            e.preventDefault();
            var s = ta.selectionStart, en = ta.selectionEnd;
            ta.value = ta.value.slice(0, s) + "  " + ta.value.slice(en);
            ta.selectionStart = ta.selectionEnd = s + 2;
          }
          if (e.key === "s" && (e.ctrlKey || e.metaKey)) { e.preventDefault(); doSave(); }
          if (e.key === "Escape") { e.preventDefault(); exitEdit(); }
        });
        ovl.appendChild(bar);
        ovl.appendChild(ta);
        body.appendChild(ovl);   // append 安全：React 只管理自己的子节点
        overlay = ovl;
        var b = document.querySelector("." + NS + "-btn");
        if (b) b.textContent = "退出编辑";
        ta.focus();
      }).catch(function (err) { toast("读取失败: " + String(err && err.message || err)); });
    });
  }

  function currentTA() {
    return overlay ? overlay.querySelector("." + NS + "-ta") : null;
  }

  function doSave() {
    var ta = currentTA();
    if (!ta || !relPath || !overlay) return;
    var app = A();
    if (!app || !app.WriteFileForTab) { toast("WriteFileForTab 不可用"); return; }
    var sb = overlay.querySelector("." + NS + "-btn.save");
    if (sb) { sb.disabled = true; sb.textContent = "保存中…"; }
    app.WriteFileForTab(tabId, relPath, ta.value).then(function (r) {
      if (r && r.ok) {
        toast("✓ 已保存 " + relPath);
        exitEdit();
        // 触发原生重读显示新内容
        var active = document.querySelector(".workspace-tree__row--active");
        if (active) { try { active.click(); } catch (e) {} }
      } else {
        toast("保存失败: " + ((r && r.error) || "未知错误"));
        if (sb) { sb.disabled = false; sb.textContent = "保存"; }
      }
    }).catch(function (err) {
      toast("保存失败: " + String(err && err.message || err));
      if (sb) { sb.disabled = false; sb.textContent = "保存"; }
    });
  }

  // 退出编辑：仅移除自己的 overlay（React 节点零触碰）
  function exitEdit() {
    if (overlay && overlay.parentNode) overlay.parentNode.removeChild(overlay);
    overlay = null;
    relPath = "";
    var b = document.querySelector("." + NS + "-btn");
    if (b) b.textContent = "✏️ 编辑";
  }

  var lastRel = "";
  function scan() {
    ensureEditBtn();
    var rel = currentRel();
    if (rel !== lastRel) {
      // 文件切换：若在编辑则退出（overlay 可能已被 React 重建的 body 移除——这里兜底）
      if (overlay && rel && rel !== relPath) exitEdit();
      lastRel = rel;
    }
    // overlay 节点被外部移除(React 重建 body)而状态残留 → 复位
    if (overlay && !overlay.isConnected) { overlay = null; relPath = ""; var bb = document.querySelector("." + NS + "-btn"); if (bb) bb.textContent = "✏️ 编辑"; }
  }

  function boot() {
    var timer = null;
    var obs = new MutationObserver(function () {
      if (timer) clearTimeout(timer);
      timer = setTimeout(scan, 200);
    });
    obs.observe(document.documentElement, { childList: true, subtree: true });
    setTimeout(scan, 1500);
    setInterval(scan, 3000);
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", boot);
  } else {
    boot();
  }
})();
