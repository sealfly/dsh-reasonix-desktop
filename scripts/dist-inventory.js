#!/usr/bin/env node
/*
 * dist-inventory.js — 盘点 frontend/dist/index.html 里的全部内联自定义块。
 *
 * 用途：升级上游前端（换 dist）前，先确认当前 dist 承载了哪些本项目注入，
 * 以便逐项重放。凡是没有对应 scripts/dsh-*.js 源 + applier 的块，都是升级风险。
 */
"use strict";

const fs = require("fs");
const path = require("path");

const root = path.resolve(__dirname, "..");
const htmlPath = path.join(root, "frontend", "dist", "index.html");
const html = fs.readFileSync(htmlPath, "utf8");

console.log(`file: ${path.relative(root, htmlPath)}  (${(html.length / 1024).toFixed(1)}KB)`);

const re = /<script\b([^>]*)>([\s\S]*?)<\/script>/g;
let m;
let n = 0;
const rows = [];
while ((m = re.exec(html)) !== null) {
  n += 1;
  const attr = m[1].trim();
  const body = m[2];
  const marks = [...new Set(body.match(/__DSH_[A-Z_]+/g) || [])];
  const first = body
    .trim()
    .split("\n")
    .slice(0, 2)
    .map((s) => s.trim())
    .join(" ")
    .slice(0, 120);
  rows.push({ n, attr, len: body.length, marks, first });
}

console.log(`inline <script> blocks: ${rows.length}\n`);

// 只关心自定义块：带 __DSH_ 标记，或 block 很小且不是打包产物
for (const r of rows) {
  const custom = r.marks.length > 0;
  const tag = custom ? "CUSTOM" : r.len > 100000 ? "bundle" : "small ";
  console.log(`#${String(r.n).padStart(3)} ${tag} len=${String(r.len).padStart(7)}${r.attr ? ` attr="${r.attr.slice(0, 60)}"` : ""}`);
  if (r.marks.length) console.log(`        marks: ${r.marks.join(", ")}`);
  if (r.len < 200000) console.log(`        head : ${r.first}`);
}

// 源脚本 ↔ 清单 ↔ dist 对照
const scriptDir = path.join(root, "scripts");
const scripts = fs.readdirSync(scriptDir).filter((f) => /^dsh-.*\.js$/.test(f));
const manifestPath = path.join(scriptDir, "injections.json");
const manifest = fs.existsSync(manifestPath) ? JSON.parse(fs.readFileSync(manifestPath, "utf8")) : { injections: [] };
const byFile = new Map(manifest.injections.map((e) => [e.file, e]));
const distText = html;
console.log("\n== scripts/dsh-*.js ↔ injections.json ↔ dist ==");
for (const f of scripts) {
  const src = fs.readFileSync(path.join(scriptDir, f), "utf8");
  const marks = [...new Set(src.match(/__DSH_[A-Z_]+/g) || [])];
  const hit = marks.filter((k) => distText.includes(k));
  const entry = byFile.get(f);
  const listed = entry ? (entry.enabled ? "清单:启用" : "清单:禁用") : "清单:未收录";
  const sentinel = entry ? distText.includes(`/* dsh-inject:${entry.id} */`) : false;
  const state = sentinel ? "已内联" : entry && entry.enabled ? "缺失!" : "未内联";
  console.log(
    `  ${state.padEnd(6)} ${f.padEnd(28)} ${listed.padEnd(10)} marks=${marks.join(",") || "-"}${marks.length && hit.length !== marks.length ? ` 命中${hit.length}/${marks.length}` : ""}`,
  );
}
if (manifest.injections.some((e) => e.enabled && !fs.existsSync(path.join(scriptDir, e.file)))) {
  console.log("  [FAIL] 清单里启用的注入缺少源脚本");
}
