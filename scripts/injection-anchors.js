#!/usr/bin/env node
/*
 * injection-anchors.js — 检查各注入脚本依赖的 DOM 锚点在目标前端源码里是否仍存在。
 *
 * 用途：升级上游前端前，先判定每个注入是「可平移」还是「必须改锚点」。
 * 判据：脚本里出现的 class / querySelector 字面量，能否在目标前端 src 中找到。
 *
 * 用法：
 *   node scripts/injection-anchors.js <target-frontend-src-dir>
 */
"use strict";

const fs = require("fs");
const path = require("path");

const root = path.resolve(__dirname, "..");
const target = process.argv[2];
if (!target || !fs.existsSync(target)) {
  console.error("usage: node scripts/injection-anchors.js <target-frontend-src-dir>");
  process.exit(2);
}

// 目标前端源码全文（tsx/ts/css）
function collect(dir) {
  const out = [];
  const walk = (d) => {
    for (const e of fs.readdirSync(d, { withFileTypes: true })) {
      const p = path.join(d, e.name);
      if (e.isDirectory()) {
        if (e.name === "node_modules" || e.name === "__tests__" || e.name === "dist") continue;
        walk(p);
      } else if (/\.(tsx?|css)$/.test(e.name)) {
        out.push(fs.readFileSync(p, "utf8"));
      }
    }
  };
  walk(dir);
  return out.join("\n");
}

const hay = collect(target);
console.log(`target: ${target}`);
console.log(`scanned: ${(hay.length / 1024 / 1024).toFixed(1)}MB of ts/tsx/css\n`);

const scripts = fs.readdirSync(path.join(root, "scripts")).filter((f) => /^dsh-.*\.js$/.test(f));

for (const f of scripts) {
  const src = fs.readFileSync(path.join(root, "scripts", f), "utf8");
  // 收集选择器字面量：querySelector/closest/matches 的字符串 + className 里的类名
  const sels = new Set();
  for (const m of src.matchAll(/(?:querySelector(?:All)?|closest|matches)\(\s*['"`]([^'"`]+)['"`]/g)) {
    sels.add(m[1]);
  }
  for (const m of src.matchAll(/\.([a-z][a-z0-9]*(?:__[a-z0-9-]+)?(?:--[a-z0-9-]+)?)\b/g)) {
    if (/^[a-z]/.test(m[1]) && m[1].length > 3) sels.add("." + m[1]);
  }
  // 只保留看起来像界面 anchor 的（排除我们自己的 NS 前缀）
  const anchors = [...sels].filter((s) => !/dsh-|DSH|__DSH/.test(s) && (s.includes("__") || s.includes("--") || s.startsWith(".workspace") || s.startsWith(".settings") || s.startsWith(".composer") || s.startsWith(".workbench")));
  const hit = anchors.filter((a) => hay.includes(a.replace(/^\./, "").split(/[\s>]/).pop().replace(/^\./, "")));
  const miss = anchors.filter((a) => !hit.includes(a));
  console.log(`${f}`);
  console.log(`   anchors=${anchors.length}  命中=${hit.length}  未命中=${miss.length}`);
  if (miss.length && miss.length <= 12) console.log(`   未命中: ${miss.join(", ")}`);
  else if (miss.length) console.log(`   未命中(前12): ${miss.slice(0, 12).join(", ")}`);
  console.log("");
}
