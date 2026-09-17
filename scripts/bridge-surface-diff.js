#!/usr/bin/env node
/*
 * bridge-surface-diff.js — 对照两版桌面前端 lib/bridge.ts 的「宿主方法面」。
 *
 * 背景：v1.31.4 与 1.38.2 的 bridge.ts 都是「运行时解析 window.go.main.App」的
 * React-to-Go 契约模式（不是 1.38.8 的 window.reasonixDesktop + generated 契约）。
 * 所以升级评估的关键问题是：1.38.2 的界面比我们多调了哪些宿主方法。
 *
 * 用法：
 *   node scripts/bridge-surface-diff.js <base-bridge.ts> <target-bridge.ts> <repo-root>
 *
 * 输出：方法面差集 + 与我们 Go 桥导出方法的缺口/独有。
 */
"use strict";

const fs = require("fs");
const path = require("path");

const [, , basePath, targetPath, repoRoot] = process.argv;
if (!basePath || !targetPath) {
  console.error("usage: node scripts/bridge-surface-diff.js <base.ts> <target.ts> [repo-root]");
  process.exit(2);
}

/** 从 bridge.ts 里提取接口方法名（形如 `  MethodName(...)` 的行）。 */
function extractMethods(file) {
  const src = fs.readFileSync(file, "utf8");
  const out = new Set();
  // 只取「接口成员」形态：行首 2 空格缩进 + PascalCase 标识符 + 左括号。
  const re = /^\s{1,4}([A-Z][A-Za-z0-9]{2,60})\s*\(/gm;
  let m;
  while ((m = re.exec(src)) !== null) out.add(m[1]);
  return out;
}

/** 从 Go 源里提取 `func (a *App) Method(` 的方法名。 */
function extractGoMethods(root) {
  const out = new Set();
  if (!root) return out;
  const files = fs.readdirSync(root).filter((f) => f.endsWith(".go") && !f.endsWith("_test.go"));
  for (const f of files) {
    const src = fs.readFileSync(path.join(root, f), "utf8");
    const re = /^func \(a \*App\) ([A-Za-z][A-Za-z0-9]*)\s*\(/gm;
    let m;
    while ((m = re.exec(src)) !== null) out.add(m[1]);
  }
  return out;
}

const base = extractMethods(basePath);
const target = extractMethods(targetPath);
const go = extractGoMethods(repoRoot);

const lower = (s) => s.toLowerCase();
const goLower = new Set([...go].map(lower));

const added = [...target].filter((n) => !base.has(n)).sort();
const removed = [...base].filter((n) => !target.has(n)).sort();
const missing = [...target].filter((n) => !goLower.has(lower(n))).sort();

console.log("== bridge.ts 方法面 ==");
console.log(`  base   (${path.basename(path.dirname(path.dirname(path.dirname(basePath))))}): ${base.size}`);
console.log(`  target (${path.basename(path.dirname(path.dirname(path.dirname(targetPath))))}): ${target.size}`);
console.log("");
console.log(`== target 新增 (${added.length}) ==`);
for (const n of added) console.log(`  + ${n}${goLower.has(lower(n)) ? "   [桥已有]" : "   [桥缺失]"}`);
console.log("");
console.log(`== target 移除 (${removed.length}) ==`);
for (const n of removed) console.log(`  - ${n}`);
console.log("");
if (repoRoot) {
  console.log(`== Go 桥导出方法: ${go.size} ==`);
  console.log(`== target 期望但我们没有: ${missing.length} ==`);
  for (const n of missing) console.log(`  ! ${n}`);
}
