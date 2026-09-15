// subagent-panel-domtest.js — 无浏览器环境下验证「子代理」注入脚本：
//   1) 从 frontend/dist/index.html 提取真实内联块（验证的是实际发布的产物，不是源码）
//   2) 在最小假 DOM 里执行，模拟 Reasonix 右侧栏结构 + mock Go 桥
//   3) 断言：tab 注入 / overlay 挂载 / 定位加固 / 卡片与进程行渲染 / React 占位节点未被动 / 幂等
// 用法：node scripts/subagent-panel-domtest.js
'use strict';
const fs = require('fs');
const path = require('path');

// ---------- 最小假 DOM ----------
class El {
  constructor(tag) {
    this.tagName = String(tag).toUpperCase();
    this.childNodes = [];
    this.attrs = {};
    this.style = {};
    this.className = '';
    this.id = '';
    this.title = '';
    this.type = '';
    this.onclick = null;
    this.parentNode = null;
    this._text = '';
  }
  get children() { return this.childNodes.filter(function (c) { return c.tagName !== '#TEXT'; }); }
  appendChild(c) { c.parentNode = this; this.childNodes.push(c); return c; }
  removeChild(c) {
    const i = this.childNodes.indexOf(c);
    if (i >= 0) { this.childNodes.splice(i, 1); c.parentNode = null; }
    return c;
  }
  get textContent() { return this._text + this.childNodes.map(function (c) { return c.textContent; }).join(''); }
  set textContent(v) { this._text = String(v); this.childNodes = []; }
  setAttribute(k, v) { this.attrs[k] = String(v); }
  getAttribute(k) { return Object.prototype.hasOwnProperty.call(this.attrs, k) ? this.attrs[k] : null; }
  addEventListener(type, fn) {
    this._listeners = this._listeners || {};
    this._listeners[type] = (this._listeners[type] || []).concat([fn]);
  }
  querySelector(sel) {
    let found = null;
    walk(this, function (n) { if (!found && matchesSelector(n, sel)) found = n; });
    return found;
  }
  querySelectorAll(sel) {
    const out = [];
    walk(this, function (n) { if (matchesSelector(n, sel)) out.push(n); });
    return out;
  }
}

