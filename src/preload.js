'use strict';
// Reasonix 前端 → DSH 桥接层
// 注入 window.go.main.App（DSH 实现 + mock 回退）和 window.runtime（事件通道）
const { ipcRenderer, contextBridge } = require('electron');
const { homedir } = require('os');

// 会话 cwd 缺失时的回退根目录：用户主目录
// （原代码硬编码了作者机器路径，任何其他机器上都会指向不存在的目录）
const HOME_FALLBACK = () => homedir() || 'C:/';

// ---------- 外观与语言默认偏好 ----------
// 默认中文界面：前端 i18n 的 detectLocale 用 navigator.language 判断（zh* → 'zh'）。
// preload 在页面脚本前运行，这里把 navigator.language 固定为中文；用户设置页切换语言后
// 由 SetDesktopLanguage 写入 localStorage('dsh:language')，下次启动读它保持用户选择。
try {
  const savedLang = (typeof localStorage !== 'undefined' && localStorage.getItem('dsh:language')) || 'zh';
  const navLang = savedLang === 'en' ? 'en-US' : savedLang === 'zh-TW' ? 'zh-TW' : 'zh-CN';
  Object.defineProperty(navigator, 'language', { get: () => navLang, configurable: true });
  Object.defineProperty(navigator, 'languages', { get: () => [navLang, 'zh-CN', 'en'], configurable: true });
} catch (e) { console.warn('[dsh] language override failed:', e && e.message || e); }

// 默认外观：深色（前端 initTheme 读 localStorage('reasonix-theme')；用户设置页切换主题时
// 由 SetDesktopAppearance 持久化到这里，下次启动保持用户选择）。
// 注意：preload 顶层 document 未就绪时 localStorage.setItem 会失败（读取正常），
// 所以延迟到 DOMContentLoaded 后写；前端 initTheme 在模块加载时执行，当前会话用默认值，
// 写入后从下次启动开始保持深色。
function applyDefaultAppearance() {
  try { if (!localStorage.getItem('reasonix-theme')) localStorage.setItem('reasonix-theme', 'dark'); } catch {}
}
if (typeof document !== 'undefined' && document.readyState !== 'loading') applyDefaultAppearance();
else if (typeof document !== 'undefined') document.addEventListener('DOMContentLoaded', applyDefaultAppearance);
else applyDefaultAppearance();

// 会话/项目"钉住"集合（localStorage 持久化；前端右键菜单"钉住"调用 SetTopicPinned/SetProjectPinned）
const PINNED_TOPICS_KEY = 'dsh:pinned-topics';
const PINNED_PROJECTS_KEY = 'dsh:pinned-projects';
function readPinnedList(key) {
  try { const a = JSON.parse(localStorage.getItem(key) || '[]'); return Array.isArray(a) ? a : []; } catch { return []; }
}
function savePinnedList(key, list) {
  try { localStorage.setItem(key, JSON.stringify(list)); } catch {}
}

// ---------- 事件通道（window.runtime.EventsOn） ----------
const eventChannels = new Map(); // channel -> Set<cb>
function eventsOn(channel, cb) {
  if (!eventChannels.has(channel)) eventChannels.set(channel, new Set());
  eventChannels.get(channel).add(cb);
  return () => eventChannels.get(channel)?.delete(cb);
}
function eventsEmit(channel, payload) {
  eventChannels.get(channel)?.forEach((cb) => { try { cb(payload); } catch {} });
}

// 主进程事件 → 渲染层
ipcRenderer.on('dsh:event', (_e, wire) => {
  // Reasonix 的 onEvent 监听 EVENT_CHANNEL = "agent:event"
  eventsEmit('agent:event', wire);
});

// ---------- 费用计算（价格外置 prices.json，可编辑/可抓取更新） ----------
// 默认价格（prices.json 缺失时的兜底；正常从配置加载）
const DEFAULT_PRICES = {
  deepseekOfficial: {
    'deepseek-v4-flash': { cacheHit: 0.02, input: 1, output: 2 },
    'deepseek-v4-pro': { cacheHit: 0.025, input: 3, output: 6 },
    default: { cacheHit: 0.02, input: 1, output: 2 },
  },
  relays: {},
  relayProviders: [
    { id: 'deepseek-official', name: 'DeepSeek 官方', baseUrl: 'https://api.deepseek.com', official: true },
  ],
};
let DEEPSEEK_OFFICIAL_PRICES = DEFAULT_PRICES.deepseekOfficial;
let RELAY_PRICES = DEFAULT_PRICES.relays;
let RELAY_PROVIDERS = DEFAULT_PRICES.relayProviders;

// 从 prices.json 加载（主进程读文件）
async function loadPrices() {
  try {
    const cfg = await ipcRenderer.invoke('prices:load');
    if (cfg && cfg.deepseekOfficial) {
      DEEPSEEK_OFFICIAL_PRICES = cfg.deepseekOfficial;
      RELAY_PRICES = cfg.relays || {};
      RELAY_PROVIDERS = cfg.relayProviders || RELAY_PROVIDERS;
      return { ok: true, updatedAt: cfg.updatedAt };
    }
  } catch (e) { console.warn('[dsh] prices.json 加载失败，使用默认价格:', e && e.message || e); }
  return { ok: false };
}
// 保存价格到 prices.json
async function savePrices() {
  try {
    await ipcRenderer.invoke('prices:save', {
      updatedAt: new Date().toISOString().slice(0, 7),
      source: 'https://api-docs.deepseek.com/zh-cn/quick_start/pricing',
      deepseekOfficial: DEEPSEEK_OFFICIAL_PRICES,
      relays: RELAY_PRICES,
      relayProviders: RELAY_PROVIDERS,
    });
  } catch {}
}
// 抓取官方最新价格并应用
async function fetchOfficialPrices() {
  const res = await ipcRenderer.invoke('prices:fetchOfficial');
  if (res && res.ok && res.prices) {
    for (const [model, price] of Object.entries(res.prices)) {
      DEEPSEEK_OFFICIAL_PRICES[model] = price;
    }
    await savePrices();
    return { ok: true, models: Object.keys(res.prices), updatedAt: new Date().toISOString().slice(0, 7) };
  }
  return { ok: false, error: res && res.error };
}
// 计算费用：token 用量 + provider/model → 元
function calcCost(usage, provider, model) {
  const key = String(model || '').toLowerCase();
  const table = RELAY_PRICES[provider] || DEEPSEEK_OFFICIAL_PRICES[key] || DEEPSEEK_OFFICIAL_PRICES.default || { cacheHit: 0.02, input: 1, output: 2 };
  const cacheRead = (usage && usage.cacheReadTokens) || 0;
  const input = (usage && usage.uncachedInputTokens) || 0;
  const output = (usage && usage.outputTokens) || 0;
  return (cacheRead * table.cacheHit + input * table.input + output * table.output) / 1e6;
}
// 启动时加载配置
void loadPrices();

// ---------- 用量分析：DSH tokenUsage → usage 事件 ----------
async function emitUsageEvent(sessionId) {
  try {
    const list = await rpc('session.list', {});
    const s = (list && list.items || []).find((x) => x.sessionId === sessionId);
    const v = (s && s.projections && s.projections.values) || {};
    const tu = v.tokenUsage || {};
    const cacheHit = tu.cacheReadTokens || 0;
    const cacheMiss = tu.uncachedInputTokens || 0;
    const output = tu.outputTokens || 0;
    // 动态取当前 provider/model 算费用（不要写死官方 flash）
    let provider = 'deepseek-official';
    let model = 'deepseek-v4-flash';
    try {
      const models = await rpc('session.models', { sessionId });
      if (models && models.current) {
        provider = models.current.provider || provider;
        model = models.current.model || model;
      }
    } catch {}
    const usage = {
      promptTokens: cacheMiss,
      completionTokens: output,
      totalTokens: cacheHit + cacheMiss + output,
      cacheHitTokens: cacheHit,
      cacheMissTokens: cacheMiss,
      reasoningTokens: 0,
      sessionCacheHitTokens: cacheHit,
      sessionCacheMissTokens: cacheMiss,
      source: 'dsh',
      cost: calcCost(tu, provider, model),
      currencyCode: 'CNY',
    };
    eventsEmit('agent:event', { kind: 'usage', usage, tabId: sessionId });
  } catch {}
}

// ---------- DSH 历史 → WireEvent 重放 ----------
// 事件类型归一化：DSH 实时流/历史记录的事件名写法可能不同
// （turn/started vs turn/start、turn/done vs turn/end），统一匹配两种写法。
function normType(t) {
  return String(t || '')
    .replace(/\/started$/, '/start')
    .replace(/\/done$/, '/end');
}

// 重放代次：切 tab 后旧代次的延迟重放全部作废，防止历史串台到新 tab
let replayGen = 0;
let lastReplay = null; // { tabId, at } —— 同一 tab 短时间内重复重放（如打开 tab 触发多次）只播一次

// DSH session.history 返回 events 数组；转成 Reasonix WireEvent 序列喂给前端，
// 前端 transcript store 据此构建对话历史。
function replayHistory(sessionId, events) {
  if (!events || !events.length) return;
  // 幂等去重：同一 tab 3 秒内重复触发只重放一次
  const now = Date.now();
  if (lastReplay && lastReplay.tabId === sessionId && now - lastReplay.at < 3000) return;
  lastReplay = { tabId: sessionId, at: now };
  const gen = ++replayGen;
  const wire = [];
  let currentTurn = null;
  let textBuf = '';
  const flushText = () => {
    if (textBuf) {
      wire.push({ kind: 'text', text: textBuf, tabId: sessionId });
      textBuf = '';
    }
  };
  const pushMsg = (role, contentBlocks) => {
    flushText();
    const blocks = Array.isArray(contentBlocks) ? contentBlocks : [];
    for (const b of blocks) {
      if (b.type === 'text' && b.text) {
        wire.push({ kind: 'text', text: b.text, tabId: sessionId });
      } else if (b.type === 'tool-call' || b.type === 'tool_call') {
        wire.push({
          kind: 'tool_dispatch',
          tool: { name: b.name || 'tool', callId: b.id || b.callId },
          detail: b.arguments ? String(b.arguments).slice(0, 500) : undefined,
          tabId: sessionId,
        });
      } else if (b.type === 'tool-result' || b.type === 'tool_result') {
        wire.push({
          kind: 'tool_result',
          tool: { name: b.name || 'tool', callId: b.callId || b.id },
          detail: b.result ? String(b.result).slice(0, 500) : undefined,
          tabId: sessionId,
        });
      }
    }
  };
  for (const { event } of events) {
    const d = event.data || {};
    const t = normType(event.type);
    if (t === 'turn/start') {
      currentTurn = d.turn;
      wire.push({ kind: 'turn_started', tabId: sessionId });
    } else if (t === 'turn/end') {
      wire.push({ kind: 'turn_done', tabId: sessionId });
      currentTurn = null;
    } else if (event.type === 'user/message' || event.type === 'user/prompt') {
      // 系统注入（plugin/goal 等）的 user/message 带 source.kind !== 'user'，不按用户消息重放
      if (d.source && d.source.kind !== 'user') continue;
      pushMsg('user', d.content || d.prompt);
    } else if (event.type === 'assistant/message') {
      pushMsg('assistant', d.message && d.message.content);
    } else if (event.type === 'tool/call') {
      // 工具调用是独立会话事件：转 wire tool_dispatch（与主进程 dshEventToWire 同构）
      const t = d.tool || {};
      wire.push({
        kind: 'tool_dispatch',
        tool: { name: t.name || d.name || d.toolName || 'tool', callId: d.callId || t.callId },
        tabId: sessionId,
      });
    } else if (event.type === 'tool/result') {
      const t = d.tool || {};
      wire.push({
        kind: 'tool_result',
        tool: { name: t.name || d.name || d.toolName || 'tool', callId: d.callId || t.callId },
        detail: d.result ? String(d.result).slice(0, 500) : undefined,
        tabId: sessionId,
      });
    }
  }
  flushText();
  // 延迟重放（限 300 条，避免海量 setTimeout 拖垮前端）；
  // 每条发送前检查代次：切到别的 tab 后，旧 tab 的排队重放直接丢弃
  const limited = wire.slice(0, 300);
  limited.forEach((w, i) => setTimeout(() => { if (gen === replayGen) eventsEmit('agent:event', w); }, i * 5));
  // 重放后补发 usage 事件（驱动用量分析卡）
  setTimeout(() => { if (gen === replayGen) emitUsageEvent(sessionId); }, limited.length * 5 + 50);
}

