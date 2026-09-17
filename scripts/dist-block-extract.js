#!/usr/bin/env node
/*
 * dist-block-extract.js — 从 frontend/dist/index.html 抽出内联自定义 <script> 块。
 *
 * 用途：升级上游 dist 前，把只存在于 dist 里的本项目注入反向提取出来，
 * 使每个注入都有可重放的源文件（PRINCIPLES 原则 6 类别 5）。
 *
 * 用法：
 *   node scripts/dist-block-extract.js list                 # 列内联块
 *   node scripts/dist-block-extract.js dump <n> [outfile]   # 导出第 n 块
 *   node scripts/dist-block-extract.js find <regex>         # 按内容找块
 *   node scripts/dist-block-extract.js promote <n> <script-path> [--slice <regex>]
 *                                                          # 把块内容反向提升为可重放源脚本
 *                                                          # （语法门禁：不通过则拒绝写入）
 */
"use strict";

const fs = require("fs");
const path = require("path");

const root = path.resolve(__dirname, "..");
const htmlPath = path.join(root, "frontend", "dist", "index.html");
const html = fs.readFileSync(htmlPath, "utf8");

function blocks() {
  const re = /<script\b([^>]*)>([\s\S]*?)<\/script>/g;
  const out = [];
  let m;
  while ((m = re.exec(html)) !== null) {
    out.push({ attr: m[1].trim(), body: m[2], start: m.index });
  }
  return out;
}

const [cmd, arg, out] = process.argv.slice(2);
const bs = blocks();

if (cmd === "list" || !cmd) {
  bs.forEach((b, i) => {
    const marks = [...new Set(b.body.match(/__DSH_[A-Z_]+/g) || [])];
    const head = b.body.trim().split("\n")[0].trim().slice(0, 100);
    console.log(`#${i + 1}  len=${String(b.body.length).padStart(7)}  ${marks.join(",") || "-"}`);
    console.log(`     ${head}`);
  });
  console.log(`\ntotal blocks: ${bs.length}`);
} else if (cmd === "dump") {
  const i = Number(arg);
  if (!Number.isInteger(i) || i < 1 || i > bs.length) {
    console.error(`bad block index ${arg} (1..${bs.length})`);
    process.exit(2);
  }
  const b = bs[i - 1];
  const target = out || path.join(root, "docs", `dist-block-${i}.js`);
  fs.mkdirSync(path.dirname(target), { recursive: true });
  fs.writeFileSync(target, b.body.replace(/^\n/, ""));
  console.log(`dumped block #${i} (${b.body.length} bytes) -> ${path.relative(root, target)}`);
} else if (cmd === "promote") {
  // 把 dist 内联块提升为 scripts/ 下的源脚本（可重放），带语法门禁。
  const i = Number(arg);
  const targetArg = process.argv[4];
  const sliceIdx = process.argv.indexOf("--slice");
  const sliceRe = sliceIdx >= 0 ? new RegExp(process.argv[sliceIdx + 1]) : null;
  if (!Number.isInteger(i) || i < 1 || i > bs.length || !targetArg) {
    console.error("usage: dist-block-extract.js promote <n> <script-path> [--slice <regex>]");
    process.exit(2);
  }
  let body = bs[i - 1].body.replace(/^\r?\n/, "").trimEnd() + "\n";
  if (sliceRe) {
    const at = body.search(sliceRe);
    if (at < 0) {
      console.error(`[promote] --slice 未匹配到内容，拒绝写入`);
      process.exit(1);
    }
    body = body.slice(at);
  }
  // 语法门禁：经典脚本用 new Function 解析（能识破块 #3 那类 export 残片）。
  try {
    new Function(body);
  } catch (err) {
    console.error(`[promote] 语法门禁未通过，拒绝写入：${err.message}`);
    console.error(`[promote] 该块很可能是上游 bundle 残片/错位补丁，需人工改写而不是原样提升。`);
    process.exit(1);
  }
  const target = path.resolve(root, targetArg);
  fs.mkdirSync(path.dirname(target), { recursive: true });
  fs.writeFileSync(target, body);
  console.log(`promoted block #${i} (${body.length} bytes) -> ${path.relative(root, target)}`);
  console.log(`syntax gate: OK`);
} else if (cmd === "find") {
  const re = new RegExp(arg, "m");
  let hits = 0;
  bs.forEach((b, i) => {
    if (re.test(b.body)) {
      hits += 1;
      const head = b.body.trim().split("\n")[0].trim().slice(0, 100);
      console.log(`#${i + 1}  len=${b.body.length}  ${head}`);
    }
  });
  console.log(`matches: ${hits}`);
} else {
  console.error("usage: dist-block-extract.js list|dump <n> [out]|find <regex>|promote <n> <path> [--slice <regex>]");
  process.exit(2);
}
