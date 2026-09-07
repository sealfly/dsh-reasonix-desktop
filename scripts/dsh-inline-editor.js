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

  // A() 取 Wails Go 桥对象 window.go.main.App（前端注入上下文里唯一与 Go 通信的入口）。
  // 取不到时返回 null，调用方都做空安全（App 还没就绪时静默跳过，后续 scan 轮询会补上）。
  function A() {
    try { return window.go && window.go.main && window.go.main.App ? window.go.main.App : null; }
    catch (e) { return null; }
  }

  // 全部样式内联进一个 <style>（用 NS 前缀防与前端类名冲突）：
  //  - dsh-ie-btn     预览窗口操作栏里的「✏️ 编辑 / 退出编辑」按钮
  //  - dsh-ie-ovl     编辑覆盖层（absolute 盖住预览容器，不动 React 子树）
  //  - dsh-ie-bar     覆盖层顶栏（文件名 + 快捷键提示 + 保存按钮）
  //  - dsh-ie-ta      覆盖层里的编辑区（textarea，等宽字体，2 空格缩进）
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

  var overlay = null;   // 当前编辑覆盖层元素（只含自己创建的节点，退出时可整体移除）
  var relPath = "";     // 正在编辑的文件相对路径（工作区相对；保存时传给 WriteFileForTab）
  var tabId = "";       // 正在编辑的文件所属会话 tabId（进入编辑时从 Tabs 解析）

  // toast 屏幕底部轻提示（一次性，2.2s 淡出自毁）。
  function toast(m) {
    var el = document.createElement("div");
    el.style.cssText = "position:fixed;bottom:24px;left:50%;transform:translateX(-50%);z-index:2147483005;background:#1c1d21;color:#f4f4f3;border:1px solid #3d4450;border-radius:8px;padding:8px 16px;font:13px Inter,sans-serif;box-shadow:0 4px 16px rgba(0,0,0,.5);opacity:0;transition:opacity .2s;pointer-events:none";
    el.textContent = m;
    document.body.appendChild(el);
    requestAnimationFrame(function () { el.style.opacity = "1"; });
    setTimeout(function () { el.style.opacity = "0"; setTimeout(function () { el.remove(); }, 220); }, 2200);
  }

  // resolveTab 异步取"当前活动会话"的 tabId（写文件需要 tabId → 工作区根）。
  // 优先 active 会话，其次列表第一个；拿不到就回调空串（保存会走绝对路径分支）。
  function resolveTab(cb) {
    var app = A();
    if (!app || !app.Tabs) return cb("");
    app.Tabs().then(function (tabs) {
      // Tabs 返回数组；兼容 {items:[...]} 包裹
      var arr = Array.isArray(tabs) ? tabs : (tabs && tabs.items) ? tabs.items : [];
      for (var i = 0; i < arr.length; i++) { if (arr[i] && arr[i].active) { cb(arr[i].tabId || ""); return; } }
      if (arr[0]) cb(arr[0].tabId || ""); else cb("");
    }).catch(function () { cb(""); });
  }

  // currentRel 从原生文件树的"活动行"取当前文件的相对路径。
  // 原生行 DOM: .workspace-tree__row--active 上带 data-workspace-path（与我们 Go 桥的 rel 同义）。
  function currentRel() {
    var active = document.querySelector(".workspace-tree__row--active[data-workspace-path]");
    return active ? (active.getAttribute("data-workspace-path") || "") : "";
  }

  // previewBody 原生预览内容容器（React 的 CodeViewer/Markdown 渲染都挂在这里）。
  // 编辑覆盖层就叠在这个容器上（它需要是定位上下文，见 enterEdit）。
  function previewBody() {
    return document.querySelector(".workspace-preview__body") || null;
  }

  // windowActions 原生预览窗口的右上操作栏（✏️ 编辑按钮就插进这里）。
  function windowActions() {
    return document.querySelector(".workspace-preview__window-actions") || null;
  }

  // ensureEditBtn 保证操作栏里存在编辑按钮（没有就补一个，已存在则跳过——防重复注入）。
  // 按钮文案按当前是否在编辑态切换（"✏️ 编辑"/"退出编辑"）。
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

  // enterEdit 进入就地编辑：
  // 1) 读活动文件的 rel（没有 → 提示先选文件）；
  // 2) resolveTab 拿到 tabId，调 ReadFileForTab 读当前内容（err/binary → 提示后放弃）；
  // 3) 在预览容器上叠一个自建的 overlay（textarea），React 渲染的预览节点原样保留在下面；
  // 4) 挂自动补全（input 事件 + 键盘导航），Ctrl+S 保存、Esc 退出。
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
        // ---- 自动补全支持 ----
        ta.__lang = langOf(rel);
        ta.addEventListener("input", function () { scheduleComplete(ta, false); });
        ta.addEventListener("keydown", function (e) {
          // 补全弹层开着: Enter/Tab/上下接受或导航
          if (completeBox && completeBox.__ta === ta && (e.key === "ArrowDown" || e.key === "ArrowUp" || e.key === "Enter")) {
            e.preventDefault();
            if (e.key === "ArrowDown") { moveComplete(1); return; }
            if (e.key === "ArrowUp") { moveComplete(-1); return; }
            applyComplete();
            return;
          }
          if (e.key === "Tab") {
            e.preventDefault();
            var s = ta.selectionStart, en = ta.selectionEnd;
            ta.value = ta.value.slice(0, s) + "  " + ta.value.slice(en);
            ta.selectionStart = ta.selectionEnd = s + 2;
          }
          if (e.key === "s" && (e.ctrlKey || e.metaKey)) { e.preventDefault(); doSave(); }
          if (e.key === "Escape") { e.preventDefault(); hideComplete(); if (!completeBox) exitEdit(); }
          if ((e.key === " " || e.key === ".") && (e.ctrlKey || e.metaKey)) { e.preventDefault(); scheduleComplete(ta, true); }
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

  // currentTA 取当前 overlay 里的 textarea（编辑态专用；非编辑态返回 null）。
  function currentTA() {
    return overlay ? overlay.querySelector("." + NS + "-ta") : null;
  }

  // doSave 把 textarea 当前内容经 WriteFileForTab 写回文件：
  // 保存中禁用按钮防重复提交；成功 → 提示 + 退出编辑 + 点一下活动行触发原生重读预览；
  // 失败 → 恢复按钮并提示（不改动已编辑内容，用户可继续改）。
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
        // 触发原生重读显示新内容（模拟点击活动行，让预览区重新拉文件）
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
  // scan 周期性自检（MutationObserver 防抖 200ms + 1.5s 首轮 + 3s 兜底轮询）：
  //  - ensureEditBtn：操作栏出现后补编辑按钮
  //  - 文件切换（活动行 rel 变化）且正在编辑 → 退出编辑
  //  - overlay 被外部移除（React 重建预览容器）而状态残留 → 复位状态与按钮文案
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

  // boot 挂全局 MutationObserver 监听 DOM 变化（React 懒加载的面板随时可能出现），
  // 变化后防抖触发 scan；另设 3s 周期兜底（Observer 漏掉的情况）。
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


  // ===== 自动补全（语言关键字 + 文件内标识符） =====
  var completeBox = null;   // 当前补全弹层
  var completeIdx = 0;
  var completeItems = [];
  var completeTimer = null;

  // 语言关键字表（按扩展名粗分）
  var KEYWORDS = {
    ".go": ["func","package","import","return","if","else","for","range","var","const","type","struct","interface","map","chan","go","defer","select","switch","case","break","continue","fallthrough","default","nil","true","false","len","cap","make","new","append","panic","recover","error","string","int","bool","float64"],
    ".js": ["function","return","var","let","const","if","else","for","while","do","switch","case","break","continue","new","typeof","instanceof","true","false","null","undefined","class","extends","super","import","export","default","try","catch","finally","throw","async","await","yield"],
    ".ts": ["interface","type","enum","namespace","public","private","protected","readonly","abstract","implements","function","return","var","let","const","if","else","for","while","class","extends","import","export","async","await","try","catch","throw","true","false","null","undefined"],
    ".tsx": ["function","return","const","let","var","import","export","default","if","else","for","class","extends","interface","type","async","await","try","catch","throw","true","false","null","undefined","useState","useEffect","useCallback","useMemo","useRef"],
    ".jsx": ["function","return","const","let","var","import","export","default","if","else","for","class","extends","async","await","true","false","null","undefined","useState","useEffect"],
    ".py": ["def","class","return","if","elif","else","for","while","import","from","as","with","try","except","finally","raise","lambda","pass","break","continue","None","True","False","and","or","not","in","is","global","nonlocal","yield","async","await","self","print"],
    ".rs": ["fn","let","mut","const","static","struct","enum","impl","trait","mod","use","pub","return","if","else","match","for","while","loop","break","continue","unsafe","async","await","move","ref","self","true","false","Some","None","Ok","Err","vec","String","Option","Result"],
    ".c": ["int","char","float","double","void","return","if","else","for","while","do","switch","case","break","continue","struct","union","enum","typedef","static","extern","const","unsigned","signed","long","short","sizeof","NULL"],
    ".h": ["int","char","float","double","void","return","if","else","for","while","do","switch","case","break","continue","struct","union","enum","typedef","static","extern","const","unsigned","signed","long","short","sizeof","NULL","define","ifdef","ifndef","endif","include","pragma"],
    ".cpp": ["int","char","float","double","void","return","if","else","for","while","do","switch","case","break","continue","struct","union","enum","typedef","static","extern","const","unsigned","signed","long","short","sizeof","NULL","class","public","private","protected","virtual","new","delete","this","namespace","using","template","typename","auto","nullptr","true","false"],
    ".java": ["public","private","protected","class","interface","enum","extends","implements","return","if","else","for","while","do","switch","case","break","continue","new","this","super","static","final","abstract","void","int","long","double","float","boolean","char","byte","short","String","null","true","false","import","package","try","catch","finally","throw","throws"],
    ".json": ["true","false","null"],
    ".sh": ["if","then","else","elif","fi","for","while","do","done","case","esac","function","return","local","export","echo","exit","read","cd","set","unset","shift","break","continue","source"],
    ".ps1": ["function","param","begin","process","end","if","else","elseif","switch","foreach","for","while","do","until","try","catch","finally","throw","return","new","class","enum","using","filter","workflow","$true","$false","$null","Write-Host","Write-Output","Write-Error","Get-Item","Set-Item","Remove-Item","Get-Content","Set-Content"],
    ".sql": ["SELECT","FROM","WHERE","INSERT","INTO","VALUES","UPDATE","SET","DELETE","CREATE","TABLE","ALTER","DROP","INDEX","VIEW","JOIN","LEFT","RIGHT","INNER","OUTER","ON","GROUP","BY","ORDER","HAVING","LIMIT","OFFSET","AND","OR","NOT","NULL","PRIMARY","KEY","FOREIGN","REFERENCES","DEFAULT","UNIQUE","AS","DISTINCT","COUNT","SUM","AVG","MIN","MAX"],
    ".html": ["div","span","p","a","img","ul","ol","li","table","tr","td","th","form","input","button","select","option","textarea","script","style","link","meta","title","head","body","header","footer","nav","section","article","aside","class","id","href","src","style"],
    ".css": ["color","background","background-color","margin","padding","border","display","position","top","right","bottom","left","width","height","font","font-size","font-weight","font-family","text-align","flex","grid","align-items","justify-content","overflow","opacity","z-index","transition","transform","cursor","float","clear"],
    ".md": ["#","##","###","- ","* ","1. ","`","[","]","(",")","```"],
  };

  // langOf 按文件扩展名取语言（查不到的语言统一按 .txt 处理——无关键字表，只补文件内标识符）。
  function langOf(path) {
    if (!path) return ".txt";
    var i = path.lastIndexOf(".");
    if (i < 0) return ".txt";
    var ext = path.slice(i).toLowerCase();
    return KEYWORDS[ext] ? ext : ".txt";
  }

  // wordBefore 取光标前正在输入的那个词（标识符前缀），用于补全匹配；无则返回 null。
  function wordBefore(ta) {
    var v = ta.value, pos = ta.selectionStart;
    var m = v.slice(0, pos).match(/[A-Za-z_$][A-Za-z0-9_$]*$/);
    return m ? { word: m[0], start: pos - m[0].length } : null;
  }

  // collectIdentifiers 扫描整个 textarea 收集"文件内标识符"（≥3 字符的去重集合），
  // 与语言关键字合并成补全候选——同文件里用过的变量/函数名都能补。
  function collectIdentifiers(ta) {
    var ids = {};
    var m, re = /[A-Za-z_$][A-Za-z0-9_$]*/g;
    while ((m = re.exec(ta.value))) {
      if (m[0].length >= 3) ids[m[0]] = true;
    }
    return Object.keys(ids);
  }

  // scheduleComplete 防抖调度补全弹层：普通输入 220ms 后弹（避免打字抖动），
  // Ctrl+Space 强制立即弹。
  function scheduleComplete(ta, force) {
    if (completeTimer) { clearTimeout(completeTimer); completeTimer = null; }
    completeTimer = setTimeout(function () { showComplete(ta, force); }, force ? 0 : 220);
  }

  // showComplete 计算并显示补全弹层：
  //  - 前提：光标在编辑区 + 有前缀（强制模式除外）
  //  - 候选 = 语言关键字 ∪ 文件内标识符，按前缀过滤排序，最多 40 条
  //  - 弹层定位：按光标前换行数估算行高放到 textarea 上方（固定浮层，跟随粗略即可）
  function showComplete(ta, force) {
    if (document.activeElement !== ta) return;
    var wb = wordBefore(ta);
    var prefix = wb ? wb.word : "";
    // 触发条件: 有词或强制(Ctrl+Space)
    if (!force && prefix.length < 2) { hideComplete(); return; }
    // 候选: 关键字 + 文件标识符
    var ext = ta.__lang || langOf(relPath);
    var set = {};
    (KEYWORDS[ext] || []).forEach(function (k) { set[k] = true; });
    collectIdentifiers(ta).forEach(function (id) { set[id] = true; });
    var items = Object.keys(set).filter(function (it) { return it !== prefix && it.indexOf(prefix) === 0; }).sort();
    if (!items.length) { hideComplete(); return; }
    completeItems = items.slice(0, 40);
    completeIdx = 0;
    // 定位弹层: 在 textarea 光标处(粗略: 靠近光标字符数估算行/列不可靠, 用 textarea 顶部右下角固定浮层)
    var rect = ta.getBoundingClientRect();
    if (!completeBox) {
      completeBox = document.createElement("div");
      completeBox.style.cssText = "position:fixed;z-index:2147483003;min-width:200px;max-width:340px;max-height:260px;overflow-y:auto;background:#1c1e24;border:1px solid #3a4150;border-radius:8px;box-shadow:0 6px 20px rgba(0,0,0,.5);font:12px Consolas,monospace;color:#dbe2ea;padding:3px";
      completeBox.__ta = ta;
      document.body.appendChild(completeBox);
    }
    completeBox.__ta = ta;
    renderComplete();
    // 定位在光标上方
    var lh = 20, linesBefore = (ta.value.slice(0, ta.selectionStart).split("\n").length - 1);
    var top = rect.top + lh * linesBefore - completeBox.offsetHeight - 6;
    if (top < rect.top + 30) top = rect.top + 30;
    completeBox.style.left = (rect.left + Math.min(12, rect.width / 3)) + "px";
    completeBox.style.top = top + "px";
    completeBox.style.display = "block";
  }

  // renderComplete 重绘弹层列表：当前项高亮；鼠标进入/按下即接受（mousedown 防止 textarea 失焦先于点击）。
  function renderComplete() {
    if (!completeBox) return;
    completeBox.innerHTML = "";
    completeItems.forEach(function (it, i) {
      var d = document.createElement("div");
      d.style.cssText = "padding:3px 8px;cursor:pointer;border-radius:5px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis" + (i === completeIdx ? ";background:#0153e5;color:#fff" : "");
      d.textContent = it;
      d.onmousedown = function (ev) { ev.preventDefault(); completeIdx = i; applyComplete(); };
      d.onmouseenter = function () { completeIdx = i; renderComplete(); };
      completeBox.appendChild(d);
    });
  }

  // moveComplete 上下移动选中项（循环），重绘高亮。
  function moveComplete(d) {
    completeIdx = (completeIdx + d + completeItems.length) % completeItems.length;
    renderComplete();
  }

  // applyComplete 把当前选中项插入 textarea（替换光标前的前缀词），随后收起弹层并聚焦。
  function applyComplete() {
    var ta = completeBox && completeBox.__ta;
    if (!ta) { hideComplete(); return; }
    var wb = wordBefore(ta);
    var item = completeItems[completeIdx];
    if (!item) { hideComplete(); return; }
    var s = wb ? wb.start : ta.selectionStart;
    ta.value = ta.value.slice(0, s) + item + ta.value.slice(ta.selectionStart);
    ta.selectionStart = ta.selectionEnd = s + item.length;
    hideComplete();
    ta.focus();
  }

  // hideComplete 收起补全弹层并清定时器（幂等：无弹层/无定时器时静默）。
  function hideComplete() {
    if (completeTimer) { clearTimeout(completeTimer); completeTimer = null; }
    if (completeBox) { completeBox.remove(); completeBox = null; }
  }

  // 启动：DOM 已就绪直接 boot；否则等 DOMContentLoaded（脚本可能在 head 阶段就被注入）。
  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", boot);
  } else {
    boot();
  }
})();