// ---------- DSH history → HistorySlice（前端历史加载核心） ----------
function historyEventsToSlice(events, tabId) {
  // 按 turn 分组，把 assistant/chunk 的 text-delta 合并进 assistant/message
  const entries = [];
  let order = 0;
  let chunkBuf = {}; // index -> text
  let currentEntry = null;
  const flushChunks = () => {
    if (currentEntry && Object.keys(chunkBuf).length) {
      const text = Object.keys(chunkBuf).sort((a, b) => Number(a) - Number(b)).map((k) => chunkBuf[k]).join('');
      if (text) {
        const existing = currentEntry.message.content || '';
        // DSH 常把最终全文同时写在 assistant/message.content，chunk 流与它同文：
        // content 已包含 chunk 流时以 content 为准（避免重复），否则按 content + chunk 流顺序拼接。
        if (!existing.includes(text)) currentEntry.message.content = existing + text;
      }
      chunkBuf = {};
    }
  };
  for (const { event } of events || []) {
    const d = event.data || {};
    if (event.type === 'user/message' || event.type === 'assistant/message') {
      // chunkBuf 只属于"当前打开的条目"：只有换到新条目（用户消息）时才落盘上一个
      // assistant 条目的 chunk 流；assistant/message 分支在创建好条目之后再 flush，
      // 把流式文本合并进新助手条目，而不是上一条用户消息。
      if (event.type === 'user/message') flushChunks();
      const msg = event.type === 'user/message' ? d : (d.message || d);
      const role = msg.role || (event.type.startsWith('user') ? 'user' : 'assistant');
      // content blocks → 文本 + 工具调用
      let content = '';
      const toolCalls = [];
      const blocks = Array.isArray(msg.content) ? msg.content : [];
      for (const b of blocks) {
        if (b.type === 'text' && b.text) content += b.text;
        else if (b.type === 'tool-call' || b.type === 'tool_call') {
          toolCalls.push({ id: b.id || b.callId, name: b.name || 'tool', arguments: b.arguments ? String(b.arguments) : '' });
        }
      }
      if (role === 'assistant' && !content && toolCalls.length === 0) {
        // 纯推理消息：跳过（reasoning 单独处理）
        continue;
      }
      currentEntry = {
        entryId: 's:' + tabId + ':m:' + order,
        turn: d.turn || (d.message && d.message.turn) || 0,
        order: order++,
        message: {
          role,
          content,
          createdAt: event.time,
          ...(toolCalls.length ? { toolCalls } : {}),
        },
        refs: [],
      };
      entries.push(currentEntry);
      // assistant/message：chunk 流合并进这个新助手条目（content 文本 + chunk 流按顺序拼接）
      if (event.type === 'assistant/message') flushChunks();
    } else if (event.type === 'assistant/chunk') {
      const c = d.chunk;
      if (!c) continue;
      if (c.type === 'text-delta') {
        chunkBuf[c.index || 0] = (chunkBuf[c.index || 0] || '') + c.text;
      }
    } else if (event.type === 'tool/call') {
      // 工具调用是独立会话事件（不是 assistant/chunk）：推入 slices 让历史面板可见
      const t = d.tool || {};
      entries.push({
        entryId: 's:' + tabId + ':t:' + order,
        turn: d.turn || (currentEntry && currentEntry.turn) || 0,
        order: order++,
        message: {
          role: 'tool',
          content: '',
          toolCalls: [{ id: d.callId || t.callId, name: t.name || d.name || d.toolName || 'tool', arguments: '' }],
          createdAt: event.time,
        },
        refs: [],
      });
    } else if (event.type === 'tool/result') {
      const t = d.tool || {};
      entries.push({
        entryId: 's:' + tabId + ':r:' + order,
        turn: d.turn || (currentEntry && currentEntry.turn) || 0,
        order: order++,
        message: {
          role: 'tool',
          content: d.result ? String(d.result).slice(0, 500) : '',
          toolCalls: [{ id: d.callId || t.callId, name: t.name || d.name || d.toolName || 'tool', arguments: '' }],
          createdAt: event.time,
        },
        refs: [],
      });
    }
  }
  flushChunks();
  return entries;
}

// ---------- 安全模式 ↔ DSH 权限映射 ----------
// Reasonix ToolApprovalMode: ask | auto | yolo
// DSH permissions: read-only | workspace-write | danger-full-access
const MODE_TO_PERMISSION = { ask: 'read-only', auto: 'workspace-write', yolo: 'danger-full-access' };
const PERMISSION_TO_MODE = { 'read-only': 'ask', 'workspace-write': 'auto', 'danger-full-access': 'yolo' };
// 新建会话统一使用 code preset（功能最全的编码 Agent）。
// 权限三档（read-only/workspace-write/danger-full-access）独立由 /permission 命令控制，
// 不与 agentPreset 绑定（preset 管工具集，permission 管沙箱权限，是两个维度）。
const DEFAULT_PRESET = 'code';
let currentMode = 'auto'; // 默认 workspace-write
let activeTabId = null;   // 前端记忆的当前 tab（主进程的 active 只是"列表第一个"）

// ---------- DSH 工具函数 ----------
const rpc = (method, payload, timeoutMs) => ipcRenderer.invoke('dsh:rpc', method, payload, timeoutMs);
const sessions = () => ipcRenderer.invoke('dsh:sessions');
const history = (sid) => ipcRenderer.invoke('dsh:history', sid);
const prompt = (sid, text, timeoutMs) => ipcRenderer.invoke('dsh:prompt', sid, text, timeoutMs);
// 提交消息的统一错误处理：失败不静默，console 有痕迹、调用方拿得到结果
const submitPrompt = async (sid, input) => {
  try {
    const r = await prompt(sid, input, 10 * 60 * 1000);
    if (r && r.ok === false) console.error('[dsh] prompt failed:', r.error);
    return r;
  } catch (e) { console.error('[dsh] prompt failed:', e && e.message || e); return { ok: false, error: String(e && e.message || e) }; }
};
// 切换 DSH 权限：通过 commands/execute Typert 端点执行 /permission 命令。
// 注意：不能用 session.prompt 发 /permission（那会当普通消息发给模型，不执行命令）。
// 正确做法是 POST /api/commands/execute，payload 为 { args: { agentId, line } }。
async function setDshPermission(sessionId, mode) {
  const perm = MODE_TO_PERMISSION[mode];
  if (!perm || !sessionId) return;
  const tryPerm = async (p) => {
    try {
      const r = await rpc('commands/execute', { args: { agentId: sessionId, line: '/permission ' + p } });
      const rr = r && r.result;
      if (rr && rr.kind === 'error') return { ok: false, error: rr.text };
      return { ok: true };
    } catch (e) { return { ok: false, error: String(e && e.message || e) }; }
  };
  let res = await tryPerm(perm);
  // /permission 实参是 preset 表键：默认配置没有 read-only preset（只有 workspace-write /
  // danger-full-access），所以 ask 模式（read-only）首次切换会报 unknown preset，
  // 失败时回退到 DSH 的"每次询问"语义 preset = workspace-write（sandbox=workspace-write, approval=ask）
  if (!res.ok && mode === 'ask') res = await tryPerm('workspace-write');
  if (!res.ok) console.warn('[dsh] /permission ' + perm + ' failed:', res.error);
  currentMode = mode;
  // 完全授权（yolo / danger-full-access）时通知主进程解除桥的防御性校验（原则 2）
  ipcRenderer.send('bridge:setFullAccess', mode === 'yolo');
}
const createSession = (cwd, agentPreset) => ipcRenderer.invoke('dsh:create', cwd, agentPreset);
const cancelSession = (sid) => ipcRenderer.invoke('dsh:cancel', sid);

// 取某 tab 对应会话的工作区 cwd（文件系统根目录）
async function cwdOfTab(tabID) {
  try {
    const tabs = await sessions();
    const t = tabID ? tabs.find((x) => x.id === tabID) : (tabs.find((x) => x.active) || tabs[0]);
    const c = (t && (t.workspaceRoot || t.cwd)) || '';
    if (c) return c;
  } catch {}
  return HOME_FALLBACK();
}

// 定位"活跃 tab"的会话 id：优先渲染层记忆的 activeTabId（主进程的 active 只是
// "列表第一个"，多 tab 时会把消息发到错误会话），其次主进程标记的 x.active，最后兜底列表第一个。
async function activeTabIdOf() {
  if (activeTabId) return activeTabId;
  try {
    const tabs = await sessions();
    const active = tabs.find((t) => t.active) || tabs[0];
    return (active && active.id) || null;
  } catch { return null; }
}

