#!/usr/bin/env node
/*
 * ensure-bom.js — 保证指定文本文件带 UTF-8 BOM。
 *
 * 为什么：Windows PowerShell 5.1 读取无 BOM 的 UTF-8 .ps1 时按 GBK 解码，
 * 会把脚本里的中文（例如安装包名、桌面复制路径）读成乱码，导致
 * makensis "Bad text encoding" 或 "Illegal characters in path"。
 * 编辑器/打补丁工具重写文件后常丢 BOM，所以把它做成显式可重放步骤。
 *
 * 用法：
 *   node scripts/ensure-bom.js <file...>              # 缺则补，已有则跳过
 *   node scripts/ensure-bom.js --check <file...>      # 只检查，缺 BOM 返回 1
 *   node scripts/ensure-bom.js --strip <file...>      # 去掉 UTF-8 BOM
 *
 * 注意：Node 的 CommonJS 加载器不会在 shebang 判断前剥掉 BOM，因此给 .js 加上 BOM 会让
 * 首行 `#!/usr/bin/env node` 变成语法错误（SyntaxError: Invalid or unexpected token）。
 * 只有 .ps1 需要 BOM；.js 必须无 BOM。PowerShell 5.1 的 `Set-Content -Encoding UTF8`
 * 会写 BOM，用它改过 JS 后务必 `--strip`。
 */
"use strict";

const fs = require("fs");
const path = require("path");

const args = process.argv.slice(2);
const checkOnly = args.includes("--check");
const strip = args.includes("--strip");
const files = args.filter((a) => !a.startsWith("--"));

if (files.length === 0) {
  console.error("usage: node scripts/ensure-bom.js [--check|--strip] <file...>");
  process.exit(2);
}

const BOM = Buffer.from([0xef, 0xbb, 0xbf]);
let missing = 0;

for (const f of files) {
  const p = path.resolve(f);
  if (!fs.existsSync(p)) {
    console.error(`  MISS ${f} (不存在)`);
    missing += 1;
    continue;
  }
  const buf = fs.readFileSync(p);
  const has = buf.length >= 3 && buf[0] === 0xef && buf[1] === 0xbb && buf[2] === 0xbf;
  if (strip) {
    if (!has) {
      console.log(`  ok   ${f} 本来就没有 BOM`);
      continue;
    }
    fs.writeFileSync(p, buf.subarray(3));
    console.log(`  strip ${f} 已去掉 UTF-8 BOM`);
    continue;
  }
  if (has) {
    console.log(`  ok   ${f}`);
    continue;
  }
  if (checkOnly) {
    console.error(`  FAIL ${f} 缺 UTF-8 BOM`);
    missing += 1;
    continue;
  }
  fs.writeFileSync(p, Buffer.concat([BOM, buf]));
  console.log(`  fix  ${f} 已补 UTF-8 BOM`);
}

process.exit(missing === 0 ? 0 : 1);
