#!/usr/bin/env node
/*
 * apply-monaco-vendor.js — 把本项目 vendor 的 Monaco（VS Code 内核）复制进 frontend/dist。
 *
 * 背景（2026-09-18 实测回归）：我们就地编辑器会优先用 Monaco（assets/monaco/vs 离线内置），
 * 失败才回退 textarea。但**上游两个版本都不依赖 monaco**（package.json 里没有），
 * 这 143 个文件是本项目自己 vendor 的 —— 升级换 dist 时被整体冲掉，于是 Monaco 加载 404、
 * 回退到 textarea；而回退用的 textarea 又被放进 display:block 的宿主里、flex:1 失效，
 * 只剩 ~70px 高 —— 用户看到的现象就是"点编辑后编辑窗口只剩上面一部分"。
 *
 * 所以这里把 Monaco 提升为**项目自有资产**并纳入可重放构建步骤（与 branding / 注入同级）：
 *   third_party/monaco/vs/**  →  frontend/dist/assets/monaco/vs/**
 *
 * ⚠ 目录名**不能**叫 `vendor/`：Go 会把模块根下的 vendor 当成依赖目录，导致
 * `inconsistent vendoring` 构建失败（实测踩过）。故用 `third_party/`。
 *
 * 幂等：逐文件比对大小，全部一致则跳过复制（[noop]）。
 * 失败即停：vendor 缺失或缺 loader.js 直接报错（宁可构建失败，也不产出没有编辑器的包）。
 *
 * 用法：node scripts/apply-monaco-vendor.js [dist-dir]
 */
"use strict";

const fs = require("fs");
const path = require("path");

const root = path.resolve(__dirname, "..");
const distArg = process.argv.slice(2).find((a) => !a.startsWith("--"));
const distDir = distArg ? path.resolve(distArg) : path.join(root, "frontend", "dist");

const srcDir = path.join(root, "third_party", "monaco", "vs");
const destDir = path.join(distDir, "assets", "monaco", "vs");

if (!fs.existsSync(path.join(srcDir, "loader.js"))) {
  console.error(`[monaco] 资产缺失或损坏：找不到 ${path.relative(root, path.join(srcDir, "loader.js"))}`);
  console.error("[monaco] 该目录是项目自有资产（上游不提供），不能删；从 git 历史恢复：");
  console.error('[monaco]   git archive --format=tar -o m.tar <含它的提交> frontend/dist/assets/monaco');
  console.error("[monaco]   tar -xf m.tar && 把 frontend/dist/assets/monaco 移到 third_party/monaco");
  process.exit(1);
}
if (!fs.existsSync(distDir)) {
  console.error(`[monaco] 找不到目标 dist：${distDir}`);
  process.exit(1);
}

/** 递归列文件（相对路径 → 大小）。 */
function listFiles(base) {
  const out = new Map();
  const walk = (dir) => {
    for (const e of fs.readdirSync(dir, { withFileTypes: true })) {
      const p = path.join(dir, e.name);
      if (e.isDirectory()) walk(p);
      else out.set(path.relative(base, p).split(path.sep).join("/"), fs.statSync(p).size);
    }
  };
  walk(base);
  return out;
}

const srcFiles = listFiles(srcDir);
let same = 0;
let copied = 0;
let created = 0;

for (const [rel, size] of srcFiles) {
  const from = path.join(srcDir, rel);
  const to = path.join(destDir, rel);
  const exists = fs.existsSync(to);
  if (exists && fs.statSync(to).size === size) {
    same += 1;
    continue;
  }
  fs.mkdirSync(path.dirname(to), { recursive: true });
  fs.copyFileSync(from, to);
  if (exists) copied += 1;
  else created += 1;
}

console.log(`[monaco] target: ${path.relative(root, destDir)}`);
console.log(`[monaco] 资产 ${srcFiles.size} 个文件：新增 ${created} / 覆盖 ${copied} / 一致 ${same}`);

// 目标状态断言：这三件在位，Monaco 才能被 loader 拉起（缺任一个 → 编辑器退回小 textarea）
const must = ["loader.js", "editor/editor.main.js", "editor/editor.main.css"];
let bad = 0;
for (const rel of must) {
  const ok = fs.existsSync(path.join(destDir, rel));
  if (!ok) bad += 1;
  console.log(`  ${ok ? "ok  " : "FAIL"} ${rel}`);
}
if (bad) {
  console.error(`[monaco] 关键文件缺失（${bad} 个），编辑器的 Monaco 路径会失效`);
  process.exit(1);
}
console.log(copied + created === 0 ? "  [noop] 已是目标状态（幂等）" : "  [write] 已同步 Monaco 资源");
process.exit(0);
