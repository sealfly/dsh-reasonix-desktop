'use strict';
/**
 * ============================================================================
 * DshClient — DSH 后端 RPC 客户端（直连 127.0.0.1:3080）
 * DSH web service RPC client (talks to the local DSH backend on port 3080)
 * ============================================================================
 *
 * 【关键细节 Key details】
 *   - DSH 后端是本会话共享实例（端口 3080），多客户端、多路复用。杀它会杀本会话，
 *     所以这里只做"通用透传"（transparent passthrough），绝不主动关闭后端。
 *   - rpc(method, payload)：POST /api/<method>，body={type:'client-request',rpcId,...}；
 *     成功 resolve parsed.result.value，失败 reject error.message。
 *   - KNOWN_METHODS 只是"能力基线"提示（供前端发现能力），不是白名单——插件动态
 *     注册的方法即使不在表里也能通过 rpc() 透传调用（遵循 PRINCIPLES.md 原则 1：
 *     不限制 DSH 能力）。
 *   - 事件帧经 subscribeRaw 订阅（见下方），preload 再转成 window.runtime.EventsOn。
 */
// DSH web 服务 RPC 客户端（直连 3080 /api 协议）
// 设计原则（项目原则 1，见 PRINCIPLES.md）：DSH 后端是多客户端、多路复用的独立服务，这里只做"通用透传"，
// 不设方法白名单——任意 DSH 方法（含插件动态注册的）都能通过 rpc() 调，
// 任意事件帧都能通过 subscribeRaw() 收到，前端不阉割 DSH 的能力。
const http = require('http');
const { connect: wsConnect } = require('./dsh-ws');

// ---------------------------------------------------------------------------
// DSH 0.1.5-rc.2 的 /api 线协议（据 dsh-client-connection/lib/client.js 的
// createWebConnectionRpc 与 dsh-api-gateway 核对）：
//   1) 鉴权：BrowserAuth —— 每个 /api 请求必须带 Cookie（见 src/dsh-auth.js）；
//      缺 Cookie 时网关在进入路由前就返回 401 "unauthorized"。
//   2) 路径：POST /api/<namespace>/<method>，endpoint 是「斜杠分隔」的两段
//      （如 session/list），不是旧写法 session.list。
//   3) 信封：{ type:'client-request', rpcId, method:<endpoint>, payload:{ args } }
//      —— method 字段也填 endpoint；业务参数包在 payload.args 里。
//   4) args 的键名由每个 endpoint 的描述符决定（session/list 是 `_request`，
//      session/prompt 是 `request`），故这里带一次自动探测 + 缓存。
// ---------------------------------------------------------------------------

// 老式点号方法名 → 新 endpoint 的显式改名表。
// 命名空间复数化是 0.1.5 的主要变化（subagents / skills / goals / agentPresets），
// 另有少数方法被改名或合并。未列出的走默认翻译（a.b → a/b）。
const ENDPOINT_ALIASES = {
  // session：history/models 两个名字已不存在
  'session.history': 'session/page',
  'session.models': 'session/modelCatalog',
  // subagent → subagents（复数）
  'subagent.list': 'subagents/list',
  'subagent.history': 'subagents/list',
  'subagent.prompt': 'subagents/prompt',
  'subagent.interrupt': 'subagents/interruptByParent',
  // skill → skills
  'skill.list': 'skills/list',
  // agentPreset → agentPresets；remove → deletePreset
  'agentPreset.list': 'agentPresets/list',
  'agentPreset.select': 'agentPresets/select',
  'agentPreset.read': 'agentPresets/read',
  'agentPreset.copy': 'agentPresets/copy',
  'agentPreset.remove': 'agentPresets/deletePreset',
  // goal → goals
  'goal.create': 'goals/create',
  'goal.edit': 'goals/edit',
  'goal.pause': 'goals/pause',
  'goal.resume': 'goals/resume',
  'goal.complete': 'goals/complete',
  'goal.clear': 'goals/clear',
  'goal.get': 'goals/get',
  // llm：方法名变了
  'llm.providers': 'llm/listProviders',
  'llm.models': 'llm/listConfigurableProviders',
  // host.* 命名空间在 0.1.5 已不存在，目录选择器独立成 directoryPicker
  'host.pickDirectory': 'directoryPicker/pick',
  'host.listDirectory': 'directoryPicker/list',
  'host.createDirectory': 'directoryPicker/createDirectory',
};

