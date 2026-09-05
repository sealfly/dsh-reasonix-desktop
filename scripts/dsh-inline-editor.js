// dsh-inline-editor.js — 就地代码编辑：在原生文件预览处直接编辑 + 保存。
// 用户点开文件看到预览的地方就能改（不另开面板）。
// 机制：探测原生预览容器(.workspace-preview__body)，注入「✏️ 编辑」按钮；
// 点击后把当前文件(来自 .workspace-tree__row--active 的 data-workspace-path)内容
// 载入 textarea 就地编辑；Ctrl+S 或「保存」→ WriteFileForTab 写回后恢复预览。
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
    "." + NS + "-ta{box-sizing:border-box;width:100%;height:100%;min-height:240px;resize:none;border:0;outline:none;background:#15161a;color:#dbe2ea;padding:14px 16px;font:13px/1.6 Consolas,monospace;white-space:pre;overflow:auto;tab-size:2;display:block}",
    "." + NS + "-bar{display:flex;align-items:center;gap:8px;padding:5px 12px;background:rgba(255,255,255,.03);border-bottom:1px solid rgba(128,128,128,.15);font:11.5px Inter,system-ui,sans-serif;color:#9aa3af}",
    "." + NS + "-bar .name{color:#cbd5e1;font-weight:600;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}",
    "." + NS + "-bar .sp{flex:1}",
    "." + NS + "-hint{color:#6b7280}",
  ].join("\n");
  if (!document.getElementById(NS + "-css")) {
    var s = document.createElement("style");
    s.id = NS + "-css";
    s.textContent = css;
    document.head.appendChild(s);
  }

  var editing = false;   // 当前是否处于编辑态
  var relPath = "";      // 正在编辑的文件 rel
  var tabId = "";

  function toast(m) {
    var el = document.createElement("div");
    el.style.cssText = "position:fixed;bottom:24px;left:50%;transform:translateX(-50%);z-index:2147483005;background:#1c1d21;color:#f4f4f3;border:1px solid #3d4450;border-radius:8px;padding:8px 16px;font:13px Inter,sans-serif;box-shadow:0 4px 16px rgba(0,0,0,.5);opacity:0;transition:opacity .2s;pointer-events:none";
    el.textContent = m;
    document.body.appendChild(el);
    requestAnimationFrame(function () { el.style.opacity = "1"; });
    setTimeout(function () { el.style.opacity = "0"; setTimeout(function () { el.remove(); }, 220); }, 2200);
  }

  // 找当前活动 tabId
  function resolveTab(cb) {
    var app = A();
    if (!app || !app.Tabs) return cb("");
    app.Tabs().then(function (tabs) {
      var arr = Array.isArray(tabs) ? tabs : (tabs && tabs.items) ? tabs.items : [];
      for (var i = 0; i < arr.length; i++) { if (arr[i] && arr[i].active) { cb(arr[i].tabId || ""); return; } }
      if (arr[0]) cb(arr[0].tabId || ""); else cb("");
    }).catch(function () { cb(""); });
  }

  // 找当前预览文件的 rel：树里激活行 data-workspace-path；兜底从文件名反查
  function currentRel() {
    var active = document.querySelector(".workspace-tree__row--active[data-workspace-path]");
    if (active) return active.getAttribute("data-workspace-path") || "";
    return "";
  }

  // 找预览容器（文件内容展示区）
  function previewBody() {
    return document.querySelector(".workspace-preview__body") || null;
  }

  // 找操作按钮区（预览 head 的 window-actions）
  function windowActions() {
    return document.querySelector(".workspace-preview__window-actions") || null;
  }

  // 注入「编辑」按钮到操作区（head 重建后由 observer 重挂）
  function ensureEditBtn() {
    var actions = windowActions();
    if (!actions || actions.querySelector("." + NS + "-btn")) return;
    var b = document.createElement("button");
    b.type = "button";
    b.className = NS + "-btn";
    b.title = "就地编辑此文件（Ctrl+S 保存）";
    b.textContent = editing ? "退出编辑" : "✏️ 编辑";
    b.onclick = function () { editing ? exitEdit() : enterEdit(); };
    actions.appendChild(b);
  }

  // 进入编辑态：读 active 行 rel → ReadFileForTab → body 换 textarea
  function enterEdit() {
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
        // 记住原内容用于取消/比较（只读代码视图已渲染在 body，先隐藏）
        body.__dshPrevHTML = body.innerHTML;
        body.innerHTML = "";
        // 编辑工具条
        var bar = document.createElement("div");
        bar.className = NS + "-bar";
        var nm = document.createElement("span");
        nm.className = "name";
        nm.textContent = rel;
        var sp = document.createElement("span");
        sp.className = "sp";
        var hint = document.createElement("span");
        hint.className = "hint";
        hint.textContent = pv && pv.truncated ? "（内容超限已截断，保存会覆盖）" : "编辑中 · Tab 缩进 · Ctrl+S 保存";
        var save = document.createElement("button");
        save.type = "button";
        save.className = NS + "-btn save";
        save.textContent = "保存";
        save.onclick = doSave;
        bar.appendChild(nm);
        bar.appendChild(sp);
        bar.appendChild(hint);
        bar.appendChild(save);
        // textarea
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
          if (e.key === "Escape") { exitEdit(); }
        });
        body.appendChild(bar);
        body.appendChild(ta);
        editing = true;
        ta.focus();
        // 按钮状态
        var b = document.querySelector("." + NS + "-btn");
        if (b) b.textContent = "退出编辑";
      }).catch(function (err) { toast("读取失败: " + String(err && err.message || err)); });
    });
  }

  function currentTA() {
    var body = previewBody();
    return body ? body.querySelector("." + NS + "-ta") : null;
  }

  function doSave() {
    var ta = currentTA();
    if (!ta || !relPath) return;
    var app = A();
    if (!app || !app.WriteFileForTab) { toast("WriteFileForTab 不可用"); return; }
    var b = document.querySelector("." + NS + "-btn.save");
    if (b) { b.disabled = true; b.textContent = "保存中…"; }
    app.WriteFileForTab(tabId, relPath, ta.value).then(function (r) {
      if (r && r.ok) {
        toast("✓ 已保存 " + relPath);
        exitEdit(true);
      } else {
        toast("保存失败: " + ((r && r.error) || "未知错误"));
        if (b) { b.disabled = false; b.textContent = "保存"; }
      }
    }).catch(function (err) {
      toast("保存失败: " + String(err && err.message || err));
      if (b) { b.disabled = false; b.textContent = "保存"; }
    });
  }

  // 退出编辑态（可选刷新预览）
  function exitEdit(refresh) {
    var body = previewBody();
    if (!body) { editing = false; return; }
    if (body.__dshPrevHTML != null) {
      body.innerHTML = body.__dshPrevHTML;
      body.__dshPrevHTML = null;
    }
    editing = false;
    relPath = "";
    var b = document.querySelector("." + NS + "-btn");
    if (b) b.textContent = "✏️ 编辑";
    // 刷新预览显示新内容（触发原生重读：点同文件行）
    if (refresh) {
      var active = document.querySelector(".workspace-tree__row--active");
      if (active) active.click();
    }
  }

  // 观察预览区：head 重建后重挂按钮；文件切换且处于编辑态则退出
  var lastSig = "";
  function scan() {
    ensureEditBtn();
    // 检测预览文件切换（active 行路径变了）——若在编辑且文件变了则退出
    var rel = currentRel();
    if (rel !== lastSig) {
      if (editing && relPath && rel !== relPath) exitEdit();
      lastSig = rel;
    }
    // 预览 body 被 React 重建(textarea 没了)但 editing 仍 true → 复位
    if (editing && !currentTA()) {
      editing = false;
      var b = document.querySelector("." + NS + "-btn");
      if (b) b.textContent = "✏️ 编辑";
    }
  }

  function boot() {
    // 启动延迟扫描（等 React 挂载）
    var timer = null;
    var obs = new MutationObserver(function () {
      if (timer) clearTimeout(timer);
      timer = setTimeout(scan, 200);
    });
    obs.observe(document.documentElement, { childList: true, subtree: true });
    setTimeout(scan, 1500);
    setInterval(scan, 3000); // 兜底
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", boot);
  } else {
    boot();
  }
})();
