#!/usr/bin/env node
/*
 * apply-branding.js — 把 branding/ 下的品牌资产幂等地写进 frontend/dist。
 *
 * 背景：前端 dist 是构建产物，上游升级会整体替换。品牌改造（DSH-Reasonix 名称、
 * 启动壳 logo、深色主题橙色 logo、启动光晕）必须是可重放脚本步骤，
 * 否则每次升级都会丢（PRINCIPLES 原则 6）。
 *
 * 幂等性：每个改动都以「目标状态断言」实现——已经是目标状态就跳过，跑 N 次结果一致。
 *
 * 用法：
 *   node scripts/apply-branding.js [dist-dir]
 */
"use strict";

const fs = require("fs");
const path = require("path");

const root = path.resolve(__dirname, "..");
const distDir = process.argv[2] ? path.resolve(process.argv[2]) : path.join(root, "frontend", "dist");
const brandDir = path.join(root, "branding");
const htmlPath = path.join(distDir, "index.html");

if (!fs.existsSync(htmlPath)) {
  console.error(`[branding] 找不到 ${htmlPath}`);
  process.exit(1);
}

const facts = JSON.parse(fs.readFileSync(path.join(brandDir, "brand.json"), "utf8"));
const bootPng = fs.readFileSync(path.join(brandDir, "boot-mark.png"));
const bootUri = "data:image/png;base64," + bootPng.toString("base64");
const brandCss = fs.readFileSync(path.join(brandDir, "brand.css"), "utf8").trim();
const headHook = fs.readFileSync(path.join(brandDir, "head-error-hook.js"), "utf8").trim();

let html = fs.readFileSync(htmlPath, "utf8");
const before = html;
const log = [];

// 1. boot-shell aria-label
const ariaTarget = `class="boot-shell" role="status" aria-label="${facts.bootAria}"`;
const ariaAny = /class="boot-shell" role="status" aria-label="[^"]*"/;
if (ariaAny.test(html)) {
  html = html.replace(ariaAny, ariaTarget);
  log.push(`aria-label -> "${facts.bootAria}"`);
} else {
  log.push("[warn] boot-shell aria-label 未匹配");
}

// 2. boot-shell 品牌名
const nameAny = /(<div class="boot-shell__name">)[^<]*(<\/div>)/;
if (nameAny.test(html)) {
  html = html.replace(nameAny, `$1${facts.bootName}$2`);
  log.push(`boot-shell__name -> "${facts.bootName}"`);
} else {
  log.push("[warn] boot-shell__name 未匹配");
}

// 3. boot-shell mark 位图（覆盖上游 svg data URI）
const markAny = /(<div class="boot-shell__mark"[^>]*>\s*<img src=")[^"]*(")/;
if (markAny.test(html)) {
  html = html.replace(markAny, `$1${bootUri}$2`);
  log.push(`boot-shell__mark -> PNG (${bootPng.length} bytes)`);
} else {
  log.push("[warn] boot-shell__mark 未匹配");
}

// 4. 品牌 CSS：写进 head 的 <style> 块（用唯一哨兵标记包裹，保证幂等）
//    注意：不能用 brand.css 内部的 "/* local brand:" 做标记——它自身出现 3 次，
//    lastIndexOf 会命中块内最后一条注释，导致替换区间算错、重复注入。
const cssStartMark = "/* dsh-brand:start */";
const cssEndMark = "/* dsh-brand:end */";
const cssBlock = `    ${cssStartMark}\n${brandCss}\n    ${cssEndMark}`;
const styleEnd = html.indexOf("</style>");
if (styleEnd > 0) {
  const s = html.indexOf(cssStartMark);
  const e = html.indexOf(cssEndMark);
  if (s >= 0 && e > s) {
    html = html.slice(0, s) + cssBlock.replace(/^\s+/, "") + html.slice(e + cssEndMark.length);
    log.push(`brand css 已替换 (${brandCss.length} bytes)`);
  } else {
    html = html.slice(0, styleEnd) + cssBlock + "\n    " + html.slice(styleEnd);
    log.push(`brand css 已注入 (${brandCss.length} bytes)`);
  }
} else {
  log.push("[warn] 找不到 </style>，品牌 CSS 未注入");
}

// 5. head 错误钩子（把 ERR:/REJ: 写进标题，便于肉眼发现问题）
const hookMarker = "window.addEventListener('error'";
if (html.includes(hookMarker)) {
  const start = html.lastIndexOf("<script>", html.indexOf(hookMarker));
  const end = html.indexOf("</script>", html.indexOf(hookMarker));
  if (start >= 0 && end > start) {
    html = html.slice(0, start) + "<script>" + headHook + "</script>" + html.slice(end + "</script>".length);
    log.push("head 错误钩子已替换");
  } else {
    log.push("[warn] 错误钩子块边界异常");
  }
} else {
  const headEnd = html.indexOf("</head>");
  if (headEnd > 0) {
    html = html.slice(0, headEnd) + "  <script>" + headHook + "</script>\n  " + html.slice(headEnd);
    log.push("head 错误钩子已注入");
  } else {
    log.push("[warn] 找不到 </head>");
  }
}

// 6. 品牌资产（按名覆盖 dist/assets/）
const svgSrcDir = path.join(brandDir, "assets");
let copied = 0;
if (fs.existsSync(svgSrcDir)) {
  const destDir = path.join(distDir, "assets");
  fs.mkdirSync(destDir, { recursive: true });
  for (const f of fs.readdirSync(svgSrcDir)) {
    fs.copyFileSync(path.join(svgSrcDir, f), path.join(destDir, f));
    copied += 1;
  }
}
log.push(`品牌资产已复制 ${copied} 个 -> dist/assets/`);

// 报告
console.log(`[branding] target: ${path.relative(root, htmlPath)}`);
for (const l of log) console.log(`  ${l}`);

if (html === before) {
  console.log("  [noop] index.html 已是目标状态（幂等）");
} else {
  fs.writeFileSync(htmlPath, html);
  console.log(`  [write] index.html ${before.length} -> ${html.length} bytes`);
}

// 目标状态断言（含唯一性：重复注入会被抓出来）
const count = (needle) => html.split(needle).length - 1;
const check = [
  [`aria-label="${facts.bootAria}"`, count(`aria-label="${facts.bootAria}"`) === 1],
  [`boot-shell__name="${facts.bootName}"`, count(`>${facts.bootName}</div>`) === 1],
  ["boot mark 为 PNG", count("data:image/png;base64,") === 1],
  ["品牌 CSS 哨兵唯一", count(cssStartMark) === 1 && count(cssEndMark) === 1],
  ["head 错误钩子唯一", count(hookMarker) === 1],
];
let bad = 0;
for (const [name, pass] of check) {
  if (!pass) bad += 1;
  console.log(`  ${pass ? "ok  " : "FAIL"} ${name}`);
}
process.exit(bad === 0 ? 0 : 1);
