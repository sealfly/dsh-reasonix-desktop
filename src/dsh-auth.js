'use strict';
/**
 * ============================================================================
 * DSH 浏览器鉴权（BrowserAuth）适配层
 * ============================================================================
 *
 * 【为什么需要】
 *   DSH 0.1.5-rc.2 起，`/api` RPC 通道由 BrowserAuth 把关：
 *
 *     // dsh-client-connection/lib/index.js
 *     requestRejection(request) {
 *       if (!isTrustedApiRequest(request, this.trustedHosts)) return 403;
 *       return this.browserAuth.isAuthenticated(request) ? void 0 : 401;
 *     }
 *
 *   浏览器路径是「启动令牌换 Cookie」：`dsh web` 打印带 ?token=… 的 URL，
 *   浏览器 GET 它 → 303 + Set-Cookie → 之后所有 /api 请求带该 Cookie。
 *   本桥接层不加载 DSH 的 Web UI，拿不到那个令牌（且 dsh web 以 stdio:'ignore'
 *   拉起，连打印出来的 URL 都没捕获），因此历史上所有 RPC 都是 401。
 *
 * 【本层做法：自铸 Cookie】
 *   签名密钥由 DSH 持久化在 `<DSH_HOME>/.credentials.yaml`：
 *
 *     records:
 *       client-connection/browser-session:
 *         kind: grant
 *         payload:
 *           version: 1
 *           secret: <base64url，解码后 32 字节>
 *
 *   Cookie 契约（同 dsh-client-connection 的 BrowserAuth）：
 *     name  = "dsh-auth-" + base64url(sha256(authority))
 *     value = "v1." + base64url(JSON{version,authority,issuedAt,expiresAt})
 *                  + "." + base64url(HMAC-SHA256(secret, body))
 *     authority = new URL("http://" + Host 头).host，即 "127.0.0.1:3080"
 *
 *   用持久化密钥自铸，好处是**对已经在跑的 DSH 实例同样有效**（无需重启、
 *   无需捕获启动 URL）；代价是依赖 DSH 的 Cookie 格式，故实现全部集中在本文件。
 *
 * 【失败语义（原则 3：失败留痕、兜底不崩溃）】
 *   密钥缺失/格式异常时返回 null，调用方退回「不带 Cookie」的旧行为——
 *   请求会得到 401，但不会崩溃，且 console 有明确痕迹。
 * ============================================================================
 */
const fs = require('fs');
const os = require('os');
const path = require('path');
const crypto = require('crypto');

/** 与 DSH 侧常量保持一致（dsh-client-connection/lib/index.js）。 */
const TOKEN_QUERY = 'token';
const COOKIE_PREFIX = 'dsh-auth-';
const COOKIE_PAYLOAD_VERSION = 1;
const CREDENTIAL_RECORD = 'client-connection/browser-session';
const SECRET_BYTES = 32;
/**
 * Cookie 有效期。DSH 侧校验 `expiresAt - issuedAt <= cookieMaxAgeDays`（默认 30 天，
 * schema 下限 1 天），故取 1 天——对任何合法配置都成立，且每次启动重新铸造。
 */
const COOKIE_TTL_MS = 24 * 60 * 60 * 1000;

function b64url(buf) {
  return Buffer.from(buf).toString('base64').replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
}

/**
 * DSH home 解析：与 DSH 生态插件（@linxin666/dsh-usage 的 dsh-home）保持一致——
 * DSH_HOME 环境变量优先（支持 ~ 展开、相对路径按 cwd 解析），否则回退 <home>/.dsh。
 */
function resolveDshHome() {
  const raw = process.env.DSH_HOME;
  if (typeof raw === 'string' && raw.trim() !== '') {
    let p = raw.trim();
    if (p === '~') p = os.homedir();
    else if (p.startsWith('~/') || p.startsWith('~\\')) p = path.join(os.homedir(), p.slice(2));
    return path.isAbsolute(p) ? p : path.resolve(process.cwd(), p);
  }
  return path.join(os.homedir(), '.dsh');
}

/**
 * 从 .credentials.yaml 里取 browser-session 的签名密钥。
 *
 * 这里刻意不引入 YAML 依赖（本项目运行时零依赖）：目标结构固定且浅——
 * 找到 `client-connection/browser-session:` 顶层键，再取其块内（缩进更深）的
 * `secret:` 行。解析失败一律返回 undefined，由调用方兜底。
 *
 * @returns {Buffer|undefined} 32 字节密钥；缺失或格式不符时为 undefined。
 */