function classList(el) {
  return String(el.className || '').split(/\s+/).filter(Boolean);
}
function matchesSimple(el, sel) {
  if (!el || el.tagName === '#TEXT') return false;
  const parts = sel.match(/(^[a-zA-Z][\w-]*)|(\.[\w-]+)|(#[\w-]+)/g) || [];
  let tagOk = true;
  for (const p of parts) {
    if (p.startsWith('.')) { if (classList(el).indexOf(p.slice(1)) < 0) return false; }
    else if (p.startsWith('#')) { if (el.id !== p.slice(1)) return false; }
    else { if (el.tagName !== p.toUpperCase()) tagOk = false; }
  }
  return tagOk;
}
function walk(root, fn) {
  for (const c of root.childNodes) { fn(c); walk(c, fn); }
}
function matchesSelector(el, sel) {
  const groups = sel.split(',').map(function (s) { return s.trim(); }).filter(Boolean);
  for (const g of groups) {
    const parts = g.split(/\s+/).filter(Boolean);
    if (matchesSimple(el, parts[parts.length - 1])) {
      if (parts.length === 1) return true;
      let anc = el.parentNode;
      let need = parts.length - 2;
      while (anc && need >= 0) {
        if (matchesSimple(anc, parts[need])) need--;
        anc = anc.parentNode;
      }
      if (need < 0) return true;
    }
  }
  return false;
}

function makeDoc() {
  const doc = {
    documentElement: new El('html'),
    readyState: 'complete'
  };
  doc.head = doc.documentElement.appendChild(new El('head'));
  doc.body = doc.documentElement.appendChild(new El('body'));
  doc.createElement = function (t) { return new El(t); };
  doc.createElementNS = function (ns, t) { return new El(t); };   // SVG（脚本用 createElementNS）
  doc.createTextNode = function (t) { const e = new El('#text'); e._text = String(t); return e; };
  doc.getElementById = function (id) {
    let found = null;
    if (doc.documentElement.id === id) return doc.documentElement;
    walk(doc.documentElement, function (n) { if (!found && n.id === id) found = n; });
    return found;
  };
  doc.querySelector = function (sel) {
    let found = null;
    if (matchesSelector(doc.documentElement, sel)) return doc.documentElement;
    walk(doc.documentElement, function (n) { if (!found && matchesSelector(n, sel)) found = n; });
    return found;
  };
  doc.querySelectorAll = function (sel) {
    const out = [];
    walk(doc.documentElement, function (n) { if (matchesSelector(n, sel)) out.push(n); });
    return out;
  };
  doc.addEventListener = function () { /* no-op */ };
  return doc;
}

// ---------- 1) 从 dist/index.html 提取真实内联块 ----------
const here = __dirname;
const root = path.resolve(here, '..');
const html = fs.readFileSync(path.join(root, 'frontend', 'dist', 'index.html'), 'utf8');
const MARK = '// dsh-subagent-panel.js';
const at = html.indexOf(MARK);
if (at < 0) { console.error('FAIL: dist/index.html 未内联 dsh-subagent-panel.js'); process.exit(1); }
const open = html.lastIndexOf('<script>', at);
const close = html.indexOf('</' + 'script>', at);
const inlined = html.slice(open + '<script>'.length, close);
console.log('[1] 提取内联块: ' + inlined.length + ' 字节');

// ---------- 2) 构建假 DOM + mock 桥 ----------
const doc = makeDoc();
doc.body.innerHTMLLike = true;

// 模拟 Reasonix 右栏（workbench-dock）：tabs 行 + body（body 保持 static，用于检验定位加固）
const dock = doc.body.appendChild(new El('div')); dock.className = 'workbench-dock';
const tools = dock.appendChild(new El('div')); tools.className = 'workbench-dock__tools';
const tabs = tools.appendChild(new El('div')); tabs.className = 'workbench-dock__tabs';
const nativeTab1 = tabs.appendChild(new El('button')); nativeTab1.className = 'workbench-dock__tab'; nativeTab1.textContent = '概览';
const nativeTab2 = tabs.appendChild(new El('button')); nativeTab2.className = 'workbench-dock__tab'; nativeTab2.textContent = '终端';
const bodyHost = dock.appendChild(new El('div')); bodyHost.className = 'workbench-dock__body';
const reactPlaceholder = bodyHost.appendChild(new El('div'));
reactPlaceholder.className = 'placeholder';
reactPlaceholder.textContent = '（原生内容占位）';

const mockResult = {
  ok: true,
  sessionId: 'session-8b83f456-aa4c-4a16-bc28-325a099d0590',
  parentTitle: '工作区内容与项目精神了解',
  counts: { subagents: 2, active: 1, processes: 3, jobs: 2, jobsActive: 1 },
  notes: ['说明一', '说明二'],
  jobs: [
    { id: 'pwsh-14', kind: 'pwsh', label: '& "C:\\x\\upload.ps1" 2>&1 | Select-Object -Last 3', status: 'completed',
      statusLabel: '已完成', detail: 'exit code: 0', startedAt: 1789382071836, finishedAt: 1789382087149, durationMs: 15313, running: false },
    { id: 'pwsh-15', kind: 'pwsh', label: '.\build-installer.ps1 -Bundle', status: 'running',
      statusLabel: '运行中', detail: '', startedAt: Date.now() - 4000, finishedAt: 0, durationMs: 4000, running: true }
  ],
  subagents: [
    { id: 'aaa', sessionId: 'session-aaa', kind: 'child', mode: 'continuable', label: '安全审计三个记忆插件', activity: 'active', running: true, cwd: 'C:/proj/dsh-reasonix-desktop', agentPreset: 'standard', title: '审计员', updatedAt: Date.now() },
    { id: 'bbb', sessionId: 'session-bbb', kind: 'child', mode: 'one-shot', label: '调研 dsh-std 协议', activity: 'inactive', running: false, cwd: 'C:/proj/dsh-reasonix-desktop', hasChildren: true, updatedAt: Date.now() - 1000 }
  ],
  processes: [
    { pid: 11268, ppid: 1, name: 'node.exe', memMb: 2596.2, category: 'DSH', cmd: 'node dsh.js' },
    { pid: 9240, ppid: 2, name: 'msedgewebview2.exe', memMb: 110.4, category: '本项目', cmd: 'webview2' },
    { pid: 6456, ppid: 11268, name: 'python.exe', memMb: 22.1, category: 'MCP', cmd: 'mcp_server' }
  ]
};

const sandbox = {
  window: null, document: doc, console: console,
  getComputedStyle: function (el) { return { position: (el && el.style && el.style.position) || 'static' }; },
  setInterval: function () { return 0; },
  clearInterval: function () { },
  setTimeout: function (fn, t) { return setTimeout(fn, t); },
  MutationObserver: undefined,
  Promise: Promise
};
sandbox.window = sandbox;
sandbox.window.__DSH_SUBAGENT_PANEL__ = undefined;
const diagLog = [];
sandbox.window.go = { main: { App: {
  SubagentPanel: function () { return Promise.resolve(mockResult); },
  LogFromFrontend: function (msg) { diagLog.push(String(msg)); }
} } };
sandbox.window.setInterval = sandbox.setInterval;
sandbox.window.clearInterval = sandbox.clearInterval;
sandbox.window.addEventListener = function () { };
sandbox.window.setTimeout = sandbox.setTimeout;

// ---------- 3) 执行内联脚本 ----------
const vm = require('vm');
vm.createContext(sandbox);
vm.runInContext(inlined, sandbox, { filename: 'inlined-subagent-panel.js' });
console.log('[2] 内联脚本执行完成（无异常）');

// ---------- 4) 断言 ----------
const results = [];
function check(name, cond, extra) { results.push({ name: name, ok: !!cond, extra: extra || '' }); }

const tab = doc.getElementById('dsh-sp-tab');
check('tab 注入到 .workbench-dock__tabs', !!tab && tab.parentNode === tabs, tab ? ('parent=' + (tab.parentNode && tab.parentNode.className)) : 'no tab');
check('tab 文案为「子代理」', tab && tab.textContent.indexOf('子代理') >= 0, tab ? tab.textContent : '');
check('按钮融入原生样式类 workbench-dock__tab', tab && classList(tab).indexOf('workbench-dock__tab') >= 0, tab ? tab.className : '');
check('原生 tab 未被移除', tabs.children.length === 3 && nativeTab1.parentNode === tabs && nativeTab2.parentNode === tabs,
  'children=' + tabs.children.length);
check('诊断已上报(桥 LogFromFrontend)', diagLog.some(function (m) { return m.indexOf('tab-injected') >= 0; }),
  diagLog.join(' | ').slice(0, 160));

// 点击打开面板 → 等 Promise 落地
if (tab && tab.onclick) tab.onclick();
setTimeout(function () {
  const panel = doc.getElementById('dsh-sp-panel');
  check('overlay 面板已挂载到 dock body', !!panel && panel.parentNode === bodyHost);
  check('宿主定位加固为 relative（原为 static）', bodyHost.style.position === 'relative', 'position=' + bodyHost.style.position);
  check('React 占位节点仍在', reactPlaceholder.parentNode === bodyHost && reactPlaceholder.textContent.indexOf('占位') >= 0);
  const cards = doc.querySelectorAll('.dsh-sp-card--sub');
  const rows = doc.querySelectorAll('.dsh-sp-table tbody tr');
  check('子智能体卡片渲染 2 张', cards.length === 2, 'got ' + cards.length);
  check('进程表渲染 3 行', rows.length === 3, 'got ' + rows.length);
  const activeCard = doc.querySelectorAll('.dsh-sp-card--sub.dsh-sp-card--active');
  check('活跃子智能体高亮 1 张', activeCard.length === 1, 'got ' + activeCard.length);
  // 图标：tab 内应有 svg（与原生 tab 同风格）
  check('tab 含 SVG 图标', !!(tab && tab.querySelector('svg')), tab ? tab.textContent : '');
  check('tab 文本用原生 label 类', !!(tab && tab.querySelector('.workbench-dock__tab-label')));
  // 后台任务区块（含状态）
  const jobCards = doc.querySelectorAll('.dsh-sp-card--job');
  const chips = doc.querySelectorAll('.dsh-sp-status');
  check('后台任务渲染 2 条', jobCards.length === 2, 'got ' + jobCards.length);
  check('任务状态徽标 2 个', chips.length === 2, 'got ' + chips.length);
  check('状态文案含已完成/运行中', (function () {
    const txt = chips.map(function (c) { return c.textContent; }).join(',');
    return txt.indexOf('已完成') >= 0 && txt.indexOf('运行中') >= 0;
  })(), chips.map(function (c) { return c.textContent; }).join(','));
  check('运行中任务高亮卡片', doc.querySelectorAll('.dsh-sp-card--job.dsh-sp-card--active').length === 1);
  check('面板含任务计数摘要', panel && /后台任务（2/.test(panel.textContent), panel ? '' : '');
  check('面板含会话与计数摘要', panel && /子智能体 2/.test(panel.textContent) && /进程 3/.test(panel.textContent));
  check('活跃徽标显示 1', (function () { const b = doc.getElementById('dsh-sp-badge'); return b && b.textContent === '1'; })());

  // 关闭面板：只应移除我们自己的 overlay，React 节点不受影响
  if (panel) {
    const btns = panel.querySelectorAll('.dsh-sp-btn');
    const closeBtn = btns[btns.length - 1];
    if (closeBtn && closeBtn.onclick) closeBtn.onclick();
    check('关闭后 overlay 移除', !doc.getElementById('dsh-sp-panel'));
    check('关闭后 React 节点完好', bodyHost.childNodes.indexOf(reactPlaceholder) >= 0 && tabs.children.length === 3);
  }

  // 事件委托：点击原生 tab（概览/终端）应收起我们的面板
  if (tab && tab.onclick) tab.onclick();          // 再次打开
  const delegated = !!tabs.attrs['dsh-sp-delegated'];
  check('tab 栏已挂事件委托', delegated);
  if (tabs._listeners && tabs._listeners.click && tabs._listeners.click.length) {
    const ev = { target: nativeTab2 };
    tabs._listeners.click[0](ev);
    check('点原生 tab 后自动收起面板', !doc.getElementById('dsh-sp-panel'));
  } else {
    check('点原生 tab 后自动收起面板', false, 'no click listener captured');
  }

  // 幂等：再次执行脚本不应重复注入
  sandbox.window.__DSH_SUBAGENT_PANEL__ = true; // 模拟已加载
  vm.runInContext(inlined, sandbox, { filename: 'inlined-subagent-panel-again.js' });
  check('幂等：重复加载不新增 tab', tabs.children.length === 3, 'children=' + tabs.children.length);

  const failed = results.filter(function (r) { return !r.ok; });
  results.forEach(function (r) {
    console.log((r.ok ? '  PASS  ' : '  FAIL  ') + r.name + (r.extra ? '  [' + r.extra + ']' : ''));
  });
  console.log(failed.length ? ('\nDOM-TEST FAILED (' + failed.length + '/' + results.length + ')') : ('\nDOM-TEST OK (' + results.length + '/' + results.length + ')'));
  process.exit(failed.length ? 1 : 0);
}, 300);
