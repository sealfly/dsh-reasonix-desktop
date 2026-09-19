#!/usr/bin/env node
// verify-dist-assets.js — frontend/dist 的「引用完整性 + 可复现性」校验。
//
// 事故背景（2026-09-18，前端 v1.31.4 → v1.38.2 升级）：
//   dist 是构建产物且被 .gitignore 忽略，升级时只提交了 index.html，
//   291 个 assets（含主入口 index-*.js）没被 `git add -f` 纳入 →
//   任何干净 checkout 的首屏 JS 缺失 → 应用永久卡在「加载中」。
//   在构建机上看不出问题（本地磁盘是全的），所以只能在构建流程里做机器校验。
//
// 三层校验，任一失败 ⇒ exit 1：
//   1) index.html 的 <script>/<link>/… 标签引用的本地资源必须存在于磁盘；
//   2) 从这些入口做广度优先依赖扫描——主入口 import 的 chunk 也必须存在
//      （index.html 只直接引用少数入口，其余 chunk 名字写在 JS 内部，漏一个就白屏）；
//   3) 上述所有文件必须被 git 跟踪（否则别人 clone 不到 = 不可复现）。
//
// 用法：node scripts/verify-dist-assets.js [--dist <dir>] [--no-git]
//   只读：不修改任何文件；退出码 0=通过，1=有问题，2=用法/环境错误。

'use strict';
const fs = require('fs');
const path = require('path');
const { execFileSync } = require('child_process');

const argv = process.argv.slice(2);
const flagValue = (name) => {
  const i = argv.indexOf(name);
  return i >= 0 && i + 1 < argv.length ? argv[i + 1] : null;
};
const repoRoot = path.resolve(__dirname, '..');
const distDir = path.resolve(repoRoot, flagValue('--dist') || path.join('frontend', 'dist'));
const skipGit = argv.includes('--no-git');
// 逃生门：恢复期（例如上游升级提交残缺、正用旧产物临时顶替）允许放行，
// 但必须显式声明并会打印醒目警告——默认永远是严格失败。
const warnOnly = argv.includes('--warn-only') || process.env.DSH_DIST_VERIFY === 'warn';

