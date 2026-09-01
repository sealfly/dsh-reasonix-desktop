/* __DSH_MEMORY_PLUGINS v1 (记忆插件管理 - 设置-记忆页, 本地注入) */
// dsh-memory-plugins.js — 在设置面板「记忆」页注入记忆插件管理区：
// 推荐插件（@openviking/dsh-memory-plugin / hindsight / memos-local）安装·启用·禁用·卸载、
// 本机已装记忆插件检测（与插件市场关联：用户从插件市场装的记忆插件在此可见）、
// 记忆插件市场浏览（dsh-1024store memory 分类，同源）。
// 默认关闭：插件需手动启用（持久化到 ~/.reasonix/memory-plugins.json）。
// 独立普通脚本（非 module）；通过 window.go.main.App 桥调用 Go。

(function () {
  "use strict";
  if (window.__DSH_MEMORY_PLUGINS__) return;
  window.__DSH_MEMORY_PLUGINS__ = true;

  var NS = "dsh-mem";
  var css = [
    "." + NS + "{margin:14px 0;padding:14px 16px;border:1px solid var(--border,#333);border-radius:10px;background:var(--surface,#17181b)}",
    "." + NS + "__head{display:flex;align-items:center;justify-content:space-between;gap:8px;margin-bottom:8px}",
    "." + NS + "__title{font-weight:600;font-size:13px}",
    "." + NS + "__sub{font-size:11px;opacity:.65;margin:2px 0 10px}",
    "." + NS + "__card{border:1px solid #333;border-radius:8px;padding:10px 12px;margin:8px 0}",
    "." + NS + "__card-head{display:flex;align-items:center;gap:8px;flex-wrap:wrap}",
    "." + NS + "__name{font-weight:600;font-size:12px}",
    "." + NS + "__stars{font-size:11px;opacity:.6}",
    "." + NS + "__desc{font-size:11px;opacity:.8;margin:6px 0 8px;line-height:1.5}",
    "." + NS + "__badge{font-size:10px;padding:1px 7px;border-radius:999px;background:rgba(255,255,255,.07);white-space:nowrap}",
    "." + NS + "__badge--on{background:rgba(34,197,94,.18);color:#4ade80}",
    "." + NS + "__badge--off{background:rgba(255,255,255,.07);color:inherit}",
    "." + NS + "__badge--miss{background:rgba(148,163,184,.15);color:#94a3b8}",
    "." + NS + "__btn{padding:4px 11px;border-radius:6px;border:1px solid #333;background:rgba(255,255,255,.06);color:inherit;font-size:11px;cursor:pointer}",
    "." + NS + "__btn:hover{background:rgba(255,255,255,.12)}",
    "." + NS + "__btn--primary{border-color:#2563eb;background:rgba(1,83,229,.25);color:#7ab2ff}",
    "." + NS + "__btn:disabled{opacity:.5;cursor:not-allowed}",
    "." + NS + "__list{font-size:11px;margin-top:8px}",
    "." + NS + "__list-item{display:flex;align-items:center;gap:8px;margin:4px 0;flex-wrap:wrap}",
    "." + NS + "__mkt{max-height:260px;overflow:auto;margin-top:8px;border-top:1px dashed #333;padding-top:8px}",
    "." + NS + "__mkt-item{display:flex;align-items:center;gap:8px;margin:5px 0;flex-wrap:wrap;font-size:11px}",
    "." + NS + "__status{font-size:11px;opacity:.8;margin-top:8px;min-height:14px;word-break:break-all}",
    "." + NS + "__err{color:#f87171;font-size:11px;margin-top:4px}",
    "." + NS + "__hint{font-size:10px;opacity:.5;margin-top:6px;line-height:1.5}"
  ].join("\n");
  var styleEl = document.createElement("style");
  styleEl.id = "dsh-memory-plugins-css";
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
    setTimeout(function () { el.style.opacity = "0"; setTimeout(function () { el.remove(); }, 250); }, 2400);
  }
  function esc(s) {
    return String(s == null ? "" : s).replace(/[&<>"']/g, function (c) {
      return { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c];
    });
  }
  function fmtStars(n) { return n ? (n >= 1000 ? (n / 1000).toFixed(1) + "k" : String(n)) : ""; }
  function statusOf(p) {
    if (!p.installed) return '<span class="' + NS + '__badge ' + NS + '__badge--miss">未安装</span>';
    return p.enabled
      ? '<span class="' + NS + '__badge ' + NS + '__badge--on">已启用</span>'
      : '<span class="' + NS + '__badge ' + NS + '__badge--off">已安装 · 未启用</span>';
  }

  // 推荐插件卡片
  function renderRecommended(box, data) {
    (data.recommended || []).forEach(function (p) {
      var card = document.createElement("div");
      card.className = NS + "__card";
      var head = document.createElement("div");
      head.className = NS + "__card-head";
      head.innerHTML = '<span class="' + NS + '__name">' + esc(p.name) + "</span>" +
        '<span class="' + NS + '__stars">★ ' + fmtStars(p.stars) + "</span>" + statusOf(p);
      var desc = document.createElement("div");
      desc.className = NS + "__desc";
      desc.textContent = p.desc;
      var actions = document.createElement("div");
      actions.style.cssText = "display:flex;gap:6px;flex-wrap:wrap";
      if (!p.installed) {
        var inst = document.createElement("button");
        inst.className = NS + "__btn " + NS + "__btn--primary";
        inst.textContent = "安装";
        inst.addEventListener("click", function () { doInstall(p, inst); });
        actions.appendChild(inst);
      } else {
        if (!p.enabled) {
          var en = document.createElement("button");
          en.className = NS + "__btn " + NS + "__btn--primary";
          en.textContent = "启用";
          en.addEventListener("click", function () { setEnabled(p, true, en); });
          actions.appendChild(en);
        } else {
          var dis = document.createElement("button");
          dis.className = NS + "__btn";
          dis.textContent = "禁用";
          dis.addEventListener("click", function () { setEnabled(p, false, dis); });
          actions.appendChild(dis);
        }
        var un = document.createElement("button");
        un.className = NS + "__btn";
        un.textContent = "卸载";
        un.addEventListener("click", function () { doUninstall(p, un); });
        actions.appendChild(un);
      }
      card.appendChild(head);
      card.appendChild(desc);
      card.appendChild(actions);
      box.appendChild(card);
    });
  }

  // 本机已装记忆插件（含用户自装——插件市场关联）
  function renderInstalled(box, data) {
    var list = (data.installed || []).filter(function (i) {
      return !(data.recommended || []).some(function (r) { return r.id === i.id || r.name === i.id; });
    });
    if (!list.length) return;
    var wrap = document.createElement("div");
    wrap.className = NS + "__list";
    wrap.innerHTML = '<div style="font-weight:600;margin:6px 0 4px">已安装的记忆插件（含插件市场安装）</div>';
    list.forEach(function (i) {
      var item = document.createElement("div");
      item.className = NS + "__list-item";
      var idSpan = document.createElement("span");
      idSpan.style.cssText = "font-weight:600";
      idSpan.textContent = i.id;
      var badge = document.createElement("span");
      badge.className = i.enabled ? (NS + "__badge " + NS + "__badge--on") : (NS + "__badge " + NS + "__badge--off");
      badge.textContent = i.enabled ? "已启用" : "未启用";
      var enBtn = document.createElement("button");
      enBtn.className = NS + "__btn " + (i.enabled ? "" : NS + "__btn--primary");
      enBtn.textContent = i.enabled ? "禁用" : "启用";
      enBtn.addEventListener("click", function () {
        setEnabled({ id: i.id, name: i.id }, !i.enabled, enBtn);
      });
      item.appendChild(idSpan);
      item.appendChild(badge);
      item.appendChild(enBtn);
      wrap.appendChild(item);
    });
    box.appendChild(wrap);
  }

  // 记忆插件市场浏览
  function renderMarket(box) {
    var wrap = document.createElement("div");
    wrap.className = NS + "__list";
    var head = document.createElement("div");
    head.style.cssText = "font-weight:600;margin:8px 0 4px;display:flex;align-items:center;gap:8px";
    head.innerHTML = '<span>记忆插件市场（memory 分类 · dsh-1024store）</span>';
    var more = document.createElement("button");
    more.className = NS + "__btn";
    more.textContent = "刷新";
    more.addEventListener("click", function () { loadMarket(wrap, more); });
    head.appendChild(more);
    wrap.appendChild(head);
    var mkt = document.createElement("div");
    mkt.className = NS + "__mkt";
    mkt.textContent = "加载中…";
    wrap.appendChild(mkt);
    box.appendChild(wrap);
    loadMarket(wrap, more);
  }

  function loadMarket(wrap, btn) {
    var app = bridge();
    var mkt = wrap.querySelector("." + NS + "__mkt");
    if (!app || !app.MemoryPluginMarket) { mkt.textContent = "桥方法不可用"; return; }
    if (btn) btn.disabled = true;
    app.MemoryPluginMarket("", 1).then(function (d) {
      if (btn) btn.disabled = false;
      var items = (d && d.items) || [];
      if (!items.length) { mkt.textContent = "暂无记忆插件（" + ((d && d.error) || "网络异常") + "）"; return; }
      mkt.innerHTML = "";
      items.forEach(function (p) {
        var item = document.createElement("div");
        item.className = NS + "__mkt-item";
        var nm = document.createElement("span");
        nm.style.cssText = "font-weight:600;max-width:220px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap";
        nm.textContent = p.name;
        var st = document.createElement("span");
        st.className = NS + "__badge " + (p.installed ? NS + "__badge--on" : NS + "__badge--off");
        st.textContent = p.installed ? "已安装" : "★ " + fmtStars(p.stars);
        var installBtn = document.createElement("button");
        installBtn.className = NS + "__btn " + (p.installed ? "" : NS + "__btn--primary");
        installBtn.textContent = p.installed ? "已安装" : "安装";
        if (!p.installed) {
          installBtn.addEventListener("click", function () {
            doInstall({ id: p.id, name: p.name, install: p.install }, installBtn);
          });
        }
        var link = document.createElement("a");
        link.href = p.url || "#";
        link.target = "_blank";
        link.rel = "noopener";
        link.textContent = "详情";
        link.style.cssText = "color:#7ab2ff;text-decoration:none";
        item.appendChild(nm);
        item.appendChild(st);
        item.appendChild(installBtn);
        item.appendChild(link);
        mkt.appendChild(item);
      });
      mkt.appendChild((function () {
        var h = document.createElement("div");
        h.className = NS + "__hint";
        h.textContent = "共 " + (d.total || 0) + " 个记忆插件；更多可到「插件」页浏览完整市场。";
        return h;
      })());
    }).catch(function () { if (btn) btn.disabled = false; mkt.textContent = "市场加载失败"; });
  }

  function doInstall(p, btn) {
    var app = bridge();
    var status = document.querySelector("." + NS + "__status");
    if (!app || !app.InstallMemoryPlugin) { toast("桥方法不可用"); return; }
    var spec = (p.install || "").replace(/^dsh plugin --profile web add\s*/, "") || p.id;
    if (btn) { btn.disabled = true; btn.textContent = "安装中…（可能几分钟）"; }
    if (status) status.textContent = "正在安装 " + (p.name || spec) + "（npm/pnpm 下载）…";
    app.InstallMemoryPlugin(spec).then(function (r) {
      if (btn) { btn.disabled = false; }
      if (r && r.ok) {
        if (status) status.textContent = "✅ 已安装 " + (p.name || spec) + "。重启 DSH (Harness Desktop) 后生效，然后回到此处启用。";
        toast("安装成功：" + (p.name || spec) + "，重启 DSH 后启用");
        setTimeout(refreshAll, 800);
      } else {
        if (status) status.textContent = "❌ 安装失败：" + ((r && r.error) || "未知错误");
        if (btn) btn.textContent = "重试";
        toast("安装失败");
      }
    }).catch(function () { if (btn) { btn.disabled = false; btn.textContent = "重试"; } if (status) status.textContent = "安装调用异常"; });
  }

  function doUninstall(p, btn) {
    var app = bridge();
    var status = document.querySelector("." + NS + "__status");
    if (!app || !app.UninstallMemoryPlugin) { toast("桥方法不可用"); return; }
    var spec = (p.install || "").replace(/^dsh plugin --profile web add\s*/, "") || p.id;
    if (btn) { btn.disabled = true; btn.textContent = "卸载中…"; }
    if (status) status.textContent = "正在卸载 " + (p.name || spec) + "…";
    app.UninstallMemoryPlugin(spec).then(function (r) {
      if (btn) btn.disabled = false;
      if (r && r.ok) {
        if (status) status.textContent = "✅ 已卸载 " + (p.name || spec) + "。重启 DSH 后完全移除。";
        toast("已卸载：" + (p.name || spec));
        setTimeout(refreshAll, 800);
      } else {
        if (status) status.textContent = "❌ 卸载失败：" + ((r && r.error) || "未知错误");
        toast("卸载失败");
      }
    }).catch(function () { if (btn) btn.disabled = false; });
  }

  function setEnabled(p, enabled, btn) {
    var app = bridge();
    var status = document.querySelector("." + NS + "__status");
    if (!app || !app.SetMemoryPluginEnabled) { toast("桥方法不可用"); return; }
    if (btn) btn.disabled = true;
    app.SetMemoryPluginEnabled(p.id, enabled).then(function (r) {
      if (btn) btn.disabled = false;
      if (r && r.ok) {
        if (status) status.textContent = enabled ? "✅ 已启用 " + (p.name || p.id) + "（默认关闭，启用后生效）" : "已禁用 " + (p.name || p.id);
        toast((enabled ? "已启用 " : "已禁用 ") + (p.name || p.id));
        setTimeout(refreshAll, 500);
      } else if (status) {
        status.textContent = "设置失败：" + ((r && r.error) || "未知错误");
      }
    }).catch(function () { if (btn) btn.disabled = false; });
  }

  function refreshAll() {
    var app = bridge();
    if (!app || !app.MemoryPlugins) return;
    app.MemoryPlugins().then(function (d) {
      var boxes = document.querySelectorAll("." + NS + "__rec");
      for (var i = 0; i < boxes.length; i++) {
        var box = boxes[i];
        box.innerHTML = "";
        renderRecommended(box, d || {});
        renderInstalled(box, d || {});
      }
    }).catch(function () {});
  }

  // 注入主区块
  function renderBlock(host, data) {
    var wrap = document.createElement("div");
    wrap.className = NS;
    wrap.innerHTML =
      '<div class="' + NS + '__head"><span class="' + NS + '__title">记忆插件（DSH 记忆能力来自插件生态）</span></div>' +
      '<div class="' + NS + '__sub">DSH 原生无记忆 API；记忆由插件提供。默认关闭——安装后请在此启用。启用后插件注册的 RPC 由桥透传，前端即可使用。</div>' +
      '<div class="' + NS + '__rec"></div>' +
      '<div class="' + NS + '__status" data-role="status"></div>' +
      '<div class="' + NS + '__hint">安装位置：~/.dsh/profiles/web（dsh plugin --profile web add）。安装/卸载后需重启 DSH (Harness Desktop) 才生效。与「插件」页同源（dsh-1024store）：从插件市场安装的记忆插件会自动出现在上方的「已安装」列表。</div>';
    var box = wrap.querySelector("." + NS + "__rec");
    renderRecommended(box, data);
    renderInstalled(box, data);
    renderMarket(box);
    host.appendChild(wrap);
  }

  // dataset 键必须用合法 identifier（连字符名会抛 SyntaxError，导致注入脚本整体崩溃）
  var hostMark = "dshMemHost";
  var injectTimer = null;
  var injectCount = 0;
  function inject() {
    // 节流：mutation 高频触发时合并为每 300ms 一次，防止设置面板渲染风暴下主线程卡死
    if (injectTimer) return;
    injectTimer = setTimeout(function () {
      injectTimer = null;
      doInject();
    }, 300);
  }
  function doInject() {
    // 全局注入标志：注入过一次后不再注入（settings-center 被 React 重建也有效）
    if (window.__DSH_MEM_PLUGINS_INJECTED) return;
    var centers = document.querySelectorAll(".settings-center");
    for (var c = 0; c < centers.length; c++) {
      var center = centers[c];
      if (center.dataset && center.dataset[hostMark] === "1") continue;
      // 等待记忆页签内容出现：MemoryPanel 渲染 .mem-section
      var mem = center.querySelector(".mem-section, .mem-facts, .mem-empty");
      if (!mem) continue;
      if (center.dataset) center.dataset[hostMark] = "1";
      window.__DSH_MEM_PLUGINS_INJECTED = true;
      var container = document.createElement("div");
      container.style.width = "100%";
      mem.parentNode.insertBefore(container, mem.nextSibling);
      var status = document.createElement("div");
      status.className = NS + "__status";
      status.textContent = "正在读取记忆插件状态…";
      container.appendChild(status);
      var app = bridge();
      if (app && app.MemoryPlugins) {
        app.MemoryPlugins().then(function (d) {
          if (!container.isConnected) return;
          status.remove();
          renderBlock(container, d || {});
        }).catch(function () {
          status.textContent = "读取失败（桥不可用？）";
        });
      } else {
        status.textContent = "桥方法不可用";
      }
    }
  }

  function start() {
    var obs = new MutationObserver(function () { inject(); });
    obs.observe(document.documentElement, { childList: true, subtree: true });
    doInject();
    // 注入成功后 3 秒断开观察（管理区已渲染，无需持续监控）
    var guard = setInterval(function () {
      if (window.__DSH_MEM_PLUGINS_INJECTED) {
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
