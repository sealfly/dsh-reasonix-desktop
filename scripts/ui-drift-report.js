#!/usr/bin/env node
/*
 * ui-drift-report.js — 官方 Reasonix 桌面前端「UI / 契约漂移」自动对照。
 *
 * 用途：每次官方发版后跑一次，输出"哪些 UI 与契约变了"，并判定哪些变化会影响本项目
 * （注入锚点是否还在、宿主契约层是否出现、桥调用模式是否改变）。回答"要不要跟、跟的代价"。
 *
 * 用法：
 *   node scripts/ui-drift-report.js --baseline <src> --target <src> [--md out.md] [--json out.json]
 *   node scripts/ui-drift-report.js --upstream-root <目录> --baseline-version 1.38.2 --version 1.38.10
 *   node scripts/ui-drift-report.js --upstream-root <目录> --list-versions
 *
 * 版本目录约定（与本项目的历史解压布局一致）：
 *   1.31.4 -> <root>\src1314\**\desktop\frontend\src
 *   其它    -> <root>\probe\src<版本>\**\desktop\frontend\src
 *   最新    -> <root>\src\**\desktop\frontend\src
 *
 * 退出码：0=无破坏性变化；1=有破坏性变化（需人工评估）；2=用法/路径错误。
 */
"use strict";

const fs = require("fs");
const path = require("path");

// ── 参数解析 ────────────────────────────────────────────────────────
const argv = process.argv.slice(2);
function argValue(flag, fallback) {
  const i = argv.indexOf(flag);
  return i >= 0 && argv[i + 1] ? argv[i + 1] : fallback;
}
const upstreamRoot = argValue("--upstream-root", "");
const baselineVersion = argValue("--baseline-version", "");
const targetVersion = argValue("--version", "");
const mdOut = argValue("--md", "");
const jsonOut = argValue("--json", "");

/** 在 <root> 下找到包含 desktop/frontend/src 的目录。 */
function resolveVersionDir(root, version) {
  const candidateRoots = [];
  if (version === "1.31.4") candidateRoots.push(path.join(root, "src1314"));
  if (version === "1.38.8") candidateRoots.push(path.join(root, "src"));
  candidateRoots.push(path.join(root, "probe", `src${version}`));
  candidateRoots.push(path.join(root, "probe", `desktop-v${version}`));
  for (const base of candidateRoots) {
    if (!fs.existsSync(base)) continue;
    const stack = [base];
    while (stack.length) {
      const dir = stack.pop();
      const direct = path.join(dir, "desktop", "frontend", "src");
      if (fs.existsSync(direct)) return direct;
      let entries = [];
      try {
        entries = fs.readdirSync(dir, { withFileTypes: true });
      } catch {
        continue;
      }
      for (const e of entries) {
        if (e.isDirectory() && !/node_modules|\.git/.test(e.name)) stack.push(path.join(dir, e.name));
      }
    }
  }
  return null;
}

function listVersions(root) {
  const found = [];
  const probe = path.join(root, "probe");
  if (fs.existsSync(probe)) {
    for (const d of fs.readdirSync(probe)) {
      const m = /^src(\d+\.\d+\.\d+)$/.exec(d);
      if (m && resolveVersionDir(root, m[1])) found.push(m[1]);
    }
  }
  if (resolveVersionDir(root, "1.31.4")) found.push("1.31.4");
  if (resolveVersionDir(root, "1.38.8")) found.push("1.38.8");
  return [...new Set(found)].sort((a, b) => cmpVersion(a, b));
}

function cmpVersion(a, b) {
  const pa = a.split(".").map(Number);
  const pb = b.split(".").map(Number);
  for (let i = 0; i < 3; i += 1) if (pa[i] !== pb[i]) return pa[i] - pb[i];
  return 0;
}

if (argv.includes("--list-versions")) {
  if (!upstreamRoot) { console.error("需要 --upstream-root"); process.exit(2); }
  const vs = listVersions(upstreamRoot);
  console.log(`本地可对照的官方版本（${vs.length}）：${vs.join(", ")}`);
  process.exit(0);
}

