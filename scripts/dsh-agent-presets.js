/* __DSH_AGENT_PRESETS v2 (DSH 四模式/Agent 预设选择器注入, 含选中箭头指示) */
// dsh-agent-presets.js — DSH 四模式（Agent 预设）选择器注入。
// 在 Composer「执行方式」菜单里追加 Agent 模式分组，点击切换 DSH 会话预设。
// v2: 自带选中视觉——高亮背景 + 右侧箭头指示当前模式（不依赖官方 CSS-in-JS 样式）。
// 独立普通脚本（非 module），不改动压缩 JSX；通过 window.go.main.App 桥调用 Go。

(function () {
  "use strict";
  var PRESETS = [
    { id: "standard", name: "标准模式", desc: "功能完整编码 Agent" },
    { id: "code", name: "PTC 模式", desc: "Code Mode SDK · TypeScript 组合" },
    { id: "minimal", name: "极简模式", desc: "bash + str_replace_editor" },
    { id: "cordis", name: "创造模式", desc: "创建自定义 preset" }
  ];
  var current = null; // 当前 preset id（本地记录）
  var injectedKey = "dsh-presets-injected";

  // 自带样式：选中项 = 高亮背景 + 右侧箭头 ▶（用户可明确看出当前模式）
  var css = [
    "." + injectedKey + "{display:flex;align-items:center;justify-content:space-between;width:100%;box-sizing:border-box;background:transparent;border:none;color:inherit;padding:7px 10px;margin:1px 0;text-align:left;cursor:pointer;font:inherit;border-radius:6px}",
    "." + injectedKey + ":hover{background:rgba(255,255,255,.08)}",
    "." + injectedKey + ".composer-access-menu__item--active,." + injectedKey + "[aria-checked='true']{background:rgba(1,83,229,.22)}",
    "." + injectedKey + ".composer-access-menu__item--active::after,." + injectedKey + "[aria-checked='true']::after{content:'\\25B6';font-size:10px;color:#5b9bff;flex:none;margin-left:8px}",
    "." + injectedKey + " .composer-access-menu__copy{display:flex;flex-direction:column;min-width:0}",
    "." + injectedKey + " .composer-access-menu__title{font-weight:600;font-size:13px;line-height:1.3}",
    "." + injectedKey + " .composer-access-menu__desc{font-size:11px;opacity:.65;margin-top:2px;line-height:1.3}"
  ].join("\n");
  var styleEl = document.createElement("style");
  styleEl.id = "dsh-presets-css";
  styleEl.textContent = css;
  document.head.appendChild(styleEl);

  function bridge() {
    try { return window.go && window.go.main && window.go.main.App ? window.go.main.App : null; }
    catch (e) { return null; }
  }

  // 菜单里插入 Agent 模式分组
  function injectInto(menu) {
    var section = menu.querySelector(".composer-access-menu__section");
    if (!section || section.querySelector("." + injectedKey)) return;
    var label = document.createElement("div");
    label.className = "composer-access-menu__label";
    label.textContent = "Agent 模式";
    section.appendChild(label);
    PRESETS.forEach(function (p) {
      var btn = document.createElement("button");
      btn.type = "button";
      btn.role = "menuitemradio";
      btn.setAttribute("aria-checked", current === p.id ? "true" : "false");
      btn.className = "composer-access-menu__item composer-intent-menu__item " + injectedKey + (current === p.id ? " composer-access-menu__item--active" : "");
      btn.title = p.desc;
      var copy = document.createElement("span");
      copy.className = "composer-access-menu__copy";
      var t = document.createElement("span");
      t.className = "composer-access-menu__title";
      t.textContent = p.name;
      var d = document.createElement("span");
      d.className = "composer-access-menu__desc";
      d.textContent = p.desc;
      copy.appendChild(t);
      copy.appendChild(d);
      btn.appendChild(copy);
      btn.addEventListener("click", function () {
        var app = bridge();
        if (!app || !app.SetAgentPresetForTab) return;
        // tabId 传空 → Go 端 activeSessionID 落到活跃会话
        app.SetAgentPresetForTab("", p.id).then(function (r) {
          if (r && r.ok) {
            current = p.id;
            syncChecks();
            if (window.__dshPresetToast) window.__dshPresetToast("已切换到 " + p.name);
          } else {
            var err = (r && r.error) || "切换失败";
            // DSH 语义：已启动(running)会话的 preset 锁定(agent-preset-locked)，
            // 不能原地切换——用该模式新开会话（session.create 带 agentPreset）。
            if (err.indexOf("locked") >= 0 || err.indexOf("started") >= 0 || err.indexOf("fixed") >= 0) {
              if (app.CreateSession) {
                if (window.__dshPresetToast) window.__dshPresetToast("当前会话已启动(预设固定)，正在用「" + p.name + "」新开会话…");
                app.CreateSession("", p.id).then(function (r2) {
                  var nid = r2 && (r2.id || r2.tabId || r2.sessionId);
                  if (nid) {
                    current = p.id;
                    syncChecks();
                    if (window.__dshPresetToast) window.__dshPresetToast("已创建「" + p.name + "」新会话 " + String(nid).substring(0, 8) + "… 请在会话列表查看");
                  } else if (window.__dshPresetToast) {
                    window.__dshPresetToast("新开会话失败：" + ((r2 && r2.error) || "未知错误"));
                  }
                }).catch(function () {
                  if (window.__dshPresetToast) window.__dshPresetToast("新开会话调用失败");
                });
              } else if (window.__dshPresetToast) {
                window.__dshPresetToast("当前会话已启动，预设固定(DSH限制)。请新开会话后再选模式");
              }
            } else if (window.__dshPresetToast) {
              window.__dshPresetToast(err);
            }
          }
        }).catch(function () {});
      });
      section.appendChild(btn);
    });
    menu.setAttribute("data-dsh-presets", "1");
  }

  // 同步所有已注入菜单的选中态（按标题匹配 preset 名，不依赖索引）
  function syncChecks() {
    var menus = document.querySelectorAll(".composer-intent-menu[data-dsh-presets='1']");
    for (var m = 0; m < menus.length; m++) {
      var items = menus[m].querySelectorAll("." + injectedKey);
      for (var i = 0; i < items.length; i++) {
        var title = items[i].querySelector(".composer-access-menu__title");
        var on = false;
        if (title) {
          for (var j = 0; j < PRESETS.length; j++) {
            if (title.textContent === PRESETS[j].name) { on = current === PRESETS[j].id; break; }
          }
        }
        items[i].setAttribute("aria-checked", on ? "true" : "false");
        items[i].classList.toggle("composer-access-menu__item--active", on);
      }
    }
  }

  // 读取当前默认 preset（首次）
  function loadCurrent() {
    var app = bridge();
    if (!app || !app.AgentPresets) return;
    app.AgentPresets().then(function (v) {
      if (!v || !v.presets) return;
      for (var i = 0; i < v.presets.length; i++) {
        if (v.presets[i].isDefault) { current = v.presets[i].id; break; }
      }
      syncChecks();
    }).catch(function () {});
  }

  // 轻量 toast（复用 body 内联样式）
  if (!window.__dshPresetToast) {
    window.__dshPresetToast = function (msg) {
      var el = document.createElement("div");
      el.style.cssText = "position:fixed;bottom:24px;left:50%;transform:translateX(-50%);z-index:99999;background:#1c1d21;color:#f4f4f3;border:1px solid #333;border-radius:8px;padding:8px 16px;font:13px Inter,sans-serif;box-shadow:0 4px 16px rgba(0,0,0,.5);pointer-events:none;opacity:0;transition:opacity .2s";
      el.textContent = msg;
      document.body.appendChild(el);
      requestAnimationFrame(function () { el.style.opacity = "1"; });
      setTimeout(function () { el.style.opacity = "0"; setTimeout(function () { el.remove(); }, 250); }, 1800);
    };
  }

  function start() {
    loadCurrent();
    var observer = new MutationObserver(function () {
      var menus = document.querySelectorAll(".composer-intent-menu");
      for (var i = 0; i < menus.length; i++) injectInto(menus[i]);
    });
    observer.observe(document.documentElement, { childList: true, subtree: true });
    // 立即尝试一次
    var menus = document.querySelectorAll(".composer-intent-menu");
    for (var i = 0; i < menus.length; i++) injectInto(menus[i]);
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", start);
  } else {
    start();
  }
})();