function readBrowserSessionSecret(home) {
  let yaml;
  try {
    yaml = fs.readFileSync(path.join(home || resolveDshHome(), '.credentials.yaml'), 'utf8');
  } catch {
    return undefined;
  }
  let inBlock = false;
  let blockIndent = -1;
  for (const line of yaml.split(/\r?\n/)) {
    if (/^\s*$/.test(line) || /^\s*#/.test(line)) continue;
    const indent = line.length - line.replace(/^\s+/, '').length;
    const trimmed = line.trim();
    if (!inBlock) {
      if (trimmed === CREDENTIAL_RECORD + ':') { inBlock = true; blockIndent = indent; }
      continue;
    }
    if (indent <= blockIndent) break; // 块结束（同级或更浅的键）
    const m = /^secret:\s*(.+?)\s*$/.exec(trimmed);
    if (m) {
      const raw = m[1].replace(/^['"]|['"]$/g, '');
      const buf = Buffer.from(raw, 'base64url');
      return buf.length === SECRET_BYTES ? buf : undefined;
    }
  }
  return undefined;
}

/**
 * 铸造一个针对 authority 的 Cookie 值。
 * @param {string} authority - `host:port`（必须与请求的 Host 头一致）
 * @param {Buffer} secret - 32 字节签名密钥
 * @param {number} [now] - 注入时钟（测试用）
 * @returns {{name: string, value: string, header: string, expiresAt: number}}
 */
function mintCookie(authority, secret, now) {
  const issuedAt = typeof now === 'number' ? now : Date.now();
  const expiresAt = issuedAt + COOKIE_TTL_MS;
  const name = COOKIE_PREFIX + b64url(crypto.createHash('sha256').update(authority).digest());
  const body = b64url(Buffer.from(JSON.stringify({
    version: COOKIE_PAYLOAD_VERSION, authority, issuedAt, expiresAt,
  }), 'utf8'));
  const sig = b64url(crypto.createHmac('sha256', secret).update(body).digest());
  const value = 'v1.' + body + '.' + sig;
  return { name, value, header: name + '=' + value, expiresAt };
}

/**
 * 建一个鉴权句柄：启动时铸一次 Cookie，供所有 /api 请求与 WS 升级复用。
 * 密钥缺失时 header 恒为 null，调用方按「无鉴权」继续（会 401，但不崩）。
 *
 * @param {number} port - DSH 端口（决定 Cookie 的 authority）
 * @returns {{header: () => (string|null), authority: string, ready: boolean,
 *            refresh: () => boolean, reason?: string}}
 */
function createAuth(port) {
  const authority = '127.0.0.1:' + port;
  let secret;
  let current = null;
  let reason;

  try {
    secret = readBrowserSessionSecret();
  } catch (e) {
    reason = 'secret read failed: ' + ((e && e.message) || e);
  }
  if (!secret) {
    reason = reason || 'browser-session secret not found in ' +
      path.join(resolveDshHome(), '.credentials.yaml') + ' (DSH 尚未在该 DSH_HOME 启动过？)';
    console.warn('[dsh-auth] 无法铸造鉴权 Cookie：' + reason + '；RPC 将退回无鉴权（预期 401）');
  } else {
    current = mintCookie(authority, secret);
    console.log('[dsh-auth] 已铸造 BrowserAuth Cookie（authority=' + authority +
      '，有效期至 ' + new Date(current.expiresAt).toISOString() + '）');
  }

  return {
    authority,
    get ready() { return current !== null; },
    /** 当前 Cookie 头（`name=value`），不可用时返回 null。 */
    header() { return current ? current.header : null; },
    /** 立刻重新铸造（长驻进程过期自愈 / 收到 401 时用）。 */
    refresh() {
      if (!secret) return false;
      current = mintCookie(authority, secret);
      return true;
    },
    reason,
  };
}

module.exports = {
  createAuth,
  resolveDshHome,
  readBrowserSessionSecret,
  mintCookie,
  b64url,
  TOKEN_QUERY,
  COOKIE_PREFIX,
  COOKIE_TTL_MS,
};