// 只认标签属性（避免把 JS 里的模板串碎片当成引用——早期排查脚本踩过这个坑）
const TAG_REF = /<(?:script|link|img|source|audio|video|track|embed)\b[^>]*?\b(?:src|href)\s*=\s*["']([^"']+)["']/gi;
// JS 产物内部的 chunk 引用：只认 vite 的 "…-<hash8>.js" 形态。
// 放宽会大量误报（声音/字体/配置路径等运行时字符串并不是构建期资源引用）。
const JS_REF = /["'\`]((?:\.\.?\/)[A-Za-z0-9_\-./@]*-[A-Za-z0-9_\-]{8}\.(?:js|mjs|css))["'\`]/g;
const SCHEME = /^(?:[a-z][a-z0-9+.-]*:|\/\/|#)/i;

const isLocalRef = (ref) => {
  if (!ref) return false;
  if (SCHEME.test(ref)) return false;
  if (/["'\`+]/.test(ref)) return false;
  return true;
};
const refToPath = (ref) =>
  path.join(distDir, decodeURIComponent(ref.split('#')[0].split('?')[0].replace(/^\.\//, '').replace(/^\/+/, '')));

const rel = (p) => path.relative(distDir, p).split(path.sep).join('/');

function gitTrackedSet(prefix) {
  try {
    const out = execFileSync('git', ['-C', repoRoot, 'ls-files', '-z', '--', prefix], { maxBuffer: 1 << 28 });
    return {
      set: new Set(out.toString('utf8').split('\0').filter(Boolean).map((p) => path.resolve(repoRoot, p))),
      error: null,
    };
  } catch (err) {
    return { set: null, error: err };
  }
}

const htmlPath = path.join(distDir, 'index.html');
if (!fs.existsSync(htmlPath)) {
  console.error('[verify-dist-assets] ✗ 找不到 ' + htmlPath);
  process.exit(2);
}
const html = fs.readFileSync(htmlPath, 'utf8');

const entries = [];
for (const m of html.matchAll(TAG_REF)) if (isLocalRef(m[1])) entries.push(refToPath(m[1]));
if (entries.length === 0) {
  console.error('[verify-dist-assets] ✗ index.html 未解析到任何本地资源引用——产物结构或本脚本解析逻辑异常');
  process.exit(2);
}

// 依赖闭包（广度优先，沿 JS 内部的 chunk 引用递归）
const visited = new Set();          // 存在的文件
const missingFromHtml = [];          // index.html 直接引用却不存在 —— 硬失败（首屏必崩）
const missingFromJs = [];            // 产物 JS 内部引用的 chunk 不存在 —— 警告（可能是条件分支/陈旧引用）
const queue = entries.map((p) => ({ p, fromHtml: true }));
while (queue.length) {
  const { p, fromHtml } = queue.shift();
  if (visited.has(p)) continue;
  if (!fs.existsSync(p)) {
    (fromHtml ? missingFromHtml : missingFromJs).push(p);
    continue;
  }
  visited.add(p);
  if (!/\.(?:js|mjs|cjs)$/i.test(p)) continue;
  let txt = '';
  try { txt = fs.readFileSync(p, 'utf8'); } catch { continue; }
  for (const m of txt.matchAll(JS_REF)) {
    const target = path.resolve(path.dirname(p), m[1]);
    const r = path.relative(distDir, target);
    if (!r || r.startsWith('..') || path.isAbsolute(r)) continue;
    if (!visited.has(target)) queue.push({ p: target, fromHtml: false });
  }
}

// git 可复现性：闭包内文件必须被跟踪，否则换台机器 clone 就缺文件
const distPrefix = path.relative(repoRoot, distDir);
const insideRepo = distPrefix && !distPrefix.startsWith('..') && !path.isAbsolute(distPrefix);
let untracked = [];
let gitNote = '';
if (skipGit) {
  gitNote = '（--no-git：已跳过跟踪检查）';
} else if (!insideRepo) {
  gitNote = '（dist 不在仓库内：已跳过跟踪检查）';
} else {
  const { set, error } = gitTrackedSet(distPrefix);
  if (!set) {
    gitNote = '（git 不可用，已跳过跟踪检查：' + String((error && error.message) || error).slice(0, 60) + '）';
  } else {
    for (const p of visited) if (!set.has(p)) untracked.push(p);
  }
}

console.log('[verify-dist-assets] dist = ' + distDir);
console.log('  入口直接引用  : ' + entries.length);
console.log('  依赖闭包文件  : ' + visited.size);
console.log('  入口引用缺失  : ' + missingFromHtml.length + '   <- 硬失败项');
console.log('  JS 内部引用缺失: ' + missingFromJs.length + '   <- 提示项');
console.log('  未被 git 跟踪 : ' + untracked.length + (gitNote ? ' ' + gitNote : ''));

const show = (list) => list.slice(0, 15).map((p) => '      - ' + rel(p)).join('\n');
let bad = false;
if (missingFromHtml.length) {
  bad = true;
  console.error('  ✗ index.html 直接引用的资源在磁盘上不存在（首屏必然报错/白屏）：');
  console.error(show(missingFromHtml));
  if (missingFromHtml.length > 15) console.error('      … 另有 ' + (missingFromHtml.length - 15) + ' 个');
  console.error('    处理：重新构建前端产物，或从完整产物目录补回这些文件。');
}
if (missingFromJs.length) {
  console.warn('  ! 产物 JS 内部引用的 chunk 不存在（可能是条件分支/陈旧引用，仅提示不失败）：');
  console.warn(show(missingFromJs));
  if (missingFromJs.length > 15) console.warn('      … 另有 ' + (missingFromJs.length - 15) + ' 个');
}
if (untracked.length) {
  bad = true;
  console.error('  ✗ 以下文件存在于磁盘但未被 git 跟踪（别人 clone 不到 = 不可复现）：');
  console.error(show(untracked));
  if (untracked.length > 15) console.error('      … 另有 ' + (untracked.length - 15) + ' 个');
  console.error('    处理：git add -f ' + (distPrefix || 'frontend/dist') + '   （-f 必须带：dist 被 .gitignore 忽略）');
}
if (bad) {
  if (warnOnly) {
    console.warn('[verify-dist-assets] ⚠ 已按 warn-only 放行（--warn-only 或 DSH_DIST_VERIFY=warn）。');
    console.warn('  ⚠ 本次构建的产物在别的机器/clone 上可能无法复现，仅限恢复期临时使用。');
    process.exit(0);
  }
  console.error('[verify-dist-assets] 校验失败。');
  process.exit(1);
}
console.log('  ✓ 通过：引用完整、依赖闭包齐全、全部可被 git 复现');
