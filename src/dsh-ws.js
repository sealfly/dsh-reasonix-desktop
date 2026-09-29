'use strict';
/**
 * ============================================================================
 * 极简 WebSocket 客户端（RFC 6455）—— 只做 DSH 流通道需要的那部分
 * ============================================================================
 *
 * 【为什么自己写】
 *   DSH 0.1.5-rc.2 的 Remote 流（`session/follow`、`$events`）走
 *   `ws://127.0.0.1:3080/api/remote.mux`，且该升级请求**同样要过 BrowserAuth**
 *   （dsh-api-gateway 在 upgrade handler 里调 connection.requestRejection），
 *   所以必须能自定义 Cookie 头。
 *
 *   - 浏览器 WebSocket API 不允许设置请求头 → 用不了；
 *   - Node 24 的全局 WebSocket 是浏览器 API 形状，同样不能设头；
 *   - `ws` 包本项目没有依赖，且打包后的应用不含 node_modules（package.json 的
 *     build.files 只收 src/renderer 等），require('ws') 在正式包中会失败。
 *
 *   因此这里实现一个最小客户端：握手 + 文本帧收发 + ping/pong + close。
 *   只支持文本帧与服务端→客户端的非分片/分片文本重组；二进制帧忽略。
 *
 * 【支持范围】
 *   - 客户端→服务端：文本、掩码（RFC 要求必须掩码）
 *   - 服务端→客户端：文本（含分片重组）、ping→pong、close
 *   - 帧长 7 位 / 16 位 / 64 位三种长度编码
 * ============================================================================
 */
const http = require('http');
const crypto = require('crypto');

/** RFC 6455 握手用的固定 GUID。 */
const WS_GUID = '258EAFA5-E914-47DA-95CA-C5AB0DC85B11';

const OP_CONT = 0x0;
const OP_TEXT = 0x1;
const OP_BINARY = 0x2;
const OP_CLOSE = 0x8;
const OP_PING = 0x9;
const OP_PONG = 0xa;

/**
 * 建立一个 WebSocket 连接。
 *
 * @param {string} url - ws:// 地址
 * @param {{headers?: Record<string,string>, timeoutMs?: number}} [options]
 * @returns {Promise<{send: (text: string) => void, close: (code?: number) => void,
 *                    onMessage: (cb: (text: string) => void) => void,
 *                    onClose: (cb: (info: {code?: number, reason?: string}) => void) => void,
 *                    onError: (cb: (err: Error) => void) => void}>}
 */
function connect(url, options = {}) {
  const target = new URL(url);
  const key = crypto.randomBytes(16).toString('base64');
  const headers = Object.assign({
    Connection: 'Upgrade',
    Upgrade: 'websocket',
    'Sec-WebSocket-Version': '13',
    'Sec-WebSocket-Key': key,
    Host: target.host,
  }, options.headers || {});

  return new Promise((resolve, reject) => {
    const req = http.request({
      host: target.hostname,
      port: target.port || 80,
      path: target.pathname + target.search,
      headers,
      timeout: options.timeoutMs || 15000,
    });

    let settled = false;
    const fail = (err) => { if (!settled) { settled = true; reject(err); } };

    req.on('response', (res) => {
      // 非 101：服务端拒绝升级（未鉴权时是 401 "unauthorized"）
      let body = '';
      res.on('data', (c) => { body += c; });
      res.on('end', () => fail(new Error('WebSocket upgrade rejected: HTTP ' + res.statusCode + ' ' + body.slice(0, 80))));
    });
    req.on('timeout', () => { req.destroy(); fail(new Error('WebSocket upgrade timeout')); });
    req.on('error', (e) => fail(e));

    req.on('upgrade', (res, socket, head) => {
      // 校验 Sec-WebSocket-Accept
      const expect = crypto.createHash('sha1').update(key + WS_GUID).digest('base64');
      if (res.headers['sec-websocket-accept'] !== expect) {
        socket.destroy();
        return fail(new Error('WebSocket handshake failed: bad Sec-WebSocket-Accept'));
      }
      settled = true;
      resolve(makeConnection(socket, head));
    });

    req.end();
  });
}

