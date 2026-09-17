#!/usr/bin/env node
/*
 * sync-upstream-dist.js — 用上游构建产物替换 frontend/dist，并重放本项目的品牌改造与全部注入。
 *
 * 背景：本项目不携带上游前端源码，只消费上游构建产物（dist）。升级流程必须是可重放的：
 *   1) 在上游检出里构建前端（pnpm install && npx vite build）
 *   2) 用本脚本把产物同步进 frontend/dist
 *   3) 品牌改造 + 全部注入自动重放（PRINCIPLES 原则 6）
 *
 * 用法：
 *   node scripts/sync-upstream-dist.js <upstream-dist-dir> [--dry]
 */
"use strict";

const fs = require("fs");
const path = require("path");
const { execFileSync } = require("child_process");

const root = path.resolve(__dirname, "..");
const args = process.argv.slice(2);
const dry = args.includes("--dry");
const srcArg = args.find((a) => !a.startsWith("--"));

if (!srcArg) {
  console.error("usage: node scripts/sync-upstream-dist.js <upstream-dist-dir> [--dry]");
  process.exit(2);
}

const src = path.resolve(srcArg);
const dest = path.join(root, "frontend", "dist");

if (!fs.existsSync(path.join(src, "index.html"))) {
  console.error(`[sync] 上游产物缺少 index.html: ${src}`);
  console.error("[sync] 先在上游检出里跑 pnpm install && npx vite build");
  process.exit(1);
}

// 前置校验：产物必须仍是「Wails 宿主 + boot-shell」形态，否则本项目的桥与品牌锚点不成立
const upstreamHtml = fs.readFileSync(path.join(src, "index.html"), "utf8");
const preflight = [
  ["boot-shell 启动壳", upstreamHtml.includes("boot-shell")],
  ["外部 module bundle", /<script type="module" src="\.\/assets\/[^"]+"><\/script>/.test(upstreamHtml)],
  ["wails-spinner 锚点", upstreamHtml.includes("wails-spinner")],
];
let bad = 0;
for (const [name, ok] of preflight) {
  console.log(`[sync] ${ok ? "ok  " : "WARN"} ${name}`);
  if (!ok) bad += 1;
}
if (bad) {
  console.error("[sync] 上游产物形态与预期不符，需先人工评估（合同/品牌锚点可能已变）");
  process.exit(1);
}

const stat = (d) => ({
  files: fs.readdirSync(d, { withFileTypes: true }).reduce((n, e) => n + (e.isDirectory() ? stat(path.join(d, e.name)).files : 1), 0),
  bytes: (function walk(x) {
    let t = 0;
    for (const e of fs.readdirSync(x, { withFileTypes: true })) {
      const p = path.join(x, e.name);
      t += e.isDirectory() ? walk(p) : fs.statSync(p).size;
    }
    return t;
  })(d),
});

const beforeStat = stat(dest);
const afterStat = stat(src);
console.log(`[sync] ${path.relative(root, dest)}: ${beforeStat.files} 文件 / ${(beforeStat.bytes / 1024 / 1024).toFixed(1)}MB`);
console.log(`[sync] 上游产物: ${afterStat.files} 文件 / ${(afterStat.bytes / 1024 / 1024).toFixed(1)}MB`);

if (dry) {
  console.log("[sync] --dry 模式，未改动任何文件");
  process.exit(0);
}

// 替换 dist 内容（保留 .git 无关；dist 由 git 跟踪，可回滚）
fs.rmSync(dest, { recursive: true, force: true });
fs.mkdirSync(dest, { recursive: true });
fs.cpSync(src, dest, { recursive: true });
console.log("[sync] 上游产物已复制到 frontend/dist");

// 重放品牌改造 + 全部注入
const steps = ["apply-branding.js", "apply-all-injections.js"];
for (const s of steps) {
  console.log(`[sync] == node scripts/${s} ==`);
  try {
    const out = execFileSync(process.execPath, [path.join(root, "scripts", s)], { encoding: "utf8" });
    process.stdout.write(out);
  } catch (err) {
    process.stdout.write(err.stdout || "");
    process.stderr.write(err.stderr || "");
    console.error(`[sync] ${s} 失败，dist 处于未完成状态（git checkout -- frontend/dist 可回滚）`);
    process.exit(1);
  }
}

console.log("[sync] 完成：dist 已同步并重放品牌+注入");
