#!/usr/bin/env node
/*
 * ui-test-provider-flow.js — 在**真实界面**上验证「模型服务」页的供应商与模型链路。
 *
 * 背景：Wails 会覆盖 WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS，CDP 不可用（见 ui_test_hook.go 注释），
 * 因此通过应用内的调试钩子（启动时设 DSH_UI_TEST_PORT）执行 JS 来驱动界面。
 *
 * 覆盖的链路：
 *   1) 打开设置 → 模型服务页
 *   2) 看到本机 DSH 已配置的供应商（含测试用假端点供应商）
 *   3) 点该卡片的「刷新模型」→ 桥方法 FetchProviderModelCatalog → 假端点 /v1/models
 *   4) 勾选拉到的模型
 *   5) 点「保存启用模型」→ 桥方法 SaveProvider → DSH settings.llm-pi-ai.providers.<route>.models
 *   6) 复核 DSH 侧确实写入（读回校验），随后清理
 *
 * 另加一个**负例**：把供应商改成不可达端点后「刷新模型」，必须出现失败提示而不是静默成功。
 *
 * 用法：
 *   node scripts/ui-test-provider-flow.js [--hook 9310] [--dsh 3080] [--fake 9411] [--explore]
 * 前置：应用以 DSH_UI_TEST_PORT=<hook> 启动；假端点已用 scripts/fake-openai-provider.js 起好。
 */
"use strict";

const HOOK = argValue("--hook", "9310");
const DSH = argValue("--dsh", "3080");
const FAKE_PORT = argValue("--fake", "9411");
const EXPLORE = process.argv.includes("--explore");

function argValue(flag, fallback) {
  const i = process.argv.indexOf(flag);
  return i >= 0 && process.argv[i + 1] ? process.argv[i + 1] : fallback;
}

let route = "ui-flow-fake"; // 实际路由在 resolveRoute() 之后确定（可能带随机后缀）
let pass = 0;
let fail = 0;
function check(name, ok, detail) {
  if (ok) {
    pass += 1;
    console.log(`  PASS  ${name}${detail ? `  [${detail}]` : ""}`);
  } else {
    fail += 1;
    console.log(`  FAIL  ${name}${detail ? `  [${detail}]` : ""}`);
  }
}

async function evalJS(js) {
  const res = await fetch(`http://127.0.0.1:${HOOK}/eval`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ js }),
  });
  const body = await res.json();
  if (!body.ok) throw new Error(body.error || "eval 失败");
  return body.value;
}

async function click(selector) {
  const res = await fetch(`http://127.0.0.1:${HOOK}/click`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ selector }),
  });
  return res.json();
}

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

/** 轮询直到表达式为真（React 重渲染是异步的，固定 sleep 会假失败）。 */
async function waitFor(expr, { timeoutMs = 12000, intervalMs = 400 } = {}) {
  const deadline = Date.now() + timeoutMs;
  let last = null;
  while (Date.now() < deadline) {
    last = await evalJS(expr);
    if (last) return last;
    await sleep(intervalMs);
  }
  return last;
}

// ── DSH 侧（测试前置与复核）────────────────────────────────────────
async function dshRpc(method, payload) {
  const res = await fetch(`http://127.0.0.1:${DSH}/api/${method}`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ type: "client-request", rpcId: `ui-test-${Date.now()}`, method, payload }),
  });
  const body = await res.json();
  if (!body.result || body.result.ok !== true) {
    throw new Error(`${method} 失败: ${JSON.stringify(body.result?.error || body)}`);
  }
  return body.result.value;
}

async function providerProfile(route) {
  const value = await dshRpc("settings.describe", {});
  const ns = (value.namespaces || []).find((n) => n.ns === "llm-pi-ai");
  return ns?.value?.providers?.[route];
}

const ROUTE = "ui-flow-fake";

/**
 * 解析**实际创建**的 route。
 *
 * 添加时若同名 route 已存在，uniqueProviderRoute 会加随机后缀；脚本必须用实际 route
 * 去选中/复核，否则会选到上一轮残留的（没有密钥的）那条（实测踩过：
 * 页面显示"无密钥"、保存按钮禁用，复核还读错了 profile）。
 */
async function resolveRoute() {
  const names = await evalJS(`(async function(){
    try {
      var s = await window.go.main.App.Settings();
      return ((s && s.providers) || []).map(function(p){return p.name}).filter(function(n){return n.indexOf(${JSON.stringify(ROUTE)})===0});
    } catch(e) { return []; }
  })()`);
  return Array.isArray(names) && names.length ? names[0] : ROUTE;
}

