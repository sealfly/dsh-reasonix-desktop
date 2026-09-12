// apply-inline-editor.js — 把 scripts/dsh-inline-editor.js 同步内联进 frontend/dist/index.html。
// 前端 dist 是构建产物（上游更新会覆盖），所以内联必须是可重复执行的脚本步骤，
// 不能手工改 index.html（PRINCIPLES P6：DSH 适配不能被上游升级冲掉）。
// 用法：node scripts/apply-inline-editor.js
const fs = require('fs');
const path = require('path');

const here = __dirname;
const root = path.resolve(here, '..');
const srcPath = path.join(here, 'dsh-inline-editor.js');
const htmlPath = path.join(root, 'frontend', 'dist', 'index.html');

const src = fs.readFileSync(srcPath, 'utf8');
if (!src.includes('__DSH_INLINE_EDITOR__')) {
  console.error('[apply-inline-editor] 源脚本缺少 __DSH_INLINE_EDITOR__ 幂等标记，拒绝内联');
  process.exit(1);
}
let html = fs.readFileSync(htmlPath, 'utf8');

const MARK = '// dsh-inline-editor.js';
const CLOSE = '</' + 'script>';
const body = '<script>' + '\n' + src.replace(/<\/script>/g, '<\\/script>') + '\n' + CLOSE;

const at = html.indexOf(MARK);
if (at < 0) {
  const before = html.lastIndexOf('</body>');
  html = before >= 0 ? html.slice(0, before) + body + '\n' + html.slice(before) : html + '\n' + body;
  console.log('[apply-inline-editor] 首次内联（插入 ' + body.length + ' 字节）');
} else {
  const open = html.lastIndexOf('<script>', at);
  const close = html.indexOf(CLOSE, at);
  if (open < 0 || close < 0) {
    console.error('[apply-inline-editor] 找不到内联块的 <script> 边界');
    process.exit(1);
  }
  html = html.slice(0, open) + body + html.slice(close + CLOSE.length);
  console.log('[apply-inline-editor] 已替换既有内联块（' + body.length + ' 字节）');
}
fs.writeFileSync(htmlPath, html);
console.log('[apply-inline-editor] 写入 ' + htmlPath);
