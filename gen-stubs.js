// 生成缺失的 Go stub 方法（从 preload.js + 前端调用清单对比）
const fs = require('fs');
const path = require('path');

const FRONTEND = 'C:/Users/chenz/Desktop/DSH-deskop/reasonix-desktop/desktop/frontend/src';
const PRELOAD = 'C:/Users/chenz/Desktop/dsh-reasonix-desktop/src/preload.js';
const GO_DIR = 'C:/Users/chenz/Desktop/dsh-reasonix-wails';
const OUT = path.join(GO_DIR, 'app_stubs2.go');

// 1. 提取 preload.js 的方法定义（名 -> 参数串）
const preloadSrc = fs.readFileSync(PRELOAD, 'utf8');
const preloadDef = {};
const re = /^\s{2}([A-Z][A-Za-z0-9]+):\s*(?:async\s*)?\(([^)]*)\)/gm;
let mm;
while ((mm = re.exec(preloadSrc)) !== null) {
  preloadDef[mm[1]] = mm[2];
}

// 2. 前端调用的方法
const frontendMethods = new Set();
function walk(dir) {
  for (const e of fs.readdirSync(dir, { withFileTypes: true })) {
    const p = path.join(dir, e.name);
    if (e.isDirectory()) {
      if (e.name === '__tests__' || e.name === 'node_modules') continue;
      walk(p);
    } else if (/\.(ts|tsx)$/.test(e.name)) {
      const src = fs.readFileSync(p, 'utf8');
      const r = /app\.[A-Z][A-Za-z0-9]+\(/g;
      let m2;
      while ((m2 = r.exec(src)) !== null) {
        frontendMethods.add(m2[0].replace('app.', '').replace('(', ''));
      }
    }
  }
}
walk(FRONTEND);

// 3. Go 已实现
const goMethods = new Set();
for (const f of fs.readdirSync(GO_DIR)) {
  if (!f.endsWith('.go')) continue;
  const src = fs.readFileSync(path.join(GO_DIR, f), 'utf8');
  const r = /func \(a \*App\) ([A-Z][A-Za-z0-9]+)\(/g;
  let m3;
  while ((m3 = r.exec(src)) !== null) goMethods.add(m3[1]);
}

// 4. 内部函数（不生成）
const internal = new Set(['applyDefaultApproval','buildProjectTree','call','catalog','clearHistory','dshModelsFor','getConfig','history','onEvent','prompt','rpc','sessions','sessionToHistoryMeta','setConfig','setOfficialPrice','setRelayPrice','updateDsh','versions','cancel','deleteSession','createSession','EventsOff']);

// 5. 缺失
const missing = [...frontendMethods].filter(m => !goMethods.has(m) && !internal.has(m)).sort();
console.log(`前端调用 ${frontendMethods.size}，Go 已实现 ${goMethods.size}，缺失 ${missing.length}`);

// 参数类型启发式
function goType(p) {
  p = p.trim();
  if (p === '') return null;
  if (/^(req|options|state|payload|config|settings|body|params|args|input)$/.test(p)) return 'map[string]any';
  if (/^(enabled|pinned|sandbox|force|checked|visible|running|open|active|explicit|auto|dryRun|allowAll)$/.test(p)) return 'bool';
  if (/^(factor|zoom|ratio|ms|depth|n|cols|rows|index|limit|cursor|count|size|revision|turns|port|timeout|height|width|score|generation)$/.test(p)) return 'int';
  return 'string';
}

// 返回值启发式
function retType(m) {
  if (/^(List|Query|Scan|Jobs|Checkpoints|Models|Plugins|Skills|Commands|ThemePacks|Extensions|RemoteHosts|RemoteForwards|RemoteConnectionStatuses|BackgroundRuntimes|InboxSnapshot|AvailableSubagentTools|SubagentsForTab|SubagentProgressForTab|ListTask|ListTasks|ListHistory|ListDir|ListRemoteDir|ListWorkspaces|ListProject|ListTrashed|ListSessions|MCPServers|MCPMarketplace|TodoSnapshot|WorkspaceChanges|WorkspaceGitHistory|HistorySlice|Checkpoint|MemoryRevisions|ProviderPresets|FetchAllProviderModels|ScanSSHConfig|ScanPromptHistory|ExtensionCatalog|ExtensionStatus|HeartbeatListTasks)$/.test(m)) return 'list';
  if (/^(Get|Meta|Balance|Capabilities|Capability|Status|Catalog|Snapshot|Summary|Info|State|Goal|Mode|Plan|Usage|Context|Shell|Diagnostic|Version|Zoom|SlashArgs|Hooks|ExternalOpeners|WorkspaceConflict|WorkspaceRevision|WorkspaceGitCommitDetail|CheckpointSummary|Recovery|SessionCatalog|TaskCatalog|HistoryCatalog|BlankProject|CurrentTask|ProviderModels|PreviewSession|PreviewWorkspace|DeliveryWorktree|IsolatedWorktree|GetRecoveryLineage|GetUserConfigPath|GetTopicSummary|GetSessionCatalogStatus|GetTaskCatalogStatus|BotSettings|BotRuntimeStatus|BotConnectionDiagnostic|ToolResult|ToolApprovalMode|InboxHasItems|RemoteLastWorkspace|RemoteServerStatus|RemoteServerLogs|ShellForTerminal|ThemePackForTab)$/.test(m)) return 'map';
  if (/^(Is|Has|Needs|Available|Enabled|Running|Supported)$/.test(m)) return 'bool';
  if (/^(Current|Pick|Choose|Resolve|Read|ConnectKey|SaveClipboard|SavePasted|AttachmentDataURL|ExportThemePack|BrowserOpenURL|GetUserConfigPath|ShellForTerminal|ZoomFactor)$/.test(m)) return 'str';
  return 'err';
}

const lines = ['package main', '', '// 批量生成的空实现（gen-stubs.js 从 preload.js + 前端调用清单对比生成）。', '// 覆盖前端调用但 Go 端缺失的方法，返回安全空态/降级，防 not-a-function 崩溃。', ''];
for (const m of missing) {
  const params = (preloadDef[m] || '').split(',').map(s => s.trim()).filter(Boolean);
  const goParams = params.map(p => { const t = goType(p); return t ? `_${p} ${t}` : null; }).filter(Boolean);
  const goArgs = params.map(p => goType(p) ? `_${p}` : null).filter(Boolean);
  const paramStr = goParams.join(', ');
  const argStr = goArgs.length ? ', ' + goArgs.join(', ') : '';
  const rt = retType(m);
  if (rt === 'list') lines.push(`func (a *App) ${m}(${paramStr}) []any { return []any{} }`);
  else if (rt === 'map') lines.push(`func (a *App) ${m}(${paramStr}) map[string]any { return nil }`);
  else if (rt === 'bool') lines.push(`func (a *App) ${m}(${paramStr}) bool { return false }`);
  else if (rt === 'str') lines.push(`func (a *App) ${m}(${paramStr}) string { return "" }`);
  else lines.push(`func (a *App) ${m}(${paramStr}) error { return nil }`);
}

fs.writeFileSync(OUT, lines.join('\n') + '\n', 'utf8');
console.log(`已生成 ${OUT}，共 ${lines.length} 行`);