/**
 * 前置：用**应用自己的添加方法**建一个指向假端点的供应商。
 *
 * 这样顺带覆盖了添加路径本身（含 DSH 对"未知路由必须显式列出模型"的约束 ——
 * 桥侧会在写入被拒时用端点探测自动补模型）。这里的调用与前端点"添加"走同一条桥方法。
 */
async function setupProvider() {
  // 先清理同名/前缀残留：否则 uniqueProviderRoute 会加随机后缀，
  // 页面上出现多个相似条目，测试可能选到上一轮留下的"没有密钥"的那个（实测踩过）。
  await cleanup();
  const result = await evalJS(`(async function(){
    try {
      var warning = await window.go.main.App.AddProviderConnectionWithOptions(
        "", "UI Flow Fake", "test-key-123", "http://127.0.0.1:${FAKE_PORT}/v1", "openai-completions");
      return { ok: true, warning: warning || "" };
    } catch (e) { return { ok: false, error: String((e && e.message) || e) }; }
  })()`);
  return result;
}

async function cleanup() {
  try {
    // 直接问 DSH 要 providers 列表（比 app.Settings() 快得多：后者会连带算子代理缓存/模型目录，
    // 实测会在钩子 20s 超时里跑不完，导致后缀残留一直删不掉）。
    const value = await dshRpc("settings.describe", {});
    const ns = (value.namespaces || []).find((n) => n.ns === "llm-pi-ai");
    const names = Object.keys(ns?.value?.providers || {}).filter((n) => n.indexOf(ROUTE) === 0);
    for (const name of names) {
      try {
        await evalJS(`(async function(){ await window.go.main.App.DeleteProvider(${JSON.stringify(name)}); return true })()`);
      } catch {
        /* 单个失败不阻断其余清理 */
      }
    }
    if (names.length) console.log(`  info: 清理 ${names.length} 条残留: ${names.join(", ")}`);
  } catch {
    /* 钩子可能已不可用 */
  }
  try {
    await dshRpc("settings.mutate", { ns: "llm-pi-ai", ops: [{ op: "unset", path: ["providers", ROUTE] }] });
  } catch {
    /* 清理失败不掩盖测试结果 */
  }
}

// ── 界面辅助 ────────────────────────────────────────────────────────
//
// 重要：**不能**用 document.body.innerText 做断言 —— 应用里同时渲染着会话内容
// （实测撞到过：我那句话的文本被当成"界面提示"匹配上了）。
// 所有查询都限定在设置面板/供应商卡片子树内，并给命中的元素打 data-uit-* 标记再由钩子点击。

/** 打开设置面板（点侧栏设置按钮）。
 *  实测：侧栏入口是 `button.sidebar__utility-button`，文本正好"设置"。
 *  注意排除我们注入的连接横幅按钮（`.btn-set`，文本"⚙️ 连接设置"）。
 *
 *  面板**已开时必须关掉再打开**：面板只在挂载时拉一次设置，供应商列表是缓存的；
 *  复用已开的面板会拿到上一轮的旧列表（实测踩过：选中了早已删除的旧供应商条目）。 */
async function openSettings() {
  const markEntry = `(function(){
    var all=[...document.querySelectorAll('button')].filter(function(el){return !el.classList.contains('btn-set')});
    var el=all.filter(function(b){return (b.textContent||'').trim()==='设置'})[0]
        || all.filter(function(b){return b.classList.contains('sidebar__utility-button')})[0];
    if(!el) return null;
    el.setAttribute('data-uit-settings','1');
    return {tag:el.tagName, cls:(el.className||'').toString().slice(0,40), text:(el.textContent||'').trim().slice(0,10)};
  })()`;

  const entry = await evalJS(markEntry);
  if (!entry) return null;

  const isOpen = () => evalJS(`Boolean(document.querySelector('button.settings-center__navitem'))`);
  if (await isOpen()) {
    // 关闭（强制下次挂载时重新加载设置）
    await click('[data-uit-settings="1"]');
    await waitFor(`!Boolean(document.querySelector('button.settings-center__navitem'))`, { timeoutMs: 6000 });
    await evalJS(markEntry); // 关掉后面板重渲染，需要重新打标记
  }
  await click('[data-uit-settings="1"]');
  await waitFor(`Boolean(document.querySelector('button.settings-center__navitem'))`, { timeoutMs: 10000 });
  return entry;
}

