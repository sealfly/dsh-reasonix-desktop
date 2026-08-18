'use strict';
const { app, BrowserWindow, ipcMain, Menu, dialog } = require('electron');
const fs = require('fs');
const path = require('path');
const net = require('net');
const os = require('os');
const { spawn, execSync } = require('child_process');
const { DshClient } = require('./dsh-client');

// 设置干净的 app 名称：package.json 的 name 含括号（dsh-(reasonix)UI-desktop），
// 会导致 userData 目录名含括号（%APPDATA%\dsh-(reasonix)UI-desktop），
// 这里显式设置一个干净名称，并把 userData 收进应用目录（绿色便携，不污染 %APPDATA%）。
app.setName('DSH-ReasonixUI');
try {
  app.setPath('userData', path.join(__dirname, '..', '.userdata'));
} catch (e) { console.log('[APP] userData redirect failed:', e && e.message); }

// 任务栏分组身份：不设置的话 Windows 会把窗口归到"未分组"，且多窗口不合并。
// 必须与 electron-builder 的 build.appId 一致（package.json 里是 com.dsh.reasonix.ui）。
// （在 whenReady 中调用 setAppUserModelId）

// reasonix 前端 dist 路径：打包后随 app 一起分发（renderer/dist），开发模式用 reasonix-reference
const REASONIX_DIST = (() => {
  const bundled = path.join(__dirname, '..', 'renderer', 'dist', 'index.html');
  if (fs.existsSync(bundled)) return bundled;
  return path.join(__dirname, '..', '..', 'reasonix-reference', 'desktop', 'frontend', 'dist', 'index.html');
})();
let win = null;
let dsh = null;

// 会话模型转换：DSH session → Reasonix TabMeta
// 修复会话标题的 mojibake：DSH 存储标题若为 UTF-8 字节被按 Latin-1 解码，
// 中文会显示成乱码（如 "GitHub上..." → "GitHubä¸Š..."），尝试可逆还原。
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
  // agentPreset 在 session.list 的顶层字段（不在 projections.values 里）
  const preset = s.agentPreset || v.agentPreset || 'code';
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
    label: preset,
    ready: true,
    running: !!s.running,
    pendingPrompt: false,
    backgroundJobs: 0,
    cancelRequested: false,
    cancellable: !!s.running,
    mode: 'normal',
    collaborationMode: 'normal',
    tokenMode: 'full',
    agentPreset: preset,
    toolApprovalMode: v.permissions?.currentValue ? { 'read-only': 'ask', 'workspace-write': 'auto', 'danger-full-access': 'yolo' }[v.permissions.currentValue] || 'auto' : 'auto',
    permissions: v.permissions,
    active: idx === 0,
    cwd: workspaceRoot,
  };
}

// 事件类型归一化：DSH 实时流与历史记录的事件名写法可能不同
// （turn/started vs turn/start、turn/done vs turn/end），统一匹配两种写法。
function normEventType(t) {
  return String(t || '')
    .replace(/\/started$/, '/start')
    .replace(/\/done$/, '/end');
}

// DSH 事件 → Reasonix WireEvent
function dshEventToWire(frame) {
  const ev = frame?.event;
  if (!ev) return null;
  const d = ev.data || {};
  const base = { tabId: frame.sessionId, runtimeEpoch: String(ev.seq) };
  switch (normEventType(ev.type)) {
    case 'turn/start': return { kind: 'turn_started', ...base };
    case 'assistant/chunk': {
      const c = d.chunk;
      if (!c) return null;
      if (c.type === 'reasoning-delta') return { kind: 'reasoning', text: c.text, reasoning: c.text, ...base };
      if (c.type === 'text-delta') return { kind: 'text', text: c.text, ...base };
      if (c.type === 'block-start') return { kind: 'text', text: '', ...base };
      return null;
    }
    // 工具是独立的会话事件（tool/call、tool/result），不是 assistant/chunk；
    // DSH 的 StreamChunk 只有 block-start/text-delta/reasoning-delta
    case 'tool/call': {
      const t = d.tool || {};
      return { kind: 'tool_dispatch', tool: { name: t.name || d.name || d.toolName || 'tool', callId: d.callId || t.callId }, ...base };
    }
    case 'tool/result': {
      const t = d.tool || {};
      return { kind: 'tool_result', tool: { name: t.name || d.name || d.toolName || 'tool', callId: d.callId || t.callId }, detail: d.result ? String(d.result).slice(0, 500) : undefined, ...base };
    }
    case 'turn/end': case 'assistant/end': return { kind: 'turn_done', ...base };
    case 'user/prompt': return null; // 前端自己乐观渲染用户消息
    default: return null;
  }
}

// ---------- DSH 服务自动启动 ----------
function checkPort(port = 3080, ms = 600) {
  return new Promise((resolve) => {
    const sock = net.connect({ host: '127.0.0.1', port }, () => { sock.destroy(); resolve(true); });
    sock.on('error', () => resolve(false));
    sock.setTimeout(ms, () => { sock.destroy(); resolve(false); });
  });
}

// 无会话 cwd 时的回退根目录：用户主目录
// （原代码硬编码了作者机器路径，任何其他机器上文件树/记忆/改动栏都会指向不存在的目录）
const HOME_DIR = os.homedir() || 'C:\\';

// 完全授权模式（yolo / danger-full-access）：解除桥自身的防御性校验，
// 让前端对 DSH 及其 AI 零限制（开发原则 2）。由 preload 在安全模式切换时通知。
let bridgeFullAccess = false;