// 描述符里 args 的键名探测缓存：endpoint → 键名
const ARG_KEY_CACHE = new Map();
// 首选键名（覆盖绝大多数 endpoint，命中可省一次往返）
const ARG_KEY_PREFERRED = ['_request', 'request'];
// 「裸 args」哨兵：args 本身就是业务对象，不再套参数名。
// 网关内部端点 $events/result 即此形状（它直接读 payload.args 作为事件回执文档）。
const ARG_KEY_BARE = '__bare__';
const BARE_ARGS_ENDPOINTS = new Set(['$events/result']);
// 方法名 → endpoint 的翻译：先查显式改名表，否则把点号换成斜杠（session.list → session/list）
function endpointOf(method) {
  if (ENDPOINT_ALIASES[method] !== undefined) return ENDPOINT_ALIASES[method];
  return String(method).replace(/\./g, '/');
}

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

// 逻辑流 id 计数器（mux 上每条 open 都要一个唯一 streamId）
let STREAM_SEQ = 0;

class DshClient {
  /**
   * @param {number} [port] - DSH 端口
   * @param {object|null} [auth] - src/dsh-auth.js 的 createAuth() 句柄（可为 null）
   */
  constructor(port = 3080, auth = null) {
    this.port = port;
    this.sock = null;
    this.auth = auth;
    this._mux = null;
    this._muxConnecting = null;
  }

  /** 单次 HTTP POST（不重试）。返回 { status, parsed, raw }。 */
  _post(endpoint, args, timeoutMs) {
    return new Promise((resolve, reject) => {
      const rpcId = 'dsh-' + Math.random().toString(36).slice(2, 10);
      const body = JSON.stringify({ type: 'client-request', rpcId, method: endpoint, payload: { args } });
      const headers = {
        'Content-Type': 'application/json',
        'Content-Length': Buffer.byteLength(body),
        // Host 必须显式给出：BrowserAuth 的 Cookie 是按 authority(=host:port) 绑定并校验的
        Host: '127.0.0.1:' + this.port,
      };
      if (this.auth) {
        const cookie = this.auth.header();
        if (cookie) headers.Cookie = cookie;
      }
      const req = http.request({
        host: '127.0.0.1', port: this.port, path: '/api/' + endpoint,
        method: 'POST', headers, timeout: timeoutMs,
      }, (res) => {
        let data = '';
        res.on('data', (c) => { data += c; });
        res.on('end', () => {
          let parsed;
          try { parsed = JSON.parse(data); } catch { parsed = undefined; }
          resolve({ status: res.statusCode, parsed, raw: data });
        });
      });
      req.on('timeout', () => req.destroy(new Error('RPC timeout: ' + endpoint)));
      req.on('error', reject);
      req.write(body);
      req.end();
    });
  }