/** 设置面板根节点（含「模型服务」页签的那个容器）。 */
const SETTINGS_SCOPE = `(function(){
  var tabs=[...document.querySelectorAll('button,[role="tab"]')].filter(function(b){return (b.textContent||'').trim()==='模型服务'});
  if(!tabs.length) return null;
  var p=tabs[0];
  for(var i=0;i<10&&p;i++){ p=p.parentElement; if(p&&p.querySelectorAll('button').length>3) return p; }
  return tabs[0];
})()`;

/** 切到「模型服务」页签（实测页签是 button.settings-center__navitem，激活态加 --active 类）。 */
async function openModelServicesTab() {
  const found = await evalJS(`(function(){
    var navs=[...document.querySelectorAll('button.settings-center__navitem')];
    var el=navs.filter(function(b){return (b.textContent||'').trim()==='模型服务'})[0];
    if(!el) return {found:false, navCount:navs.length};
    el.setAttribute('data-uit-tab','1');
    return {found:true, navCount:navs.length, active:(el.className||'').toString().indexOf('--active')>=0};
  })()`);
  if (!found || !found.found) return null;
  await click('[data-uit-tab="1"]');
  // 等内容区渲染（卡片/按钮出现）
  for (let i = 0; i < 10; i += 1) {
    await sleep(500);
    const ready = await evalJS(`(function(){
      var el=[...document.querySelectorAll('button.settings-center__navitem')].filter(function(b){return (b.textContent||'').trim()==='模型服务'})[0];
      return Boolean(el && (el.className||'').toString().indexOf('--active')>=0);
    })()`);
    if (ready) break;
  }
  await sleep(800);
  return found;
}

/**
 * 选中左侧列表里的目标供应商。
 *
 * 实测：模型服务页是**两栏结构** —— 左侧 `button.provider-connections__item`（首字母 + route 名），
 * 右侧是该供应商的详情表单（标题是 `button.connection-title`）。
 * 不先选中就会一直在编辑别的供应商（实测踩过：误改到 deepseek-official 的表单）。
 */
async function selectProvider(route) {
  // 列表项文本 = 首字母 + route（如 "uui-flow-fake"）。必须**精确匹配**：
  // 只用"包含"会命中带随机后缀的残留条目（实测踩过：选到没有密钥的旧供应商，
  // 于是卡片显示"无密钥"、保存按钮禁用，测试假失败）。
  const found = await waitFor(`(function(){
    return Boolean([...document.querySelectorAll('button.provider-connections__item')].filter(function(b){
      var t=(b.textContent||'').trim();
      return t===${JSON.stringify(route)} || t.slice(1)===${JSON.stringify(route)};
    })[0]);
  })()`);
  if (!found) {
    const items = await evalJS(`[...document.querySelectorAll('button.provider-connections__item')].map(function(b){return (b.textContent||'').trim().slice(0,30)})`);
    return { clicked: false, items: items };
  }
  const clicked = await evalJS(`(function(){
    var el=[...document.querySelectorAll('button.provider-connections__item')].filter(function(b){
      var t=(b.textContent||'').trim();
      return t===${JSON.stringify(route)} || t.slice(1)===${JSON.stringify(route)};
    })[0];
    el.setAttribute('data-uit-prov','1');
    return {clicked:true, text:(el.textContent||'').trim().slice(0,30)};
  })()`);
  await click('[data-uit-prov="1"]');
  // 等详情表单切到该供应商
  await waitFor(`(function(){
    return Boolean([...document.querySelectorAll('button.connection-title')].filter(function(b){
      return (b.textContent||'').trim().indexOf(${JSON.stringify(route)})>=0;
    })[0]);
  })()`);
  return clicked;
}

/** 标记详情表单容器：从标题按钮（文本 == route）向上找最近的、含「刷新模型」按钮的祖先。 */
function markDetailJS(route, buttonLabel) {
  return `(function(){
    var title=[...document.querySelectorAll('button.connection-title')].filter(function(b){
      return (b.textContent||'').trim().indexOf(${JSON.stringify(route)})>=0;
    })[0];
    if(!title) return {title:false};
    var p=title;
    for(var d=0; d<16 && p; d++){
      p=p.parentElement;
      if(!p) break;
      var btn=[...p.querySelectorAll('button')].filter(function(b){
        var hay=(b.textContent||'')+' '+(b.getAttribute('aria-label')||'')+' '+(b.getAttribute('title')||'');
        return hay.indexOf(${JSON.stringify(buttonLabel)})>=0;
      })[0];
      if(btn){ p.setAttribute('data-uit-card','1'); btn.setAttribute('data-uit-action','1');
        return {title:true, card:true, depth:d, refreshDisabled:!!btn.disabled}; }
    }
    return {title:true, card:false, reason:'no-refresh-button-in-detail'};
  })()`;
}