// 后端 DSH 版本（与前端版本分开显示；探测不到显示 unknown）
let dshBackendVersion = 'unknown';
async function detectDshVersion() {
  const probes = ['host.describe', 'system.info', 'version'];
  for (const m of probes) {
    try {
      const r = await dsh.rpc(m, {}, 5000);
      if (r) {
        if (r.version || r.dshVersion) { dshBackendVersion = String(r.version || r.dshVersion); break; }
        if (m === 'host.describe' && typeof r === 'object') { dshBackendVersion = String(r.version || r.dshVersion || JSON.stringify(r).slice(0, 120)); break; }
      }
    } catch {}
  }
  console.log('[DSH] backend version:', dshBackendVersion);
  logToFile('info', 'DSH backend version: ' + dshBackendVersion);
}

// 本应用拉起的 DSH 相关进程 pid（用于退出/更新时精确清理，绝不动用户其他进程）
const spawnedDshPids = new Set();
// 杀掉本应用拉起的 DSH 进程树
function killSpawnedDsh() {
  for (const pid of spawnedDshPids) {
    try { execSync('taskkill /PID ' + pid + ' /T /F', { stdio: 'ignore', windowsHide: true }); } catch {}
  }
  spawnedDshPids.clear();
}

// 路径沙箱：target 必须解析后位于 root 之内（防 ../ 穿越）。
// root 缺失一律拒绝（防止渲染层绕过根限制直接读任意路径）。
function insideRoot(target, root) {
  try {
    if (!root) return null;
    const t = path.resolve(String(target || ''));
    const r = path.resolve(String(root));
    return (t === r || t.startsWith(r + path.sep)) ? t : null;
  } catch { return null; }
}

// 找到可用的 node（Electron 内没有系统 node，需找 PATH 里的；
// 安装包会自带便携 node（resources/node），优先用自带的，机器上没装 Node 也能跑）
function findNode() {
  const bundled = path.join(process.resourcesPath, 'node', 'node.exe');
  const candidates = [
    process.env.NODE_EXE, // 安装脚本注入
    bundled,              // 打包自带的便携 Node（resources/node/node.exe）
    'node',
    'C:\\Program Files\\nodejs\\node.exe',
    'C:\\Users\\' + (process.env.USERNAME || '') + '\\AppData\\Local\\Programs\\nodejs\\node.exe',
  ];
  for (const c of candidates) {
    if (!c) continue;
    try { execSync('"' + c + '" --version', { stdio: 'ignore', windowsHide: true }); return c; } catch {}
  }
  return null;
}

// ---------- DSH 配置（自动更新 / 手动指定版本 / 手动指定路径） ----------
// 配置文件放在 userData 目录（不随安装包覆盖），记录用户对 DSH 的偏好。
const DSH_CONFIG_FILE = () => path.join(app.getPath('userData'), 'dsh-config.json');

// 简单文件日志：写入 userData/logs/app.log，方便在没终端/没控制台的机器上排查
function logToFile(level, msg) {
  try {
    const dir = path.join(app.getPath('userData'), 'logs');
    fs.mkdirSync(dir, { recursive: true });
    fs.appendFileSync(path.join(dir, 'app.log'), new Date().toISOString() + ' [' + level + '] ' + msg + '\n');
  } catch {}
}

function loadDshConfig() {
  try {
    const raw = fs.readFileSync(DSH_CONFIG_FILE(), 'utf8');
    const cfg = JSON.parse(raw);
    return {
      autoUpdate: cfg.autoUpdate !== false, // 默认 true（自动更新）
      pinnedVersion: typeof cfg.pinnedVersion === 'string' ? cfg.pinnedVersion : '',
      dshPath: typeof cfg.dshPath === 'string' ? cfg.dshPath : '',
    };
  } catch {
    return { autoUpdate: true, pinnedVersion: '', dshPath: '' };
  }
}
function saveDshConfig(cfg) {
  try {
    fs.mkdirSync(app.getPath('userData'), { recursive: true });
    fs.writeFileSync(DSH_CONFIG_FILE(), JSON.stringify(cfg, null, 2), 'utf8');
    return { ok: true };
  } catch (e) { return { ok: false, error: String(e && e.message || e) }; }
}

