// cdp-probe.js — 通过 WebView2 远程调试端口（CDP）查询 DSH-ReasonixUI 前端的真实 DOM。
// 用法：node cdp-probe.js "表达式1" "表达式2" ...
//   或 node cdp-probe.js --file exprs.json
// 依赖：Node 22+ 内置全局 WebSocket；先以 WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS=--remote-debugging-port=9222 启动 app。
'use strict';

const PORT = process.env.CDP_PORT || 9222;

async function getTarget() {
  const res = await fetch(`http://127.0.0.1:${PORT}/json/list`);
  const list = await res.json();
  const page = list.find(t => t.type === 'page' && t.webSocketDebuggerUrl);
  if (!page) throw new Error('no page target: ' + JSON.stringify(list.map(t => t.type)));
  return page;
}

function evalIn(ws, id, expr) {
  return new Promise((resolve, reject) => {
    const onMsg = (ev) => {
      let msg;
      try { msg = JSON.parse(ev.data); } catch (e) { return; }
      if (msg.id !== id) return;
      ws.removeEventListener('message', onMsg);
      if (msg.error) return reject(new Error(JSON.stringify(msg.error)));
      const r = msg.result && msg.result.result;
      resolve(r && Object.prototype.hasOwnProperty.call(r, 'value') ? r.value : (r ? r.description : null));
    };
    ws.addEventListener('message', onMsg);
    ws.send(JSON.stringify({
      id: id,
      method: 'Runtime.evaluate',
      params: { expression: expr, returnByValue: true, awaitPromise: true }
    }));
    setTimeout(() => reject(new Error('timeout')), 15000);
  });
}

(async () => {
  const exprs = process.argv.slice(2);
  if (!exprs.length) {
    console.error('usage: node cdp-probe.js "<expr>" ["<expr>" ...]');
    process.exit(2);
  }
  const target = await getTarget();
  console.log('[cdp] target: ' + target.url);
  const ws = new WebSocket(target.webSocketDebuggerUrl);
  await new Promise((res, rej) => {
    ws.addEventListener('open', res);
    ws.addEventListener('error', rej);
    setTimeout(() => rej(new Error('ws timeout')), 10000);
  });
  let id = 1;
  for (const e of exprs) {
    try {
      const v = await evalIn(ws, id++, e);
      console.log('--- EXPR: ' + e.slice(0, 120));
      console.log(typeof v === 'string' ? v : JSON.stringify(v, null, 2));
    } catch (err) {
      console.log('--- EXPR: ' + e.slice(0, 120));
      console.log('ERROR: ' + err.message);
    }
  }
  ws.close();
  process.exit(0);
})().catch(err => { console.error('FATAL: ' + err.message); process.exit(1); });