/** 在已标记的卡片里按文本或 aria-label/title 找按钮并点击。
 *  优先点**可用**的那个：详情表单里可能有多个同文本按钮（实测「保存更改」会撞到别的行）。 */function clickInCardJS(buttonLabel) {
  return `(function(){
    var card=document.querySelector('[data-uit-card="1"]');
    if(!card) return {clicked:false, reason:'no-card'};
    var matches=[...card.querySelectorAll('button')].filter(function(b){
      var hay=(b.textContent||'')+' '+(b.getAttribute('aria-label')||'')+' '+(b.getAttribute('title')||'');
      return hay.indexOf(${JSON.stringify(buttonLabel)})>=0;
    });
    if(!matches.length) return {clicked:false, reason:'no-button'};
    var enabled=matches.filter(function(b){return !b.disabled});
    var btn=enabled.length ? enabled[enabled.length-1] : matches[0];
    if(btn.disabled) return {clicked:false, disabled:true, total:matches.length};
    btn.click();
    return {clicked:true, total:matches.length, text:(btn.textContent||'').trim().slice(0,20) || (btn.getAttribute('aria-label')||'').slice(0,20)};
  })()`;
}

/** 卡片文本快照（用于等模型出现）。 */
const CARD_TEXT = `(function(){
  var card=document.querySelector('[data-uit-card="1"]');
  return card ? (card.innerText||'').replace(/\\s+/g,' ').slice(0,600) : '';
})()`;

