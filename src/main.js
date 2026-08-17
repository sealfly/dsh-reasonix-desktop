'use strict';
const { app, BrowserWindow, ipcMain, Menu, dialog } = require('electron');
const fs = require('fs');
const path = require('path');
const net = require('net');
const os = require('os');
const { spawn, execSync } = require('child_process');
const { DshClient } = require('./dsh-client');

// reasonix 前端 dist 路径：打包后随 app 一起分发（renderer/dist），开发模式用 reasonix-reference
const REASONIX_DIST = (() => {
  const bundled = path.join(__dirname, '..', 'renderer', 'dist', 'index.html');
  if (fs.existsSync(bundled)) return bundled;
  return path.join(__dirname, '..', '..', 'reasonix-reference', 'desktop', 'frontend', 'dist', 'index.html');
})();
let win = null;
let dsh = null;

// 会话模型转换：DSH session → Reasonix TabMeta
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
      if (c.type === 'tool-call-start') return { kind: 'tool_dispatch', tool: { name: c.name || c.toolName, callId: c.callId }, ...base };
      if (c.type === 'tool-call-result') return { kind: 'tool_result', tool: { name: c.name, callId: c.callId }, detail: c.result ? String(c.result).slice(0, 500) : undefined, ...base };
      return null;
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
  await ensureDsh();
  dsh = new DshClient(3080);
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
    const res = await dsh.rpc('session.list', {});
    return (res.items || []).map(sessionToTabMeta);
  });
  ipcMain.handle('dsh:history', (_e, sid) => dsh.rpc('session.history', { sessionId: sid, limit: 300 }));
  // session.prompt 是长请求（模型生成可能要几分钟），用 10 分钟超时而不是默认 60s
  ipcMain.handle('dsh:prompt', (_e, sid, text, timeoutMs) => dsh.rpc('session.prompt', { sessionId: sid, mode: 'steer', content: [{ type: 'text', text }] }, (typeof timeoutMs === 'number' && timeoutMs > 0) ? timeoutMs : 10 * 60 * 1000));
  ipcMain.handle('app:version', () => app.getVersion());
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
    const target = rel && rel !== '.' && rel !== './'
      ? (rel.startsWith(base) ? rel : require('path').join(base, rel))
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
      const file = require('path').join(dir, '.dsh', 'memory.md');
      const exists = fs.existsSync(file);
      return { exists, body: exists ? fs.readFileSync(file, 'utf8') : '', path: file };
    } catch (e) { return { exists: false, body: '', path: '', error: String(e && e.message || e) }; }
  });
  ipcMain.handle('memory:write', async (_e, cwd, body) => {
    try {
      const dir = String(cwd || '');
      if (!dir || !path.isAbsolute(dir)) return { ok: false, error: 'invalid cwd' };
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
  ipcMain.handle('git:changes', (_e, cwd) => {
    const status = runGit(cwd, 'status --porcelain=v1 -z');
    if (status === null) {
      // 非 git 仓库：回退到"最近修改的文件"（session 改动近似），让改动栏有内容
      const files = [];
      const pathMod = require('path');
      const base = cwd || HOME_DIR;
      try {
        const walk = (dir, depth) => {
          if (depth > 3) return;
          let entries;
          try { entries = fs.readdirSync(dir, { withFileTypes: true }); } catch { return; }
          for (const ent of entries) {
            if (ent.name === 'node_modules' || ent.name === '.git') continue;
            const full = pathMod.join(dir, ent.name);
            if (ent.isDirectory()) { walk(full, depth + 1); continue; }
            try {
              const st = fs.statSync(full);
              if (Date.now() - st.mtimeMs < 24 * 60 * 60 * 1000) { // 24 小时内修改
                files.push({ path: pathMod.relative(base, full), sources: ['session'], gitStatus: 'M', latestTime: st.mtimeMs });
              }
            } catch {}
          }
        };
        walk(base, 0);
        files.sort((a, b) => (b.latestTime || 0) - (a.latestTime || 0));
      } catch {}
      return { gitAvailable: false, gitErr: 'not a git repo; showing recently modified files', files: files.slice(0, 50) };
    }
    const files = [];
    const parts = status.split('\0').filter(Boolean);
    for (let i = 0; i < parts.length; i += 2) {
      const xy = parts[i] || '';
      const p = parts[i + 1];
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

  // ---------- DSH 更新/配置控制（用户可手动指定、手动更新、关闭自动更新） ----------
  ipcMain.handle('dsh:config', () => loadDshConfig());
  ipcMain.handle('dsh:config:set', (_e, patch) => {
    const cur = loadDshConfig();
    const next = { ...cur };
    if (patch && typeof patch === 'object') {
      if (typeof patch.autoUpdate === 'boolean') next.autoUpdate = patch.autoUpdate;
      if (typeof patch.pinnedVersion === 'string') next.pinnedVersion = patch.pinnedVersion.trim();
      if (typeof patch.dshPath === 'string') next.dshPath = patch.dshPath.trim();
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