async function ensureDsh() {
  if (await checkPort()) return true; // 已有 DSH 实例在跑，直接复用（不独占、不重复拉起）
  const cfg = loadDshConfig();
  const node = findNode();
  if (!node) { console.log('[DSH] 未找到 Node.js，请先安装 Node.js'); logToFile('error', 'Node.js not found'); return false; }
  // 若用的是打包自带的 Node，把它所在目录加入 PATH，保证后续 npx.cmd 能被找到
  const nodeDir = path.dirname(node);
  if (nodeDir && process.env.PATH && process.env.PATH.split(';').indexOf(nodeDir) === -1) {
    process.env.PATH = nodeDir + ';' + process.env.PATH;
  }

  // 1) 用户手动指定了 DSH 可执行文件路径 → 直接用，不装不更新
  if (cfg.dshPath) {
    console.log('[DSH] 使用手动指定的 DSH:', cfg.dshPath);
    try {
      const child = spawn(cfg.dshPath, ['web'], { detached: true, stdio: 'ignore', windowsHide: true, shell: true });
      if (child.pid) spawnedDshPids.add(child.pid);
      child.unref();
    } catch (e) { console.log('[DSH] 指定 DSH 启动失败:', e.message); }
    for (let i = 0; i < 180; i++) {
      if (await checkPort()) { console.log('[DSH] 服务已就绪'); return true; }
      await new Promise((r) => setTimeout(r, 1000));
    }
    console.log('[DSH] 指定 DSH 180s 内未就绪');
    return false;
  }

  // 2) 计算要用的 DSH 包规格：锁版本 or 最新 or 本地已有
  let pkg = '@deepseek-ai/dsh';
  if (cfg.pinnedVersion) {
    pkg = '@deepseek-ai/dsh@' + cfg.pinnedVersion;
    console.log('[DSH] 使用手动锁定的 DSH 版本:', cfg.pinnedVersion);
  } else if (cfg.autoUpdate) {
    pkg = '@deepseek-ai/dsh@latest';
    console.log('[DSH] 自动更新开启：每次启动使用 DSH 最新版');
  } else {
    console.log('[DSH] 自动更新关闭：使用本地已安装的 DSH');
  }
  console.log('[DSH] 启动 dsh web 服务...');
  // 只拉起一个 npx 进程（原代码同时 spawn 两个，会竞争 3080 端口、可能双写状态）
  try {
    const child = spawn('npx.cmd', ['-y', pkg, 'web'], {
      detached: true, stdio: 'ignore', windowsHide: true, shell: true,
    });
    if (child.pid) spawnedDshPids.add(child.pid);
    child.unref();
  } catch (e) { console.log('[DSH] npx 启动失败:', e.message); }
  for (let i = 0; i < 180; i++) {
    if (await checkPort()) { console.log('[DSH] 服务已就绪'); return true; }
    await new Promise((r) => setTimeout(r, 1000));
  }
  console.log('[DSH] 服务 180s 内未就绪');
  logToFile('error', 'DSH did not become ready within 180s');
  return false;
}

