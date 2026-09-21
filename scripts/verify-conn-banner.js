#!/usr/bin/env node
/**
 * verify-conn-banner.js — 连接横幅（scripts/dsh-conn-banner.js → 注入 dist）的关键不变量检查。
 *
 * 为什么需要：横幅"连上后不消失"这个 bug 在构建期**完全看不出来**（语法正确、注入成功），
 * 只在真机上表现为一直挂着一块误导性的提示。2026-09-21 实测：DshConnStatus() 返回
 * connected:true 时 #dsh-conn-banner 仍是 class=show / display:block / opacity:1。
 * 根因是 check() 只有 showBanner 没有 hideBanner、且只在启动 4s 后查一次（没有轮询）。
 *
 * 用法：node scripts/verify-conn-banner.js [repoRoot]
 * 退出码：0 = 不变量齐备；1 = 缺失（会被 build-*.ps1 之前的检查发现）。
 */
"use strict";
const fs = require("fs");
const path = require("path");

const root = process.argv[2] || path.join(__dirname, "..");
const scriptPath = path.join(root, "scripts", "dsh-conn-banner.js");
const distIndex = path.join(root, "frontend", "dist", "index.html");

const rawSrc = fs.readFileSync(scriptPath, "utf8");
// 先去掉注释再做模式匹配：否则"if (connected) { …注释… hideBanner()"这类会被字符数窗口卡掉
// （注释写得越详细越容易误报 —— 本检查自己就踩过一次）。
const src = rawSrc
  .replace(/\/\*[\s\S]*?\*\//g, "")
  .replace(/(^|[^:])\/\/[^\n]*/g, "$1");
const required = [
  ["hideBanner 定义", /function\s+hideBanner\s*\(/],
  ["连上时调用 hideBanner", /if\s*\(\s*connected\s*\)\s*\{[\s\S]{0,400}?hideBanner\s*\(/],
  ["周期复检（schedule）", /function\s+schedule\s*\(ms\)/],
  ["未连接轮询间隔", /POLL_DISCONNECTED_MS\s*=\s*\d+/],
  ["已连接轮询间隔", /POLL_CONNECTED_MS\s*=\s*\d+/],
  ["桥调用超时竞速（防永不 settle 拖死轮询）", /function\s+withTimeout\s*\(/],
  ["诊断出口 window.__dshBanner", /window\.__dshBanner\s*=/],
  ["聚焦/可见性时复检", /visibilitychange/],
  // 「断开 → 连上」自动重载：否则"应用先起、后端后起"时项目树会一直停在空态
  // （真机实测：DSH 里 12 个项目 / 39 个会话，界面显示「还没有项目」，刷新后才恢复）。
  ["跃迁识别 wasConnected", /wasConnected\s*===\s*false\s*&&\s*connected/],
  ["跃迁时自动重载", /function\s+reloadForReconnect\s*\(/],
  ["重载冷却（防抖动反复刷新）", /RELOAD_AT_KEY/],
];

let failed = 0;
console.log("检查 scripts/dsh-conn-banner.js：");
for (const [name, re] of required) {
  const ok = re.test(src);
  if (!ok) failed++;
  console.log(`  ${ok ? "ok  " : "缺失"} ${name}`);
}

// 注入产物侧：dist 里必须恰好有 1 份带修复标记的横幅脚本
if (fs.existsSync(distIndex)) {
  const html = fs.readFileSync(distIndex, "utf8");
  const count = (s) => html.split(s).length - 1;
  const scripts = (html.match(/<script\b[^>]*>[\s\S]*?<\/script>/g) || []).filter((b) => b.includes("dsh-conn-start"));
  const fixedCopies = scripts.filter((b) => b.includes("POLL_DISCONNECTED_MS")).length;
  console.log("\n检查注入产物 frontend/dist/index.html：");
  console.log(`  ${scripts.length === 1 ? "ok  " : "异常"} 横幅脚本份数 = ${scripts.length}（期望 1）`);
  console.log(`  ${fixedCopies === 1 ? "ok  " : "异常"} 含修复标记的份数 = ${fixedCopies}（期望 1）`);
  console.log(`  ${count("POLL_DISCONNECTED_MS") >= 1 ? "ok  " : "缺失"} 轮询标记出现 ${count("POLL_DISCONNECTED_MS")} 次`);
  if (scripts.length !== 1 || fixedCopies !== 1 || count("POLL_DISCONNECTED_MS") < 1) failed++;
} else {
  console.log("\n（未找到 frontend/dist/index.html，跳过注入产物检查）");
}

console.log(`\n结论：${failed ? "存在缺失项（横幅可能再次出现“连上不消失”）" : "不变量齐备"}`);
process.exit(failed ? 1 : 0);