  /**
   * 调一个 endpoint，自动适配 args 键名。
   *
   * args 的键名由每个 endpoint 的描述符决定（`_request` / `request` / 无参），
   * 网关校验不通过时会在 message 里点名缺失的键——据此探测一次并缓存，
   * 以后同一 endpoint 直接命中（原则 1：不硬编码能力清单，跟着 DSH 走）。
   */
  async _call(endpoint, payload, timeoutMs) {
    const business = (payload === undefined || payload === null) ? {} : payload;
    const cached = ARG_KEY_CACHE.get(endpoint);
    // null 代表「该 endpoint 不需要 args」；ARG_KEY_BARE 代表「args 即业务对象」
    const candidates = BARE_ARGS_ENDPOINTS.has(endpoint)
      ? [ARG_KEY_BARE]
      : (cached !== undefined ? [cached] : ARG_KEY_PREFERRED.concat([null]));
    let last;
    for (let i = 0; i < candidates.length; i++) {
      const key = candidates[i];
      const args = key === ARG_KEY_BARE ? business : (key === null ? {} : { [key]: business });
      let res = await this._post(endpoint, args, timeoutMs);
      // 401：Cookie 缺失/过期 —— 重铸一次并重试（只重试一次）
      if (res.status === 401 && this.auth && this.auth.refresh()) {
        res = await this._post(endpoint, args, timeoutMs);
      }
      last = res;
      if (res.status !== 200) return res; // 404 等交给上层报错，不再换键名
      const result = res.parsed && res.parsed.result;
      if (result && result.ok) { ARG_KEY_CACHE.set(endpoint, key); return res; }
      const err = result && result.error;
      if (!err || err.code !== 'gateway/arguments-invalid') return res; // 业务错误，不再换键名
      // 参数键名不对：把错误点名的键名提到下一位优先尝试
      ARG_KEY_CACHE.delete(endpoint);
      const missing = /missing "([^"]+)"/.exec(err.message || '');
      if (missing && candidates.indexOf(missing[1]) === -1) candidates.splice(i + 1, 0, missing[1]);
    }
    return last;
  }

  /**
   * 通用 RPC 透传。
   * @param {string} method - 点号方法名（如 session.list）或已是 endpoint（如 session/list）
   * @param {object} [payload] - 业务参数
   * @param {number} [timeoutMs]
   * @returns {Promise<any>} 成功时 resolve `result.value`
   */
  async rpc(method, payload, timeoutMs = 60000) {
    const endpoint = endpointOf(method);
    if (!endpoint) throw new Error('RPC method not available on DSH 0.1.5: ' + method);
    const res = await this._call(endpoint, payload, timeoutMs);
    if (res.status === 401) {
      throw new Error('RPC unauthorized (401)：DSH BrowserAuth 未通过，需检查 ' + (this.auth && this.auth.reason ? this.auth.reason : '鉴权 Cookie'));
    }
    if (res.status !== 200) throw new Error('RPC failed: HTTP ' + res.status + ' for ' + endpoint);
    if (!res.parsed) throw new Error('bad response');
    const result = res.parsed.result;
    if (result && result.ok) return result.value;
    throw new Error((result && result.error && result.error.message) || 'RPC failed');
  }

  // 通用 RPC 透传（与 rpc 同义，语义更明确：任意方法可调）
  call(method, payload, timeoutMs) {
    return this.rpc(method, payload, timeoutMs);
  }

  // -------------------------------------------------------------------------
  // Remote 流通道（WebSocket /api/remote.mux）
  // -------------------------------------------------------------------------
  // DSH 0.1.5-rc.2 的实时数据不再走旧的 events.mux（该路由已删除），改为在
  // 一条 WebSocket 上做「逻辑流多路复用」：
  //   客户端 → { type:'open', streamId, endpoint, payload }
  //            { type:'cancel', streamId }
  //   服务端 → { type:'item', streamId, value? }
  //            { type:'end', streamId }
  //            { type:'error', streamId, error:{code,message,details} }
  // 升级请求同样要过 BrowserAuth（见 dsh-api-gateway 的 upgrade handler），
  // 所以连接必须带 Cookie —— 这也是自实现 WS 客户端的原因（见 dsh-ws.js）。
  //
  // 会话实时内容来自 `session/follow` 流：开启帧是 snapshot（含 records 历史 +
  // projections），之后是 { type:'event', event } 与 { type:'assistant-stream', frame }。

  /** 确保 mux 连接存在（并发调用共享同一次连接）。 */
  _ensureMux() {
    if (this._mux && !this._mux.conn.closed) return Promise.resolve(this._mux);
    if (this._muxConnecting) return this._muxConnecting;
    const headers = {};
    if (this.auth) {
      const cookie = this.auth.header();
      if (cookie) headers.Cookie = cookie;
    }
    this._muxConnecting = wsConnect('ws://127.0.0.1:' + this.port + '/api/remote.mux', { headers })
      .then((conn) => {
        const mux = { conn, streams: new Map() };
        conn.onMessage((text) => {
          let frame;
          try { frame = JSON.parse(text); } catch { return; }
          const s = mux.streams.get(frame.streamId);
          if (!s) return;
          if (frame.type === 'item') { try { s.onItem(frame.value); } catch {} return; }
          if (frame.type === 'end') { mux.streams.delete(frame.streamId); try { s.onEnd(); } catch {} return; }
          if (frame.type === 'error') {
            mux.streams.delete(frame.streamId);
            const err = new Error((frame.error && frame.error.message) || 'stream error');
            err.code = frame.error && frame.error.code;
            try { s.onError(err); } catch {}
          }
        });
        conn.onClose((info) => {
          this._mux = null;
          // 连接断开：让所有在途流以错误收尾，由上层决定是否重开
          for (const [, s] of mux.streams) {
            try { s.onError(new Error('remote.mux closed' + (info && info.code ? ' code=' + info.code : ''))); } catch {}
          }
          mux.streams.clear();
        });
        conn.onError((e) => { console.warn('[dsh-client] remote.mux error:', e && e.message); });
        this._mux = mux;
        console.log('[dsh-client] remote.mux connected');
        return mux;
      })
      .catch((e) => { this._mux = null; throw e; })
      .finally(() => { this._muxConnecting = null; });
    return this._muxConnecting;
  }

  /**
   * 在 mux 上开一条逻辑流。
   * @param {string} endpoint - 如 'session/follow'、'$events'
   * @param {object} payload - 流开启载荷（如 { request: {...} }）
   * @param {{onItem: Function, onEnd?: Function, onError?: Function}} handlers
   * @returns {Promise<{streamId: string, cancel: Function}>}
   */
  async openStream(endpoint, payload, handlers) {
    const mux = await this._ensureMux();
    const streamId = 's' + (++STREAM_SEQ) + '-' + Math.random().toString(36).slice(2, 7);
    mux.streams.set(streamId, {
      onItem: handlers.onItem || (() => {}),
      onEnd: handlers.onEnd || (() => {}),
      onError: handlers.onError || (() => {}),
    });
    mux.conn.send(JSON.stringify({ type: 'open', streamId, endpoint, payload: payload || { args: {} } }));
    return {
      streamId,
      cancel: () => {
        if (!mux.streams.has(streamId)) return;
        mux.streams.delete(streamId);
        try { mux.conn.send(JSON.stringify({ type: 'cancel', streamId })); } catch {}
      },
    };
  }

  /** 关闭 mux（应用退出时调用）。 */
  closeMux() {
    if (this._mux) { try { this._mux.conn.close(); } catch {} this._mux = null; }
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

  // ⚠️ 已废弃（DEPRECATED）：DSH 0.1.5-rc.2 已删除 events.mux 路由，以下方法指向的
  // 旧地址不再存在。实时事件请改用 openStream()（session/follow、$events）——
  // main.js 的 startEventBridge() 就是新用法。保留仅为历史参考，勿在新代码中调用。
  //
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

  // 单连接多订阅：所有订阅者共享一条 WS；任一订阅触发连接，close() 统一关闭。
  _attach(handler) {
    if (typeof WebSocket === 'undefined') {
      console.warn('[dsh-client] WebSocket unavailable in this runtime — DSH 实时事件通道不可用');
      return;
    }
    this._handlers = this._handlers || new Set();
    this._handlers.add(handler);
    if (this.sock && this.sock.readyState === 1 /* OPEN */) return; // 已有连接
    if (this._retryTimer) return; // 已有重连在途
    const connect = () => {
      if (this._disposed) return;
      this._retryTimer = null;
      const sock = new WebSocket('ws://127.0.0.1:' + this.port + '/api/events.mux');
      this.sock = sock;
      sock.onopen = () => {
        console.log('[dsh-client] events.mux connected');
        this._retry = 0;
      };
      sock.onmessage = (ev) => {
        try {
          const frame = JSON.parse(ev.data);
          // 全量透传：不筛选帧类型，插件广播的自定义事件也能到达
          for (const h of this._handlers) { try { h(frame); } catch {} }
        } catch {}
      };
      sock.onerror = (e) => {
        // 握手失败/网络异常至少要留痕迹，避免"事件静默缺失"
        console.warn('[dsh-client] events.mux error:', e && (e.message || e.type) || 'ws error');
      };
      sock.onclose = () => {
        console.log('[dsh-client] events.mux closed');
        this.sock = null;
        if (this._disposed) return;
        // 指数退避重连（1s→2s→4s…封顶 30s），DSH 重启/网络抖动后事件流自动恢复
        const delay = Math.min(30000, 1000 * Math.pow(2, this._retry || 0));
        this._retry = (this._retry || 0) + 1;
        this._retryTimer = setTimeout(connect, delay);
      };
    };
    connect();
  }

  // 主动关闭（应用退出时调用，停止重连）
  close() {
    this._disposed = true;
    if (this._retryTimer) { clearTimeout(this._retryTimer); this._retryTimer = null; }
    if (this.sock) { try { this.sock.close(); } catch {} this.sock = null; }
    if (this._handlers) this._handlers.clear();
  }
}

module.exports = { DshClient };