(async () => {
  console.log("== UI 供应商/模型链路测试 ==");
  console.log(`hook=127.0.0.1:${HOOK}  dsh=127.0.0.1:${DSH}  fake=127.0.0.1:${FAKE_PORT}`);

  // 0) 钩子与 DSH 就绪
  const health = await fetch(`http://127.0.0.1:${HOOK}/health`).then((r) => r.json());
  console.log(`  hook: ${JSON.stringify(health)}`);
  check("UI 测试钩子可用", health.ok === true);

  const added = await setupProvider();
  check("经应用自身添加方法写入供应商", added.ok === true, added.ok ? `warning="${added.warning}"` : added.error);
  // 用实际路由（可能带后缀）做后续所有定位与复核
  route = await resolveRoute();
  console.log(`  info: 实际 route = ${route}`);
  const created = await providerProfile(route);
  check("DSH 侧已存在该供应商", Boolean(created));
  check(
    "添加时自动补齐了模型（DSH 对未知路由的要求）",
    (created?.models || []).length > 0,
    JSON.stringify((created?.models || []).map((m) => m.id)),
  );

  if (EXPLORE) {
    console.log("\n== 探索模式：设置面板内的可交互元素 ==");
    await openSettings();
    await openModelServicesTab();
    const dump = await evalJS(`(function(){
      var root=${SETTINGS_SCOPE};
      if(!root) return {scope:false};
      var out=[];
      root.querySelectorAll('button,[role="tab"],input').forEach(function(el){
        var t=((el.textContent||'')+'|'+(el.getAttribute('aria-label')||'')+'|'+(el.getAttribute('placeholder')||'')).trim().replace(/\\s+/g,' ').slice(0,40);
        if(t.trim()) out.push({tag:el.tagName, t:t, cls:(el.className||'').toString().slice(0,40)});
      });
      return {scope:true, items:out.slice(0,60)};
    })()`);
    console.log(JSON.stringify(dump, null, 1));
    await cleanup();
    process.exit(0);
  }

  // 1) 打开设置 → 模型服务
  const opened = await openSettings();
  check("设置面板可打开", Boolean(opened), opened ? `${opened.tag} "${opened.label}"` : "未找到设置入口");

  const tab = await openModelServicesTab();
  check("「模型服务」页签可打开", Boolean(tab && tab.found), tab ? `navCount=${tab.navCount} active=${tab.active}` : "未找到页签");

  // 2) 选中目标供应商（模型服务页是两栏：左列表 / 右详情）
  const listed = await evalJS(`[...document.querySelectorAll('button.provider-connections__item')].map(function(b){return (b.textContent||'').trim().slice(0,30)})`);
  console.log(`  info: 列表项 = ${JSON.stringify(listed)}`);
  // 陈旧检测：设置面板只在挂载时拉一次设置，若它缓存了 DSH 已不存在的供应商，
  // 后续"选中目标条目"会选到幽灵条目（实测：DSH 里没有 -345435，面板却显示它）。
  const dshNames = await evalJS(`(async function(){
    var s = await window.go.main.App.Settings();
    return ((s && s.providers) || []).map(function(p){return p.name});
  })()`);
  const ghosts = (listed || []).map((t) => t.slice(1)).filter((n) => !dshNames.includes(n));
  if (ghosts.length) {
    console.log(`  WARN  设置面板存在 ${ghosts.length} 个陈旧条目（${ghosts.join(", ")}）—— 请重启应用后重跑本测试`);
  }
  const selected = await selectProvider(route);
  check("左侧列表可选中测试供应商", Boolean(selected.clicked), JSON.stringify(selected));

  const detail = await evalJS(markDetailJS(route, "刷新模型"));
  check("详情表单含目标供应商与「刷新模型」按钮", Boolean(detail.card), JSON.stringify(detail));
  // 3) 点「刷新模型」（详情表单里的按钮；实测条件是 baseUrl 非空且连接已配置）
  const fetched = await evalJS(markDetailJS(route, "刷新模型"));
  check("「刷新模型」按钮可用（baseUrl 已回填）", Boolean(fetched.card) && !fetched.refreshDisabled, JSON.stringify(fetched));
  const clicked = await evalJS(clickInCardJS("刷新模型"));
  check("点击「刷新模型」", Boolean(clicked.clicked), JSON.stringify(clicked));

  // 4) 模型候选出现在详情表单里（假端点返回 fake-alpha / fake-beta / fake-vl-vision）
  const gotModels = await waitFor(`(function(){
    var card=document.querySelector('[data-uit-card="1"]');
    var t=card?(card.innerText||''):'';
    return t.indexOf('fake-alpha')>=0 && t.indexOf('fake-beta')>=0;
  })()`, { timeoutMs: 15000 });
  const cardText = await evalJS(CARD_TEXT);
  check("刷新后详情里出现假端点的模型", Boolean(gotModels), cardText.replace(/\s+/g, " ").slice(0, 120));

  // 5) 改动表单，制造"未保存更改"（实测：无改动时「保存更改」是禁用的）
  //    注意：程序化 .click() 对 React 受控 checkbox 不生效（实测 checkedAfter 仍为 true），
  //    所以用**受控文本框**来制造改动 —— 文本框 + input 事件是 React 能可靠接收的。
  const NEW_URL = `http://127.0.0.1:${FAKE_PORT}/v1`;
  const edited = await evalJS(`(function(){
    var card=document.querySelector('[data-uit-card="1"]');
    if(!card) return {ok:false, reason:'no-card'};
    var input=[...card.querySelectorAll('input.provider-url-input, input.mem-input[type=text]')][0];
    if(!input) return {ok:false, reason:'no-url-input'};
    var setter=Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype,'value').set;
    setter.call(input, ${JSON.stringify(NEW_URL + "?ui=1")});
    input.dispatchEvent(new Event('input', {bubbles:true}));
    input.dispatchEvent(new Event('change', {bubbles:true}));
    return {ok:true, value:input.value};
  })()`);
  check("改动 API 地址（制造未保存更改）", Boolean(edited.ok), JSON.stringify(edited));
  const dirtyShown = await waitFor(`(function(){
    var card=document.querySelector('[data-uit-card="1"]');
    return card ? (card.innerText||'').indexOf('未保存更改')>=0 : false;
  })()`, { timeoutMs: 10000 });
  check("卡片显示「未保存更改」", Boolean(dirtyShown));

  // 6) 保存（实测按钮文案是「保存更改」）
  //    先给桥方法装探针，记录前端实际发了什么（否则"没生效"只能猜）。
  await evalJS(`(function(){
    if (window.__uiTestSpy) return true;
    window.__uiTestSpy = { calls: [] };
    var A = window.go.main.App;
    ['SaveProvider','SaveProviderWithKey','SetConnectionKey','DeleteProvider'].forEach(function(name){
      var orig = A[name];
      if (typeof orig !== 'function') return;
      A[name] = function(){
        var args = Array.prototype.slice.call(arguments);
        try { window.__uiTestSpy.calls.push({ m: name, args: JSON.parse(JSON.stringify(args)) }); } catch(e) {}
        return orig.apply(A, args);
      };
    });
    return true;
  })()`);

  //    注意：前端 apply() 后会 reload（调我们的 Settings()），期间 busy=true → 保存按钮临时禁用；
  //    所以要**等按钮变可用**再点，而不是立刻点（实测踩过：立刻点得到 disabled）。
  const saveEnabled = await waitFor(`(function(){
    var card=document.querySelector('[data-uit-card="1"]');
    if(!card) return false;
    var save=[...card.querySelectorAll('button')].filter(function(b){return (b.textContent||'').trim()==='保存更改'})[0];
    return Boolean(save && !save.disabled);
  })()`, { timeoutMs: 40000, intervalMs: 700 });
  check("「保存更改」变为可用（详情已 dirty 且不再 busy）", Boolean(saveEnabled));
  const saved = await evalJS(clickInCardJS("保存更改"));
  check("点击「保存更改」", Boolean(saved.clicked), JSON.stringify(saved));
  if (saved.clicked) {
    await waitFor(`(function(){
      var card=document.querySelector('[data-uit-card="1"]');
      return card ? (card.innerText||'').indexOf('无未保存更改')>=0 : false;
    })()`, { timeoutMs: 10000 });
  }

  // 7) 复核 DSH 侧确实写入（读回，不信界面显示）
  //    保存是异步的（前端 apply → 桥 → DSH），所以要轮询等它落地。
  let profile = null;
  for (let i = 0; i < 12; i += 1) {
    profile = await providerProfile(route);
    const url = profile?.baseURL || "";
    if (url.includes("?ui=1")) break;
    await sleep(800);
  }
  const spyCalls = await evalJS(`(window.__uiTestSpy && window.__uiTestSpy.calls) || []`);
  const saveCalls = (spyCalls || []).filter((c) => /SaveProvider/.test(c.m));
  console.log(`  info: 前端保存时调用的桥方法 = ${JSON.stringify(saveCalls.map((c) => ({ m: c.m, baseUrl: c.args?.[0]?.baseUrl, requestUrl: c.args?.[0]?.requestUrl, chatUrl: c.args?.[0]?.chatUrl, models: (c.args?.[0]?.models||[]).slice(0,4), keys: Object.keys(c.args?.[0] || {}).length })))}`);
  check(
    "DSH 侧已保存界面改动（baseURL 带上了 ?ui=1 标记）",
    (profile?.baseURL || "").includes("?ui=1"),
    JSON.stringify(profile?.baseURL),
  );
  check(
    "DSH 侧模型列表完好（刷新得到的 3 个模型）",
    (profile?.models || []).length === 3,
    JSON.stringify((profile?.models || []).map((m) => m.id)),
  );

  // 8) 负例：不可达端点必须报错，不能静默成功
  await dshRpc("settings.mutate", {
    ns: "llm-pi-ai",
    ops: [{ op: "set", path: ["providers", route, "baseURL"], value: "http://127.0.0.1:9/v1" }],
  });
  await sleep(600);
  const negative = await evalJS(clickInCardJS("刷新模型"));
  await sleep(4500);
  const warn = await evalJS(`(function(){
    var card=document.querySelector('[data-uit-card="1"]');
    var txt=card?(card.innerText||''):'';
    // 覆盖真实的提示文案：实测不可达端点时前端显示的是"没有自动获取到模型。可以直接在下方手动填写模型 ID。"
    var m=txt.match(/[^\\n]*(失败|无法|没有自动获取|未获取|could not|Failed|failed|unreachable)[^\\n]*/);
    return {hasFail: Boolean(m), sample: m?m[0].slice(0,140):'', tail: txt.replace(/\\s+/g,' ').slice(-200)};
  })()`);
  check("不可达端点：卡片内出现失败提示（不静默）", Boolean(negative.clicked) && warn.hasFail, warn.sample || warn.tail);

  await cleanup();
  const after = await providerProfile(route);
  check("清理完成（供应商已删除）", after === undefined);

  console.log(`\nUI-FLOW ${fail === 0 ? "OK" : "FAILED"} (${pass}/${pass + fail})`);
  process.exit(fail === 0 ? 0 : 1);
})().catch(async (err) => {
  console.error(`FATAL: ${err.message}`);
  await cleanup();
  process.exit(1);
});