function makeConnection(socket, head) {
  const messageHandlers = [];
  const closeHandlers = [];
  const errorHandlers = [];
  let closed = false;
  let buffer = head && head.length ? Buffer.from(head) : Buffer.alloc(0);
  let fragmentOpcode = null;
  let fragments = [];

  const emitClose = (info) => {
    if (closed) return;
    closed = true;
    for (const cb of closeHandlers) { try { cb(info); } catch {} }
  };
  const emitError = (err) => { for (const cb of errorHandlers) { try { cb(err); } catch {} } };

  /** 发一个帧（客户端帧必须掩码）。 */
  function writeFrame(opcode, payload) {
    if (closed || socket.destroyed) return;
    const data = Buffer.isBuffer(payload) ? payload : Buffer.from(String(payload), 'utf8');
    const len = data.length;
    let header;
    if (len < 126) {
      header = Buffer.alloc(6);
      header[1] = 0x80 | len;
    } else if (len < 65536) {
      header = Buffer.alloc(8);
      header[1] = 0x80 | 126;
      header.writeUInt16BE(len, 2);
    } else {
      header = Buffer.alloc(14);
      header[1] = 0x80 | 127;
      header.writeBigUInt64BE(BigInt(len), 2);
    }
    header[0] = 0x80 | opcode; // FIN + opcode
    const mask = crypto.randomBytes(4);
    mask.copy(header, header.length - 4);
    const masked = Buffer.allocUnsafe(len);
    for (let i = 0; i < len; i++) masked[i] = data[i] ^ mask[i & 3];
    try { socket.write(Buffer.concat([header, masked])); } catch (e) { emitError(e); }
  }

  /** 从缓冲区里尽可能多地解析完整帧。 */
  function drain() {
    while (true) {
      if (buffer.length < 2) return;
      const b0 = buffer[0];
      const b1 = buffer[1];
      const fin = (b0 & 0x80) !== 0;
      const opcode = b0 & 0x0f;
      const masked = (b1 & 0x80) !== 0;
      let len = b1 & 0x7f;
      let offset = 2;
      if (len === 126) {
        if (buffer.length < offset + 2) return;
        len = buffer.readUInt16BE(offset);
        offset += 2;
      } else if (len === 127) {
        if (buffer.length < offset + 8) return;
        const big = buffer.readBigUInt64BE(offset);
        if (big > BigInt(Number.MAX_SAFE_INTEGER)) { emitError(new Error('WebSocket frame too large')); socket.destroy(); return; }
        len = Number(big);
        offset += 8;
      }
      let maskKey = null;
      if (masked) {
        if (buffer.length < offset + 4) return;
        maskKey = buffer.subarray(offset, offset + 4);
        offset += 4;
      }
      if (buffer.length < offset + len) return; // 帧还没收全
      let payload = buffer.subarray(offset, offset + len);
      if (maskKey) {
        const un = Buffer.allocUnsafe(len);
        for (let i = 0; i < len; i++) un[i] = payload[i] ^ maskKey[i & 3];
        payload = un;
      } else {
        payload = Buffer.from(payload);
      }
      buffer = buffer.subarray(offset + len);

      if (opcode === OP_PING) { writeFrame(OP_PONG, payload); continue; }
      if (opcode === OP_PONG) continue;
      if (opcode === OP_CLOSE) {
        writeFrame(OP_CLOSE, Buffer.alloc(0));
        try { socket.end(); } catch {}
        emitClose({ code: payload.length >= 2 ? payload.readUInt16BE(0) : undefined });
        return;
      }
      if (opcode === OP_BINARY) { fragmentOpcode = null; fragments = []; continue; } // 二进制不支持
      if (opcode === OP_CONT) {
        if (fragmentOpcode === null) continue; // 没有起始帧的续帧：忽略
        fragments.push(payload);
        if (fin) {
          const full = Buffer.concat(fragments).toString('utf8');
          const op = fragmentOpcode;
          fragmentOpcode = null;
          fragments = [];
          if (op === OP_TEXT) for (const cb of messageHandlers) { try { cb(full); } catch {} }
        }
        continue;
      }
      if (opcode === OP_TEXT) {
        if (fin) {
          const text = payload.toString('utf8');
          for (const cb of messageHandlers) { try { cb(text); } catch {} }
        } else {
          fragmentOpcode = OP_TEXT;
          fragments = [payload];
        }
        continue;
      }
      // 未知 opcode：忽略
    }
  }

  socket.on('data', (chunk) => {
    buffer = buffer.length ? Buffer.concat([buffer, chunk]) : chunk;
    try { drain(); } catch (e) { emitError(e); }
  });
  socket.on('close', () => emitClose({}));
  socket.on('error', (e) => { emitError(e); emitClose({}); });

  return {
    /** 发送一个文本帧。 */
    send(text) { writeFrame(OP_TEXT, text); },
    /** 主动关闭（发 close 帧后结束 socket）。 */
    close(code = 1000) {
      if (closed) return;
      const payload = Buffer.alloc(2);
      payload.writeUInt16BE(code, 0);
      writeFrame(OP_CLOSE, payload);
      try { socket.end(); } catch {}
      emitClose({ code });
    },
    onMessage(cb) { messageHandlers.push(cb); },
    onClose(cb) { closeHandlers.push(cb); },
    onError(cb) { errorHandlers.push(cb); },
    get closed() { return closed; },
  };
}

module.exports = { connect, WS_GUID };
