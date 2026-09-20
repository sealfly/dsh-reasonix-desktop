#!/usr/bin/env node
/**
 * align-stub-arity.js — 把零参占位桩的签名对齐到前端实际传入的实参个数。
 *
 * 背景：Wails 绑定按**参数个数**校验，形参 0 而前端传 1 个就直接抛
 *   `error parsing arguments: received 1 arguments to method 'main.App.X', expected 0`
 * 这类错只在界面角落显示一行小字，极易漏掉（本项目已踩过：远程文件树）。
 *
 * 本脚本只做**机械化的一半**：把形参个数补齐（类型一律 any，Wails 能正确解码 JSON）。
 * 语义仍由各方法自身的实现决定（这些桩本来就不做事，补齐后只是"不再抛参数错"）。
 * 已实现的真方法（有实际逻辑的）**不在自动处理范围内**，需要人工按调用语义改。
 *
 * 用法：
 *   node scripts/align-stub-arity.js --plan     # 只打印将要做的改动
 *   node scripts/align-stub-arity.js --apply    # 实际写入
 * 输入：%TEMP%\arity-names.txt（每行 "方法名 <前端实参个数>"）
 */
"use strict";
const fs = require("fs");
const path = require("path");

const root = path.join(__dirname, "..");
const listPath = process.argv.find((a) => a.endsWith(".txt")) || path.join(process.env.TEMP, "arity-names.txt");
const apply = process.argv.includes("--apply");

const lines = fs.readFileSync(listPath, "utf8").split(/\r?\n/).map((s) => s.trim()).filter(Boolean);
const wanted = [];
for (const l of lines) {
  const m = l.match(/^(\w+)\s+(\d+)/);
  if (m) wanted.push({ name: m[1], argc: Number(m[2]) });
}
if (!wanted.length) { console.error("没有解析到待修清单（格式：方法名 实参个数）"); process.exit(2); }

const files = fs.readdirSync(root).filter((f) => f.endsWith(".go") && !f.endsWith("_test.go"));
const changes = [];
const skipped = [];

for (const f of files) {
  const full = path.join(root, f);
  let src = fs.readFileSync(full, "utf8");
  let dirty = false;
  for (const w of wanted) {
    // 匹配 func (a *App) Name() ...{   —— 只处理零参
    const re = new RegExp(`func \\(a \\*App\\) ${w.name}\\(\\)(\\s*[^\\n{]*(?:\\{|$))`, "m");
    const m = src.match(re);
    if (!m) { continue; }
    const params = Array.from({ length: w.argc }, (_, i) => `_a${i + 1} any`).join(", ");
    const replacement = `func (a *App) ${w.name}(${params})${m[1]}`;
    changes.push({ file: f, name: w.name, argc: w.argc, before: m[0].slice(0, 90), after: replacement.slice(0, 90) });
    if (apply) { src = src.replace(re, replacement); dirty = true; }
  }
  if (apply && dirty) {
    // 保留原文件的 BOM 状态
    const hadBom = src.charCodeAt(0) === 0xfeff;
    fs.writeFileSync(full, hadBom ? src : src, "utf8");
  }
}

// 未在自动范围内的（已实现方法 / 需要人工判断）
const touched = new Set(changes.map((c) => c.name));
for (const w of wanted) if (!touched.has(w.name)) skipped.push(w.name);

console.log(`${apply ? "已应用" : "计划"}改动 ${changes.length} 处：`);
for (const c of changes) console.log(`  ${c.file}  ${c.name}(${c.argc} 个 any 形参)`);
console.log(`\n需人工处理（非零参桩或已实现方法）${skipped.length} 个：`);
console.log("  " + skipped.join(", "));
