'use strict';
const { app, BrowserWindow, ipcMain, Menu, dialog } = require('electron');
const fs = require('fs');
const path = require('path');
const net = require('net');
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

// DSH 事件 → Reasonix WireEvent
function dshEventToWire(frame) {
  const ev = frame?.event;
  if (!ev) return null;
  const d = ev.data || {};
  const base = { tabId: frame.sessionId, runtimeEpoch: String(ev.seq) };
  switch (ev.type) {
    case 'turn/started': return { kind: 'turn_started', ...base };
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
    case 'turn/done': case 'assistant/done': return { kind: 'turn_done', ...base };
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

// 找到可用的 node（Electron 内没有系统 node，需找 PATH 里的）
function findNode() {
  const candidates = [
    process.env.NODE_EXE, // 安装脚本注入
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
  if (!node) { console.log('[DSH] 未找到 Node.js，请先安装 Node.js'); return false; }

  // 1) 用户手动指定了 DSH 可执行文件路径 → 直接用，不装不更新
  if (cfg.dshPath) {
    console.log('[DSH] 使用手动指定的 DSH:', cfg.dshPath);
    try {
      const child = spawn(cfg.dshPath, ['web'], { detached: true, stdio: 'ignore', windowsHide: true, shell: true });
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
  try {
    const child = spawn(node, ['-e', 'require("child_process").spawn("npx.cmd",["-y",' + JSON.stringify(pkg) + ',"web"],{stdio:"inherit",shell:true})'], {
      detached: true, stdio: 'ignore', windowsHide: true,
    });
    child.unref();
    const child2 = spawn('npx.cmd', ['-y', pkg, 'web'], {
      detached: true, stdio: 'ignore', windowsHide: true, shell: true,
    });
    child2.unref();
  } catch (e) { console.log('[DSH] npx 启动失败:', e.message); }
  for (let i = 0; i < 180; i++) {
    if (await checkPort()) { console.log('[DSH] 服务已就绪'); return true; }
    await new Promise((r) => setTimeout(r, 1000));
  }
  console.log('[DSH] 服务 180s 内未就绪');
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
  win.webContents.on('did-fail-load', (_e, code2, desc, url) => console.log('[WIN] did-fail-load', code2, desc, url));
  win.webContents.on('render-process-gone', (_e, d) => console.log('[WIN] render-process-gone', JSON.stringify(d)));
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














  // IPC：渲染层调用 DSH（通用透传，任意 method/payload，插件方法也可调）
  ipcMain.handle('dsh:rpc', (_e, method, payload) => dsh.rpc(method, payload));
  ipcMain.handle('dsh:catalog', () => dsh.catalog());
  ipcMain.handle('dsh:sessions', async () => {
    const res = await dsh.rpc('session.list', {});
    return (res.items || []).map(sessionToTabMeta);
  });
  ipcMain.handle('dsh:history', (_e, sid) => dsh.rpc('session.history', { sessionId: sid, limit: 300 }));
  ipcMain.handle('dsh:prompt', (_e, sid, text) => dsh.rpc('session.prompt', { sessionId: sid, mode: 'steer', content: [{ type: 'text', text }] }));
  ipcMain.handle('dsh:create', (_e, cwd, agentPreset) => {
    const payload = {};
    if (cwd) payload.cwd = cwd;
    if (agentPreset) payload.agentPreset = agentPreset;
    return dsh.rpc('session.create', payload);
  });
  ipcMain.handle('dsh:pickFolder', async () => {
    const r = await dialog.showOpenDialog(win, { properties: ['openDirectory', 'createDirectory'], title: '选择项目父目录' });
    return r.canceled ? null : r.filePaths[0];
  });
  ipcMain.handle('dsh:createFolder', async (_e, target) => {
    fs.mkdirSync(target, { recursive: true });
    return target;
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
        https.get(url, { headers: { 'User-Agent': 'Mozilla/5.0' } }, (res) => {
          if (res.statusCode >= 300 && res.statusCode < 400 && res.headers.location) {
            const next = res.headers.location.startsWith('http') ? res.headers.location : 'https://api-docs.deepseek.com' + res.headers.location;
            res.resume();
            return fetchUrl(next).then(resolve).catch(reject);
          }
          let d = '';
          res.on('data', (c) => { d += c; });
          res.on('end', () => resolve(d));
        }).on('error', reject);
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
  ipcMain.handle('fs:list', async (_e, rel) => {
    const base = 'C:\\Users\\ROG Zephyrus G16\\Desktop\\DSH';
    const target = rel && rel !== '.' && rel !== './'
      ? (rel.startsWith(base) ? rel : require('path').join(base, rel))
      : base;
    try {
      const entries = fs.readdirSync(target, { withFileTypes: true });
      const pathMod = require('path');
      return entries.map((ent) => ({
        name: ent.name,
        path: pathMod.join(target, ent.name),
        isDir: ent.isDirectory(),
      }));
    } catch { return []; }
  });
  ipcMain.handle('fs:read', async (_e, file) => {
    try { return fs.readFileSync(file, 'utf8'); } catch { return ''; }
  });
  // 记忆（Memory）：跟着项目走，存 <cwd>/.dsh/memory.md
  // read 返回 { exists, body }；write 写入；返回 { ok } 或 { ok:false, error }
  ipcMain.handle('memory:read', async (_e, cwd) => {
    try {
      const dir = String(cwd || '');
      const file = require('path').join(dir, '.dsh', 'memory.md');
      const exists = fs.existsSync(file);
      return { exists, body: exists ? fs.readFileSync(file, 'utf8') : '', path: file };
    } catch (e) { return { exists: false, body: '', path: '', error: String(e && e.message || e) }; }
  });
  ipcMain.handle('memory:write', async (_e, cwd, body) => {
    try {
      const dir = String(cwd || '');
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
      const base = cwd || 'C:\\Users\\ROG Zephyrus G16\\Desktop\\DSH';
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
      // 杀掉现有 DSH 进程（由 npx 拉起的），再强制拉最新
      execSync('taskkill /F /IM node.exe /FI "WINDOWTITLE eq dsh" 2>nul', { stdio: 'ignore', windowsHide: true });
      const child = spawn(node, ['-e', 'require("child_process").spawn("npx.cmd",["-y","@deepseek-ai/dsh@latest","web"],{stdio:"inherit",shell:true})'], {
        detached: true, stdio: 'ignore', windowsHide: true,
      });
      child.unref();
      for (let i = 0; i < 60; i++) {
        if (await checkPort()) return { ok: true, message: 'DSH 已更新并重启' };
        await new Promise((r) => setTimeout(r, 1000));
      }
      return { ok: false, error: 'DSH 更新后 60s 内未就绪' };
    } catch (e) { return { ok: false, error: String(e && e.message || e) }; }
  });
});
app.on('window-all-closed', () => app.quit());