let baselineDir = argValue("--baseline", "");
let targetDir = argValue("--target", "");
if (upstreamRoot && baselineVersion) baselineDir = resolveVersionDir(upstreamRoot, baselineVersion) || "";
if (upstreamRoot && targetVersion) targetDir = resolveVersionDir(upstreamRoot, targetVersion) || "";
// --history 模式不需要 baseline/target（它扫描全部本地版本），所以这里跳过校验
const historyMode = argv.includes("--history");
if (!historyMode && (!baselineDir || !targetDir || !fs.existsSync(baselineDir) || !fs.existsSync(targetDir))) {
  console.error("需要 --baseline 与 --target（或 --upstream-root + --baseline-version + --version）");
  console.error("提示：解压 tar 包请用 C:\\Windows\\System32\\tar.exe 的绝对路径（MSYS 的 tar 会把 C:\\ 当远程主机，解出空目录）");
  process.exit(2);
}

// ── 源码装载（一次读入，供各探针复用）────────────────────────────────
//
// 除 src/** 外，还读 frontend 根目录的关键文件（index.html / package.json /
// vite.config.ts / scripts/*.mjs）——品牌锚点（boot-shell、wails-spinner）与边界校验脚本
// 都在那儿；不读会把它们误判成"锚点消失"（实测踩过）。
function loadTree(srcDir) {
  const files = new Map();
  const walk = (dir) => {
    let entries = [];
    try { entries = fs.readdirSync(dir, { withFileTypes: true }); } catch { return; }
    for (const e of entries) {
      const p = path.join(dir, e.name);
      if (e.isDirectory()) {
        if (/node_modules|\.git|__tests__/.test(e.name)) continue;
        walk(p);
      } else if (/\.(tsx?|css)$/.test(e.name)) {
        files.set(path.relative(srcDir, p).split(path.sep).join("/"), fs.readFileSync(p, "utf8"));
      }
    }
  };
  walk(srcDir);

  // frontend 根（src 的上一级）：index.html、package.json、scripts/*.mjs
  const frontendRoot = path.dirname(srcDir);
  const rootFiles = ["index.html", "package.json", "vite.config.ts"];
  for (const rel of rootFiles) {
    const p = path.join(frontendRoot, rel);
    if (fs.existsSync(p)) files.set(`@root/${rel}`, fs.readFileSync(p, "utf8"));
  }
  const scriptsDir = path.join(frontendRoot, "scripts");
  if (fs.existsSync(scriptsDir)) {
    for (const e of fs.readdirSync(scriptsDir, { withFileTypes: true })) {
      if (e.isFile() && /\.(mjs|js)$/.test(e.name)) {
        files.set(`@root/scripts/${e.name}`, fs.readFileSync(path.join(scriptsDir, e.name), "utf8"));
      }
    }
  }

  return {
    dir: srcDir,
    frontendRoot,
    files,
    file(rel) { return files.get(rel) || ""; },
    exists(rel) { return files.has(rel) || fs.existsSync(path.join(srcDir, rel)); },
    /** 在全部源码里做正则计数 */
    count(re) { let n = 0; for (const t of files.values()) n += (t.match(re) || []).length; return n; },
    /** 在全部源码里找命中文件（相对路径） */
    filesMatching(re) {
      const out = [];
      for (const [rel, t] of files) if (re.test(t)) out.push(rel);
      return out.sort();
    },
    /** 某文件里的正则计数 */
    countIn(rel, re) { return ((this.file(rel)).match(re) || []).length; },
  };
}

const B = loadTree(baselineDir);
const T = loadTree(targetDir);

