#!/usr/bin/env node
/*
 * apply-all-injections.js — 按 scripts/injections.json 把全部注入源脚本内联进 frontend/dist/index.html。
 *
 * 为什么需要它：这些注入只存在于构建产物 index.html 里，上游升级会整体替换 dist。
 * 旧做法只有 2 个 apply-*.js，其余 5 个注入靠手工内联 → 一升级就丢。
 * 本脚本让「全部注入」变成单一可重放步骤，并加两道门禁：
 *   1) 语法门禁：每个源脚本必须先通过 new Function 解析（经典脚本），否则整体失败。
 *      —— 旧 dsh-plugin-market-inject.js 开头是一段 `ce as T,...}` export 残片，
 *         内联后整块 SyntaxError、从未执行；语法门禁能让这类错误在构建时就炸出来。
 *   2) 唯一性断言：每个 id 的内联块必须恰好 1 个，防止重复注入/漏注入。
 *
 * 幂等性：每次运行先删除全部已知注入块（新哨兵 + 旧标记），再按清单顺序重新追加，
 * 因此结果只取决于清单和源文件，与历史无关。
 *
 * 用法：
 *   node scripts/apply-all-injections.js [dist-dir] [--check]
 *     --check  只校验（不写文件），全部源脚本语法通过 + 每个 id 恰好 1 块才返回 0
 */
"use strict";

const fs = require("fs");
const path = require("path");

const root = path.resolve(__dirname, "..");
const args = process.argv.slice(2);
const checkOnly = args.includes("--check");
const distArg = args.find((a) => !a.startsWith("--"));
const distDir = distArg ? path.resolve(distArg) : path.join(root, "frontend", "dist");
const htmlPath = path.join(distDir, "index.html");

const manifest = JSON.parse(fs.readFileSync(path.join(root, "scripts", "injections.json"), "utf8"));
const all = manifest.injections;
const enabled = all.filter((e) => e.enabled);
const legacyMarkers = manifest.legacyMarkers || [];

if (!fs.existsSync(htmlPath)) {
  console.error(`[inject] 找不到 ${htmlPath}`);
  process.exit(1);
}

const CLOSE = "</" + "script>";
const sentinel = (id) => `/* dsh-inject:${id} */`;

// ── 读取并校验源脚本 ────────────────────────────────────────────────
const sources = new Map();
let gateFailures = 0;
for (const entry of all) {
  const p = path.join(root, "scripts", entry.file);
  if (!fs.existsSync(p)) {
    console.error(`[inject] ${entry.enabled ? "FAIL" : "skip"} 源脚本缺失: scripts/${entry.file}`);
    if (entry.enabled) gateFailures += 1;
    continue;
  }
  const src = fs.readFileSync(p, "utf8");
  // 语法门禁：内联后是经典脚本，必须能被 Function 构造器解析
  try {
    // eslint-disable-next-line no-new-func
    new Function(src);
  } catch (err) {
    console.error(`[inject] FAIL 语法门禁未通过 scripts/${entry.file}: ${err.message}`);
    console.error(`[inject]      该文件不能作为经典脚本内联（例如含 export/import 残片）。`);
    gateFailures += 1;
    continue;
  }
  sources.set(entry.id, src);
  if (entry.enabled) console.log(`[inject] ok   语法门禁 scripts/${entry.file} (${src.length} bytes)`);
  else console.log(`[inject] skip 已禁用 ${entry.id} (scripts/${entry.file})`);
}

if (gateFailures > 0) {
  console.error(`[inject] ${gateFailures} 个启用的注入源脚本不可用，拒绝写入`);
  process.exit(1);
}

// ── 解析 index.html：区域哨兵 + 旧块迁移 ────────────────────────────
// 幂等关键：所有注入放在 <!-- dsh-inject:begin --> ... <!-- dsh-inject:end --> 之间，
// 每次运行只是「整段替换」，结果只取决于清单与源文件，与历史无关。
let html = fs.readFileSync(htmlPath, "utf8");
const before = html;
const BEGIN = "<!-- dsh-inject:begin -->";
const END = "<!-- dsh-inject:end -->";
const blockRe = /<script\b([^>]*)>([\s\S]*?)<\/script>/g;

