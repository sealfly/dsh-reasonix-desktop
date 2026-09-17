#!/usr/bin/env node
/*
 * settings-contract-check.js — 对照前端设置快照契约（SettingsView / DesktopStartupSettingsView）
 * 与 Go 侧实际返回的键，找出"前端会读到 undefined 因而回落到默认值"的字段。
 *
 * 背景（2026-09-17 主题跳变）：Settings() 曾只返回 themeExperience 契约的键名
 * （themeMode/baseStyle），而设置面板读的是 desktopTheme/desktopThemeStyle，
 * 于是每次设置重读都把主题重置为 DEFAULT_THEME("auto")+默认风格 —— 表现为
 * "深色模式下按别的按钮就跳"。键名缺失不会报错，只会静默回落，所以需要静态对照。
 *
 * 用法：
 *   node scripts/settings-contract-check.js <上游 frontend/src/lib/types.ts>
 */
"use strict";

const fs = require("fs");
const path = require("path");

const root = path.resolve(__dirname, "..");
const typesPath = process.argv[2];
if (!typesPath || !fs.existsSync(typesPath)) {
  console.error("usage: node scripts/settings-contract-check.js <types.ts>");
  process.exit(2);
}

/** 从 types.ts 抽取某个 interface 的字段名。 */
function interfaceFields(src, name) {
  const re = new RegExp(`export interface ${name}\\b[^{]*\\{`, "m");
  const m = re.exec(src);
  if (!m) return null;
  let depth = 1;
  let i = m.index + m[0].length;
  const start = i;
  while (i < src.length && depth > 0) {
    const ch = src[i];
    if (ch === "{") depth += 1;
    else if (ch === "}") depth -= 1;
    i += 1;
  }
  const body = src.slice(start, i - 1);
  const fields = new Set();
  for (const line of body.split("\n")) {
    const f = line.match(/^\s*([A-Za-z_][A-Za-z0-9_]*)\??\s*:/);
    if (f) fields.add(f[1]);
  }
  return fields;
}

/** 抽取 Go 文件里指定函数体中的 map 键字面量。 */
function goKeys(src, funcSignature) {
  const idx = src.indexOf(funcSignature);
  if (idx < 0) return null;
  // 从函数签名起，按花括号配平取函数体
  let i = src.indexOf("{", idx);
  let depth = 1;
  const start = i + 1;
  i += 1;
  while (i < src.length && depth > 0) {
    if (src[i] === "{") depth += 1;
    else if (src[i] === "}") depth -= 1;
    i += 1;
  }
  const body = src.slice(start, i - 1);
  const keys = new Set();
  for (const m of body.matchAll(/"([A-Za-z][A-Za-z0-9_]*)"\s*:/g)) keys.add(m[1]);
  return keys;
}

const types = fs.readFileSync(typesPath, "utf8");
const go = fs.readFileSync(path.join(root, "app_settings.go"), "utf8");

const settingsKeys = goKeys(go, "func (a *App) Settings() map[string]any");
const startupKeys = goKeys(go, "func (a *App) DesktopStartupSettings() map[string]any");
const prefKeys = goKeys(go, "func (a *App) desktopPreferenceKeys() map[string]any");

if (!settingsKeys || !startupKeys) {
  console.error("无法解析 app_settings.go 中的 Settings/DesktopStartupSettings（签名变了？）");
  process.exit(1);
}
// desktopPreferenceKeys 通过 mergeKeys 并入两个载荷，视为两者都有
const effectiveSettings = new Set([...settingsKeys, ...(prefKeys || [])]);
const effectiveStartup = new Set([...startupKeys, ...(prefKeys || [])]);

const contracts = [
  ["SettingsView", effectiveSettings, "Settings()"],
  ["DesktopStartupSettingsView", effectiveStartup, "DesktopStartupSettings()"],
];

console.log(`contract: ${path.basename(typesPath)}`);
console.log(`go keys: Settings=${settingsKeys.size} +prefs=${prefKeys ? prefKeys.size : 0}, Startup=${startupKeys.size}\n`);

let missingTotal = 0;
for (const [type, have, label] of contracts) {
  const fields = interfaceFields(types, type);
  if (!fields) {
    console.log(`[warn] 未找到 interface ${type}`);
    continue;
  }
  const missing = [...fields].filter((f) => !have.has(f)).sort();
  const extra = [...have].filter((f) => !fields.has(f)).sort();
  missingTotal += missing.length;
  console.log(`== ${type} vs ${label} ==`);
  console.log(`   契约字段 ${fields.size}，我们提供 ${[...fields].filter((f) => have.has(f)).length}`);
  console.log(`   缺失 ${missing.length}: ${missing.join(", ") || "-"}`);
  console.log(`   额外 ${extra.length}（前端忽略）: ${extra.join(", ") || "-"}`);
  console.log("");
}

console.log(missingTotal === 0
  ? "结论: 契约字段全覆盖"
  : `结论: 仍缺 ${missingTotal} 个字段 —— 缺失项会让前端静默回落到默认值（主题跳变就是这一类）`);