// ---------- 记忆（Memory）：跟着项目走，存 <cwd>/.dsh/memory.md ----------
// 格式：markdown，每条记忆 = 一个 "## <name>" 标题块，标题下是 body。
// 解析成 Reasonix 的 MemoryFact[]，文档(docs)即 memory.md 本身。
function memoryPathOf(cwd) {
  const base = String(cwd || '').replace(/[\/]+$/, '');
  return base + '/.dsh/memory.md';
}
function parseMemoryMd(body, storeDir, scope) {
  const facts = [];
  const lines = String(body || '').replace(/\r/g, '').split('\n');
  let cur = null;
  for (const line of lines) {
    const m = line.match(/^##[ \t]+(.+?)[ \t]*$/);
    if (m) {
      if (cur) facts.push(cur);
      const name = m[1].trim();
      cur = { id: name, name, description: name, type: 'project', scope, body: '', freshness: 'fresh' };
    } else if (cur) {
      cur.body = cur.body ? cur.body + '\n' + line : line;
    }
  }
  if (cur) facts.push(cur);
  return facts;
}
function serializeMemoryMd(facts) {
  const parts = [];
  for (const f of facts || []) {
    parts.push('## ' + (f.name || f.id || ''));
    if (f.body) parts.push(String(f.body).replace(/[ \t]+$/, ''));
    parts.push('');
  }
  return parts.join('\n');
}
// 解析某 tab 的 MemoryView
async function memoryViewOf(tabID) {
  let cwd = '';
  try {
    const tabs = await sessions();
    const t = tabID ? tabs.find((x) => x.id === tabID) : (tabs.find((x) => x.active) || tabs[0]);
    cwd = (t && (t.workspaceRoot || t.cwd)) || '';
  } catch {}
  if (!cwd) cwd = HOME_FALLBACK();
  const storeDir = memoryPathOf(cwd);
  let body = '';
  let exists = false;
  try {
    const r = await ipcRenderer.invoke('memory:read', cwd);
    exists = !!r.exists; body = r.body || '';
  } catch {}
  const facts = parseMemoryMd(body, storeDir, 'project');
  return {
    docs: exists ? [{ path: storeDir, scope: 'project', body, imports: [], depth: 0, order: 0, precedence: 0 }] : [],
    facts,
    archives: [],
    scopes: [{ scope: 'project', path: storeDir }],
    conflicts: [],
    instructionDiagnostics: [],
    lastRecall: { query: '', hits: [], omitted: 0, charBudget: 0, usedChars: 0 },
    storeDir,
    storeGlobalDir: undefined,
    available: true,
  };
}
// 在指定 tab 的 memory.md 里追加一条记忆（记住）
async function rememberInto(tabID, scope, note) {
  let cwd = '';
  try {
    const tabs = await sessions();
    const t = tabID ? tabs.find((x) => x.id === tabID) : (tabs.find((x) => x.active) || tabs[0]);
    cwd = (t && (t.workspaceRoot || t.cwd)) || '';
  } catch {}
  if (!cwd) cwd = HOME_FALLBACK();
  const r = await ipcRenderer.invoke('memory:read', cwd);
  const body = r.body || '';
  const text = String(note || '').trim();
  const firstLine = text.replace(/\r/g, '').split('\n')[0].trim();
  const name = (firstLine || 'memory').slice(0, 60);
  const fact = { name, body: text, type: 'project', scope: scope || 'project', freshness: 'fresh' };
  const facts = parseMemoryMd(body, memoryPathOf(cwd), 'project');
  facts.push(fact);
  await ipcRenderer.invoke('memory:write', cwd, serializeMemoryMd(facts));
  return name;
}
// 删除指定名字的记忆
async function forgetFrom(tabID, name) {
  let cwd = '';
  try {
    const tabs = await sessions();
    const t = tabID ? tabs.find((x) => x.id === tabID) : (tabs.find((x) => x.active) || tabs[0]);
    cwd = (t && (t.workspaceRoot || t.cwd)) || '';
  } catch {}
  if (!cwd) cwd = HOME_FALLBACK();
  const r = await ipcRenderer.invoke('memory:read', cwd);
  const facts = parseMemoryMd(r.body || '', memoryPathOf(cwd), 'project');
  const kept = facts.filter((f) => f.name !== name);
  await ipcRenderer.invoke('memory:write', cwd, serializeMemoryMd(kept));
}

// ---------- DSH → TabMeta 转换（与主进程一致） ----------
// 修复会话标题 mojibake（UTF-8 被按 Latin-1 解码的乱码），与主进程 fixMojibake 一致
function fixMojibake(s) {
  if (typeof s !== 'string' || !s) return s;
  if (/[\u00c0-\u00ff]/.test(s)) {
    try {
      const fixed = Buffer.from(s, 'latin1').toString('utf8');
      if (!fixed.includes('\uFFFD') && /[^\x00-\x08\x0B\x0C\x0E-\x1F]/.test(fixed)) return fixed;
    } catch {}
  }
  return s;
}
function sessionToTabMeta(s, idx) {
  const v = s.projections?.values || {};
  const title = fixMojibake(v.title) || '未命名会话';
  const workspaceRoot = s.cwd || 'C:\\';
  const wsName = workspaceRoot.split(/[\\/]/).filter(Boolean).pop() || 'workspace';
  return {
    id: s.sessionId,
    tabType: 'session',
    scope: 'project',
    workspaceRoot,
    workspaceName: wsName,
    workspacePath: workspaceRoot,
    topicId: s.sessionId,
    topicTitle: title,
    sessionPath: s.sessionId + '.jsonl',
    label: v.agentPreset || 'code',
    ready: true,
    running: !!s.running,
    pendingPrompt: false,
    backgroundJobs: 0,
    cancelRequested: false,
    cancellable: !!s.running,
    mode: 'normal',
    collaborationMode: 'normal',
    tokenMode: 'full',
    agentPreset: v.agentPreset || 'code',
    toolApprovalMode: v.permissions?.currentValue ? PERMISSION_TO_MODE[v.permissions.currentValue] || 'auto' : 'auto',
    permissions: v.permissions,
    active: idx === 0,
    cwd: workspaceRoot,
  };
}

// ---------- App 方法实现 ----------
const appImpl = {
  // ===== 平台/窗口 =====
  Platform: async () => 'windows',
  MinimiseMainWindow: async () => { ipcRenderer.send('win:min'); },
  ToggleMaximiseMainWindow: async () => { ipcRenderer.send('win:max'); },
  IsMainWindowMaximised: async () => ipcRenderer.invoke('win:isMaximized'),
  CloseMainWindow: async () => { ipcRenderer.send('win:close'); },

  // ===== Tab / 会话 =====
  ListTabs: async () => {
    // 主进程 dsh:sessions 已转换好 TabMeta，这里直接用（避免二次转换把 id 弄丢）
    const tabs = await sessions();
    // 恢复前端记忆的当前 tab（主进程的 active 只是"列表第一个"，切 tab 后重拉列表会跳回）
    if (activeTabId) {
      const cur = tabs.find((t) => t.id === activeTabId);
      if (cur) { for (const t of tabs) t.active = (t.id === activeTabId); }
      else activeTabId = null;
    }
    const active = tabs.find((t) => t.active) || tabs[0];
    if (active?.permissions?.currentValue) {
      currentMode = PERMISSION_TO_MODE[active.permissions.currentValue] || currentMode;
      // 当前会话已是完全授权时，同步解除桥的防御性校验
      ipcRenderer.send('bridge:setFullAccess', active.permissions.currentValue === 'danger-full-access');
    }
    return tabs;
  },

  HistorySliceForTab: async (tabID, req) => {
    try {
      const h = await history(tabID);
      const entries = historyEventsToSlice(h.events, tabID);
      return {
        entries,
        nextCursor: '',
        // 主进程 dsh:history 已改为 { maxMessages: 300 }：hasMore 表示还有更早的历史。
        // 本次不做真正翻页，只正确反映是否还有更早历史
        hasOlder: h && h.hasMore === true,
        totalTurns: entries.length,
        startTurn: entries.length ? 1 : 0,
        endTurn: entries.length,
        stale: false,
        revision: 1,
        revisionKnown: true,
        digest: 'dsh-' + tabID,
        source: 'dsh',
      };
    } catch (e) {
      return { entries: [], nextCursor: '', hasOlder: false, totalTurns: 0, startTurn: 0, endTurn: 0, stale: false, revision: 0, error: String(e && e.message || e) };
    }
  },

  // ===== 项目树（DSH 会话按 cwd 分组） =====
  buildProjectTree: async () => {
    const list = await rpc('session.list', {});
    const items = (list && list.items) || [];
    const byRoot = new Map();
    for (const s of items) {
      const v = s.projections?.values || {};
      const root = s.cwd || 'C:\\';
      const title = v.title || '未命名会话';
      if (!byRoot.has(root)) byRoot.set(root, []);
      byRoot.get(root).push({
        key: s.sessionId,
        kind: 'topic', // 用 'topic' 而非 'session'：前端 ProjectTree 只给 topic 节点渲染行级操作
        // （hover 归档按钮 + 右键菜单：钉住/重命名/移入回收站）；session 节点会被
        // projectTreeShouldRenderTopicActions 的 !isSessionNode gate 隐藏，导致无法删除对话。
        label: title,
        root, // 所属项目根目录：前端 projectTreeTopicOpenRequest 用 node.root 作为 workspaceRoot，缺失会导致"无法打开会话"
        topicId: s.sessionId, sessionPath: s.sessionId + '.jsonl',
        turns: v.sessionStats?.turns, turnsState: 'valid', health: 'ok',
        lastActivityAt: s.updatedAt, open: true, running: !!s.running,
        pinned: readPinnedList(PINNED_TOPICS_KEY).includes(s.sessionId), // 会话"钉住"（右键菜单）
        children: [],
      });
    }
    const projects = [];
    for (const [root, sessionsArr] of byRoot) {
      const name = root.split(/[\\/]/).filter(Boolean).pop() || 'workspace';
      projects.push({
        key: 'p:' + root, kind: 'project', label: name, root,
        projectColor: undefined, pinned: readPinnedList(PINNED_PROJECTS_KEY).includes(root), open: true,
        children: sessionsArr,
      });
    }
    return {
      revision: 1,
      projects,
      catalog: { state: 'ready', mode: 'memory', revision: 1, indexed: items.length, total: items.length, repairPending: 0 },
      indexed: items.length,
      total: items.length,
      indexingDone: true,
    };
  },
  GetProjectTreeSnapshot: async () => {
    const tree = await appImpl.buildProjectTree();
    return {
      revision: 1,
      projects: tree.projects, // 不按 pinned 过滤：DSH 会话没有 pinned 标记，过滤会导致空树
      catalog: tree.catalog,
      indexed: tree.indexed,
      total: tree.total,
      indexingDone: true,
    };
  },
  // 运行时投影快照：前端 useProjectTreeRuntimeProjection 用它叠加会话运行状态（open/running/status）
  GetProjectTreeRuntimeSnapshot: async () => {
    const list = await rpc('session.list', {});
    const items = (list && list.items) || [];
    return {
      revision: Date.now(),
      topics: items.map((s) => ({
        node: {
          topicId: s.sessionId,
          scope: 'project',
          workspaceRoot: s.cwd || 'C:\\',
          sessionPath: s.sessionId + '.jsonl',
          open: true,
          running: !!s.running,
          status: s.running ? 'running' : 'idle',
          children: [],
        },
      })),
    };
  },
  ListProjectTopics: async (req) => {
    const tree = await appImpl.buildProjectTree();
    const folder = req.scope === 'global'
      ? tree.projects.find((p) => p.kind === 'global_folder')
      : tree.projects.find((p) => p.kind === 'project' && p.root === req.workspaceRoot);
    const all = (folder && folder.children) || [];
    const start = Math.max(0, Number.parseInt(req.cursor || '0', 10) || 0);
    const limit = Math.min(200, Math.max(1, req.limit || 50));
    const items = all.slice(start, start + limit);
    return {
      items,
      nextCursor: start + items.length < all.length ? String(start + items.length) : undefined,
      revision: 1,
      complete: true,
      readyDirectories: 1,
      pendingDirectories: 0,
      failedDirectories: 0,
    };
  },
  GetTopicSummary: async (key) => {
    const tree = await appImpl.buildProjectTree();
    const project = tree.projects.find((p) => p.root === key.workspaceRoot);
    return (project && project.children && project.children.find((c) => c.topicId === key.topicId))
      || { key: '', kind: key.scope === 'global' ? 'global_topic' : 'topic', label: '', children: [] };
  },
  GetSessionCatalogStatus: async () => {
    const list = await rpc('session.list', {});
    const n = (list && list.items && list.items.length) || 0;
    return { state: 'ready', mode: 'memory', revision: 1, indexed: n, total: n, repairPending: 0 };
  },
  RebuildSessionCatalog: async () => {},
  Meta: async () => {
    const tabs = await sessions();
    const active = tabs.find((t) => t.active) || tabs[0];
    const workspacePath = active?.workspacePath || active?.cwd || 'C:\\';
    const mode = active?.toolApprovalMode || currentMode;
    return {
      label: active?.label || 'DeepSeek-V4-Flash',
      ready: true,
      eventChannel: 'agent:event',
      cwd: workspacePath,
      workspaceRoot: workspacePath,
      workspaceName: active?.workspaceName || 'workspace',
      workspacePath,
      sandboxPath: workspacePath,
      gitBranch: 'main',
      imageInputEnabled: true,
      autoApproveTools: mode === 'yolo',
      bypass: false,
      collaborationMode: 'normal',
      toolApprovalMode: mode,
      goal: '',
      goalStatus: 'stopped',
    };
  },
  MetaForTab: async (tabID) => {
    const tabs = await sessions();
    // 用 tabID 定位会话（取它的 workspaceRoot/cwd 等）；找不到回退当前活跃/首个
    const active = (tabID ? tabs.find((t) => t.id === tabID) : null) || (tabs.find((t) => t.active) || tabs[0]);
    const workspacePath = active?.workspacePath || active?.cwd || 'C:\\';
    const mode = active?.toolApprovalMode || currentMode;
    return {
      label: active?.label || 'DeepSeek-V4-Flash',
      ready: true,
      eventChannel: 'agent:event',
      cwd: workspacePath,
      workspaceRoot: workspacePath,
      workspaceName: active?.workspaceName || 'workspace',
      workspacePath,
      sandboxPath: workspacePath,
      gitBranch: 'main',
      imageInputEnabled: true,
      autoApproveTools: mode === 'yolo',
      bypass: false,
      collaborationMode: 'normal',
      toolApprovalMode: mode,
      goal: '',
      goalStatus: 'stopped',
    };
  },
  SetActiveTab: async (tabID) => {
    activeTabId = tabID;
    try { const h = await history(tabID); replayHistory(tabID, h.events); } catch {}
  },
  OpenProjectTab: async (workspaceRoot, topicId) => {
    // 复用已有会话（topicId = sessionId），否则新建
    let sid = topicId;
    if (!sid) {
      const tabs = await sessions();
      const existing = tabs.find((s) => s.cwd === workspaceRoot);
      sid = existing ? existing.id : (await createSession(workspaceRoot, DEFAULT_PRESET)).sessionId;
    }
    try { const h = await history(sid); replayHistory(sid, h.events); } catch {}
    return sessionToTabMeta({ sessionId: sid, cwd: workspaceRoot, running: false, projections: {} }, 0);
  },
  OpenGlobalTab: async () => {
    const res = await createSession(undefined, DEFAULT_PRESET);
    return sessionToTabMeta({ sessionId: res.sessionId, cwd: undefined, running: false, projections: {} }, 0);
  },
  EnsureBlankTab: async () => ({}),
  EnsureBlankSurface: async () => ({}),
  CloseTab: async (tabID) => {},
  ReorderTabs: async () => {},
  CreateTopic: async () => ({}),
  // 重命名对话 → DSH session.rename
  RenameTopic: async (topicID, title) => {
    try { await rpc('session.rename', { sessionId: topicID, title: String(title || '') }); }
    catch (e) { console.error('[dsh] RenameTopic failed:', e && e.message || e); }
  },
  DeleteTopic: async () => {},
  // 移入回收站（前端两段式确认后调用）→ 归档 + 物理删除日志（与 deleteSession 同语义）
  TrashTopic: async (topicID) => {
    try {
      const r = await ipcRenderer.invoke('dsh:deleteSession', topicID);
      if (!r || !r.ok) console.error('[dsh] TrashTopic failed:', r && r.error);
      return r;
    } catch (e) { console.error('[dsh] TrashTopic failed:', e && e.message || e); return { ok: false, error: String(e && e.message || e) }; }
  },
  RenameProject: async () => {},
  RemoveWorkspace: async () => {},
  PickWorkspace: async () => {
    const picked = await ipcRenderer.invoke('dsh:pickFolder');
    return picked || '';
  },
  SwitchWorkspace: async (path) => {
    if (!path) return '';
    // 打开该目录的会话：找已有 or 新建
    const tabs = await sessions();
    const existing = tabs.find((s) => s.cwd === path);
    if (existing) return existing.id;
    const res = await createSession(path, DEFAULT_PRESET);
    return res.sessionId || '';
  },
  IsolatedWorktreeAvailability: async () => ({ available: true, repoRoot: '', branch: 'main', sourceDirty: false }),
  DeliveryWorktreeAvailability: async () => ({ available: true, repoRoot: '', branch: 'main', sourceDirty: false }),

  // ===== 新建项目（新建空白项目） =====
  PickBlankProjectParent: async () => {
    const picked = await ipcRenderer.invoke('dsh:pickFolder');
    return picked || '';
  },
  CreateBlankProject: async (parentDir, projectName) => {
    const name = String(projectName || '').trim();
    // Windows 保留字符/保留名 + 空名校验：非法时返回 { ok:false, error }（与主进程
    // dsh:createFolder 的失败形态一致），成功仍返回目标路径字符串。
    const invalidName = !name
      || name === '.' || name === '..'
      || /[\\/:*?"<>|]/.test(name) // Windows 保留字符（含路径分隔符）
      || /[. ]$/.test(name)        // Windows 会剥离尾随点/空格，导致重名/解析异常
      || /^(con|prn|aux|nul|com[1-9]|lpt[1-9])(\.|$)/i.test(name); // Windows 保留设备名
    if (invalidName) return { ok: false, error: 'invalid project name' };
    const parent = String(parentDir || '').replace(/[\\/]+$/, '');
    const target = parent + '\\' + name;
    await ipcRenderer.invoke('dsh:createFolder', target);
    return target;
  },

  // ===== 会话管理（DSH RPC） =====
  ListSessions: async () => {
    const tabs = await sessions();
    return tabs.map((t) => ({ path: t.sessionPath || t.id + '.jsonl', preview: t.topicTitle, turns: 0, createdAt: Date.now(), lastActivityAt: Date.now(), current: !!t.active, open: true }));
  },
  ListSessionsForTab: async () => { const tabs = await sessions(); return tabs.map((t) => ({ path: t.sessionPath, preview: t.topicTitle, turns: 0, createdAt: Date.now(), lastActivityAt: Date.now(), current: !!t.active, open: true })); },
  ListTrashedSessions: async () => [],
  ResumeSession: async (path) => {
    const id = path ? path.replace(/\.jsonl$/, '') : null;
    if (id) { try { const h = await history(id); replayHistory(id, h.events); } catch {} }
    return [];
  },
  ResumeSessionForTab: async () => [],
  ResumeSessionPage: async () => {},
  ResumeSessionPageForTab: async () => {},
  NewSession: async () => {},
  NewSessionForTab: async () => {},
  OpenChannelSessionForTab: async () => ({}),
  OpenChannelSessionPageForTab: async () => ({}),
  RenameSession: async () => {},
  DeleteSession: async () => {},
  PurgeTrashedSession: async () => {},
  RestoreSession: async () => {},
  ClearSession: async () => ({ sessionPath: '', sessionGeneration: 0 }),
  ClearSessionForTab: async () => ({ sessionPath: '', sessionGeneration: 0 }),
  Fork: async () => ({}),
  ForkForTab: async () => ({}),
  Compact: async () => ({}),
  CompactForTab: async () => ({}),
  Rewind: async () => ({}),
  RewindForTab: async () => ({}),
  CommitRewindForTab: async () => ({}),
  UndoRewindForTab: async () => ({}),
  PreviewRewindForTab: async () => ({}),
  History: async () => ({}),
  HistoryForTab: async (tabID) => {
    try { const h = await history(tabID); return historyEventsToSlice(h.events, tabID).map((e) => e.message); } catch { return []; }
  },
  HistoryPage: async () => ({ messages: [], startTurn: 0, endTurn: 0, totalTurns: 0, hasOlder: false }),
  HistoryPageForTab: async () => ({ messages: [], startTurn: 0, endTurn: 0, totalTurns: 0, hasOlder: false }),
  HistoryCheckpointTurnsForTab: async () => [],
  HistoryContentForTab: async () => ({ entryId: '', field: '', chunk: 0, chunks: 1, data: '', done: true, stale: false }),
  Checkpoints: async () => [],
  CheckpointTurnForTab: async () => 0,
  CheckpointSummaryForTab: async () => null,
  ToolResultForTab: async () => '',
  OpenTopicSession: async (scope, workspaceRoot, topicID) => {
    if (topicID) { try { const h = await history(topicID); replayHistory(topicID, h.events); } catch {} }
    return sessionToTabMeta({ sessionId: topicID, cwd: workspaceRoot, running: false, projections: {} }, 0);
  },
  // 前端某些路径（如 history 恢复）调用 OpenSession，与 OpenTopicSession 同语义
  OpenSession: async (scope, workspaceRoot, topicID, sessionPath) => {
    if (topicID) { try { const h = await history(topicID); replayHistory(topicID, h.events); } catch {} }
    return sessionToTabMeta({ sessionId: topicID, cwd: workspaceRoot, running: false, projections: {} }, 0);
  },
  ActivateTopic: async (scope, workspaceRoot, topicID) => {
    if (topicID) { try { const h = await history(topicID); replayHistory(topicID, h.events); } catch {} }
    return sessionToTabMeta({ sessionId: topicID, cwd: workspaceRoot, running: false, projections: {} }, 0);
  },
  StartTopicActivation: async (req) => {
    // 前端 singleSurfaceLayout 打开会话走 activateTopic → StartTopicActivation。
    // 必须返回 { meta, tabId, requestId }，否则前端 C=_.meta 为 undefined → "无法打开会话"。
    const rq = (req && typeof req === 'object') ? req : {};
    const scope = rq.scope;
    const workspaceRoot = rq.workspaceRoot;
    const topicId = rq.topicId;
    if (topicId) { try { const h = await history(topicId); replayHistory(topicId, h.events); } catch {} }
    const meta = sessionToTabMeta({ sessionId: topicId, cwd: workspaceRoot, running: false, projections: {} }, 0);
    return {
      meta,
      tabId: meta.id,
      requestId: rq.requestId || ('bridge-' + Date.now()),
    };
  },
  CurrentTaskSessionID: async () => '',
  OpenTaskSession: async () => ({}),
  OpenTaskSessionForTab: async () => ({}),
  Jobs: async () => [],
  Balance: async () => null,
  BalanceForTab: async () => null,
  Memory: async () => memoryViewOf(null),
  MemoryForTab: async (tabID) => memoryViewOf(tabID),
  MemoryRevisions: async () => [],
  MemoryRevisionsForTab: async () => [],
  MemorySuggestions: async () => ({ memories: [], skills: [], generatedAt: '', available: false, source: 'dsh' }),
  MemorySuggestionsForTab: async () => ({ memories: [], skills: [], generatedAt: '', available: false, source: 'dsh' }),
  Remember: async (scope, note) => rememberInto(null, scope, note),
  RememberForTab: async (tabID, scope, note) => rememberInto(tabID, scope, note),
  Forget: async (name) => forgetFrom(null, name),
  ForgetForTab: async (tabID, name) => forgetFrom(tabID, name),
  AcceptMemorySuggestion: async () => '',
  AcceptMemorySuggestionForTab: async () => '',
  RestoreArchivedMemory: async () => ({}),
  RestoreArchivedMemoryForTab: async () => ({}),
  RestoreMemoryRevision: async () => ({}),
  RestoreMemoryRevisionForTab: async () => ({}),
  AcceptSkillSuggestionForTab: async () => {},
  Version: async () => (await ipcRenderer.invoke('app:version')) || '0.1.0',
  ReloadCommands: async () => {},
  SlashArgs: async () => ({ args: [] }),
  ScanPromptHistory: async () => [],
  GetDesktopZoomFactor: async () => 1,
  SetDesktopZoomFactor: async () => {},
  SaveWindowState: async () => {},

  // ===== 文件系统（主进程 fs IPC） =====
  ListDir: async (rel) => {
    const items = await ipcRenderer.invoke('fs:list', rel);
    return (items || []).map((e) => ({ name: e.name, path: e.path, isDir: e.isDir, displayName: e.name, displayPath: e.path }));
  },
  ListDirForTab: async (tabID, rel) => {
    const cwd = await cwdOfTab(tabID);
    const dirAbs = cwd + '/' + String(rel || '').replace(/^\//, '');
    const items = await ipcRenderer.invoke('fs:listAbs', dirAbs, cwd);
    if (items && items.error) return [];
    const relBase = String(rel || '').replace(/\/$/, '');
    return (items || []).map((e) => {
      const relPath = relBase ? relBase + '/' + e.name : e.name;
      return { name: e.name, path: relPath, isDir: e.isDir, displayName: e.name, displayPath: relPath };
    });
  },
  ReadFile: async (path) => {
    const body = await ipcRenderer.invoke('fs:read', path);
    return {
      path,
      body: body || '',
      size: (body || '').length,
      truncated: false,
      binary: false,
    };
  },
  ReadFileForTab: async (tabID, path) => {
    const cwd = await cwdOfTab(tabID);
    const fileAbs = cwd + '/' + String(path || '').replace(/^\//, '');
    const r = await ipcRenderer.invoke('fs:readAbs', fileAbs, cwd);
    if (!r || !r.ok) {
      return { path, body: '', size: 0, truncated: false, binary: false, err: r && r.error };
    }
    return {
      path,
      body: r.body || '',
      size: r.size || (r.body || '').length,
      truncated: false,
      binary: false,
    };
  },
  ListWorkspaces: async () => {
    const tabs = await sessions();
    const roots = [...new Set(tabs.map((t) => t.cwd).filter(Boolean))];
    return roots.map((r) => ({ path: r, name: r.split(/[\\/]/).filter(Boolean).pop() || r, current: false }));
  },
  ListProjectTree: async () => (await appImpl.buildProjectTree()).projects.map((p) => p.root),
  RevealWorkspacePath: async () => {},
  RevealWorkspacePathForTab: async () => {},
  RevealWorkspaceWriterForTab: async () => {},
  ResolveWorkspacePathForTab: async () => '',
  OpenWorkspacePath: async () => {},
  OpenWorkspacePathForTab: async () => {},
  WorkspaceChanges: async (tabID) => {
    const cwd = (await sessions()).find((t) => t.id === tabID)?.cwd || HOME_FALLBACK();
    return await ipcRenderer.invoke('git:changes', cwd);
  },
  WorkspaceChangeDetail: async (tabID, path) => {
    const cwd = (await sessions()).find((t) => t.id === tabID)?.cwd || HOME_FALLBACK();
    return await ipcRenderer.invoke('git:detail', cwd, path);
  },
  WorkspaceConflictForTab: async () => null,
  WorkspaceRevisionForTab: async () => ({ revisions: { content: 0, tree: 0, workingTree: 0, gitMeta: 0, session: 0 }, watchState: 'active' }),
  WorkspaceGitHistory: async (tabID, path) => {
    const cwd = (await sessions()).find((t) => t.id === tabID)?.cwd || HOME_FALLBACK();
    return await ipcRenderer.invoke('git:history', cwd, path);
  },
  WorkspaceGitCommitDetail: async () => ({ diff: '' }),
  PreviewWorkspaceFileRevertForTab: async (tabID, path) => ({ ok: true, canFiles: true, path, planId: 'dsh-' + Date.now() }),
  CommitWorkspaceFileRevertForTab: async () => ({ ok: true, undoAvailable: false, transactionId: 'dsh-tx' }),
  GitBranches: async () => [],
  GitCheckout: async () => {},

  // ===== 对话 =====
  Submit: async (input) => { const sid = await activeTabIdOf(); if (sid) return submitPrompt(sid, input); },
  SubmitForTab: async (tabID, input) => submitPrompt(tabID, input),
  SubmitToTab: async (tabID, input) => submitPrompt(tabID, input),
  SubmitToTabWithID: async (tabID, input) => submitPrompt(tabID, input),
  SubmitDisplay: async (_display, input) => { const sid = await activeTabIdOf(); if (sid) return submitPrompt(sid, input); },
  SubmitDisplayToTab: async (tabID, _display, input) => submitPrompt(tabID, input),
  SubmitDisplayToTabWithID: async (tabID, _display, input) => submitPrompt(tabID, input),
  Steer: async (input) => { const sid = await activeTabIdOf(); if (sid) return submitPrompt(sid, input); },
  SteerForTab: async (tabID, input) => submitPrompt(tabID, input),
  Cancel: async (tabID) => { await cancelSession(tabID); },
  CancelForTab: async (tabID) => { await cancelSession(tabID); },
  // 工具审批：DSH 权限是预设级（read-only/workspace-write/danger-full-access），
  // 工具由 agent 按当前权限自动批准/拒绝，没有"逐项人工审批"概念。
  // 这里明确返回不支持，避免前端"点了没反应"以为是 bug。
  Approve: async () => {
    console.warn('[dsh] DSH 权限模式不支持逐项工具审批（工具由 agent 按当前权限自动批准/拒绝）；如需放行请切换安全模式到 auto/yolo');
    return { ok: false, reason: 'unsupported' };
  },
  Reject: async () => {
    console.warn('[dsh] DSH 权限模式不支持逐项工具审批');
    return { ok: false, reason: 'unsupported' };
  },
  ApproveTab: async () => ({ ok: false, reason: 'unsupported' }),
  AnswerQuestion: async () => {},

  Settings: async () => ({
    providers: [], defaultModel: 'deepseek-v4-flash', plannerModel: 'deepseek-v4-flash',
    subagentModel: 'deepseek-v4-flash', subagentEffort: 'auto', maxSubagentDepth: 3,
    maxSubagentConcurrency: 2, maxParallelWriters: 1, autoPlan: 'none',
    defaultToolApprovalMode: 'ask', compactRatio: 1,
  }),
  HooksSettings: async () => ({ hooks: [] }),
  SaveHooksSettings: async () => {},
  SaveHooksSettingsForRoot: async () => {},
  TrustProjectHooks: async () => {},
  TrustProjectHooksForRoot: async () => {},

  // ===== 安全模式（三档 ↔ DSH 权限） =====
  SetToolApprovalMode: async (mode) => {
    if (!(mode in MODE_TO_PERMISSION)) return;
    try {
      const tabs = await sessions();
      const t = tabs.find((x) => x.active) || tabs[0];
      if (t) await setDshPermission(t.id, mode);
      else currentMode = mode;
    } catch { currentMode = mode; }
  },
  SetToolApprovalModeForTab: async (tabID, mode) => {
    if (!(mode in MODE_TO_PERMISSION)) return;
    await setDshPermission(tabID, mode);
  },
  ToolApprovalMode: async () => currentMode,
  ToolApprovalModeForTab: async () => currentMode,
  SetMode: async () => {},
  SetModeForTab: async () => {},
  Mode: async () => 'normal',
  ModeForTab: async () => 'normal',
  SetTokenMode: async () => {},
  SetTokenModeForTab: async () => {},
  SetCollaborationMode: async () => {},
  SetCollaborationModeForTab: async () => {},
  SetGoal: async () => {},
  SetGoalForTab: async () => {},
  ClearGoal: async () => {},
  ClearGoalForTab: async () => {},

  // ===== 模型（DSH session.models / selectModel 真实映射） =====
  dshModelsFor: async (tabID) => {
    try {
      const list = await rpc('session.list', {});
      const sid = tabID || (list && list.items && list.items[0] && list.items[0].sessionId);
      if (!sid) return null;
      return await rpc('session.models', { sessionId: sid });
    } catch { return null; }
  },
  SetModel: async (ref) => {
    try {
      const sid = await activeTabIdOf();
      if (sid && ref) {
        const parts = String(ref).split('/');
        const provider = parts[0], model = parts[1] || parts[0];
        await rpc('session.selectModel', { sessionId: sid, provider, model });
      }
    } catch {}
  },
  SetModelForTab: async (tabID, ref) => {
    try {
      if (tabID && ref) {
        const parts = String(ref).split('/');
        const provider = parts[0], model = parts[1] || parts[0];
        await rpc('session.selectModel', { sessionId: tabID, provider, model });
      }
    } catch {}
  },
  SetEffort: async (effort) => {
    try {
      const sid = await activeTabIdOf();
      if (sid) {
        const m = await rpc('session.models', { sessionId: sid });
        const cur = m && m.current;
        if (cur) await rpc('session.selectModel', { sessionId: sid, provider: cur.provider, model: cur.model, reasoningEffort: effort });
      }
    } catch {}
  },
  SetEffortForTab: async (tabID, effort) => {
    try {
      const m = await rpc('session.models', { sessionId: tabID });
      const cur = m && m.current;
      if (cur) await rpc('session.selectModel', { sessionId: tabID, provider: cur.provider, model: cur.model, reasoningEffort: effort });
    } catch {}
  },
  SetDefaultModel: async () => {},
  SetPlannerModel: async () => {},
  SetSubagentModel: async () => {},
  SetSubagentEffort: async () => {},
  SetMaxSubagentDepth: async () => {},
  SetMaxSubagentConcurrency: async () => {},
  SetMaxParallelWriters: async () => {},
  Models: async () => {
    const m = await appImpl.dshModelsFor();
    if (!m) return [];
    const out = [];
    for (const g of (m.groups || [])) {
      for (const mod of (g.models || [])) {
        out.push({
          ref: g.id + '/' + mod.id,
          provider: g.id,
          model: mod.id,
          current: m.current && m.current.provider === g.id && m.current.model === mod.id,
        });
      }
    }
    return out;
  },
  ModelsForTab: async (tabID) => {
    const m = await appImpl.dshModelsFor(tabID);
    if (!m) return [];
    const out = [];
    for (const g of (m.groups || [])) {
      for (const mod of (g.models || [])) {
        out.push({
          ref: g.id + '/' + mod.id,
          provider: g.id,
          model: mod.id,
          current: m.current && m.current.provider === g.id && m.current.model === mod.id,
        });
      }
    }
    return out;
  },
  ModelForTab: async (tabID) => {
    const m = await appImpl.dshModelsFor(tabID);
    return m && m.current ? (m.current.provider + '/' + m.current.model) : 'deepseek-official/deepseek-v4-flash';
  },
  EffortForTab: async (tabID) => {
    const m = await appImpl.dshModelsFor(tabID);
    const cur = m && m.current;
    if (!cur) return { supported: false, current: 'auto', default: 'auto', levels: [] };
    return { supported: true, current: cur.reasoningEffort || 'high', default: 'high', levels: ['off', 'high', 'max'] };
  },
  Effort: async () => (await appImpl.EffortForTab()),
  DefaultModel: async () => 'deepseek-official/deepseek-v4-flash',
  SetAgentPreset: async () => {},
  SetAgentPresetForTab: async () => {},

  // ===== 主题/设置/远程/机器人（安全兜底结构） =====
  ListThemePacks: async () => [],
  GetThemeExperience: async () => ({}),
  ActivateThemePack: async () => {},
  ActivateBaseStyle: async () => {},
  DeleteThemePack: async () => {},
  DisableThemePack: async () => {},
  ExportThemePack: async () => '',
  SaveThemePack: async () => ({}),
  PickThemeBackground: async () => '',
  RestoreGraphiteAppearance: async () => {},
  SetDesktopCheckUpdates: async () => {},
  SetDesktopConversationWidth: async () => {},
  SetDesktopCurrency: async () => {},
  SetDesktopLanguage: async (lang) => {
    // 持久化语言偏好：下次启动 preload 覆盖 navigator.language 时读它（前端 detectLocale 生效）
    try {
      const norm = (lang === 'en' || lang === 'zh' || lang === 'zh-TW') ? lang : 'zh';
      localStorage.setItem('dsh:language', norm);
      return { ok: true };
    } catch (e) { return { ok: false, error: String(e && e.message || e) }; }
  },
  SetDesktopLayoutStyle: async () => {},
  SetDesktopMetrics: async () => {},
  SetDesktopTelemetry: async () => {},
  SetDesktopTerminalTheme: async () => {},
  SetDesktopUpdateChannel: async () => {},
  SetTrayLocale: async () => {},
  MigrateDesktopPreferences: async () => {},
  ReportDesktopWebViewReady: async () => {},
  RestartApplication: async () => {},
  StorageSettings: async () => ({}),
  ReloadSettings: async () => {},
  ExternalOpeners: async () => ({ openers: [], preferred: '' }),
  ExternalOpenersForTab: async () => ({ openers: [], preferred: '' }),
  SetPreferredExternalOpener: async () => {},
  SetNetwork: async () => {},
  SetAutoApproveTools: async () => {},
  SetBypass: async () => {},
  SetPermissionMode: async () => {},
  SetSandbox: async () => {},
  SetPlanMode: async () => {},
  AddPermissionRule: async () => {},
  RemovePermissionRule: async () => {},
  SaveClipboardImage: async () => '',
  SavePastedFile: async () => '',
  SavePastedImage: async () => '',
  SaveExportImageFiles: async () => {},
  AttachDropped: async () => {},
  ResolveMarkdownImageForTab: async () => '',
  SearchFileRefs: async () => [],
  SearchFileRefsForTab: async () => [],
  ListRemoteDir: async () => [],
  ReadRemoteFile: async () => '',
  WriteRemoteFile: async () => {},
  RenameRemotePath: async () => {},
  DeleteRemotePath: async () => {},
  MkdirRemote: async () => {},
  RemoteForwards: async () => [],
  RemoteServerStatus: async () => null,
  RemoteServerLogs: async () => '',
  AddRemoteHost: async () => {},
  UpdateRemoteHost: async () => {},
  RemoveRemoteHost: async () => {},
  AddRemoteForward: async () => {},
  RemoveRemoteForward: async () => {},
  ConfirmRemoteHostKey: async () => {},
  ConfirmRemoteSecret: async () => {},
  StopRemoteServer: async () => {},
  CleanRemoteLegacyWorkbenchData: async () => {},
  ScanRemoteLegacyWorkbenchData: async () => {},
  ScanSSHConfig: async () => [],
  BackgroundRuntimes: async () => [],
  RevealBackgroundRuntime: async () => ({}),
  ContextUsage: async () => ({ used: 0, window: 1, sessionTokens: 0, compactRatio: 0.8 }),
  ContextPanel: async () => ({}),
  // ===== 插件市场/商店（DSH web profile 的 dshmarket + dsh-plugin-store） =====
  Plugins: async () => {
    try {
      // pluginStore 是 Typert Remote：payload 必须包 {args:{...}}
      const r = await rpc('pluginStore/installed', { args: {} });
      const list = (r && (Array.isArray(r) ? r : r.items || r.plugins)) || [];
      return list.map((p) => ({
        name: p.packageName || p.name || p.entryId,
        version: undefined,
        description: undefined,
        source: 'npm',
        root: undefined,
        manifestKind: 'cordis',
        enabled: !!p.enabled,
        skills: [], commands: [], hooks: [], mcpServers: [], agents: [],
        warning: p.phase ? undefined : undefined,
      }));
    } catch (e) { console.warn('[dsh] pluginStore/installed failed:', e && e.message || e); return []; }
  },
  PluginDoctor: async (name) => {
    const list = await appImpl.Plugins();
    return list.find((p) => p.name === name) || null;
  },
  InstallPlugin: async (source, options) => {
    try {
      if (typeof source === 'string' && source) {
        const r = await ipcRenderer.invoke('dsh:pluginMarketAction', 'install', { url: source });
        return JSON.stringify(r);
      }
    } catch (e) { console.warn('[dsh] InstallPlugin failed:', e && e.message || e); }
    return '{}';
  },
  RemovePlugin: async (name) => {
    try {
      if (name) {
        const r = await rpc('pluginStore/uninstall', { args: { packageName: String(name), actor: 'reasonix-ui' } });
        return r || {};
      }
    } catch (e) { console.warn('[dsh] RemovePlugin failed:', e && e.message || e); }
    return {};
  },
  UpdatePlugin: async (name) => {
    try {
      if (name) return await ipcRenderer.invoke('dsh:pluginMarketAction', 'update', { name: String(name), force: false });
    } catch (e) { console.warn('[dsh] UpdatePlugin failed:', e && e.message || e); }
    return {};
  },
  PlanPluginInstall: async (source, options) => JSON.stringify({ dryRun: true, source, options }),
  PickPluginFolder: async () => ipcRenderer.invoke('dsh:pickFolder'),
  PickSkillFolder: async () => '',
  MCPServers: async () => [],
  // 市场：GET /dsh-market/registry（1341 个插件）+ /dsh-market/installed 标记已装
  MCPMarketplace: async (query) => {
    try {
      const m = await ipcRenderer.invoke('dsh:pluginMarketplace');
      if (!m || !m.registry) return { servers: [], cached: false, warning: (m && m.error) || 'market unavailable' };
      const installedSet = new Set(Object.keys(m.installed || {}));
      const q = String(query || '').trim().toLowerCase();
      const servers = (m.registry.plugins || [])
        .filter((p) => !q
          || (p.name || '').toLowerCase().includes(q)
          || ((p.description && (p.description.en || p.description.zh || '')) || '').toLowerCase().includes(q))
        .map((p) => {
          const isInstalled = installedSet.has(p.name);
          return {
            name: p.name,
            suggestedName: p.name,
            title: p.name,
            description: (p.description && (p.description.en || p.description.zh)) || '',
            version: undefined,
            repositoryUrl: p.url || p.page,
            installable: !isInstalled,
            unavailableReason: isInstalled ? '已安装' : undefined,
            transport: 'npm',
            command: p.install,
            args: [],
            url: p.url || p.page,
          };
        });
      return { servers, cached: m.source === 'cache', warning: undefined };
    } catch (e) { console.warn('[dsh] MCPMarketplace failed:', e && e.message || e); return { servers: [], cached: false, warning: String(e && e.message || e) }; }
  },
  MCPMarketplaceResolve: async (registryName) => {
    try {
      const m = await appImpl.MCPMarketplace();
      return m.servers.find((s) => s.name === registryName || s.suggestedName === registryName) || null;
    } catch { return null; }
  },
  SetMCPServerTier: async () => {},
  SetPluginEnabled: async (name, enabled) => {
    try {
      if (name) {
        const r = await rpc('pluginStore/setEnabled', { args: { packageName: String(name), enabled: !!enabled, actor: 'reasonix-ui' } });
        return r || {};
      }
    } catch (e) { console.warn('[dsh] SetPluginEnabled failed:', e && e.message || e); }
    return {};
  },
  SetProviderWebSearch: async () => {},
  SaveProviderModelCatalogs: async () => {},
  SaveProviderWithKey: async () => {},
  ClearBotSecret: async () => {},
  SetBotSecret: async () => {},
  SetBotConnectionToolApprovalMode: async () => {},
  DiagnoseBotConnection: async () => null,
  TestBotConnection: async () => ({}),
  PollBotConnectionInstall: async () => ({}),
  StartBotConnectionInstall: async () => ({}),
  ExtensionActions: async () => [],
  ExtensionStatus: async () => [],
  ExtensionCatalog: async () => [],
  InvokeExtensionAction: async () => {},
  GetTask: async () => null,
  ListTasks: async () => [],
  ListTasksForTab: async () => [],
  ListTasksForSession: async () => [],
  ListTaskEvents: async () => [],
  ListTaskEventsForTab: async () => [],
  StopTask: async () => {},
  StopTaskForTab: async () => {},
  RequeueTask: async () => {},
  RequeueTaskForTab: async () => {},
  CancelTask: async () => {},
  CancelTaskForTab: async () => {},
  InboxHasItems: async () => false,
  RefreshInboxItem: async () => {},
  RetryInboxItem: async () => {},
  ConfirmAction: async () => {},
  ApproveTab: async () => {},
  AnswerQuestionForTab: async () => {},
  CancelTab: async () => {},
  CancelTabWithInboxItems: async () => {},
  ReplayPendingPromptsForTab: async () => {},
  SummarizeFrom: async () => {},
  SummarizeFromForTab: async () => {},
  SummarizeUpTo: async () => {},
  SummarizeUpToForTab: async () => {},
  SetAgentParams: async () => {},
  SetComposerProfileForTab: async () => {},
  SetStatusBarItems: async () => {},
  SetStatusBarStyle: async () => {},
  SetProjectColor: async () => {},
  SetProjectPinned: async (root, pinned) => {
    try {
      let list = readPinnedList(PINNED_PROJECTS_KEY);
      if (pinned) { if (!list.includes(root)) list.push(root); }
      else { list = list.filter((r) => r !== root); }
      savePinnedList(PINNED_PROJECTS_KEY, list);
      return { ok: true };
    } catch (e) { return { ok: false, error: String(e && e.message || e) }; }
  },
  SetTopicPinned: async (topicId, pinned) => {
    try {
      let list = readPinnedList(PINNED_TOPICS_KEY);
      if (pinned) { if (!list.includes(topicId)) list.push(topicId); }
      else { list = list.filter((t) => t !== topicId); }
      savePinnedList(PINNED_TOPICS_KEY, list);
      return { ok: true };
    } catch (e) { return { ok: false, error: String(e && e.message || e) }; }
  },
  ReorderProjects: async () => {},
  CloseTerminalForTab: async () => {},
  RenameTerminalForTab: async () => {},
  SetDefaultAutoRecoveryCheckpoint: async () => {},
  SetRecoveryCheckpointEnabled: async () => {},
  SetRecoveryCheckpointEnabledTab: async () => {},
  RecoveryCheckpointEnabled: async () => true,
  RecoveryCheckpointEnabledTab: async () => true,
  RecoveryCheckpointEnabledTab2: async () => true,
  ChooseRecoveryBranch: async () => {},
  CleanRecoveryLineage: async () => {},
  DeleteRecoveryCopy: async () => {},
  PurgeRecoveryCopy: async () => {},
  ResolveRecovery: async () => {},
  ResolveRecoveryTab: async () => {},
  ResolvePlanDecision: async () => {},
  ResolvePlanDecisionTab: async () => {},
  PauseGoalForTab: async () => {},
  ResumeGoalForTab: async () => {},
  CancelJob: async () => false,
  CancelJobsForTab: async () => ({ cancelled: [], notRunning: [] }),
  SaveDoc: async (path, body) => {
    let cwd = '';
    try {
      const tabs = await sessions();
      const t = tabs.find((x) => x.active) || tabs[0];
      cwd = (t && (t.workspaceRoot || t.cwd)) || '';
    } catch {}
    if (!cwd) return;
    try { await ipcRenderer.invoke('memory:write', cwd, body); } catch {}
  },
  SaveDocForTab: async (tabID, path, body) => {
    let cwd = '';
    try {
      const tabs = await sessions();
      const t = tabs.find((x) => x.id === tabID) || tabs[0];
      cwd = (t && (t.workspaceRoot || t.cwd)) || '';
    } catch {}
    if (!cwd) return;
    try { await ipcRenderer.invoke('memory:write', cwd, body); } catch {}
  },
  PreviewSession: async () => ({}),
  CloseTabWithPolicy: async () => {},
  OpenWorkspaceInExternalOpener: async () => {},
  OpenWorkspaceInExternalOpenerForTab: async () => {},

  CreateIsolatedWorktree: async (workspaceRoot) => {
    const res = await createSession(workspaceRoot, DEFAULT_PRESET);
    const tab = sessionToTabMeta({ sessionId: res.sessionId, cwd: workspaceRoot, running: false, projections: {} }, 0);
    tab.isolatedWorktree = true;
    tab.gitBranch = 'reasonix/isolated-' + Date.now().toString(36);
    return tab;
  },
  CreateDeliveryWorktree: async (workspaceRoot) => {
    const res = await createSession(workspaceRoot, DEFAULT_PRESET);
    return sessionToTabMeta({ sessionId: res.sessionId, cwd: workspaceRoot, running: false, projections: {} }, 0);
  },
  ReloadRuntime: async () => {},
  SubmitDeliveryRecoveryToTabWithID: async (tabID, _display, input) => { await prompt(tabID, input); },
  SubmitEditedDisplayToTabWithID: async (tabID, _display, input) => { await prompt(tabID, input); },
  SubmitInitialGoalToTabWithID: async (tabID, goal) => { await prompt(tabID, goal); },
  SubmitInvocationsToTabWithID: async (tabID, _display, input) => { await prompt(tabID, input); },

  // ===== 设置/杂项（回退 mock 用） =====
  DesktopStartupSettings: async () => ({
    bot: {},
    desktopLanguage: 'zh',
    desktopLayoutStyle: 'workbench',
    desktopTheme: 'dark',
    desktopThemeStyle: 'dark',
    desktopTerminalTheme: 'dark',
    displayMode: 'full',
    reasoningDisplayMode: 'auto',
    statusBarStyle: 'text',
    statusBarItems: ['workspace', 'model', 'context', 'usage', 'cache', 'cost'],
    checkUpdates: false,
    updateChannel: 'stable',
    conversationWidth: 'standard',
    configWarnings: [],
    configWarningsRevision: 0,
    configPath: '',
  }),
  SetDesktopAppearance: async (mode, style) => {
    // 外观（明/暗/自动 + 风格）持久化：前端 initTheme 启动时读 localStorage('reasonix-theme')
    try {
      const norm = (mode === 'auto' || mode === 'light' || mode === 'dark') ? mode : 'dark';
      localStorage.setItem('reasonix-theme', norm);
      if (style !== undefined && style !== null && style !== '') localStorage.setItem('reasonix-theme-style', String(style));
      return { ok: true };
    } catch (e) { return { ok: false, error: String(e && e.message || e) }; }
  },
  SetCloseBehavior: async () => {},
  SetDisplayMode: async () => {},
  NeedsOnboarding: async () => false,
  Commands: async () => [],
  Capabilities: async () => ({
    plugins: await appImpl.Plugins(),
    skills: [],
    diagnostics: [],
    updatedAt: Date.now(),
  }),
  SetDesktop: async () => {},
  SetStatusBar: async () => {},
  SetReasoningDisplayMode: async () => {},
  SetExpandThinking: async () => {},
  SetAutoPlan: async () => {},
  SetDefaultToolApprovalMode: async () => {},
  SetCompactRatio: async () => {},
  SetReasoningLanguage: async () => {},
  ReportCrash: async () => {},
  RecordUIPerf: async () => {},
  OpenUserConfigPath: async () => {},
  ReloadUserConfig: async () => ({ configWarnings: [], configWarningsRevision: 0, configPath: '' }),
  CheckUpdate: async () => ({ available: false }),
  ApplyUpdateRequest: async () => {},
  OpenDownloadPage: async () => {},
  GetActiveThemePack: async () => null,
  ResetThemePack: async () => {},
  ImportThemePack: async () => ({}),
  CopyThemePack: async () => '',
  ThemePacks: async () => [],
  SetThemePack: async (pack) => {
    // 主题包持久化（用户自定义/官方主题包；前端主题画廊管理）
    try {
      if (pack && typeof pack === 'object' && pack.id) localStorage.setItem('dsh:theme-pack', JSON.stringify(pack));
      return { ok: true };
    } catch (e) { return { ok: false, error: String(e && e.message || e) }; }
  },
  RefreshSkills: async () => {},
  SkillsSettings: async () => ({}),
  AddSkillPath: async () => {},
  RemoveSkillPath: async () => {},
  SetSkillPathEnabled: async () => {},
  SetSkillEnabled: async () => {},
  SetSkillImplicitInvocation: async () => {},
  AcceptSkillSuggestion: async () => {},
  AvailableSubagentTools: async () => [],
  CreateSubagentProfile: async () => ({}),
  UpdateSubagentProfile: async () => {},
  DeleteSubagentProfile: async () => {},
  SetSubagentProfileModel: async () => {},
  SetSubagentProfileEffort: async () => {},
  TrySubagentProfile: async () => {},
  CancelTrySubagentProfile: async () => {},
  AddMCPServer: async () => ({}),
  InstallMCPServer: async () => ({}),
  UpdateMCPServer: async () => {},
  RemoveMCPServer: async () => {},
  AuthorizeAndConnectMCPServer: async () => {},
  AuthenticateMCPServer: async () => {},
  ReconnectMCPServer: async () => {},
  ClearMCPServerAuthentication: async () => {},
  SetMCPServer: async () => {},
  SetMCPServerEnabled: async () => {},
  RemoteHosts: async () => [],
  RemoteConnectionStatuses: async () => [],
  RemoteLastWorkspace: async () => null,
  OpenRemoteWorkspace: async () => {},
  ConnectRemoteHost: async () => {},
  DisconnectRemoteHost: async () => {},
  HeartbeatListTasks: async () => [],
  HeartbeatReloadTasks: async () => {},
  HeartbeatSaveTasks: async () => {},
  HeartbeatReloadConfig: async () => {},
  HeartbeatSaveConfig: async () => {},
  HeartbeatTriggerNow: async () => {},
  HeartbeatGenerateID: async () => 'hb-' + Date.now(),
  InboxSnapshot: async () => [],
  EnqueueInboxFollowup: async () => {},
  SetInboxPaused: async () => {},
  SessionCatalog: async () => ({}),
  HistoryCatalog: async () => ({}),
  TaskCatalog: async () => ({}),
  BlankProject: async () => ({}),
  JobsForTab: async () => [],
  CancelJobForTab: async () => {},
  ActiveWorkForTab: async () => ({ running: false, pendingPrompt: false, cancellable: false, jobs: [] }),
  CheckpointsForTab: async () => [],
  CheckpointTurnForTab: async () => 0,
  CheckpointSummaryForTab: async () => null,
  OpenTaskSessionForTab: async () => ({}),
  ContextUsageForTab: async (tabID) => {
    try {
      const list = await rpc('session.list', {});
      const s = (list && list.items || []).find((x) => x.sessionId === tabID);
      const v = (s && s.projections && s.projections.values) || {};
      const cp = v.contextPressure || {};
      const tu = v.tokenUsage || {};
      // 费用：按官方/中转站定价计算（动态取当前 provider/model）
      let provider = 'deepseek-official';
      let model = 'deepseek-v4-flash';
      try {
        const models = await rpc('session.models', { sessionId: tabID });
        if (models && models.current) {
          provider = models.current.provider || provider;
          model = models.current.model || model;
        }
      } catch {}
      const cost = calcCost(tu, provider, model);
      return {
        used: cp.pressureTokens || 0,
        window: cp.contextWindow || 1,
        sessionTokens: tu.cacheReadTokens + tu.uncachedInputTokens + tu.outputTokens || 0,
        compactRatio: 0.8,
        cacheHitTokens: tu.cacheReadTokens || 0,
        cacheMissTokens: tu.uncachedInputTokens || 0,
        sessionCost: cost,
        sessionCurrency: 'CNY',
        sessionCostComplete: true,
        estimated: false,
      };
    } catch (e) {
      return { used: 0, window: 1, sessionTokens: 0, compactRatio: 0.8, cacheHitTokens: 0, cacheMissTokens: 0, sessionCost: 0, sessionCurrency: 'CNY', estimated: true };
    }
  },
  AttachmentDataURL: async () => '',
  PickExportFile: async () => null,
  SaveExportFile: async () => {},
  GetRecoveryLineage: async () => ({}),
  FetchProviderModels: async () => ({}),
  FetchAllProviderModels: async () => ({}),
  SaveProvider: async () => {},
  DeleteProvider: async () => {},
  SaveProviderKey: async () => {},
  SetProviderKey: async () => {},
  ClearProviderKey: async () => {},
  RemoveProviderAccess: async () => {},
  RemoveProviderAccesses: async () => {},
  AddOfficialProviderAccess: async () => {},
  AddProviderPresetAccess: async () => {},
  ResetProviderPresetAccess: async () => {},
  UpgradeDeepSeekProviderAccess: async () => {},
  ConnectKey: async () => '',
  ProviderModels: async () => ({}),
  ProviderPresets: async () => [],
  GetUserConfigPath: async () => '',
  SubagentsForTab: async () => [],
  SubagentProgressForTab: async () => [],
  SetSubagentProfile: async () => {},
  GoalForTab: async () => null,
  SetGoalForTab: async () => {},
  SetGoal: async () => {},
  PlanForTab: async () => ({}),
  SetPlan: async () => {},
  TodoSnapshotForTab: async () => [],
  DismissTodoBatchForTab: async () => {},
  UsageStats: async () => ({}),
  BalanceInfo: async () => null,
  TerminalWorkspaceForTab: async () => ({ sessions: [], cwd: '' }),
  TerminalOutputForTab: async () => '',
  CreateTerminalForTab: async () => ({ id: 'term-' + Date.now(), title: 'terminal', shell: 'powershell', cwd: '', createdAt: Date.now(), running: true }),
  WriteTerminalForTab: async () => {},
  TerminateTerminalForTab: async () => {},
  ResizeTerminalForTab: async () => {},
  SetTerminalThemeForTab: async () => {},
  ListTerminalSessionsForTab: async () => [],
  ShellForTerminal: async () => 'powershell',
  SetShellForTerminal: async () => {},
  ClearTerminalForTab: async () => {},
  ScrollTerminalForTab: async () => {},
  ExtensionStatus: async () => [],
  ExtensionCatalog: async () => [],
  InstallExtension: async () => {},
  UninstallExtension: async () => {},
  SubmitExtensionForm: async () => {},
  BotRuntimeStatus: async () => ({
    available: false, running: false, enabled: false,
    supported: false, configured: false, state: 'unavailable',
  }),
  BotSettings: async () => ({}),
  SetBotSettings: async () => {},
  BotConnectionDiagnostic: async () => null,
  ConnectBot: async () => {},
  DisconnectBot: async () => {},
  BotInstallStart: async () => ({}),
  BotInstallPoll: async () => ({}),
  CapabilityDiagnostics: async () => ({}),
  RuntimeDoctor: async () => ({}),
  RevealPath: async () => {},
  OpenLocalPath: async () => {},
  SetZoomFactor: async () => {},
  ZoomFactor: async () => 1,
  SetThemePackForTab: async (tabID, pack) => {
    try {
      if (pack && typeof pack === 'object' && pack.id) localStorage.setItem('dsh:theme-pack:' + tabID, JSON.stringify(pack));
      return { ok: true };
    } catch (e) { return { ok: false, error: String(e && e.message || e) }; }
  },
  ThemePackForTab: async () => null,
  SetTerminalShellForTab: async () => {},
  SubmitInvocationsToTab: async () => {},
  SubmitInitialGoalToTab: async () => {},
  SubmitEditedDisplayToTab: async () => {},
  SubmitDeliveryRecoveryToTab: async () => {},
  ReplayPendingPrompts: async () => {},
  RunShell: async () => {},
  RunShellForTab: async () => {},
  ReadInboxItem: async () => ({}),
  UpdateInboxItem: async () => {},
  DeleteInboxItem: async () => {},
  MoveInboxItem: async () => {},
  SteerInboxItem: async () => {},
  EnqueueInboxSteer: async () => {},
  EnqueueInboxFollowupWithInvocations: async () => {},
  QueryInbox: async () => [],
};

// ---------- 组装 window.go.main.App（Proxy：DSH 方法优先，缺失落 mock） ----------
// mock 不可直接 import（bridge.ts 内部），所以这里用"实现优先 + 未实现方法返回默认"策略。
// Reasonix bridge 的 app Proxy 会调用 window.go.main.App.xxx；若某方法未实现，
// 我们返回一个兜底 Promise，避免前端崩溃；同时对每个未实现方法提示一次（方便排查"点了没反应"）。
const warnedImpl = new Set();
const AppProxy = new Proxy({}, {
  get(_t, prop) {
    const name = String(prop);
    // hasOwnProperty：避免 name in appImpl 命中原型链（constructor/toString 等）
    if (Object.prototype.hasOwnProperty.call(appImpl, name)) return appImpl[name];
    // 未实现的方法：返回一个安全的兜底（尽力而为），并提示一次
    if (!warnedImpl.has(name)) {
      warnedImpl.add(name);
      console.warn('[dsh] 未实现的 App 方法（返回空兜底）:', name);
    }
    return (..._args) => Promise.resolve(undefined);
  },
});

// ---------- runtime ----------
const runtime = {
  EventsOn: eventsOn,
  EventsEmit: eventsEmit,
  EventsOff: (channel, cb) => { eventChannels.get(channel)?.delete(cb); },
  BrowserOpenURL: (url) => { try { require('electron').shell.openExternal(url); } catch {} },
};

// 注入（contextIsolation: false，直接挂 window）
window.go = window.go || {};
window.go.main = window.go.main || {};
window.go.main.App = AppProxy;
window.runtime = runtime;

// ---------- 拖拽区域映射（Wails --wails-draggable → Electron -webkit-app-region） ----------
// Reasonix 前端用 CSS 变量 --wails-draggable 声明拖拽区域；Electron 需要 -webkit-app-region。
// 注入样式把 .topbar 等 drag 区域映射过去；Wails 用 .app--windows 关掉 sidebar 拖拽，这里同样处理。
const dragStyle = document.createElement('style');
dragStyle.textContent = `
  .topbar, .topbar--drag, .app-chrome, .app-chrome--native-tabs, .app-chrome--tabs {
    -webkit-app-region: drag !important;
  }
  .topbar button, .topbar input, .topbar select, .topbar a,
  .app-chrome button, .app-chrome input, .app-chrome select,
  .tabbar, .tabbar *, .sidebar button, .sidebar input, .sidebar select,
  .win-controls, .win-controls * {
    -webkit-app-region: no-drag !important;
  }
  /* DSH-Reasonix 品牌标识 */
  .dsr-badge {
    display: inline-grid !important;
    place-items: center;
    width: 100%;
    height: 100%;
    border-radius: 8px;
    background: linear-gradient(135deg, #d97757, #e58a3a);
    color: #fff;
    font-weight: 700;
    font-size: 13px;
    letter-spacing: .02em;
    -webkit-app-region: no-drag;
  }
  .welcome__brand-logo { display: none !important; }
  /* wordmark 文字版 logo（侧边栏/欢迎页） */
  .dsr-wordmark {
    display: inline-flex !important;
    align-items: center;
    height: 100%;
    color: var(--fg, #f4f5f7);
    font-weight: 650;
    font-size: 14px;
    letter-spacing: .01em;
    white-space: nowrap;
    -webkit-app-region: no-drag;
  }
  .dsr-d-img {
    display: inline-block !important;
    width: 22px;
    height: 22px;
    object-fit: contain;
    filter: none; /* 保留原样：白 D + 深色鲸鱼，不额外提亮以免鲸鱼变淡 */
    -webkit-app-region: no-drag;
  }
  .dsr-wordmark { gap: 6px; }
`;
document.addEventListener('DOMContentLoaded', () => document.head.appendChild(dragStyle));
if (document.readyState !== 'loading') document.head.appendChild(dragStyle);

// ---------- 品牌覆盖：DSH-Reasonix ----------
const BRAND_NAME = 'DSH-ReasonixUI';
const D_LOGO = 'data:image/png;base64,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';
const applyBranding = () => {
  if (document.title !== BRAND_NAME) document.title = BRAND_NAME;
  // 最重的属性选择器扫描限定在 body 直系容器（.app / #app），找不到就维持 body
  const scanRoot = document.querySelector('.app') || document.querySelector('#app') || document.body;
  scanRoot.querySelectorAll('.startup-splash__name, .welcome__brand, [class*="__name"], [class*="brand"]').forEach((el) => {
    if (el.textContent.trim() === 'Reasonix') el.textContent = BRAND_NAME;
  });
  // logo 图标：把原 SVG 替换为 DSH-Reasonix 文字标识
  document.querySelectorAll('.startup-splash__mark img, .welcome__brand-logo, .sidebar__brand-logo, .onboarding__logo').forEach((img) => {
    if (img.dataset.branded) return;
    img.dataset.branded = '1';
    const parent = img.parentElement;
    img.style.display = 'none';
    const isWordmark = img.classList.contains('sidebar__brand-logo') || img.classList.contains('welcome__brand-logo') || img.classList.contains('onboarding__logo');
    if (isWordmark) {
      const wrap = document.createElement('span');
      wrap.className = 'dsr-wordmark';
      const dImg = document.createElement('img');
      dImg.className = 'dsr-d-img';
      dImg.src = D_LOGO;
      dImg.alt = 'D';
      const txt = document.createElement('span');
      txt.textContent = 'SH-ReasonixUI';
      wrap.appendChild(dImg);
      wrap.appendChild(txt);
      parent.appendChild(wrap);
    } else {
      const badge = document.createElement('span');
      badge.className = 'dsr-badge';
      badge.textContent = 'DS';
      parent.appendChild(badge);
    }
  });
};

// 启动品牌覆盖（MutationObserver 回调做节流：高频 DOM 变更不触发全树扫描）
let brandingPending = false;
const scheduleBranding = () => {
  if (brandingPending) return;
  brandingPending = true;
  setTimeout(() => { brandingPending = false; applyBranding(); }, 250);
};
if (document.readyState !== 'loading') applyBranding();
document.addEventListener('DOMContentLoaded', applyBranding);
if (typeof MutationObserver !== 'undefined') {
  // 只注册一次 observer，且只观察 document.body 的 subtree（childList + subtree）
  const brandObserver = new MutationObserver(() => scheduleBranding());
  const attachBrandObserver = () => {
    if (document.body) brandObserver.observe(document.body, { childList: true, subtree: true });
  };
  document.addEventListener('DOMContentLoaded', attachBrandObserver);
  attachBrandObserver();
}

// ---------- window.dsh：DSH 通用透传入口 ----------
// 设计原则：不设白名单。rpc 透传任意 DSH 方法（含插件动态注册的），
// onEvent 收到 events.mux 的全部原始帧（不筛选）。Reasonix 的 window.go.main.App
// 只是 DSH 能力的"一个视图"，DSH 其余能力都能从这里直达，不被前端阉割。
const dshEventListeners = new Set();
window.__dshRawEventCount = 0;
window.__dshRawEventMethods = [];
ipcRenderer.on('dsh:raw-event', (_e, frame) => {
  window.__dshRawEventCount = (window.__dshRawEventCount || 0) + 1;
  if (frame && frame.method && window.__dshRawEventMethods.length < 40) window.__dshRawEventMethods.push(frame.method);
  for (const cb of dshEventListeners) { try { cb(frame); } catch {} }
});

window.dsh = {
  // 通用 RPC：调用任意 DSH 方法。method 形如 "session.list"、"goal.create"，
  // 或插件动态注册的任意 "namespace.method"。payload 为该方法需要的参数对象。
  rpc: (method, payload) => ipcRenderer.invoke('dsh:rpc', method, payload ?? {}),
  // 语义别名
  call: (method, payload) => ipcRenderer.invoke('dsh:rpc', method, payload ?? {}),
  // 能力目录：DSH 已知方法清单（基线提示，非白名单）
  catalog: () => ipcRenderer.invoke('dsh:catalog'),
  // 订阅 events.mux 全量原始帧（不筛选），返回退订函数
  onEvent: (cb) => {
    if (typeof cb !== 'function') return () => {};
    dshEventListeners.add(cb);
    return () => dshEventListeners.delete(cb);
  },
  // 便捷方法：直接请求已知的常用方法（仍走通用 rpc，只是少写参数）
  sessions: () => ipcRenderer.invoke('dsh:rpc', 'session.list', {}),
  history: (sid) => ipcRenderer.invoke('dsh:rpc', 'session.history', { sessionId: sid, limit: 300 }),
  // session.prompt 是长请求（模型生成可能要几分钟）：走主进程 dsh:prompt 专用通道
  // （默认 10 分钟超时），不走默认 60s 的通用 rpc
  prompt: (sid, text) => ipcRenderer.invoke('dsh:prompt', sid, text),
  createSession: (cwd, agentPreset) => {
    const payload = {};
    if (cwd) payload.cwd = cwd;
    if (agentPreset) payload.agentPreset = agentPreset;
    return ipcRenderer.invoke('dsh:rpc', 'session.create', payload);
  },
  cancel: (sid) => ipcRenderer.invoke('dsh:rpc', 'session.cancel', { sessionId: sid }),
  // DSH 更新/配置控制：读配置、改配置、手动更新
  getConfig: () => ipcRenderer.invoke('dsh:config'),
  setConfig: (patch) => ipcRenderer.invoke('dsh:config:set', patch),
  updateDsh: () => ipcRenderer.invoke('dsh:update'),
  // 版本分开：frontend = 本应用版本（package.json），backend = 后端 DSH（@deepseek-ai/dsh）版本
  versions: () => ipcRenderer.invoke('dsh:versions'),
  // 会话清理：删除特定会话（运行中会拒绝）/ 清空全部历史会话
  deleteSession: (sessionId) => ipcRenderer.invoke('dsh:deleteSession', sessionId),
  clearHistory: () => ipcRenderer.invoke('dsh:purgeHistory'),
};

// 保留 __dsh 调试句柄（向后兼容），并让 __dsh.rpc 也走通用透传
window.__dsh = {
  rpc: (method, payload) => ipcRenderer.invoke('dsh:rpc', method, payload ?? {}),
  sessions, history, prompt, createSession, cancelSession,
  catalog: () => ipcRenderer.invoke('dsh:catalog'),
  getConfig: () => ipcRenderer.invoke('dsh:config'),
  setConfig: (patch) => ipcRenderer.invoke('dsh:config:set', patch),
  updateDsh: () => ipcRenderer.invoke('dsh:update'),
  versions: () => ipcRenderer.invoke('dsh:versions'),
  deleteSession: (sessionId) => ipcRenderer.invoke('dsh:deleteSession', sessionId),
  clearHistory: () => ipcRenderer.invoke('dsh:purgeHistory'),
  onEvent: window.dsh.onEvent,
  calcCost, DEEPSEEK_OFFICIAL_PRICES, RELAY_PRICES, RELAY_PROVIDERS,
  loadPrices, savePrices, fetchOfficialPrices,
  setRelayPrice: (id, table) => { if (table) RELAY_PRICES[id] = table; },
  setOfficialPrice: (model, table) => { if (table) DEEPSEEK_OFFICIAL_PRICES[model] = table; },
};
// 会话删除入口：前端项目树每行 hover 出现归档按钮 + 右键"移入回收站"（桥已映射到
// dsh:deleteSession），不再注入右下角浮动按钮。API 仍开放：window.dsh.deleteSession /
// window.dsh.clearHistory（控制台/插件可用）。
