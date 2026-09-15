// ws-capture.js — 抓取 DSH events.mux 的原始帧，用于确认后台任务(session/jobs)等数据结构。
// 用法：node scripts/ws-capture.js [秒数] [过滤关键字]
'use strict';
const SECONDS = Number(process.argv[2] || 8);
const FILTER = process.argv[3] || '';

const seen = {};
let jobsSample = null;
let total = 0;

const ws = new WebSocket('ws://127.0.0.1:3080/api/events.mux');
ws.addEventListener('open', () => {
  console.log(`[ws] connected, capturing ${SECONDS}s ...`);
});
ws.addEventListener('error', (e) => {
  console.error('[ws] error: ' + (e && e.message ? e.message : 'unknown'));
});
ws.addEventListener('message', (ev) => {
  total++;
  let obj;
  try { obj = JSON.parse(ev.data); } catch (e) { return; }
  const method = obj.method || obj.type || '(no-method)';
  const key = String(method);
  seen[key] = (seen[key] || 0) + 1;
  if (key.indexOf('job') >= 0 && !jobsSample) {
    jobsSample = obj;
  }
  if (FILTER && JSON.stringify(obj).indexOf(FILTER) >= 0 && seen['__filter__'] === undefined) {
    seen['__filter__'] = 1;
    console.log('--- FILTER HIT (' + FILTER + ') ---');
    console.log(JSON.stringify(obj, null, 2).slice(0, 4000));
  }
});

setTimeout(() => {
  console.log('\n[ws] frames total=' + total);
  console.log('[ws] method histogram:');
  Object.keys(seen).sort((a, b) => seen[b] - seen[a]).forEach(k => {
    if (k.startsWith('__')) return;
    console.log('   ' + k + '  x' + seen[k]);
  });
  if (jobsSample) {
    console.log('\n[ws] jobs frame sample:');
    console.log(JSON.stringify(jobsSample, null, 2).slice(0, 4000));
  } else {
    console.log('\n[ws] no jobs frame captured in this window');
  }
  ws.close();
  process.exit(0);
}, SECONDS * 1000);
