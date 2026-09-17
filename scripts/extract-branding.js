#!/usr/bin/env node
/*
 * extract-branding.js — 从既有 frontend/dist/index.html 抽取本项目品牌改造资产。
 *
 * 用途：升级上游前端时，品牌改造必须可重放（PRINCIPLES 原则 6：DSH 适配不能被上游覆盖）。
 * 本脚本把当前生效的品牌资产固化成 branding/ 下的数据文件，
 * 再由 scripts/apply-branding.js 幂等地写回新的 index.html。
 *
 * 用法：
 *   node scripts/extract-branding.js [dist-index.html]
 */
"use strict";

const fs = require("fs");
const path = require("path");

const root = path.resolve(__dirname, "..");
const htmlPath = process.argv[2] || path.join(root, "frontend", "dist", "index.html");
const html = fs.readFileSync(htmlPath, "utf8");
const outDir = path.join(root, "branding");
fs.mkdirSync(outDir, { recursive: true });

console.log(`source: ${path.relative(root, htmlPath)}  (${(html.length / 1024).toFixed(1)}KB)`);

let ok = 0;

// 1. boot shell 品牌 mark（data URI 里的 base64 位图）
const mark = html.match(/class="boot-shell__mark"[^>]*>\s*<img src="data:image\/png;base64,([^"]+)"/);
if (mark) {
  const buf = Buffer.from(mark[1], "base64");
  fs.writeFileSync(path.join(outDir, "boot-mark.png"), buf);
  console.log(`  boot-mark.png      ${buf.length} bytes  (base64 ${mark[1].length} chars)`);
  ok += 1;
} else {
  console.log("  [warn] boot-mark 未匹配（可能已不是 base64 PNG）");
}

// 2. 品牌 CSS 覆写块
const cssStart = html.indexOf("/* local brand:");
const cssEnd = html.indexOf("</style>", cssStart);
if (cssStart > 0 && cssEnd > cssStart) {
  const css = html.slice(cssStart, cssEnd).replace(/\s+$/, "") + "\n";
  fs.writeFileSync(path.join(outDir, "brand.css"), css);
  console.log(`  brand.css          ${css.length} bytes`);
  ok += 1;
} else {
  console.log("  [warn] 品牌 CSS 未匹配");
}

// 3. head 内的错误钩子（把 ERR:/REJ: 写进标题，便于肉眼发现问题）
const hook = html.match(/<script>(window\.addEventListener\('error',[\s\S]*?)<\/script>/);
if (hook) {
  fs.writeFileSync(path.join(outDir, "head-error-hook.js"), hook[1] + "\n");
  console.log(`  head-error-hook.js ${hook[1].length} bytes`);
  ok += 1;
} else {
  console.log("  [warn] head 错误钩子未匹配");
}

// 4. 品牌 CSS 里显式引用的资产（如 ./assets/logo-wordmark-dark.svg）——按名复制，避免猜测
const assetsDir = path.join(root, "frontend", "dist", "assets");
const cssFile = path.join(outDir, "brand.css");
if (fs.existsSync(cssFile) && fs.existsSync(assetsDir)) {
  const css = fs.readFileSync(cssFile, "utf8");
  const refs = [...new Set([...css.matchAll(/url\(\s*"\.\/assets\/([^"]+)"\s*\)/g)].map((m) => m[1]))];
  if (refs.length === 0) console.log("  [info] brand.css 未引用自定义资产");
  const svgDir = path.join(outDir, "assets");
  for (const name of refs) {
    const src = path.join(assetsDir, name);
    if (!fs.existsSync(src)) {
      console.log(`  [warn] brand.css 引用的资产缺失: assets/${name}`);
      continue;
    }
    fs.mkdirSync(svgDir, { recursive: true });
    fs.copyFileSync(src, path.join(svgDir, name));
    console.log(`  assets/${name.padEnd(28)} ${fs.statSync(src).size} bytes`);
    ok += 1;
  }
}

// 5. 记录品牌文案事实，便于 apply 阶段断言
const facts = {
  title: (html.match(/<title>([^<]*)<\/title>/) || [])[1] || "",
  bootAria: (html.match(/class="boot-shell" role="status" aria-label="([^"]*)"/) || [])[1] || "",
  bootName: (html.match(/class="boot-shell__name">([^<]*)</) || [])[1] || "",
};
fs.writeFileSync(path.join(outDir, "brand.json"), JSON.stringify(facts, null, 2) + "\n");
console.log(`  brand.json         ${JSON.stringify(facts)}`);

console.log(`\nextracted ${ok} branding artifact(s) -> branding/`);
process.exit(ok >= 3 ? 0 : 1);
