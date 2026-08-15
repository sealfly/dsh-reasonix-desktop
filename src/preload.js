'use strict';
// Reasonix 前端 → DSH 桥接层
// 注入 window.go.main.App（DSH 实现 + mock 回退）和 window.runtime（事件通道）
const { ipcRenderer, contextBridge } = require('electron');

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
  } catch {}
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
      cost: calcCost(tu, 'deepseek-official', 'deepseek-v4-flash'),
      currencyCode: 'CNY',
    };
    eventsEmit('agent:event', { kind: 'usage', usage, tabId: sessionId });
  } catch {}
}

// ---------- DSH 历史 → WireEvent 重放 ----------
// DSH session.history 返回 events 数组；转成 Reasonix WireEvent 序列喂给前端，
// 前端 transcript store 据此构建对话历史。
function replayHistory(sessionId, events) {
  if (!events || !events.length) return;
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
    if (event.type === 'turn/start') {
      currentTurn = d.turn;
      wire.push({ kind: 'turn_started', tabId: sessionId });
    } else if (event.type === 'turn/end') {
      wire.push({ kind: 'turn_done', tabId: sessionId });
      currentTurn = null;
    } else if (event.type === 'user/message') {
      pushMsg('user', d.content);
    } else if (event.type === 'assistant/message') {
      pushMsg('assistant', d.message && d.message.content);
    }
  }
  flushText();
  // 延迟重放（限 300 条，避免海量 setTimeout 拖垮前端）
  const limited = wire.slice(0, 300);
  limited.forEach((w, i) => setTimeout(() => eventsEmit('agent:event', w), i * 5));
  // 重放后补发 usage 事件（驱动用量分析卡）
  setTimeout(() => emitUsageEvent(sessionId), limited.length * 5 + 50);
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
      if (text) currentEntry.message.content += text;
      chunkBuf = {};
    }
  };
  for (const { event } of events || []) {
    const d = event.data || {};
    if (event.type === 'user/message' || event.type === 'assistant/message') {
      flushChunks();
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
    } else if (event.type === 'assistant/chunk') {
      const c = d.chunk;
      if (!c) continue;
      if (c.type === 'text-delta') {
        chunkBuf[c.index || 0] = (chunkBuf[c.index || 0] || '') + c.text;
      }
    }
  }
  flushChunks();
  return entries;
}

// ---------- 安全模式 ↔ DSH 权限映射 ----------
// Reasonix ToolApprovalMode: ask | auto | yolo
// DSH permissions: read-only | workspace-write | danger-full-access
const MODE_TO_PERMISSION = { ask: 'workspace-write', auto: 'danger-full-access', yolo: 'danger-full-access' };
const PERMISSION_TO_MODE = { 'read-only': 'ask', 'workspace-write': 'auto', 'danger-full-access': 'yolo' };
// 新建会话时按当前模式选 agentPreset（DSH 权限由 preset 决定）
const MODE_TO_PRESET = { ask: 'standard', auto: 'code', yolo: 'code' };
let currentMode = 'auto'; // 默认 workspace-write

// ---------- DSH 工具函数 ----------
const rpc = (method, payload) => ipcRenderer.invoke('dsh:rpc', method, payload);
const sessions = () => ipcRenderer.invoke('dsh:sessions');
const history = (sid) => ipcRenderer.invoke('dsh:history', sid);
const prompt = (sid, text) => ipcRenderer.invoke('dsh:prompt', sid, text);
const createSession = (cwd, agentPreset) => ipcRenderer.invoke('dsh:create', cwd, agentPreset);
const cancelSession = (sid) => ipcRenderer.invoke('dsh:cancel', sid);