// ── 特征表（声明式：每个条目 = 一个"要不要跟"的观察点）────────────────
//
// kind: anchor = 本项目注入/品牌依赖的 DOM 锚点（缺了就会碎）
//       arch   = 宿主契约/调用模式（变了要评估工作量）
//       ui     = 用户可见的界面形态
// breaking(baseForm,targetForm,detail)：返回是否需要人工评估
const FEATURES = [
  // ---------- 我们注入与品牌依赖的锚点 ----------
  { id: "anchor.workbenchDockTabs", kind: "anchor", label: "右栏 tab 容器 .workbench-dock__tabs",
    probe: (v) => v.count(/workbench-dock__tabs/g) },
  { id: "anchor.workbenchDockTab", kind: "anchor", label: "右栏 tab 按钮 .workbench-dock__tab",
    probe: (v) => v.count(/workbench-dock__tab\b/g) },
  { id: "anchor.workbenchDockBody", kind: "anchor", label: "右栏容器 .workbench-dock__body",
    probe: (v) => v.count(/workbench-dock__body/g) },
  { id: "anchor.settingsNavItem", kind: "anchor", label: "设置导航项 .settings-center__navitem",
    probe: (v) => v.count(/settings-center__navitem/g) },
  { id: "anchor.providerConnItem", kind: "anchor", label: "模型服务左列表 .provider-connections__item",
    probe: (v) => v.count(/provider-connections__item/g) },
  { id: "anchor.connectionTitle", kind: "anchor", label: "模型服务详情标题 .connection-title",
    probe: (v) => v.count(/connection-title/g) },
  { id: "anchor.providerUrlInput", kind: "anchor", label: "模型服务地址输入 .provider-url-input",
    probe: (v) => v.count(/provider-url-input/g) },
  { id: "anchor.composerAccessMenu", kind: "anchor", label: "Composer 附件菜单 .composer-access-menu__section",
    probe: (v) => v.count(/composer-access-menu__section/g) },
  { id: "anchor.workspaceTreeRow", kind: "anchor", label: "文件树行 .workspace-tree__row（内联编辑器锚点）",
    probe: (v) => v.count(/workspace-tree__row/g) },
  { id: "anchor.bootShell", kind: "anchor", label: "启动壳 boot-shell / boot-shell__mark（品牌锚点，index.html）",
    probe: (v) => {
      const html = v.file("@root/index.html");
      return { mark: (html.match(/boot-shell__mark/g) || []).length,
        name: (html.match(/boot-shell__name/g) || []).length,
        found: /boot-shell/.test(html) };
    },
    form: (d) => (d.found ? `有（mark=${d.mark}, name=${d.name}）` : "无") },
  { id: "anchor.wailsSpinner", kind: "anchor", label: "Wails 开发遮罩锚点 wails-spinner（index.html）",
    probe: (v) => ({ found: /wails-spinner/.test(v.file("@root/index.html")) }),
    form: (d) => (d.found ? "有" : "无") },

  // ---------- 宿主契约 / 调用模式 ----------
  { id: "arch.bridge", kind: "arch", label: "lib/bridge.ts（React-to-Go 契约）",
    probe: (v) => {
      const t = v.file("lib/bridge.ts");
      return { exists: t.length > 0, kb: Math.round(t.length / 1024),
        usesWindowGo: /window\.go\?\.main\?\.App|window\.go\.main\.App/.test(t),
        methods: new Set([...t.matchAll(/^\s{1,4}([A-Z][A-Za-z0-9]{2,60})\s*\(/gm)].map((m) => m[1])).size };
    },
    form: (d) => d.exists ? `${d.kb}KB / ${d.methods} 方法 / window.go=${d.usesWindowGo ? "是" : "否"}` : "缺失" },
  { id: "arch.desktopHost", kind: "arch", label: "宿主契约层 lib/desktopHost.ts",
    probe: (v) => ({ exists: v.exists("lib/desktopHost.ts") }),
    form: (d) => (d.exists ? "★出现（window.reasonixDesktop 契约层）" : "无") },
  { id: "arch.contractFile", kind: "arch", label: "generated/desktopContract.generated.ts",
    probe: (v) => ({ exists: v.exists("generated/desktopContract.generated.ts") }),
    form: (d) => (d.exists ? "★出现（命令契约文件）" : "无") },
  { id: "arch.boundaryCheck", kind: "arch", label: "scripts/check-desktop-host-boundary.mjs（禁 window.go）",
    probe: (v) => ({ exists: Boolean(v.file("@root/scripts/check-desktop-host-boundary.mjs")) }),
    form: (d) => (d.exists ? "★出现（边界校验：会禁我们的调用）" : "无") },
  { id: "arch.tabContainer", kind: "arch", label: "components/TabContainer/（可增删 tab 容器）",
    probe: (v) => ({ exists: v.filesMatching(/TabAddMenu|ADDABLE_TABS/).length > 0,
      files: v.filesMatching(/TabAddMenu|ADDABLE_TABS/).length }),
    form: (d) => (d.exists ? `★出现（${d.files} 个相关文件）` : "无") },
  { id: "arch.eventChannel", kind: "arch", label: "事件通道 agent:event",
    probe: (v) => v.count(/agent:event/g) },
  { id: "arch.bridgeMethods", kind: "arch", label: "bridge.ts 接口方法数",
    probe: (v) => {
      const t = v.file("lib/bridge.ts");
      return new Set([...t.matchAll(/^\s{1,4}([A-Z][A-Za-z0-9]{2,60})\s*\(/gm)].map((m) => m[1])).size;
    } },

  // ---------- 用户可见 UI 形态 ----------
  { id: "ui.qualityFloor", kind: "ui", label: "Composer 验收控件（标准|交付）",
    probe: (v) => {
      const t = v.file("components/Composer.tsx");
      const std = (t.match(/qualityFloorStandard/g) || []).length;
      const del = (t.match(/qualityFloorDelivery/g) || []).length;
      const choose = (t.match(/chooseQualityFloor/g) || []).length;
      const moved = v.filesMatching(/qualityFloor|QualityFloor/).filter((f) => /components\/.*\.tsx$/.test(f));
      let form = "无";
      if (std > 0 && del > 0) form = "两个并排按钮(标准|交付)";
      else if (del > 0 && choose > 0) form = "单个勾选项(交付)";
      else if (moved.length) form = `仅残留引用(${moved.length} 文件)`;
      return { form, std, del, choose, files: moved.length };
    },
    form: (d) => d.form },
  { id: "ui.modelSwitcher", kind: "ui", label: "Composer 模型切换栏（分类搜索栏）",
    probe: (v) => {
      const t = v.file("components/ModelSwitcher.tsx");
      const chips = (t.match(/activeFilter/g) || []).length;
      const fav = (t.match(/favorite/gi) || []).length;
      return { lines: t ? t.split("\n").length : 0, chips, fav,
        classified: chips > 0 && fav > 0 };
    },
    form: (d) => d.lines === 0 ? "文件不存在" : `${d.lines} 行 / 分类筛选=${d.classified ? "★有" : "无"}` },
  { id: "ui.settingsTabs", kind: "ui", label: "设置页签结构（SettingsTab 成员 / providers 是否独立）",
    probe: (v) => {
      // SettingsTab 定义在 lib/types.ts（实测：不在 SettingsPanel.tsx 里，早先按组件文件找会得出 0 个页签）
      const types = v.file("lib/types.ts");
      const union = /export type SettingsTab\s*=([\s\S]{0,900}?);/.exec(types);
      const tabs = union ? [...union[1].matchAll(/"([a-z-]+)"/g)].map((m) => m[1]) : [];
      const panel = v.file("components/SettingsPanel.tsx");
      const redirect = /initialTab === "providers" \? "models"/.test(panel);
      return { tabs: tabs.sort(), count: tabs.length, redirectLegacy: redirect,
        hasProvidersTab: tabs.includes("providers") };
    },
    form: (d) => `${d.count} 个页签 / providers=${d.hasProvidersTab ? "独立" : "无"}${d.redirectLegacy ? " / ⚠旧重定向" : ""}` },
  { id: "ui.themeKeys", kind: "ui", label: "主题契约键名（settings 快照侧）",
    probe: (v) => ({
      desktopTheme: v.count(/desktopTheme/g),
      themeMode: v.count(/themeMode/g),
      sessionExperience: v.count(/sessionExperience/g),
    }),
    form: (d) => `desktopTheme=${d.desktopTheme} / themeMode=${d.themeMode} / sessionExperience=${d.sessionExperience}` },
  { id: "ui.providerPresets", kind: "ui", label: "模型服务：供应商预设（providerPresets）",
    probe: (v) => v.count(/providerPresets/g) },
  { id: "ui.providerKinds", kind: "ui", label: "模型服务：自定义供应商类型（providerKinds）",
    probe: (v) => v.count(/providerKinds/g) },
  { id: "ui.modelCatalog", kind: "ui", label: "模型服务：模型目录方法（FetchProviderModelCatalog 等）",
    probe: (v) => new Set([...v.file("lib/bridge.ts").matchAll(/(FetchProviderModelCatalog\w*|TestProviderModel|AddProviderConnection\w*|SetConnectionKey|DeleteProvider)/g)].map((m) => m[1])).size },
  { id: "ui.dockPaneRenderer", kind: "ui", label: "右栏 tabs 渲染者",
    probe: (v) => {
      const hits = v.filesMatching(/workbench-dock__tabs/).filter((f) => f.endsWith(".tsx"));
      const which = hits.some((f) => /app-shell\//.test(f)) ? "app-shell/WorkspaceDockRegion.tsx"
        : hits.some((f) => /Tab(Container|Bar)/.test(f)) ? "TabContainer/TabBar（官方 tab 体系）"
        : hits.some((f) => f === "App.tsx") ? "App.tsx（内联）" : "未知";
      return { which, files: hits };
    },
    form: (d) => d.which },
];

// ── 版本史渲染（--history 与 --doc 共用）──────────────────────────────
function renderHistoryText() {
  const versions = listVersions(upstreamRoot);
  if (versions.length === 0) return "（没有找到已解压的版本目录）";
  const trees = new Map();
  for (const v of versions) trees.set(v, loadTree(resolveVersionDir(upstreamRoot, v)));
  const out = [];
  out.push(`# 官方前端特征版本史（本地 ${versions.length} 个版本：${versions[0]} … ${versions[versions.length - 1]}）`);
  out.push("");
  for (const f of FEATURES) {
    const forms = [];
    for (const v of versions) {
      let d;
      try { d = f.probe(trees.get(v)); } catch { d = null; }
      forms.push(f.form ? f.form(d) : String(d));
    }
    const ranges = [];
    for (let i = 0; i < versions.length; i += 1) {
      const last = ranges[ranges.length - 1];
      if (last && last.form === forms[i]) last.to = versions[i];
      else ranges.push({ from: versions[i], to: versions[i], form: forms[i] });
    }
    const changed = ranges.length > 1;
    const tag = { anchor: "锚点", arch: "契约", ui: "UI" }[f.kind] || f.kind;
    out.push(`${changed ? "◆" : " "} [${tag}] ${f.label}`);
    for (const r of ranges) {
      const span = r.from === r.to ? r.from : `${r.from}..${r.to}`;
      out.push(`      ${span.padEnd(18)} ${r.form}`);
    }
  }
  out.push("");
  out.push("◆ = 该特征在本地版本区间内发生过变化（要跟的重点）；无 ◆ 的表示全程一致。");
  return out.join("\n");
}

// ── --history：控制台版本史 ──────────────────────────────────────────
if (argv.includes("--history")) {
  if (!upstreamRoot) { console.error("需要 --upstream-root"); process.exit(2); }
  console.log(renderHistoryText());
  process.exit(0);
}

// ── 计算 ────────────────────────────────────────────────────────────

/** 探针结果是否"空"（0 / 不存在 / found=false）。 */
function isEmptyDetail(d) {
  if (typeof d === "number") return d === 0;
  if (d && typeof d === "object") {
    if ("found" in d) return !d.found;
    if ("exists" in d) return !d.exists;
    if ("form" in d) return d.form === "无";
    if ("which" in d) return d.which === "未知";
  }
  return false;
}

const rows = [];
for (const f of FEATURES) {
  let bd, td, be = null, te = null;
  try { bd = f.probe(B); } catch (e) { be = String(e.message || e); }
  try { td = f.probe(T); } catch (e) { te = String(e.message || e); }
  const bForm = f.form ? f.form(bd) : String(bd);
  const tForm = f.form ? f.form(td) : String(td);
  const same = JSON.stringify(bd) === JSON.stringify(td);
  let status = same ? "相同" : "变化";

  if (f.kind === "anchor") {
    const emptyB = isEmptyDetail(bd);
    const emptyT = isEmptyDetail(td);
    if (emptyT && !emptyB) status = "★破坏";       // 基线有、目标没有 → 我们的注入会碎
    else if (emptyT && emptyB) status = "未测到";    // 两边都没有 → 探针没覆盖到，不算破坏
  }
  if (f.kind === "arch" && /★/.test(tForm) && !/★/.test(bForm)) status = "★需评估";

  rows.push({ id: f.id, kind: f.kind, label: f.label, base: bForm, target: tForm, status,
    baseDetail: bd, targetDetail: td, err: be || te });
}

const counts = rows.reduce((acc, r) => { acc[r.status] = (acc[r.status] || 0) + 1; return acc; }, {});
const breaking = rows.filter((r) => r.status === "★破坏" || r.status === "★需评估");

// ── 输出 ────────────────────────────────────────────────────────────
const label = (dir) => {
  const m = /src(\d+\.\d+\.\d+)$/.exec(path.dirname(path.dirname(path.dirname(dir))));
  return m ? `v${m[1]}` : dir;
};
const baseLabel = path.basename(path.resolve(baselineDir, "..", ".."));
const targetLabel = path.basename(path.resolve(targetDir, "..", ".."));

const lines = [];
lines.push(`# 官方前端 UI / 契约漂移报告`);
lines.push("");
lines.push(`- 基线: ${baselineDir}`);
lines.push(`- 目标: ${targetDir}`);
lines.push(`- 结论: ${rows.length} 项观察点，${counts["相同"] || 0} 项相同、${(counts["变化"] || 0)} 项变化、` +
  `${(counts["★破坏"] || 0)} 项破坏性（锚点消失）、${(counts["★需评估"] || 0)} 项需评估（契约/架构）`);
lines.push("");
const groups = [
  ["anchor", "一、本项目注入/品牌依赖的锚点（缺一即碎）"],
  ["arch", "二、宿主契约与调用模式（决定升级工作量）"],
  ["ui", "三、用户可见 UI 形态"],
];
for (const [kind, title] of groups) {
  lines.push(`## ${title}`);
  lines.push("");
  lines.push("| 项 | 基线 | 目标 | 结论 |");
  lines.push("|---|---|---|---|");
  for (const r of rows.filter((x) => x.kind === kind)) {
    lines.push(`| ${r.label} | ${String(r.base).replace(/\|/g, "\\|")} | ${String(r.target).replace(/\|/g, "\\|")} | ${r.status} |`);
  }
  lines.push("");
}
if (breaking.length) {
  lines.push("## 需要人工评估的项");
  lines.push("");
  for (const r of breaking) lines.push(`- **${r.label}**：${r.base} → ${r.target}`);
  lines.push("");
}
lines.push("## 用法");
lines.push("");
lines.push("```powershell");
lines.push("# 列出本地已解压的官方版本");
lines.push("node scripts/ui-drift-report.js --upstream-root <目录> --list-versions");
lines.push("# 对照我们挂载的版本与新版本");
lines.push("node scripts/ui-drift-report.js --upstream-root <目录> --baseline-version 1.38.2 --version 1.38.10 --md docs/drift-1.38.10.md");
lines.push("```");
const md = lines.join("\n");
console.log(md);

if (mdOut) { fs.mkdirSync(path.dirname(mdOut), { recursive: true }); fs.writeFileSync(mdOut, md + "\n"); console.log(`\n已写入 ${mdOut}`); }
if (jsonOut) { fs.writeFileSync(jsonOut, JSON.stringify({ baselineDir, targetDir, rows, counts }, null, 2)); console.log(`已写入 ${jsonOut}`); }

// --doc：把"版本史 + 本次对照"合成一份可入库的报告（每次官方发版后一条命令产出）
const docOut = argValue("--doc", "");
if (docOut) {
  const doc = [
    "# 官方 Reasonix 桌面端 UI / 契约漂移对照（自动生成）",
    "",
    "> 由 `scripts/ui-drift-report.js` 生成，**每次官方发版后跑一次**：",
    "> `node scripts/ui-drift-report.js --upstream-root <目录> --history` 看版本史；",
    "> `node scripts/ui-drift-report.js --upstream-root <目录> --baseline-version <我们挂载的版本> --version <新版本> --doc docs/upstream-ui-drift.md` 出本报告。",
    "",
    "---",
    "",
    "## 一、跨版本特征史",
    "",
    "```",
    renderHistoryText(),
    "```",
    "",
    "---",
    "",
    "## 二、本次对照（基线 → 目标）",
    "",
    md,
    "",
  ].join("\n");
  fs.mkdirSync(path.dirname(docOut), { recursive: true });
  fs.writeFileSync(docOut, doc);
  console.log(`\n已写入 ${docOut}`);
}

process.exit(breaking.length ? 1 : 0);