app.whenReady().then(async () => {
  try { app.setAppUserModelId('com.dsh.reasonix.ui'); } catch (e) { console.log('[APP] setAppUserModelId failed:', e && e.message); }
  await ensureDsh();
  dsh = new DshClient(3080);
  detectDshVersion(); // 异步探测后端 DSH 版本（与前端版本分开）
  Menu.setApplicationMenu(null); // 去掉 File/Edit/View/Window/Help 菜单栏

  win = new BrowserWindow({
    width: 1480, height: 920,
    title: 'DSH-ReasonixUI',
    frame: false, // Reasonix 原版是无边框窗口，自带标题栏
    backgroundColor: '#111214',
    webPreferences: {
      preload: path.join(__dirname, 'preload.js'),
      contextIsolation: false, // Reasonix bridge 需要 window.go 直接可用
      nodeIntegration: false,
      sandbox: false,
    },
  });
  win.loadFile(REASONIX_DIST);

  // 全量订阅 events.mux 原始帧（不筛选），透传给渲染层做通用能力。
  // 放在 win 创建之后，连接即推的 session/subscribed 等初始帧也能到达渲染层。
  dsh.subscribeRaw((frame) => {
    if (win && !win.isDestroyed()) {
      win.webContents.send('dsh:raw-event', frame);
    }
  });
  // 同时保留 Reasonix 需要的 WireEvent 转换通道
  dsh.subscribe((frame) => {
    const wire = dshEventToWire(frame);
    if (wire && win && !win.isDestroyed()) {
      win.webContents.send('dsh:event', wire);
    }
  });

  win.on('closed', () => console.log('[WIN] closed'));
  win.on('close', () => console.log('[WIN] close event'));
  win.webContents.on('did-fail-load', (_e, code2, desc, url) => { console.log('[WIN] did-fail-load', code2, desc, url); logToFile('error', 'did-fail-load ' + code2 + ' ' + desc + ' ' + url); });
  win.webContents.on('render-process-gone', (_e, d) => { console.log('[WIN] render-process-gone', JSON.stringify(d)); logToFile('error', 'render-process-gone ' + JSON.stringify(d)); });
  win.once('ready-to-show', () => { console.log('[WIN] ready-to-show'); win.show(); });
  // 固定窗口标题，防止 Reasonix 前端 <title>Reasonix</title> 覆盖
  win.webContents.on('page-title-updated', (e) => e.preventDefault());
  win.webContents.on('did-finish-load', () => { if (win && !win.isDestroyed()) win.setTitle('DSH-ReasonixUI'); });

  // 窗口控制（前端 bridge 调用 MinimiseMainWindow 等 → 这里执行）
  ipcMain.on('win:min', () => win && win.minimize());
  ipcMain.on('win:max', () => {
    if (!win) return;
    win.isMaximized() ? win.unmaximize() : win.maximize();
  });
  ipcMain.on('win:close', () => win && win.close());
  ipcMain.handle('win:isMaximized', () => (win ? win.isMaximized() : false));
  win.on('maximize', () => win.webContents.send('win:maximized', true));
  win.on('unmaximize', () => win.webContents.send('win:maximized', false));














  // IPC：渲染层调用 DSH（通用透传，任意 method/payload，插件动态注册的方法也能调）。
  // 设计原则：桥接层不限制 DSH 原生能力——插件/前端需要什么方法就透传什么。
  // 仅保留最小的方法名格式校验（合法插件方法名均为 namespace.method 形式，不受影响）；
  // 完全授权模式（yolo）下连这层校验也解除。
  // 完全授权开关（preload 在安全模式切换/会话列表同步时调用）
  ipcMain.on('bridge:setFullAccess', (_e, on) => { bridgeFullAccess = !!on; console.log('[bridge] fullAccess =', bridgeFullAccess); });
  ipcMain.handle('dsh:rpc', async (_e, method, payload, timeoutMs) => {
    if (!bridgeFullAccess && (typeof method !== 'string' || !/^[a-zA-Z0-9_.-]+$/.test(method))) return { ok: false, error: 'invalid method name' };
    const t = (typeof timeoutMs === 'number' && timeoutMs > 0) ? Math.min(timeoutMs, 30 * 60 * 1000) : undefined;
    try { return await dsh.rpc(method, payload, t); }
    catch (e) { return { ok: false, error: String(e && e.message || e) }; }
  });
  ipcMain.handle('dsh:catalog', () => dsh.catalog());
  ipcMain.handle('dsh:sessions', async () => {
    // DSH 分组机制：归档会话（workspace.list.archivedSessionIds）从列表隐藏，
    // 只显示未归档的会话，避免历史残留把 UI 塞满。
    // 内部容错：DSH 抖动/重启时 session.list 失败不应让整个 UI 报 unhandled rejection。
    try {
      const [res, ws] = await Promise.all([
        dsh.rpc('session.list', {}),
        dsh.rpc('workspace.list', {}).catch(() => null),
      ]);
      const archived = new Set((ws && ws.archivedSessionIds) || []);
      return (res.items || [])
        .filter((s) => !archived.has(s.sessionId))
        .map(sessionToTabMeta);
    } catch (e) {
      console.log('[DSH] session.list failed:', e && e.message || e);
      return [];
    }
  });
  ipcMain.handle('dsh:history', (_e, sid) => dsh.rpc('session.history', { sessionId: sid, maxMessages: 300 }));
  // session.prompt 是长请求（模型生成可能要几分钟），用 10 分钟超时而不是默认 60s
  ipcMain.handle('dsh:prompt', (_e, sid, text, timeoutMs) => {
    // 校验参数：session.prompt 的 schema 会因缺 text 报含糊错误，这里先给明确错误
    if (typeof sid !== 'string' || !sid || typeof text !== 'string' || !text) {
      return { ok: false, error: 'invalid prompt args (sessionId and text required)' };
    }
    // session.prompt 是长请求（模型生成可能要几分钟），用 10 分钟超时而不是默认 60s
    return dsh.rpc('session.prompt', { sessionId: sid, mode: 'steer', content: [{ type: 'text', text }] }, (typeof timeoutMs === 'number' && timeoutMs > 0) ? timeoutMs : 10 * 60 * 1000);
  });
  ipcMain.handle('app:version', () => app.getVersion());
  // 版本分开返回：frontend = 本应用版本（package.json），backend = 后端 DSH（@deepseek-ai/dsh）版本
  ipcMain.handle('dsh:versions', () => ({ frontend: app.getVersion(), backend: dshBackendVersion }));
  ipcMain.handle('dsh:create', (_e, cwd, agentPreset) => {
    const payload = {};
    if (cwd) payload.cwd = cwd;
    if (agentPreset) payload.agentPreset = agentPreset;
    return dsh.rpc('session.create', payload);
  });
  ipcMain.handle('dsh:pickFolder', async () => {
    // 无边框窗口传 win 作父窗口可能导致原生对话框挂起；改为独立对话框（不传 win）更稳
    const r = await dialog.showOpenDialog({ properties: ['openDirectory', 'createDirectory'], title: '选择项目父目录' });
    return r.canceled ? null : r.filePaths[0];
  });
  ipcMain.handle('dsh:createFolder', async (_e, target) => {
    const t = String(target || '');
    if (!t || !path.isAbsolute(t)) return { ok: false, error: 'invalid path (must be absolute)' };
    // 非完全授权模式下限制在用户主目录内建目录（防渲染层在系统目录乱建）
    if (!bridgeFullAccess && !insideRoot(t, HOME_DIR)) return { ok: false, error: 'path outside home' };
    fs.mkdirSync(t, { recursive: true });
    return t;
  });
  // 价格配置：读 prices.json（用户可改，涨价后更新即可）
  ipcMain.handle('prices:load', () => {
    try {
      const p = require('path').join(__dirname, '..', 'prices.json');
      return JSON.parse(fs.readFileSync(p, 'utf8'));
    } catch { return null; }
  });
  ipcMain.handle('prices:save', (_e, data) => {
    try {
      const p = require('path').join(__dirname, '..', 'prices.json');
      fs.writeFileSync(p, JSON.stringify(data, null, 2), 'utf8');
      return { ok: true };
    } catch (e) { return { ok: false, error: String(e.message || e) }; }
  });
  // 抓取 DeepSeek 官方最新价格（api-docs 价格页）
  ipcMain.handle('prices:fetchOfficial', async () => {
    try {
      const https = require('https');
      const fetchUrl = (url) => new Promise((resolve, reject) => {
        const req = https.get(url, { headers: { 'User-Agent': 'Mozilla/5.0' }, timeout: 20000 }, (res) => {
          if (res.statusCode >= 300 && res.statusCode < 400 && res.headers.location) {
            const next = res.headers.location.startsWith('http') ? res.headers.location : 'https://api-docs.deepseek.com' + res.headers.location;
            res.resume();
            return fetchUrl(next).then(resolve).catch(reject);
          }
          let d = '';
          res.on('data', (c) => { d += c; });
          res.on('end', () => resolve(d));
        });
        req.on('timeout', () => req.destroy(new Error('fetch timeout')));
        req.on('error', reject);
      });
      const html = await fetchUrl('https://api-docs.deepseek.com/zh-cn/quick_start/pricing');
      // 解析表格：标准价表（缓存命中|未命中|输出，模型顺序与表头对应）
      const rows = [...html.matchAll(/<tr[^>]*>([\s\S]*?)<\/tr>/g)].map(m => {
        const cells = [...m[1].matchAll(/<t[dh][^>]*>([\s\S]*?)<\/t[dh]>/g)].map(c => c[1].replace(/<[^>]+>/g, '').trim());
        return cells;
      });
      const result = {};
      // 模型名（第一行: 模型 | deepseek-v4-flash | deepseek-v4-pro）
      const headerRow = rows.find(r => r[0] === '模型' && r[1] && r[1].startsWith('deepseek-'));
      const models = headerRow ? headerRow.slice(1).filter((m) => m && m.startsWith('deepseek-')) : [];
      // 标准价表：价格(1) 行含缓存命中价（列偏移+1），后两行是未命中/输出
      const priceIdx = rows.findIndex((r) => r[0] && r[0].includes('价格'));
      if (priceIdx >= 0 && models.length && priceIdx + 2 < rows.length) {
        const hitRow = rows[priceIdx];       // ["价格(1)","百万tokens输入（缓存命中）","0.02元","0.025元"]
        const inputRow = rows[priceIdx + 1]; // ["百万tokens输入（缓存未命中）","1元","3元"]
        const outputRow = rows[priceIdx + 2];// ["百万tokens输出","2元","6元"]
        if (inputRow.join('|').includes('缓存未命中') && outputRow.join('|').includes('输出')) {
          models.forEach((model, idx) => {
            const col = idx + 1;
            const hit = parseFloat(String(hitRow[col + 1] || '').replace(/[^0-9.]/g, ''));
            const inp = parseFloat(String(inputRow[col] || '').replace(/[^0-9.]/g, ''));
            const out = parseFloat(String(outputRow[col] || '').replace(/[^0-9.]/g, ''));
            if (hit > 0 && inp > 0 && out > 0) result[model] = { cacheHit: hit, input: inp, output: out };
          });
        }
      }
      return { ok: Object.keys(result).length > 0, prices: result, rows: rows.slice(0, 8) };
    } catch (e) {
      return { ok: false, error: String(e.message || e) };
    }
  });

  // 文件系统（Reasonix @ 菜单 / 工作区）
  // 根目录固定为用户主目录，且防 ../ 穿越；
  // 完全授权模式（yolo / danger-full-access）下解除路径限制（原则：对 DSH/AI 零限制）。
  ipcMain.handle('fs:list', async (_e, rel) => {
    if (bridgeFullAccess) {
      const root = String(rel || HOME_DIR);
      try {
        const entries = fs.readdirSync(root, { withFileTypes: true });
        const pathMod = require('path');
        return entries.map((ent) => ({ name: ent.name, path: pathMod.join(root, ent.name), isDir: ent.isDirectory() }));
      } catch { return []; }
    }
    const base = HOME_DIR;
    // rel 可能被渲染层传成非字符串（number/object），统一按字符串处理，防 startsWith 抛 TypeError
    const r = typeof rel === 'string' ? rel : '';
    const target = r && r !== '.' && r !== './'
      ? (r.startsWith(base) ? r : require('path').join(base, r))
      : base;
    const safe = insideRoot(target, base);
    if (!safe) return [];
    try {
      const entries = fs.readdirSync(safe, { withFileTypes: true });
      const pathMod = require('path');
      return entries.map((ent) => ({
        name: ent.name,
        path: pathMod.join(safe, ent.name),
        isDir: ent.isDirectory(),
      }));
    } catch { return []; }
  });
  ipcMain.handle('fs:read', async (_e, file) => {
    try {
      const safe = bridgeFullAccess ? String(file || '') : insideRoot(file, HOME_DIR);
      if (!safe) return '';
      return fs.readFileSync(safe, 'utf8');
    } catch { return ''; }
  });

  // 绝对路径版文件系统接口（preload 已拼好 cwd+相对路径，这里直接读绝对路径）
  // 默认带 root（会话 cwd）校验，防 ../ 穿越；完全授权模式下解除校验
  ipcMain.handle('fs:listAbs', async (_e, dir, root) => {
    try {
      const target = bridgeFullAccess ? String(dir || '') : insideRoot(dir, root);
      if (!target) return { error: 'path outside root' };
      const entries = fs.readdirSync(target, { withFileTypes: true });
      return entries.map((ent) => ({
        name: ent.name,
        isDir: ent.isDirectory(),
        // 返回相对路径（前端按相对路径拼 tree）
        relPath: ent.name,
      }));
    } catch (e) { return { error: String(e && e.message || e) }; }
  });
  ipcMain.handle('fs:readAbs', async (_e, file, root) => {
    try {
      const target = bridgeFullAccess ? String(file || '') : insideRoot(file, root);
      if (!target) return { ok: false, error: 'path outside root' };
      return { ok: true, body: fs.readFileSync(target, 'utf8'), size: fs.statSync(target).size };
    } catch (e) { return { ok: false, error: String(e && e.message || e) }; }
  });
  // 记忆（Memory）：跟着项目走，存 <cwd>/.dsh/memory.md
  // read 返回 { exists, body }；write 写入；返回 { ok } 或 { ok:false, error }
  ipcMain.handle('memory:read', async (_e, cwd) => {
    try {
      const dir = String(cwd || '');
      if (!dir || !path.isAbsolute(dir)) return { exists: false, body: '', path: '', error: 'invalid cwd' };
      // 与 fs:* 沙箱一致：非完全授权模式下只允许在主目录内读写记忆
      if (!bridgeFullAccess && !insideRoot(dir, HOME_DIR)) return { exists: false, body: '', path: '', error: 'path outside home' };
      const file = require('path').join(dir, '.dsh', 'memory.md');
      const exists = fs.existsSync(file);
      return { exists, body: exists ? fs.readFileSync(file, 'utf8') : '', path: file };
    } catch (e) { return { exists: false, body: '', path: '', error: String(e && e.message || e) }; }
  });
  ipcMain.handle('memory:write', async (_e, cwd, body) => {
    try {
      const dir = String(cwd || '');
      if (!dir || !path.isAbsolute(dir)) return { ok: false, error: 'invalid cwd' };
      if (!bridgeFullAccess && !insideRoot(dir, HOME_DIR)) return { ok: false, error: 'path outside home' };
      const pathMod = require('path');
      const dshDir = pathMod.join(dir, '.dsh');
      const file = pathMod.join(dshDir, 'memory.md');
      fs.mkdirSync(dshDir, { recursive: true });
      fs.writeFileSync(file, String(body == null ? '' : body), 'utf8');
      return { ok: true, path: file };
    } catch (e) { return { ok: false, error: String(e && e.message || e) }; }
  });
  // git 操作（改动栏）
  const { execSync } = require('child_process');
  const runGit = (cwd, args) => {
    try { return execSync('git ' + args, { cwd, encoding: 'utf8', stdio: ['ignore', 'pipe', 'ignore'], windowsHide: true }); }
    catch (e) { return null; }
  };
  ipcMain.handle('git:changes', async (_e, cwd) => {
    const status = runGit(cwd, 'status --porcelain=v1 -z');
    if (status === null) {
      // 非 git 仓库：回退到"最近修改的文件"（session 改动近似），让改动栏有内容
      // 异步递归 + 条目上限：避免同步扫整棵目录树阻塞主进程事件循环
      const files = [];
      const pathMod = require('path');
      const fsp = require('fs').promises;
      const base = cwd || HOME_DIR;
      const MAX_ENTRIES = 3000;
      let scanned = 0;
      const walk = async (dir, depth) => {
        if (depth > 3 || scanned > MAX_ENTRIES) return;
        let entries;
        try { entries = await fsp.readdir(dir, { withFileTypes: true }); } catch { return; }
        for (const ent of entries) {
          if (scanned > MAX_ENTRIES) return;
          if (ent.name === 'node_modules' || ent.name === '.git') continue;
          const full = pathMod.join(dir, ent.name);
          if (ent.isDirectory()) { await walk(full, depth + 1); continue; }
          try {
            const st = await fsp.stat(full);
            scanned++;
            if (Date.now() - st.mtimeMs < 24 * 60 * 60 * 1000) { // 24 小时内修改
              files.push({ path: pathMod.relative(base, full), sources: ['session'], gitStatus: 'M', latestTime: st.mtimeMs });
            }
          } catch {}
        }
      };
      await walk(base, 0);
      files.sort((a, b) => (b.latestTime || 0) - (a.latestTime || 0));
      return { gitAvailable: false, gitErr: 'not a git repo; showing recently modified files', files: files.slice(0, 50) };
    }
    const files = [];
    const parts = status.split('\0').filter(Boolean);
    // porcelain v1 -z：普通条目 "XY path"；rename 条目 "R  old\0new"（下一条是 new path）。
    // 逐条解析（原代码按 i+=2 解析，rename 会把 new path 当独立条目导致错位）。
    for (let i = 0; i < parts.length; i++) {
      const entry = parts[i] || '';
      const xy = entry.slice(0, 2);
      let p = entry.slice(3);
      if (xy[0] === 'R' && entry.length > 3 && i + 1 < parts.length) {
        i++;
        p = parts[i];
      }
      if (!p) continue;
      files.push({ path: p, sources: ['git'], gitStatus: xy.replace(/\s/g, '').slice(0, 2) || 'M' });
    }
    let branch = '';
    try { branch = runGit(cwd, 'branch --show-current').trim(); } catch {}
    return { gitAvailable: true, gitBranch: branch, files };
  });
  ipcMain.handle('git:detail', (_e, cwd, path) => {
    const diff = runGit(cwd, 'diff -- ' + JSON.stringify(path));
    if (diff === null) return { source: 'git', added: 0, removed: 0 };
    const added = (diff.match(/^\+(?!\+\+\+)/gm) || []).length;
    const removed = (diff.match(/^-(?!--)/gm) || []).length;
    return { source: 'git', diff, added, removed, binary: false, truncated: false };
  });
  ipcMain.handle('git:history', (_e, cwd, path) => {
    const log = runGit(cwd, 'log -5 --format=%H|%an|%aI|%s -- ' + JSON.stringify(path));
    if (log === null) return [];
    return log.trim().split('\n').filter(Boolean).map((l) => {
      const [hash, author, date, ...msg] = l.split('|');
      return { hash, author, date, message: msg.join('|') };
    });
  });
  ipcMain.handle('git:branches', (_e, cwd) => {
    const b = runGit(cwd, 'branch --format=%(refname:short)');
    return b ? b.trim().split('\n').filter(Boolean) : [];
  });
  ipcMain.handle('dsh:cancel', (_e, sid) => dsh.rpc('session.cancel', { sessionId: sid }));

  // ---------- 插件市场/商店（DSH web profile 挂载的 dshmarket + dsh-plugin-store） ----------
  // dshmarket 是 HTTP 路由（非 /api RPC），且 POST 要求 same-origin（带 Origin 头）；
  // pluginStore 是 Typert Remote RPC，payload 必须包 {args:{...}}（由 preload 包好，走通用 dsh:rpc）。
  const httpMod = require('http');
  let pluginMarketCache = { at: 0, data: null };
  function fetchDshMarket(path, timeoutMs = 15000) {
    return new Promise((resolve, reject) => {
      const req = httpMod.get({ host: '127.0.0.1', port: 3080, path, timeout: timeoutMs, headers: { Origin: 'http://127.0.0.1:3080' } }, (res) => {
        let d = '';
        res.on('data', (c) => { d += c; });
        res.on('end', () => {
          try { resolve(JSON.parse(d)); } catch { reject(new Error('bad market response')); }
        });
      });
      req.on('timeout', () => req.destroy(new Error('market timeout')));
      req.on('error', reject);
      req.end();
    });
  }
  // 市场目录 + 已装清单（合并返回；目录缓存 5 分钟，避免每次打开都拉远程）
  ipcMain.handle('dsh:pluginMarketplace', async () => {
    try {
      if (pluginMarketCache.data && Date.now() - pluginMarketCache.at < 5 * 60 * 1000) return pluginMarketCache.data;
      const [reg, installed] = await Promise.all([
        fetchDshMarket('/dsh-market/registry').catch(() => null),
        fetchDshMarket('/dsh-market/installed').catch(() => null),
      ]);
      const data = {
        ok: !!reg,
        source: (reg && reg.source) || 'error',
        registry: (reg && reg.registry) || null,
        installed: (installed && installed.installed) || {},
      };
      pluginMarketCache = { at: Date.now(), data };
      return data;
    } catch (e) { return { ok: false, error: String(e && e.message || e) }; }
  });
  // 市场动作：install/uninstall/update/toggle（白名单动作 + 大小限制，防注入）
  ipcMain.handle('dsh:pluginMarketAction', async (_e, action, body) => {
    const allowed = ['install', 'uninstall', 'update', 'toggle'];
    const a = String(action || '');
    if (allowed.indexOf(a) === -1) return { ok: false, error: 'invalid action' };
    if (!body || typeof body !== 'object' || Array.isArray(body)) return { ok: false, error: 'invalid body' };
    try {
      const payload = JSON.stringify(body);
      if (Buffer.byteLength(payload) > 4096) return { ok: false, error: 'body too large' };
      return await new Promise((resolve) => {
        const req = httpMod.request({
          host: '127.0.0.1', port: 3080, path: '/dsh-market/' + a, method: 'POST',
          headers: { 'Content-Type': 'application/json', 'Content-Length': Buffer.byteLength(payload), Origin: 'http://127.0.0.1:3080' },
          timeout: 60000,
        }, (res) => {
          let d = '';
          res.on('data', (c) => { d += c; });
          res.on('end', () => {
            try { resolve({ ok: res.statusCode < 400, status: res.statusCode, body: JSON.parse(d) }); }
            catch { resolve({ ok: res.statusCode < 400, status: res.statusCode, body: d }); }
          });
        });
        req.on('timeout', () => req.destroy());
        req.on('error', (e) => resolve({ ok: false, error: String(e && e.message || e) }));
        req.write(payload);
        req.end();
      });
    } catch (e) { return { ok: false, error: String(e && e.message || e) }; }
  });

  // ---------- 会话清理：归档（隐藏）+ 物理删除日志 ----------
  // DSH 没有删除 API；安全流程 = workspace.archiveSession（从列表隐藏）→ 删除
  // <DSH_HOME>/sessions/<项目键>/<sessionId>/ 目录（每会话独立日志文件）。
  // 运行中的会话绝不能删（会导致 corrupt session log），一律拒绝。
  const DSH_HOME_DIR = () => process.env.DSH_HOME || path.join(os.homedir(), '.dsh');
  function sessionDirFor(sessionId) {
    try {
      const root = path.join(DSH_HOME_DIR(), 'sessions');
      for (const proj of fs.readdirSync(root, { withFileTypes: true })) {
        if (!proj.isDirectory()) continue;
        const cand = path.join(root, proj.name, sessionId);
        if (fs.existsSync(cand)) return cand;
      }
    } catch {}
    return null;
  }
  async function listRawSessions() {
    try { const r = await dsh.rpc('session.list', {}); return (r && r.items) || []; }
    catch { return []; }
  }
  async function archiveSessionQuiet(id) {
    try { await dsh.rpc('workspace.archiveSession', { sessionId: id }); } catch {}
  }
  function purgeSessionDir(id) {
    const dir = sessionDirFor(id);
    if (!dir) return { purged: false };
    try { fs.rmSync(dir, { recursive: true, force: true }); return { purged: true, dir }; }
    catch (e) { return { purged: false, dir, error: String(e && e.message || e) }; }
  }
  // 删除特定会话（非 running 才允许；归档 RPC 往返后复查，防"检查→删除"间隙会话被唤醒）
  ipcMain.handle('dsh:deleteSession', async (_e, sessionId) => {
    try {
      const id = String(sessionId || '');
      if (!/^[a-zA-Z0-9-]+$/.test(id)) return { ok: false, error: 'invalid sessionId' };
      let items = await listRawSessions();
      let s = items.find((x) => x.sessionId === id);
      if (!s) return { ok: false, error: 'session not found' };
      if (s.running) return { ok: false, error: 'running session cannot be deleted' };
      await archiveSessionQuiet(id);
      // 复查：archive 的 RPC 往返期间会话可能被唤醒（steer/queue），删除活跃日志会损坏会话
      items = await listRawSessions();
      s = items.find((x) => x.sessionId === id);
      if (s && s.running) return { ok: true, archived: true, purged: false, skipped: 'became running during delete' };
      const p = purgeSessionDir(id);
      if (p.purged) return { ok: true, archived: true, purged: true, dir: p.dir };
      // 已归档但目录找不到/删不掉（布局变化或已删）：宽容处理，不报错，只记日志
      console.warn('[DSH] session ' + id + ' archived; log dir missing/undeletable:', p.error || 'not found');
      return { ok: true, archived: true, purged: false };
    } catch (e) { return { ok: false, error: String(e && e.message || e) }; }
  });
  // 清空历史：归档 + 删除所有非 running 会话（逐个复查 running，避免误删刚唤醒的会话）
  ipcMain.handle('dsh:purgeHistory', async () => {
    try {
      let items = await listRawSessions();
      const cold = items.filter((x) => !x.running);
      const details = [];
      for (const s of cold) {
        await archiveSessionQuiet(s.sessionId);
        // 复查 running：归档往返期间被唤醒的会话跳过删除
        const now = await listRawSessions();
        const cur = now.find((x) => x.sessionId === s.sessionId);
        if (cur && cur.running) { details.push({ sessionId: s.sessionId, archived: true, purged: false, skipped: 'running' }); continue; }
        const p = purgeSessionDir(s.sessionId);
        details.push({ sessionId: s.sessionId, archived: true, purged: p.purged });
      }
      return { ok: true, deleted: details.filter((d) => d.purged).length, details };
    } catch (e) { return { ok: false, error: String(e && e.message || e) }; }
  });

  // ---------- DSH 更新/配置控制（用户可手动指定、手动更新、关闭自动更新） ----------
  ipcMain.handle('dsh:config', () => loadDshConfig());
  ipcMain.handle('dsh:config:set', (_e, patch) => {
    const cur = loadDshConfig();
    const next = { ...cur };
    if (patch && typeof patch === 'object') {
      if (typeof patch.autoUpdate === 'boolean') next.autoUpdate = patch.autoUpdate;
      if (typeof patch.pinnedVersion === 'string') {
        const pv = patch.pinnedVersion.trim();
        // 防配置注入命令行：版本号只允许 semver 字符
        if (pv && !/^[0-9a-zA-Z.\-+]+$/.test(pv)) return { ok: false, error: 'invalid pinnedVersion (semver only)' };
        next.pinnedVersion = pv;
      }
      if (typeof patch.dshPath === 'string') {
        const dp = patch.dshPath.trim();
        // dshPath 会被 spawn 执行：必须是存在的绝对路径
        if (dp && (!path.isAbsolute(dp) || !fs.existsSync(dp))) return { ok: false, error: 'dshPath must be an existing absolute path' };
        next.dshPath = dp;
      }
    }
    const r = saveDshConfig(next);
    return { ok: r.ok, config: next, error: r.error };
  });
  // 手动更新 DSH：强制重装 @latest 并重启服务
  ipcMain.handle('dsh:update', async () => {
    try {
      const node = findNode();
      if (!node) return { ok: false, error: '未找到 Node.js' };
      // 先杀本应用拉起的 DSH 进程树；再按命令行精确过滤 @deepseek-ai/dsh 的 node 进程。
      // 绝不使用 taskkill /IM node.exe（会误杀用户机器上所有 node 程序）。
      killSpawnedDsh();
      try {
        require('child_process').spawnSync('powershell.exe', ['-NoProfile', '-Command',
          "Get-CimInstance Win32_Process -Filter \"Name='node.exe'\" | Where-Object { $_.CommandLine -match 'deepseek-ai/dsh' } | ForEach-Object { Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue }"],
          { windowsHide: true, stdio: 'ignore' });
      } catch {}
      const child = spawn(node, ['-e', 'require("child_process").spawn("npx.cmd",["-y","@deepseek-ai/dsh@latest","web"],{stdio:"inherit",shell:true})'], {
        detached: true, stdio: 'ignore', windowsHide: true,
      });
      if (child.pid) spawnedDshPids.add(child.pid);
      child.unref();
      for (let i = 0; i < 60; i++) {
        if (await checkPort()) return { ok: true, message: 'DSH 已更新并重启' };
        await new Promise((r) => setTimeout(r, 1000));
      }
      return { ok: false, error: 'DSH 更新后 60s 内未就绪' };
    } catch (e) { return { ok: false, error: String(e && e.message || e) }; }
  });
});
app.on('window-all-closed', () => {
  // 退出时清掉本应用拉起的 DSH 后台进程，不留残留（只杀自己 spawn 的）
  killSpawnedDsh();
  app.quit();
});