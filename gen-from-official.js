// 从官方 Go 后端提取所有方法签名，对比本项目的 Go 方法，找出签名不匹配/缺失的
const fs = require('fs');
const path = require('path');

const OFFICIAL = 'C:/Users/chenz/Desktop/DSH-deskop/reasonix-desktop/desktop';
const GO_DIR = 'C:/Users/chenz/Desktop/dsh-reasonix-wails';

// 提取官方所有 func (a *App) Xxx(...) 签名
const official = {};  // name -> {params, returns}
function walk(dir) {
  for (const e of fs.readdirSync(dir, { withFileTypes: true })) {
    const p = path.join(dir, e.name);
    if (e.isDirectory()) { if (e.name !== 'frontend' && !e.name.startsWith('.')) walk(p); }
    else if (e.name.endsWith('.go') && !e.name.endsWith('_test.go')) {
      const src = fs.readFileSync(p, 'utf8');
      const re = /func \(a \*App\) ([A-Z][A-Za-z0-9]+)\(([^)]*)\)\s*([^{]*)\{/g;
      let m;
      while ((m = re.exec(src)) !== null) {
        official[m[1]] = { params: m[2].trim(), returns: m[3].trim() };
      }
    }
  }
}
walk(OFFICIAL);
console.log(`官方方法数: ${Object.keys(official).length}`);

// 本项目 Go 方法
const mine = new Set();
for (const f of fs.readdirSync(GO_DIR)) {
  if (!f.endsWith('.go') || f.endsWith('_test.go')) continue;
  const src = fs.readFileSync(path.join(GO_DIR, f), 'utf8');
  const re = /func \(a \*App\) ([A-Z][A-Za-z0-9]+)\(/g;
  let m;
  while ((m = re.exec(src)) !== null) mine.add(m[1]);
}

// 前端调用的方法（从之前提取，重新算）
const FRONTEND = 'C:/Users/chenz/Desktop/DSH-deskop/reasonix-desktop/desktop/frontend/src';
const frontend = new Set();
function walkF(dir) {
  for (const e of fs.readdirSync(dir, { withFileTypes: true })) {
    const p = path.join(dir, e.name);
    if (e.isDirectory()) { if (e.name !== '__tests__' && e.name !== 'node_modules') walkF(p); }
    else if (/\.(ts|tsx)$/.test(e.name)) {
      const src = fs.readFileSync(p, 'utf8');
      const re = /app\.([A-Z][A-Za-z0-9]+)\(/g;
      let m;
      while ((m = re.exec(src)) !== null) frontend.add(m[1]);
    }
  }
}
walkF(FRONTEND);

// 前端调用 + 官方有 + 我缺失的
const missing = [...frontend].filter(m => official[m] && !mine.has(m)).sort();
console.log(`前端调用 ${frontend.size}，官方有 ${Object.keys(official).length}，我缺失（前端∩官方-我的）: ${missing.length}`);

// 输出缺失方法 + 官方签名
for (const m of missing) {
  const o = official[m];
  console.log(`${m}(${o.params}) -> ${o.returns}`);
}
fs.writeFileSync(path.join(GO_DIR, 'missing-official.txt'), missing.map(m => `${m}\t${official[m].params}\t${official[m].returns}`).join('\n'), 'utf8');
