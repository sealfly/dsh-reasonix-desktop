#!/usr/bin/env node
/*
 * verify-packaged-app.js — 校验构建出的 exe 里确实包含"当前 frontend/dist"与本轮新增能力。
 *
 * 为什么需要：安装包是 170+MB 的黑盒，构建日志只说"成功"。发布前用几个**只可能来自当前源码/dist**
 * 的标记做二进制检索，能在秒级确认"发出去的包不是旧的"（发布防呆）。
 *
 * 用法： node scripts/verify-packaged-app.js [exe路径]
 */
"use strict";

const fs = require("fs");
const path = require("path");

const root = path.resolve(__dirname, "..");
const exe = process.argv[2] || path.join(root, "build", "bin", "DSH-ReasonixUI.exe");

if (!fs.existsSync(exe)) {
  console.error(`找不到 ${exe}`);
  process.exit(2);
}

const buf = fs.readFileSync(exe);
// 前端注入脚本会写入加载时校验的标记；桥方法名来自本轮新增的 Go 方法/绑定；
// 1.38.2 前端特征串来自升级后的 dist（压缩关闭，所以能直接搜到）。
const checks = [
  { name: "前端注入已内联（子代理页）", needle: "dsh-inject:subagent-panel" },
  { name: "前端注入已内联（插件市场，已修复版）", needle: "dsh-inject:plugin-market" },
  { name: "前端注入已内联（Agent 预设）", needle: "dsh-inject:agent-presets" },
  { name: "1.38.2 前端（模型服务页两栏结构）", needle: "provider-connections__item" },
  { name: "1.38.2 前端（设置导航）", needle: "settings-center__navitem" },
  { name: "桥方法：新增供应商", needle: "AddProviderConnectionWithOptions" },
  { name: "桥方法：保存供应商", needle: "SaveProviderModelCatalogs" },
  { name: "桥方法：测试模型", needle: "TestProviderModel" },
  { name: "桥方法：会话体验", needle: "SetSessionExperience" },
  { name: "桥方法：UI 测试钩子回传", needle: "UiTestReport" },
  { name: "DSH 供应商命名空间", needle: "llm-pi-ai" },
];

let pass = 0;
let fail = 0;
console.log(`exe: ${path.relative(root, exe)}  (${(buf.length / 1024 / 1024).toFixed(1)}MB, ${fs.statSync(exe).mtime.toISOString().slice(0, 16).replace("T", " ")})`);
for (const c of checks) {
  const hit = buf.includes(Buffer.from(c.needle, "utf8"));
  if (hit) pass += 1;
  else fail += 1;
  console.log(`  ${hit ? "OK  " : "MISS"} ${c.name}  [${c.needle}]`);
}
console.log(`\nresolved: ${pass}/${pass + fail}`);
process.exit(fail === 0 ? 0 : 1);
