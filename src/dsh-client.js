'use strict';
// DSH web 服务 RPC 客户端（直连 3080 /api 协议）
// 设计原则：DSH 后端是多客户端、多路复用的独立服务，这里只做"通用透传"，
// 不设方法白名单——任意 DSH 方法（含插件动态注册的）都能通过 rpc() 调，
// 任意事件帧都能通过 subscribeRaw() 收到，前端不阉割 DSH 的能力。
const http = require('http');

// DSH 当前版本暴露的能力目录（namespace.method）。
// 这是"已知基线"：供前端发现能力；插件动态注册的方法即使不在表里，
// 也能通过通用 rpc() 直接透传调用，所以这份目录只是提示，不是白名单。
const KNOWN_METHODS = [
  // session
  'session.list', 'session.search', 'session.create', 'session.history',
  'session.models', 'session.selectModel', 'session.rename', 'session.fork',
  'session.prompt', 'session.attachment', 'session.updateQueue', 'session.cancel',
  // subagent
  'subagent.list', 'subagent.history', 'subagent.prompt', 'subagent.interrupt',
  // host
  'host.describe', 'host.pickDirectory', 'host.listDirectory', 'host.createDirectory', 'host.openPath',
  // workspace
  'workspace.list', 'workspace.create', 'workspace.rename', 'workspace.delete',
  'workspace.insertBefore', 'workspace.insertSessionBefore', 'workspace.archiveSession',
  // skill
  'skill.list',
  // agentPreset
  'agentPreset.list', 'agentPreset.select', 'agentPreset.read', 'agentPreset.copy',
  'agentPreset.openDocument', 'agentPreset.remove',
  // goal
  'goal.create', 'goal.edit', 'goal.pause', 'goal.resume', 'goal.complete', 'goal.clear',
  // settings
  'settings.describe', 'settings.openDocument', 'settings.update', 'settings.replace', 'settings.mutate',
  // credentials
  'credentials.describe', 'credentials.set', 'credentials.unset',
  // llm
  'llm.providers', 'llm.models', 'llm.discoverModels',
];

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

  // 通用 RPC 透传（与 rpc 同义，语义更明确：任意方法可调）
  call(method, payload, timeoutMs) {
    return this.rpc(method, payload, timeoutMs);
  }

  // 能力目录：已知方法清单（基线），供前端发现能力。
  // 注意：这是"提示"而非白名单——不在表里的方法仍可通过 rpc() 透传。
  catalog() {
    return {
      baseline: true,
      methods: KNOWN_METHODS.slice(),
      rpcOpen: true, // 通用 RPC 通道始终开放，插件方法可直接透传
    };
  }

  // 订阅 events.mux 原始帧（全量，不筛选）。
  // onFrame 收到完整帧对象：{ type, rpcId, method, payload }
  // 与 subscribe 的区别：subscribe 只透传 payload，subscribeRaw 透传完整帧（含 method 帧类型）。
  subscribeRaw(onFrame) {
    this._attach((frame) => onFrame(frame));
  }

  // 订阅 events.mux，只把 payload 传给回调（兼容旧用法）。
  // 注意：会丢弃 method（帧类型）字段；需要帧类型的场景请用 subscribeRaw。
  subscribe(onFrame) {
    this._attach((frame) => onFrame(frame.payload));
  }

  _attach(handler) {
    if (typeof WebSocket === 'undefined') return;
    const sock = new WebSocket('ws://127.0.0.1:' + this.port + '/api/events.mux');
    this.sock = sock;
    sock.onopen = () => console.log('[dsh-client] events.mux connected');
    sock.onmessage = (ev) => {
      try {
        const frame = JSON.parse(ev.data);
        // 全量透传：不筛选帧类型，插件广播的自定义事件也能到达
        handler(frame);
      } catch {}
    };
    sock.onclose = () => console.log('[dsh-client] events.mux closed');
  }
}

module.exports = { DshClient };