function blocksOf(text) {
  const out = [];
  let m;
  blockRe.lastIndex = 0;
  while ((m = blockRe.exec(text)) !== null) out.push({ attr: m[1], body: m[2], index: m.index, raw: m[0] });
  return out;
}

const hasRegion = html.includes(BEGIN) && html.includes(END);
let removed = 0;

if (hasRegion) {
  // 整段替换
  const s = html.indexOf(BEGIN);
  const e = html.indexOf(END) + END.length;
  html = html.slice(0, s) + "@@DSH_INJECT_REGION@@" + html.slice(e);
} else {
  // 首次迁移：删除已知的旧注入块（仅限无 src 的内联块，且带旧标记）
  const bs = blocksOf(html).filter(
    (b) => !/\bsrc=/.test(b.attr) && (b.body.includes("dsh-inject:") || legacyMarkers.some((k) => b.body.includes(k))),
  );
  if (bs.length) {
    for (const b of bs.reverse()) {
      html = html.slice(0, b.index) + html.slice(b.index + b.raw.length);
      removed += 1;
    }
  }
  const at = html.lastIndexOf("</body>");
  if (at < 0) {
    console.error("[inject] 找不到 </body>，无法插入注入区");
    process.exit(1);
  }
  html = html.slice(0, at) + "@@DSH_INJECT_REGION@@\n" + html.slice(at);
}
if (removed) console.log(`[inject] 迁移：移除旧格式注入块 ${removed} 个`);

// ── 生成注入区内容 ──────────────────────────────────────────────────
const blocks = [];
for (const entry of enabled) {
  const src = sources.get(entry.id);
  if (src === undefined) continue;
  const body = `${sentinel(entry.id)}\n${src.replace(new RegExp(CLOSE, "g"), "<\\/script>")}`;
  blocks.push(`<script>\n${body}\n${CLOSE}`);
}
const region = [BEGIN, blocks.join("\n"), END].join("\n");
html = html.replace("@@DSH_INJECT_REGION@@", region);

// ── 断言 ────────────────────────────────────────────────────────────
let bad = 0;
if ((html.split(BEGIN).length - 1) !== 1 || (html.split(END).length - 1) !== 1) {
  console.log("[inject] FAIL 注入区哨兵不唯一");
  bad += 1;
}
for (const entry of enabled) {
  const n = html.split(sentinel(entry.id)).length - 1;
  if (n !== 1) {
    console.log(`[inject] FAIL ${entry.id} 哨兵出现 ${n} 次（应为 1）`);
    bad += 1;
  }
}
// 区域之外不得再有旧格式注入块（源脚本内部自带的注释属正常，故只查块体）
const regionStart = html.indexOf(BEGIN);
const regionEnd = html.indexOf(END) + END.length;
const outside = html.slice(0, regionStart) + html.slice(regionEnd);
for (const b of blocksOf(outside)) {
  if (/\bsrc=/.test(b.attr)) continue;
  const stale = legacyMarkers.find((k) => b.body.includes(k));
  if (stale) {
    console.log(`[inject] FAIL 注入区外仍有旧块: ${stale}`);
    bad += 1;
  }
}

const payloadLen = region.length;
console.log(`[inject] target: ${path.relative(root, htmlPath)}`);
console.log(`[inject] 启用注入 ${enabled.length} 个，内联 ${blocks.length} 块，注入区 ${payloadLen} bytes`);
if (html === before) {
  console.log("  [noop] index.html 已是目标状态（幂等）");
} else if (checkOnly) {
  console.log("  [check] 需要重建（--check 模式不写文件）");
  bad += 1;
} else {
  fs.writeFileSync(htmlPath, html);
  console.log(`  [write] index.html ${before.length} -> ${html.length} bytes`);
}
process.exit(bad === 0 ? 0 : 1);
