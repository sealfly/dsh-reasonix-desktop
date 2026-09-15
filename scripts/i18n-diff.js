// i18n-diff.js — 对比两个版本的界面文案，用于回答"上游 UI 改了什么"。
//
// 输入可以是：
//   - 源码：desktop/frontend/src/locales/zh.ts（TS 对象字面量）
//   - 产物：desktop/frontend/dist/assets/zh-*.js（打包后的对象字面量）
// 输出：
//   - 新增文案 / 删除文案（按 key 前缀分组统计，可看出哪些界面动了）
//   - key 级差异（尽力而为：两边都取到的 key 做集合运算）
//
// 用法：node scripts/i18n-diff.js <old-file> <new-file> [--limit N]
'use strict';
const fs = require('fs');

function read(p) {
  return fs.readFileSync(p, 'utf8');
}

// 抓所有 "key": "value" / key: "value" / 'key': 'value' 对（含中文字符的值更可靠）
function extractPairs(text) {
  const pairs = new Map();   // key -> Set(values)
  const values = new Set();
  const re = /(["']?)([A-Za-z0-9_$][A-Za-z0-9_$.\-]*)\1\s*:\s*(["'])((?:\\.|(?!\3)[\s\S])*?)\3/g;
  let m;
  while ((m = re.exec(text)) !== null) {
    const key = m[2];
    const val = m[4];
    if (!val || val.length > 400) continue;
    if (!pairs.has(key)) pairs.set(key, new Set());
    pairs.get(key).add(val);
    values.add(val);
  }
  return { pairs, values };
}

function groupByPrefix(keys) {
  const g = {};
  keys.forEach(k => {
    const p = k.split('.')[0] || '(root)';
    g[p] = (g[p] || 0) + 1;
  });
  return Object.entries(g).sort((a, b) => b[1] - a[1]);
}

const [oldPath, newPath] = process.argv.slice(2);
const limitArg = process.argv.indexOf('--limit');
const LIMIT = limitArg > 0 ? Number(process.argv[limitArg + 1]) : 40;

if (!oldPath || !newPath) {
  console.error('usage: node scripts/i18n-diff.js <old> <new> [--limit N]');
  process.exit(2);
}

const oldData = extractPairs(read(oldPath));
const newData = extractPairs(read(newPath));
console.log(`[diff] old: ${oldPath}  keys=${oldData.pairs.size} values=${oldData.values.size}`);
console.log(`[diff] new: ${newPath}  keys=${newData.pairs.size} values=${newData.values.size}`);

// key 级差异
const oldKeys = new Set(oldData.pairs.keys());
const newKeys = new Set(newData.pairs.keys());
const addedKeys = [...newKeys].filter(k => !oldKeys.has(k));
const removedKeys = [...oldKeys].filter(k => !newKeys.has(k));

// 文案值差异（更能反映 UI：新增的界面文字）
const addedVals = [...newData.values].filter(v => !oldData.values.has(v));
const removedVals = [...oldData.values].filter(v => !newData.values.has(v));

console.log(`\n== key 变化 ==\n  新增 key: ${addedKeys.length}\n  删除 key: ${removedKeys.length}`);
console.log(`\n== 文案变化 ==\n  新增文案: ${addedVals.length}\n  删除文案: ${removedVals.length}`);

if (addedKeys.length) {
  console.log('\n== 新增 key 按模块分组（前 15） ==');
  groupByPrefix(addedKeys).slice(0, 15).forEach(([p, n]) => console.log(`  ${p}.*  +${n}`));
}
if (removedKeys.length) {
  console.log('\n== 删除 key 按模块分组（前 15） ==');
  groupByPrefix(removedKeys).slice(0, 15).forEach(([p, n]) => console.log(`  ${p}.*  -${n}`));
}

// 新增文案：中文优先展示（最能体现 UI 变化）
const cnAdded = addedVals.filter(v => /[\u4e00-\u9fff]/.test(v));
console.log(`\n== 新增界面文案（含中文，前 ${LIMIT} 条） ==`);
cnAdded.slice(0, LIMIT).forEach(v => console.log('  + ' + v.replace(/\s+/g, ' ').slice(0, 120)));
if (cnAdded.length > LIMIT) console.log(`  … 另有 ${cnAdded.length - LIMIT} 条`);

const cnRemoved = removedVals.filter(v => /[\u4e00-\u9fff]/.test(v));
if (cnRemoved.length) {
  console.log(`\n== 删除/改动的界面文案（含中文，前 ${Math.min(LIMIT, cnRemoved.length)} 条） ==`);
  cnRemoved.slice(0, LIMIT).forEach(v => console.log('  - ' + v.replace(/\s+/g, ' ').slice(0, 120)));
  if (cnRemoved.length > LIMIT) console.log(`  … 另有 ${cnRemoved.length - LIMIT} 条`);
}