// ---------- DSH → TabMeta 转换（与主进程一致） ----------
function sessionToTabMeta(s, idx) {
  const v = s.projections?.values || {};
  const title = v.title || '未命名会话';
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
    toolApprovalMode: 'ask',
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
    const active = tabs.find((t) => t.active) || tabs[0];
    if (active?.permissions?.currentValue) currentMode = PERMISSION_TO_MODE[active.permissions.currentValue] || currentMode;
    return tabs;
  },

  HistorySliceForTab: async (tabID, req) => {
    try {
      const h = await history(tabID);
      const entries = historyEventsToSlice(h.events, tabID);
      return {
        entries,
        nextCursor: '',
        hasOlder: false,
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
        key: s.sessionId, kind: 'session', label: title,
        topicId: s.sessionId, sessionPath: s.sessionId + '.jsonl',
        turns: v.sessionStats?.turns, turnsState: 'valid', health: 'ok',
        lastActivityAt: s.updatedAt, open: true, running: !!s.running,
        children: [],
      });
    }
    const projects = [];
    for (const [root, sessionsArr] of byRoot) {
      const name = root.split(/[\\/]/).filter(Boolean).pop() || 'workspace';
      projects.push({
        key: 'p:' + root, kind: 'project', label: name, root,
        projectColor: undefined, pinned: false, open: true,
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
      autoApproveTools: false,
      bypass: false,
      collaborationMode: 'normal',
      toolApprovalMode: 'ask',
      goal: '',
      goalStatus: 'stopped',
    };
  },
  MetaForTab: async () => {
    const tabs = await sessions();
    const active = tabs.find((t) => t.active) || tabs[0];
    const workspacePath = active?.workspacePath || active?.cwd || 'C:\\';
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
      autoApproveTools: false,
      bypass: false,
      collaborationMode: 'normal',
      toolApprovalMode: 'ask',
      goal: '',
      goalStatus: 'stopped',
    };
  },
  SetActiveTab: async (tabID) => {
    try { const h = await history(tabID); replayHistory(tabID, h.events); } catch {}
  },
  OpenProjectTab: async (workspaceRoot, topicId) => {
    // 复用已有会话（topicId = sessionId），否则新建
    let sid = topicId;
    if (!sid) {
      const tabs = await sessions();
      const existing = tabs.find((s) => s.cwd === workspaceRoot);
      sid = existing ? existing.id : (await createSession(workspaceRoot, MODE_TO_PRESET[currentMode])).sessionId;
    }
    try { const h = await history(sid); replayHistory(sid, h.events); } catch {}
    return sessionToTabMeta({ sessionId: sid, cwd: workspaceRoot, running: false, projections: {} }, 0);
  },
  OpenGlobalTab: async () => {
    const res = await createSession(undefined, MODE_TO_PRESET[currentMode]);
    return sessionToTabMeta({ sessionId: res.sessionId, cwd: undefined, running: false, projections: {} }, 0);
  },
  EnsureBlankTab: async () => ({}),
  EnsureBlankSurface: async () => ({}),
  CloseTab: async (tabID) => {},
  ReorderTabs: async () => {},
  CreateTopic: async () => ({}),
  RenameTopic: async () => {},
  DeleteTopic: async () => {},
  TrashTopic: async () => {},
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
    const res = await createSession(path, MODE_TO_PRESET[currentMode]);
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
    if (!name || name === '.' || name === '..' || /[\\/]/.test(name)) {
      throw new Error('project name must be a single folder name');
    }
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
  ActivateTopic: async (scope, workspaceRoot, topicID) => {
    if (topicID) { try { const h = await history(topicID); replayHistory(topicID, h.events); } catch {} }
    return sessionToTabMeta({ sessionId: topicID, cwd: workspaceRoot, running: false, projections: {} }, 0);
  },
  StartTopicActivation: async () => ({}),
  CurrentTaskSessionID: async () => '',
  OpenTaskSession: async () => ({}),
  OpenTaskSessionForTab: async () => ({}),
  Jobs: async () => [],
  Balance: async () => null,
  BalanceForTab: async () => null,
  Memory: async () => ({ entries: [] }),
  MemoryForTab: async () => ({ entries: [] }),
  MemoryRevisions: async () => [],
  MemoryRevisionsForTab: async () => [],
  MemorySuggestions: async () => [],
  MemorySuggestionsForTab: async () => [],
  Remember: async () => {},
  RememberForTab: async () => {},
  Forget: async () => {},
  ForgetForTab: async () => {},
  AcceptMemorySuggestion: async () => {},
  AcceptMemorySuggestionForTab: async () => {},
  RestoreArchivedMemory: async () => {},
  RestoreArchivedMemoryForTab: async () => {},
  RestoreMemoryRevision: async () => {},
  RestoreMemoryRevisionForTab: async () => {},
  AcceptSkillSuggestionForTab: async () => {},
  Version: async () => '0.1.0',
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
    const items = await ipcRenderer.invoke('fs:list', rel);
    return (items || []).map((e) => ({ name: e.name, path: e.path, isDir: e.isDir, displayName: e.name, displayPath: e.path }));
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
    const body = await ipcRenderer.invoke('fs:read', path);
    return {
      path,
      body: body || '',
      size: (body || '').length,
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
    const cwd = (await sessions()).find((t) => t.id === tabID)?.cwd || 'C:\\Users\\ROG Zephyrus G16\\Desktop\\DSH';
    return await ipcRenderer.invoke('git:changes', cwd);
  },
  WorkspaceChangeDetail: async (tabID, path) => {
    const cwd = (await sessions()).find((t) => t.id === tabID)?.cwd || 'C:\\Users\\ROG Zephyrus G16\\Desktop\\DSH';
    return await ipcRenderer.invoke('git:detail', cwd, path);
  },
  WorkspaceConflictForTab: async () => null,
  WorkspaceRevisionForTab: async () => ({ revisions: { content: 0, tree: 0, workingTree: 0, gitMeta: 0, session: 0 }, watchState: 'active' }),
  WorkspaceGitHistory: async (tabID, path) => {
    const cwd = (await sessions()).find((t) => t.id === tabID)?.cwd || 'C:\\Users\\ROG Zephyrus G16\\Desktop\\DSH';
    return await ipcRenderer.invoke('git:history', cwd, path);
  },
  WorkspaceGitCommitDetail: async () => ({ diff: '' }),
  PreviewWorkspaceFileRevertForTab: async (tabID, path) => ({ ok: true, canFiles: true, path, planId: 'dsh-' + Date.now() }),
  CommitWorkspaceFileRevertForTab: async () => ({ ok: true, undoAvailable: false, transactionId: 'dsh-tx' }),
  GitBranches: async () => [],
  GitCheckout: async () => {},

  // ===== 对话 =====
  Submit: async (input) => { const tabs = await sessions(); if (tabs.length) await prompt(tabs[0].id, input); },
  SubmitToTab: async (tabID, input) => { await prompt(tabID, input); },
  SubmitToTabWithID: async (tabID, input) => { await prompt(tabID, input); },
  SubmitDisplay: async (_display, input) => { const tabs = await sessions(); if (tabs.length) await prompt(tabs[0].id, input); },
  SubmitDisplayToTab: async (tabID, _display, input) => { await prompt(tabID, input); },
  SubmitDisplayToTabWithID: async (tabID, _display, input) => { await prompt(tabID, input); },
  Steer: async (input) => { const tabs = await sessions(); if (tabs.length) await prompt(tabs[0].id, input); },
  SteerForTab: async (tabID, input) => { await prompt(tabID, input); },
  Cancel: async (tabID) => { await cancelSession(tabID); },
  CancelForTab: async (tabID) => { await cancelSession(tabID); },
  Approve: async () => {},
  Reject: async () => {},
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
    if (mode in MODE_TO_PERMISSION) currentMode = mode;
  },
  SetToolApprovalModeForTab: async (tabID, mode) => {
    if (mode in MODE_TO_PERMISSION) currentMode = mode;
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
      const list = await rpc('session.list', {});
      const sid = list && list.items && list.items[0] && list.items[0].sessionId;
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
      const list = await rpc('session.list', {});
      const sid = list && list.items && list.items[0] && list.items[0].sessionId;
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
  SetDesktopLanguage: async () => {},
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
  Plugins: async () => [],
  PluginDoctor: async () => [],
  InstallPlugin: async () => {},
  RemovePlugin: async () => {},
  UpdatePlugin: async () => {},
  PlanPluginInstall: async () => ({}),
  PickPluginFolder: async () => '',
  PickSkillFolder: async () => '',
  MCPServers: async () => [],
  MCPMarketplace: async () => [],
  MCPMarketplaceResolve: async () => ({}),
  SetMCPServerTier: async () => {},
  SetPluginEnabled: async () => {},
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
  SetProjectPinned: async () => {},
  SetTopicPinned: async () => {},
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
  SaveDoc: async () => {},
  SaveDocForTab: async () => {},
  PreviewSession: async () => ({}),
  CloseTabWithPolicy: async () => {},
  OpenWorkspaceInExternalOpener: async () => {},
  OpenWorkspaceInExternalOpenerForTab: async () => {},

  CreateIsolatedWorktree: async (workspaceRoot) => {
    const res = await createSession(workspaceRoot, MODE_TO_PRESET[currentMode]);
    const tab = sessionToTabMeta({ sessionId: res.sessionId, cwd: workspaceRoot, running: false, projections: {} }, 0);
    tab.isolatedWorktree = true;
    tab.gitBranch = 'reasonix/isolated-' + Date.now().toString(36);
    return tab;
  },
  CreateDeliveryWorktree: async (workspaceRoot) => {
    const res = await createSession(workspaceRoot, MODE_TO_PRESET[currentMode]);
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
  SetDesktopAppearance: async () => {},
  SetCloseBehavior: async () => {},
  SetDisplayMode: async () => {},
  NeedsOnboarding: async () => false,
  Commands: async () => [],
  Capabilities: async () => ({}),
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
  SetThemePack: async () => {},
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
  SetThemePackForTab: async () => {},
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
// 我们返回一个兜底 Promise，避免前端崩溃。
const AppProxy = new Proxy({}, {
  get(_t, prop) {
    const name = String(prop);
    if (name in appImpl) return appImpl[name];
    // 未实现的方法：返回一个安全的兜底（尽力而为）
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
`;
document.addEventListener('DOMContentLoaded', () => document.head.appendChild(dragStyle));
if (document.readyState !== 'loading') document.head.appendChild(dragStyle);

// ---------- 品牌覆盖：DSH-Reasonix ----------
const BRAND_NAME = 'DSH-Reasonix';
const applyBranding = () => {
  if (document.title !== BRAND_NAME) document.title = BRAND_NAME;
  document.querySelectorAll('.startup-splash__name, .welcome__brand, [class*="__name"], [class*="brand"]').forEach((el) => {
    if (el.textContent.trim() === 'Reasonix') el.textContent = BRAND_NAME;
  });
  // logo 图标：把原 SVG 替换为 DSH-Reasonix 文字标识
  document.querySelectorAll('.startup-splash__mark img, .welcome__brand-logo, .sidebar__brand-logo, .onboarding__logo').forEach((img) => {
    if (img.dataset.branded) return;
    img.dataset.branded = '1';
    const parent = img.parentElement;
    img.style.display = 'none';
    const isWordmark = img.classList.contains('sidebar__brand-logo') || img.classList.contains('welcome__brand-logo') || img.classList.contains('onboarding__logo');
    const badge = document.createElement('span');
    badge.className = isWordmark ? 'dsr-wordmark' : 'dsr-badge';
    badge.textContent = isWordmark ? 'DSH-Reasonix' : 'DS';
    parent.appendChild(badge);
  });
};

// 启动品牌覆盖
if (document.readyState !== 'loading') applyBranding();
document.addEventListener('DOMContentLoaded', applyBranding);
if (typeof MutationObserver !== 'undefined') {
  const brandObserver = new MutationObserver(() => applyBranding());
  document.addEventListener('DOMContentLoaded', () => {
    if (document.body) brandObserver.observe(document.body, { childList: true, subtree: true, characterData: true });
  });
  if (document.body) brandObserver.observe(document.body, { childList: true, subtree: true, characterData: true });
}

// 提供 __dsh 调试句柄
window.__dsh = {
  rpc, sessions, history, prompt, createSession, cancelSession,
  calcCost, DEEPSEEK_OFFICIAL_PRICES, RELAY_PRICES, RELAY_PROVIDERS,
  loadPrices, savePrices, fetchOfficialPrices,
  setRelayPrice: (id, table) => { if (table) RELAY_PRICES[id] = table; },
  setOfficialPrice: (model, table) => { if (table) DEEPSEEK_OFFICIAL_PRICES[model] = table; },
};
