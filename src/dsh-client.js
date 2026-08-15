'use strict';
// DSH web 服务 RPC 客户端（直连 3080 /api 协议）
const http = require('http');

class DshClient {
  constructor(port = 3080) {
    this.port = port;
    this.sock = null;
  }

  rpc(method, payload, timeoutMs = 60000) {
    return new Promise((resolve, reject) => {
      const rpcId = 'dsh-' + Math.random().toString(36).slice(2, 10);
      const body = JSON.stringify({ type: 'client-request', rpcId, method, payload });
      const req = http.request({
        host: '127.0.0.1', port: this.port, path: '/api/' + method,
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'Content-Length': Buffer.byteLength(body) },
        timeout: timeoutMs,
      }, (res) => {
        let data = '';
        res.on('data', (c) => { data += c; });
        res.on('end', () => {
          try {
            const parsed = JSON.parse(data);
            if (parsed && parsed.result && parsed.result.ok) resolve(parsed.result.value);
            else reject(new Error((parsed && parsed.result && parsed.result.error && parsed.result.error.message) || 'RPC failed'));
          } catch { reject(new Error('bad response')); }
        });
      });
      req.on('timeout', () => req.destroy(new Error('RPC timeout: ' + method)));
      req.on('error', reject);
      req.write(body);
      req.end();
    });
  }

  subscribe(onFrame) {
    if (typeof WebSocket === 'undefined') return;
    const sock = new WebSocket('ws://127.0.0.1:' + this.port + '/api/events.mux');
    this.sock = sock;
    sock.onopen = () => console.log('[dsh-client] events.mux connected');
    sock.onmessage = (ev) => {
      try {
        const frame = JSON.parse(ev.data);
        if (frame && frame.type === 'server-request') onFrame(frame.payload);
      } catch {}
    };
    sock.onclose = () => console.log('[dsh-client] events.mux closed');
  }
}

module.exports = { DshClient };