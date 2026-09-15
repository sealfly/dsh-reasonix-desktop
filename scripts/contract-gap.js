// contract-gap.js — 严谨对照：官方桌面端契约命令 ↔ 我们 Wails 桥的导出方法。
// 用法：node scripts/contract-gap.js <desktopContract.generated.ts> <repo-root>
'use strict';
const fs = require('fs');
const path = require('path');

const [contractPath, repoRoot] = process.argv.slice(2);
if (!contractPath || !repoRoot) {
  console.error('usage: node scripts/contract-gap.js <contract.ts> <repo-root>');
  process.exit(2);
}

// 1) 官方契约命令（DESKTOP_COMMANDS 数组里的 PascalCase / event 名）
const contractSrc = fs.readFileSync(contractPath, 'utf8');
const cmds = new Set();
for (const m of contractSrc.matchAll(/^\s*"([A-Za-z][A-Za-z0-9_.:\-]*)",?\s*$/gm)) {
  cmds.add(m[1]);
}
const protoVer = (contractSrc.match(/DESKTOP_PROTOCOL_VERSION\s*=\s*(\d+)/) || [])[1];
const digest = (contractSrc.match(/DESKTOP_CONTRACT_DIGEST\s*=\s*"([^"]+)"/) || [])[1];

// 2) 我们桥导出的方法（app*.go: func (a *App) Name(...)）
const goFiles = fs.readdirSync(repoRoot)
  .filter(f => /^app.*\.go$/.test(f) && !/_test\.go$/.test(f));
const methods = new Set();
const methodFiles = new Map();
for (const f of goFiles) {
  const src = fs.readFileSync(path.join(repoRoot, f), 'utf8');
  for (const m of src.matchAll(/^func \(a \*App\) ([A-Za-z][A-Za-z0-9_]*)\(/gm)) {
    methods.add(m[1]);
    if (!methodFiles.has(m[1])) methodFiles.set(m[1], f);
  }
}
// node 侧还有 export 的函数式方法（app_*.go 之外的其他写法）
for (const m of contractSrc.matchAll(/DESKTOP_EVENTS\s*=\s*\[([\s\S]*?)\]/g)) {
  for (const e of m[1].matchAll(/"([A-Za-z][A-Za-z0-9_.:\-]*)"/g)) cmds.add(e[1]);
}

const lower = s => s.toLowerCase();
const cmdLower = new Map([...cmds].map(c => [lower(c), c]));
const methLower = new Map([...methods].map(m => [lower(m), m]));

const missing = [...cmdLower].filter(([l]) => !methLower.has(l)).map(([, c]) => c).sort();
const extra = [...methLower].filter(([l]) => !cmdLower.has(l)).map(([, m]) => m).sort();
const covered = [...cmdLower].filter(([l]) => methLower.has(l)).length;

console.log(`契约协议版本: ${protoVer || '?'}   digest: ${digest || '?'}`);
console.log(`官方契约命令: ${cmds.size}`);
console.log(`我们的桥方法: ${methods.size}（来自 ${goFiles.length} 个 app*.go）`);
console.log(`\n覆盖: ${covered} / ${cmds.size}`);
console.log(`缺口: ${missing.length}`);
console.log(`我们独有: ${extra.length}`);

function groupByPrefix(list) {
  const g = new Map();
  for (const n of list) {
    const p = (n.match(/^[a-z]+:/) || n.match(/^([A-Z][a-z]+)/) || [, n])[1];
    g.set(p, (g.get(p) || 0) + 1);
  }
  return [...g].sort((a, b) => b[1] - a[1]);
}

if (missing.length) {
  console.log('\n== 缺口命令按能力域分组（Top 20） ==');
  groupByPrefix(missing).slice(0, 20).forEach(([p, n]) => console.log(`  ${p}  x${n}`));
  console.log('\n== 缺口命令（前 80） ==');
  missing.slice(0, 80).forEach(c => console.log('  ' + c));
}
if (extra.length) {
  console.log('\n== 我们独有（官方契约无，可能是 DSH 桥特有；前 40） ==');
  extra.slice(0, 40).forEach(c => console.log('  ' + c));
}

// 落盘完整清单
const outDir = path.join(repoRoot, 'docs');
fs.mkdirSync(outDir, { recursive: true });
fs.writeFileSync(path.join(outDir, 'upgrade-v1388-contract-gap.txt'),
  `# 契约对照（官方 desktop-v1.38.8 vs 本项目桥）\n`
  + `协议版本: ${protoVer}\ndigest: ${digest}\n`
  + `官方命令: ${cmds.size}\n我们的方法: ${methods.size}\n覆盖: ${covered}\n缺口: ${missing.length}\n我们独有: ${extra.length}\n\n`
  + `## 缺口命令（${missing.length}）\n` + missing.join('\n')
  + `\n\n## 我们独有（${extra.length}）\n` + extra.join('\n') + '\n', 'utf8');
console.log(`\n完整清单 -> docs/upgrade-v1388-contract-gap.txt`);
