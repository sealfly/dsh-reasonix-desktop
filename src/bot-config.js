/**
 * bot-config.js — Reasonix IM Bot 网关配置的读写适配层
 *
 * 背景（实测结论，勿凭猜测改动）：
 *   Reasonix 前端「设置-机器人」页通过 GetSettings().bot 读、通过 SetBotSettings /
 *   SetBotSecret / ClearBotSecret 写。真正的配置不在 DSH 里，而在**官方 Reasonix 桌面
 *   应用的配置**：%APPDATA%\reasonix\config.toml 的 [bot] 段（含 [[bot.connections]]、
 *   [[bot.routes]]），密钥按 *_env 名字存放于 %APPDATA%\reasonix\.env。
 *   运行时的 IM 网关由官方 CLI 承载（reasonix bot start），本应用不启动任何 bot 进程；
 *   运行状态只能通过官方控制 API 读取：[bot.control].addr（默认 127.0.0.1:37913）
 *   的 GET /status，带 Authorization: Bearer <token_env 的值>。
 *
 * 写入策略（作者要求：原样保留格式 + 写前自动备份 + 原子替换）：
 *   1. 只改 [bot...] 段内的目标键；段外的每一个字节都不动。
 *   2. 标量键就地替换（保留原有缩进、`=` 间距与行尾注释）。
 *   3. 数组表（[[bot.connections]] / [[bot.routes]]）整块重写——它们是 bot 自己的键，
 *      仍在"只改 bot 段"的范围内。
 *   4. 写前把原文件复制成 config.toml.bak-dsh-reasonix-<时间戳>（每次写入一份）。
 *   5. 先写临时文件再 rename 覆盖，避免半截文件。
 *   6. 任何失败都留痕（返回 {ok:false,error} 并 console.error），绝不静默丢弃。
 */
'use strict';
const fs = require('fs');
const os = require('os');
const path = require('path');
const http = require('http');

// ---------------------------------------------------------------- 路径

/** 官方 Reasonix 配置目录。可用 REASONIX_HOME 覆盖（测试用）。 */
function reasonixHome() {
  if (process.env.REASONIX_HOME) return process.env.REASONIX_HOME;
  const appData = process.env.APPDATA || path.join(os.homedir(), 'AppData', 'Roaming');
  return path.join(appData, 'reasonix');
}
function configPath() { return path.join(reasonixHome(), 'config.toml'); }
function envPath() { return path.join(reasonixHome(), '.env'); }

// ---------------------------------------------------------------- TOML 行级工具

/** 行若为表头（[a.b] 或 [[a.b]]）返回表名，否则返回 null。 */
function tableNameOf(line) {
  const m = /^\s*\[\[?\s*([^\]]+?)\s*\]\]?\s*(?:#.*)?$/.exec(line);
  if (!m) return null;
  // 排除 `key = ["a"]` 这类以 [ 开头的值：表头必须整行只有表头（可带注释）
  if (/=/.test(line.replace(/#.*$/, ''))) return null;
  return m[1];
}
function isArrayTableLine(line) { return /^\s*\[\[/.test(line); }

/** 行内注释 '#' 的位置（双引号外的第一个 '#'），没有则 -1。 */
function commentIndex(v) {
  let inStr = false, esc = false;
  for (let i = 0; i < v.length; i++) {
    const c = v[i];
    if (inStr) {
      if (esc) { esc = false; continue; }
      if (c === '\\') { esc = true; continue; }
      if (c === '"') inStr = false;
      continue;
    }
    if (c === '"') { inStr = true; continue; }
    if (c === '#') return i;
  }
  return -1;
}

/** 去掉行内注释（# 在双引号外才算注释），返回 {body, comment}。 */
function splitComment(v) {
  const i = commentIndex(v);
  return i < 0 ? { body: v, comment: '' } : { body: v.slice(0, i), comment: v.slice(i) };
}

/**
 * 把一行 `key = value  # 注释` 的值换掉，**后缀（空格 + 注释）逐字节保留**。
 * 这样原地改写不会因为空格数不同而产生无意义的 diff。
 */
function replaceValueInLine(line, newLiteral) {
  const m = /^(\s*)([A-Za-z0-9_-]+)(\s*=\s*)(.*)$/.exec(line);
  if (!m) return null;
  const raw = m[4];
  const ci = commentIndex(raw);
  const head = ci >= 0 ? raw.slice(0, ci) : raw;
  const valueEnd = head.replace(/\s+$/, '').length;
  return m[1] + m[2] + m[3] + newLiteral + raw.slice(valueEnd);
}

function parseArrayLiteral(s) {
  const inner = s.slice(1, s.lastIndexOf(']'));
  const out = [];
  let cur = '', inStr = false, esc = false;
  for (let i = 0; i < inner.length; i++) {
    const c = inner[i];
    if (inStr) {
      cur += c;
      if (esc) { esc = false; continue; }
      if (c === '\\') { esc = true; continue; }
      if (c === '"') inStr = false;
      continue;
    }
    if (c === '"') { inStr = true; cur += c; continue; }
    if (c === ',') { if (cur.trim()) out.push(parseScalar(cur.trim())); cur = ''; continue; }
    cur += c;
  }
  if (cur.trim()) out.push(parseScalar(cur.trim()));
  return out;
}

function parseScalar(raw) {
  const s = String(raw).trim();
  if (s === 'true') return true;
  if (s === 'false') return false;
  if (/^[+-]?\d+$/.test(s)) return Number(s);
  if (/^[+-]?(\d+\.\d*|\.\d+)$/.test(s)) return Number(s);
  if (s.startsWith('"') && s.endsWith('"') && s.length >= 2) {
    try { return JSON.parse(s); } catch { return s.slice(1, -1); }
  }
  if (s.startsWith("'") && s.endsWith("'") && s.length >= 2) return s.slice(1, -1);
  if (s.startsWith('[')) return parseArrayLiteral(s);
  return s;
}

function literalOf(v) {
  if (typeof v === 'boolean') return v ? 'true' : 'false';
  if (typeof v === 'number' && Number.isFinite(v)) return String(v);
  if (Array.isArray(v)) return '[' + v.map((x) => literalOf(x)).join(', ') + ']';
  return JSON.stringify(String(v == null ? '' : v));
}

/** 读一个表下的所有简单键值。 */
function readTable(lines, startIdx, endIdx) {
  const out = {};
  for (let i = startIdx + 1; i < endIdx; i++) {
    const m = /^\s*([A-Za-z0-9_-]+)\s*=\s*(.*)$/.exec(lines[i]);
    if (!m) continue;
    const { body } = splitComment(m[2]);
    if (!body.trim()) continue;
    out[m[1]] = parseScalar(body);
  }
  return out;
}

/** 表块结束行（下一个表头所在行，或文件尾）。 */
function tableEnd(lines, headerIdx) {
  for (let i = headerIdx + 1; i < lines.length; i++) {
    if (tableNameOf(lines[i])) return i;
  }
  return lines.length;
}

/** 找出所有 bot 相关表头，返回 [bot] 段的行区间 {start,end}。 */
function findBotRegion(lines) {
  const idxs = [];
  for (let i = 0; i < lines.length; i++) {
    const t = tableNameOf(lines[i]);
    if (t && (t === 'bot' || t.startsWith('bot.') || t.startsWith('bot['))) idxs.push(i);
  }
  if (!idxs.length) return null;
  const start = idxs[0];
  let end = lines.length;
  for (let i = idxs[idxs.length - 1] + 1; i < lines.length; i++) {
    if (tableNameOf(lines[i])) { end = i; break; }
  }
  return { start, end };
}

// ---------------------------------------------------------------- 解析 → 前端形状

const CHANNEL_PLATFORMS = ['qq', 'feishu', 'weixin', 'dingtalk'];

function asArray(v) { return Array.isArray(v) ? v.map((x) => String(x)) : []; }
function asBool(v, d = false) { return typeof v === 'boolean' ? v : d; }
function asNum(v, d = 0) { const n = Number(v); return Number.isFinite(n) ? n : d; }
function asStr(v, d = '') { return v == null ? d : String(v); }

/** 读 .env（只解析 KEY=VALUE，忽略注释/空行）。 */
function readEnvFile() {
  const out = {};
  try {
    const txt = fs.readFileSync(envPath(), 'utf8');
    for (const line of txt.split('\n')) {
      const m = /^\s*(?:export\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(.*)$/.exec(line);
      if (!m) continue;
      let v = m[2].trim();
      if ((v.startsWith('"') && v.endsWith('"')) || (v.startsWith("'") && v.endsWith("'"))) v = v.slice(1, -1);
      out[m[1]] = v;
    }
  } catch { /* 不存在 .env 不算错误：视为没有任何密钥 */ }
  return out;
}

function secretIsSet(name, env) {
  const n = String(name || '').trim();
  if (!n) return false;
  return Boolean((env || readEnvFile())[n]);
}

/** 从解析出的原始表构造前端 normalizeBotSettings 期待的 camelCase 结构。 */
function toFrontendBot(raw, opts = {}) {
  const env = opts.env || readEnvFile();
  const base = raw.base || {};
  const selfUserIds = raw.selfUserIds || {};
  const control = raw.control || {};
  const pairing = raw.pairing || {};
  const allowlist = raw.allowlist || {};

  const allowlistOut = {
    enabled: asBool(allowlist.enabled, true),
    allowAll: asBool(allowlist.allow_all, false),
  };
  for (const p of CHANNEL_PLATFORMS.slice(0, 3)) {
    const P = p.charAt(0).toUpperCase() + p.slice(1);
    allowlistOut[`${p}Users`] = asArray(allowlist[`${p}_users`]);
    allowlistOut[`${p}Approvers`] = asArray(allowlist[`${p}_approvers`]);
    allowlistOut[`${p}Admins`] = asArray(allowlist[`${p}_admins`]);
    allowlistOut[`${p}Groups`] = asArray(allowlist[`${p}_groups`]);
    void P;
  }

  const qqRaw = raw.channels.qq || {};
  const qqAccess = raw.qqAccess || {};
  const qq = {
    enabled: asBool(qqRaw.enabled, false),
    appId: asStr(qqRaw.app_id),
    appSecretEnv: asStr(qqRaw.app_secret_env, 'QQ_BOT_APP_SECRET'),
    secretSet: secretIsSet(qqRaw.app_secret_env || 'QQ_BOT_APP_SECRET', env),
    sandbox: asBool(qqRaw.sandbox, false),
    model: asStr(qqRaw.model),
    toolApprovalMode: asStr(qqRaw.tool_approval_mode, 'ask'),
    workspaceRoot: asStr(qqRaw.workspace_root),
    access: {
      enabled: asBool(qqAccess.enabled, true),
      allowAll: asBool(qqAccess.allow_all, false),
      pairingEnabled: asBool(qqAccess.pairing_enabled, true),
      users: asArray(qqAccess.users),
      groups: asArray(qqAccess.groups),
      approvers: asArray(qqAccess.approvers),
      admins: asArray(qqAccess.admins),
    },
  };

  const feishuRaw = raw.channels.feishu || {};
  const feishu = {
    enabled: asBool(feishuRaw.enabled, false),
    domain: asStr(feishuRaw.domain, 'feishu'),
    appId: asStr(feishuRaw.app_id),
    appSecretEnv: asStr(feishuRaw.app_secret_env, 'FEISHU_BOT_APP_SECRET'),
    secretSet: secretIsSet(feishuRaw.app_secret_env || 'FEISHU_BOT_APP_SECRET', env),
    verificationToken: asStr(feishuRaw.verification_token),
    mode: asStr(feishuRaw.mode, 'webhook'),
    webhookPort: asNum(feishuRaw.webhook_port, 8080),
    requireMention: asBool(feishuRaw.require_mention, true),
  };

  const weixinRaw = raw.channels.weixin || {};
  const weixin = {
    enabled: asBool(weixinRaw.enabled, false),
    accountId: asStr(weixinRaw.account_id, 'default'),
    tokenEnv: asStr(weixinRaw.token_env, 'WEIXIN_BOT_TOKEN'),
    tokenSet: secretIsSet(weixinRaw.token_env || 'WEIXIN_BOT_TOKEN', env),
    apiBase: asStr(weixinRaw.api_base, 'https://ilinkai.weixin.qq.com'),
  };

  const connections = (raw.connections || []).map((c) => {
    const provider = asStr(c.provider).trim();
    const domain = asStr(c.domain, provider).trim();
    const cred = c.credential || {};
    const access = c.access || {};
    const statuses = opts.statuses || {};
    const id = asStr(c.id).trim() || [provider, domain].filter(Boolean).join('-');
    const st = statuses[id];
    return {
      id,
      provider,
      domain,
      label: asStr(c.label || c.name),
      enabled: asBool(c.enabled, false),
      status: st ? String(st.status || st) : 'disconnected',
      model: asStr(c.model),
      toolApprovalMode: asStr(c.tool_approval_mode, 'ask'),
      workspaceRoot: asStr(c.workspace_root),
      access: {
        enabled: asBool(access.enabled, true),
        allowAll: asBool(access.allow_all, false),
        pairingEnabled: asBool(access.pairing_enabled, true),
        users: asArray(access.users),
        groups: asArray(access.groups),
        approvers: asArray(access.approvers),
        admins: asArray(access.admins),
      },
      credential: {
        appId: asStr(cred.app_id),
        appSecretEnv: asStr(cred.app_secret_env),
        accountId: asStr(cred.account_id),
        tokenEnv: asStr(cred.token_env),
        secretSet: secretIsSet(cred.app_secret_env, env) || secretIsSet(cred.token_env, env),
      },
      sessionMappings: (c.session_mappings || []).map((m) => ({
        remoteId: asStr(m.remote_id),
        sessionId: asStr(m.session_id),
        sessionSource: asStr(m.session_source),
        chatType: asStr(m.chat_type),
        userId: asStr(m.user_id),
        threadId: asStr(m.thread_id),
        scope: asStr(m.scope, 'global'),
        workspaceRoot: asStr(m.workspace_root),
        updatedAt: asStr(m.updated_at),
      })),
      lastError: asStr(c.last_error),
      createdAt: asStr(c.created_at),
      updatedAt: asStr(c.updated_at),
    };
  });

  const routes = (raw.routes || []).map((r) => ({
    connectionId: asStr(r.connection_id),
    platform: asStr(r.platform),
    chatType: asStr(r.chat_type),
    chatId: asStr(r.chat_id),
    userId: asStr(r.user_id),
    threadId: asStr(r.thread_id),
    model: asStr(r.model),
    toolApprovalMode: asStr(r.tool_approval_mode, 'ask'),
    workspaceRoot: asStr(r.workspace_root),
  }));

  return {
    enabled: asBool(base.enabled, false),
    model: asStr(base.model),
    toolApprovalMode: asStr(base.tool_approval_mode, 'ask'),
    maxSteps: asNum(base.max_steps, 25),
    debounceMs: asNum(base.debounce_ms, 1500),
    queueMode: asStr(base.queue_mode, 'steer'),
    queueCap: asNum(base.queue_cap, 20),
    queueDrop: asStr(base.queue_drop, 'summarize'),
    ignoreSelfMessages: asBool(base.ignore_self_messages, true),
    selfUserIds: {
      qq: asArray(selfUserIds.qq),
      feishu: asArray(selfUserIds.feishu),
      weixin: asArray(selfUserIds.weixin),
    },
    control: {
      enabled: asBool(control.enabled, false),
      addr: asStr(control.addr, '127.0.0.1:37913'),
      tokenEnv: asStr(control.token_env, 'REASONIX_BOT_CONTROL_TOKEN'),
    },
    pairing: {
      enabled: asBool(pairing.enabled, true),
      requestTtlMinutes: asNum(pairing.request_ttl_minutes, 60),
      maxPendingPerPlatform: asNum(pairing.max_pending_per_platform, 3),
    },
    routes,
    allowlist: allowlistOut,
    qq,
    feishu,
    weixin,
    connections,
  };
}

/** 解析 config.toml 全文 → 原始（snake_case）bot 结构。 */
function parseBotRaw(text) {
  const lines = text.split('\n');
  const region = findBotRegion(lines);
  const raw = {
    present: Boolean(region),
    base: {}, selfUserIds: {}, control: {}, pairing: {}, allowlist: {},
    channels: {}, qqAccess: {}, connections: [], routes: [],
  };
  if (!region) return raw;

  const simpleTables = {
    bot: 'base',
    'bot.self_user_ids': 'selfUserIds',
    'bot.control': 'control',
    'bot.pairing': 'pairing',
    'bot.allowlist': 'allowlist',
    'bot.qq.access': 'qqAccess',
  };
  for (let i = region.start; i < region.end; i++) {
    const name = tableNameOf(lines[i]);
    if (!name || isArrayTableLine(lines[i])) continue;
    const end = Math.min(tableEnd(lines, i), region.end);
    const vals = readTable(lines, i, end);
    if (simpleTables[name]) { Object.assign(raw[simpleTables[name]], vals); continue; }
    const ch = /^bot\.(qq|feishu|weixin|dingtalk)$/.exec(name);
    if (ch) { raw.channels[ch[1]] = vals; continue; }
  }

  // 数组表：[[bot.connections]] / [[bot.routes]]（含各自的子表 credential/access/session_mappings）
  const collectArray = (arrayName) => {
    const items = [];
    for (let i = region.start; i < region.end; i++) {
      if (!isArrayTableLine(lines[i])) continue;
      const n = tableNameOf(lines[i]);
      if (n !== arrayName) continue;
      const item = readTable(lines, i, Math.min(tableEnd(lines, i), region.end));
      // 子表
      for (let j = i + 1; j < region.end; j++) {
        const sub = tableNameOf(lines[j]);
        if (!sub) { if (isArrayTableLine(lines[j])) break; continue; }
        if (isArrayTableLine(lines[j])) break;
        if (sub.startsWith(arrayName + '.')) {
          const leaf = sub.slice(arrayName.length + 1);
          const end = Math.min(tableEnd(lines, j), region.end);
          const vals = readTable(lines, j, end);
          if (leaf === 'credential') item.credential = vals;
          else if (leaf === 'access') item.access = vals;
          else if (leaf === 'session_mappings' || leaf.startsWith('session_mappings.')) {
            item.session_mappings = Object.assign(item.session_mappings || {}, vals);
          } else item[leaf.replace(/\./g, '_')] = vals;
        }
      }
      // session_mappings 在官方 schema 里可能是 [bot.connections.session_mappings.remoteId] 形式
      items.push(item);
    }
    return items;
  };
  raw.connections = collectArray('bot.connections');
  raw.routes = collectArray('bot.routes');
  return raw;
}

// ---------------------------------------------------------------- 写入（格式保留）

function patchScalar(lines, tablePath, key, value) {
  const headerIdx = lines.findIndex((l) => tableNameOf(l) === tablePath && !isArrayTableLine(l));
  if (headerIdx < 0) return false;
  const end = tableEnd(lines, headerIdx);
  for (let i = headerIdx + 1; i < end; i++) {
    const m = /^(\s*)([A-Za-z0-9_-]+)(\s*=\s*)(.*)$/.exec(lines[i]);
    if (!m || m[2] !== key) continue;
    const replaced = replaceValueInLine(lines[i], literalOf(value));
    if (replaced != null) lines[i] = replaced;
    return true;
  }
  let ins = end;
  while (ins > headerIdx + 1 && lines[ins - 1].trim() === '') ins--;
  lines.splice(ins, 0, key + ' = ' + literalOf(value));
  return true;
}

/** 该表里是否已经存在这个键（用于"没变就不新增"判断）。 */
function hasKey(lines, tablePath, key) {
  const headerIdx = lines.findIndex((l) => tableNameOf(l) === tablePath && !isArrayTableLine(l));
  if (headerIdx < 0) return false;
  const end = tableEnd(lines, headerIdx);
  for (let i = headerIdx + 1; i < end; i++) {
    const m = /^\s*([A-Za-z0-9_-]+)\s*=/.exec(lines[i]);
    if (m && m[1] === key) return true;
  }
  return false;
}

/** 只在"键已存在"或"值非默认"时才写，避免空保存给配置添垃圾行。 */
function patchScalarIfNeeded(lines, tablePath, key, value, isDefault) {
  if (isDefault && !hasKey(lines, tablePath, key)) return false;
  return patchScalar(lines, tablePath, key, value);
}

function replaceArrayBlock(lines, arrayPath, blockLines) {
  const starts = [];
  for (let i = 0; i < lines.length; i++) {
    if (isArrayTableLine(lines[i]) && tableNameOf(lines[i]) === arrayPath) starts.push(i);
  }
  if (!starts.length) return false;
  const start = starts[0];
  let end = lines.length;
  for (let i = start + 1; i < lines.length; i++) {
    const t = tableNameOf(lines[i]);
    if (t && !(t === arrayPath || t.startsWith(arrayPath + '.'))) { end = i; break; }
  }
  lines.splice(start, end - start, ...blockLines);
  return true;
}

function serializeConnections(conns) {
  const out = [];
  for (const c of conns) {
    out.push('[[bot.connections]]');
    if (c.id) out.push('id = ' + literalOf(c.id));
    out.push('provider = ' + literalOf(c.provider || ''));
    if (c.domain) out.push('domain = ' + literalOf(c.domain));
    if (c.label) out.push('name = ' + literalOf(c.label));
    out.push('enabled = ' + literalOf(Boolean(c.enabled)));
    if (c.model) out.push('model = ' + literalOf(c.model));
    if (c.toolApprovalMode && c.toolApprovalMode !== 'ask') out.push('tool_approval_mode = ' + literalOf(c.toolApprovalMode));
    if (c.workspaceRoot) out.push('workspace_root = ' + literalOf(c.workspaceRoot));

    const cred = c.credential || {};
    if (cred.appId || cred.appSecretEnv || cred.accountId || cred.tokenEnv) {
      out.push('');
      out.push('[bot.connections.credential]');
      if (cred.appId) out.push('app_id = ' + literalOf(cred.appId));
      if (cred.appSecretEnv) out.push('app_secret_env = ' + literalOf(cred.appSecretEnv));
      if (cred.accountId) out.push('account_id = ' + literalOf(cred.accountId));
      if (cred.tokenEnv) out.push('token_env = ' + literalOf(cred.tokenEnv));
    }
    const acc = c.access;
    if (acc) {
      out.push('');
      out.push('[bot.connections.access]');
      out.push('enabled = ' + literalOf(acc.enabled !== false));
      out.push('allow_all = ' + literalOf(Boolean(acc.allowAll)));
      out.push('pairing_enabled = ' + literalOf(acc.pairingEnabled !== false));
      for (const k of ['users', 'groups', 'approvers', 'admins']) {
        if (Array.isArray(acc[k]) && acc[k].length) out.push(k + ' = ' + literalOf(acc[k]));
      }
    }
    out.push('');
  }
  return out;
}

function serializeRoutes(routes) {
  const out = [];
  for (const r of routes || []) {
    out.push('[[bot.routes]]');
    if (r.connectionId) out.push('connection_id = ' + literalOf(r.connectionId));
    if (r.platform) out.push('platform = ' + literalOf(r.platform));
    if (r.chatType) out.push('chat_type = ' + literalOf(r.chatType));
    if (r.chatId) out.push('chat_id = ' + literalOf(r.chatId));
    if (r.userId) out.push('user_id = ' + literalOf(r.userId));
    if (r.threadId) out.push('thread_id = ' + literalOf(r.threadId));
    if (r.model) out.push('model = ' + literalOf(r.model));
    if (r.toolApprovalMode && r.toolApprovalMode !== 'ask') out.push('tool_approval_mode = ' + literalOf(r.toolApprovalMode));
    if (r.workspaceRoot) out.push('workspace_root = ' + literalOf(r.workspaceRoot));
    out.push('');
  }
  return out;
}

/**
 * 用前端 draft（camelCase）生成新的 config.toml 全文。
 * @returns {{ok:boolean, text?:string, changed?:boolean, error?:string}}
 */
function patchConfigText(original, draft) {
  if (!original || typeof original !== 'string') return { ok: false, error: 'config.toml 内容为空' };
  const lines = original.split('\n');
  const region = findBotRegion(lines);
  if (!region) return { ok: false, error: 'config.toml 里找不到 [bot] 段，拒绝写入（避免破坏未知结构）' };

  const changedBefore = lines.slice();

  // [bot]
  const base = {
    enabled: Boolean(draft.enabled),
    model: draft.model == null ? '' : String(draft.model),
    tool_approval_mode: String(draft.toolApprovalMode || 'ask'),
    max_steps: Number(draft.maxSteps) || 0,
    debounce_ms: Number(draft.debounceMs) || 0,
    queue_mode: String(draft.queueMode || 'steer'),
    queue_cap: Number(draft.queueCap) || 0,
    queue_drop: String(draft.queueDrop || 'summarize'),
    ignore_self_messages: draft.ignoreSelfMessages !== false,
  };
  for (const [k, v] of Object.entries(base)) {
    // model 为空表示"用默认模型"：配置里原本没有这个键时就不要凭空加一行。
    patchScalarIfNeeded(lines, 'bot', k, v, k === 'model' && v === '');
  }

  const su = draft.selfUserIds || {};
  for (const p of ['qq', 'feishu', 'weixin']) patchScalar(lines, 'bot.self_user_ids', p, asArray(su[p]));

  const ctl = draft.control || {};
  patchScalar(lines, 'bot.control', 'enabled', Boolean(ctl.enabled));
  patchScalar(lines, 'bot.control', 'addr', String(ctl.addr || '127.0.0.1:37913'));
  patchScalar(lines, 'bot.control', 'token_env', String(ctl.tokenEnv || 'REASONIX_BOT_CONTROL_TOKEN'));

  const pr = draft.pairing || {};
  patchScalar(lines, 'bot.pairing', 'enabled', pr.enabled !== false);
  patchScalar(lines, 'bot.pairing', 'request_ttl_minutes', Number(pr.requestTtlMinutes) || 0);
  patchScalar(lines, 'bot.pairing', 'max_pending_per_platform', Number(pr.maxPendingPerPlatform) || 0);

  const al = draft.allowlist || {};
  patchScalar(lines, 'bot.allowlist', 'enabled', al.enabled !== false);
  patchScalar(lines, 'bot.allowlist', 'allow_all', Boolean(al.allowAll));
  for (const p of ['qq', 'feishu', 'weixin']) {
    const P = p.charAt(0).toUpperCase() + p.slice(1);
    for (const kind of ['Users', 'Approvers', 'Admins', 'Groups']) {
      patchScalar(lines, 'bot.allowlist', `${p}_${kind.toLowerCase()}`, asArray(al[p + kind]));
      void P;
    }
  }

  const qq = draft.qq || {};
  patchScalar(lines, 'bot.qq', 'enabled', Boolean(qq.enabled));
  patchScalar(lines, 'bot.qq', 'app_id', String(qq.appId || ''));
  patchScalar(lines, 'bot.qq', 'app_secret_env', String(qq.appSecretEnv || 'QQ_BOT_APP_SECRET'));
  patchScalar(lines, 'bot.qq', 'sandbox', Boolean(qq.sandbox));
  // 下面三项：配置里原本没有、且值就是默认值时不新增（避免空保存添行）
  patchScalarIfNeeded(lines, 'bot.qq', 'model', String(qq.model || ''), !qq.model);
  patchScalarIfNeeded(lines, 'bot.qq', 'tool_approval_mode', String(qq.toolApprovalMode || 'ask'),
    !qq.toolApprovalMode || qq.toolApprovalMode === 'ask');
  patchScalarIfNeeded(lines, 'bot.qq', 'workspace_root', String(qq.workspaceRoot || ''), !qq.workspaceRoot);
  const qa = qq.access;
  if (qa) {
    const qaIsDefault = qa.enabled !== false && !qa.allowAll && qa.pairingEnabled !== false
      && !asArray(qa.users).length && !asArray(qa.groups).length
      && !asArray(qa.approvers).length && !asArray(qa.admins).length;
    const qaExists = lines.some((l) => tableNameOf(l) === 'bot.qq.access');
    // 全默认值且配置里本来就没有 [bot.qq.access] 时，不要凭空建这个表
    if (qaExists || !qaIsDefault) {
      if (!qaExists) {
        const qqHdr = lines.findIndex((l) => tableNameOf(l) === 'bot.qq');
        if (qqHdr >= 0) lines.splice(tableEnd(lines, qqHdr), 0, '', '[bot.qq.access]');
      }
      patchScalar(lines, 'bot.qq.access', 'enabled', qa.enabled !== false);
      patchScalar(lines, 'bot.qq.access', 'allow_all', Boolean(qa.allowAll));
      patchScalar(lines, 'bot.qq.access', 'pairing_enabled', qa.pairingEnabled !== false);
      for (const k of ['users', 'groups', 'approvers', 'admins']) {
        if (Array.isArray(qa[k]) && qa[k].length) patchScalar(lines, 'bot.qq.access', k, asArray(qa[k]));
      }
    }
  }

  // 遗留的 [bot.feishu] / [bot.weixin] 表：draft 里带了就同步，避免两套模型漂移
  if (draft.feishu) {
    const f = draft.feishu;
    if (lines.some((l) => tableNameOf(l) === 'bot.feishu')) {
      patchScalar(lines, 'bot.feishu', 'enabled', Boolean(f.enabled));
      if (f.appId != null) patchScalar(lines, 'bot.feishu', 'app_id', String(f.appId));
      if (f.domain) patchScalar(lines, 'bot.feishu', 'domain', String(f.domain));
      if (f.appSecretEnv) patchScalar(lines, 'bot.feishu', 'app_secret_env', String(f.appSecretEnv));
      if (f.mode) patchScalar(lines, 'bot.feishu', 'mode', String(f.mode));
      if (f.webhookPort != null) patchScalar(lines, 'bot.feishu', 'webhook_port', Number(f.webhookPort) || 0);
      if (f.requireMention != null) patchScalar(lines, 'bot.feishu', 'require_mention', Boolean(f.requireMention));
    }
  }
  if (draft.weixin) {
    const w = draft.weixin;
    if (lines.some((l) => tableNameOf(l) === 'bot.weixin')) {
      patchScalar(lines, 'bot.weixin', 'enabled', Boolean(w.enabled));
      if (w.accountId) patchScalar(lines, 'bot.weixin', 'account_id', String(w.accountId));
      if (w.tokenEnv) patchScalar(lines, 'bot.weixin', 'token_env', String(w.tokenEnv));
      if (w.apiBase) patchScalar(lines, 'bot.weixin', 'api_base', String(w.apiBase));
    }
  }

  // 数组表：整块重写（仍在 bot 段内）
  // 注意：块本身以空行结尾，插入点必须是 bot 段末尾（下一张表之前），这样"插入"与
  // "替换"产生的字节完全一致，重复调用才会幂等（changed=false）。
  const connBlock = serializeConnections(Array.isArray(draft.connections) ? draft.connections : []);
  if (replaceArrayBlock(lines, 'bot.connections', connBlock)) {
    // 已替换
  } else if (connBlock.length) {
    const r2 = findBotRegion(lines);
    lines.splice(r2 ? r2.end : lines.length, 0, ...connBlock);
  }

  const routeBlock = serializeRoutes(draft.routes);
  if (replaceArrayBlock(lines, 'bot.routes', routeBlock)) {
    // 已替换
  } else if (routeBlock.length) {
    const r3 = findBotRegion(lines);
    lines.splice(r3 ? r3.end : lines.length, 0, ...routeBlock);
  }

  const text = lines.join('\n');
  return { ok: true, text, changed: text !== changedBefore.join('\n') };
}

// ---------------------------------------------------------------- 落盘（备份 + 原子替换）

function timestamp() {
  const d = new Date();
  const p = (n, w = 2) => String(n).padStart(w, '0');
  return `${d.getFullYear()}${p(d.getMonth() + 1)}${p(d.getDate())}-${p(d.getHours())}${p(d.getMinutes())}${p(d.getSeconds())}${p(d.getMilliseconds(), 3)}`;
}

/** 备份 + 原子替换。target 必须已存在。 */
function writeFileSafely(target, text) {
  const dir = path.dirname(target);
  const backup = `${target}.bak-dsh-reasonix-${timestamp()}`;
  try {
    fs.copyFileSync(target, backup);
  } catch (e) {
    return { ok: false, error: '备份失败，已中止写入：' + (e && e.message) };
  }
  const tmp = `${target}.tmp-dsh-reasonix-${process.pid}`;
  try {
    fs.writeFileSync(tmp, text, 'utf8');
    fs.renameSync(tmp, target);
  } catch (e) {
    try { if (fs.existsSync(tmp)) fs.unlinkSync(tmp); } catch {}
    return { ok: false, error: '写入失败：' + (e && e.message) + '（备份见 ' + path.basename(backup) + '）' };
  }
  return { ok: true, backup };
}

// ---------------------------------------------------------------- 对外 API

/** 读 config.toml + 密钥状态 → 前端形状。 */
function readBotSettings() {
  const file = configPath();
  if (!fs.existsSync(file)) {
    return { ok: false, error: `找不到 Reasonix 配置文件：${file}`, path: file };
  }
  let text;
  try { text = fs.readFileSync(file, 'utf8'); }
  catch (e) { return { ok: false, error: '读取失败：' + (e && e.message), path: file }; }
  const env = readEnvFile();
  const raw = parseBotRaw(text);
  return { ok: true, path: file, raw, bot: toFrontendBot(raw, { env }) };
}

/** 写回 config.toml（只改 [bot] 段）。 */
function writeBotSettings(draft) {
  const file = configPath();
  if (!fs.existsSync(file)) return { ok: false, error: `找不到 Reasonix 配置文件：${file}` };
  let text;
  try { text = fs.readFileSync(file, 'utf8'); }
  catch (e) { return { ok: false, error: '读取失败：' + (e && e.message) }; }
  const patched = patchConfigText(text, draft || {});
  if (!patched.ok) return patched;
  if (!patched.changed) return { ok: true, changed: false };
  const res = writeFileSafely(file, patched.text);
  return res.ok ? { ok: true, changed: true, backup: res.backup } : res;
}

/** 设置一个密钥（写 .env，格式保留 + 备份 + 原子替换）。 */
function setSecret(name, value) {
  const key = String(name || '').trim();
  if (!/^[A-Za-z_][A-Za-z0-9_]*$/.test(key)) return { ok: false, error: '非法的环境变量名：' + name };
  if (value == null || String(value) === '') return { ok: false, error: '密钥为空，拒绝写入' };
  const file = envPath();
  let lines = [];
  let existed = false;
  if (fs.existsSync(file)) {
    try { lines = fs.readFileSync(file, 'utf8').split('\n'); }
    catch (e) { return { ok: false, error: '读取 .env 失败：' + (e && e.message) }; }
  }
  const lit = JSON.stringify(String(value));
  for (let i = 0; i < lines.length; i++) {
    const m = /^(\s*)([A-Za-z_][A-Za-z0-9_]*)(\s*=\s*)(.*)$/.exec(lines[i]);
    if (m && m[2] === key) { lines[i] = m[1] + m[2] + m[3] + lit; existed = true; break; }
  }
  if (!existed) {
    let ins = lines.length;
    while (ins > 0 && lines[ins - 1].trim() === '') ins--;
    lines.splice(ins, 0, key + '=' + lit);
  }
  if (!fs.existsSync(file)) {
    try { fs.writeFileSync(file, lines.join('\n'), 'utf8'); return { ok: true, created: true }; }
    catch (e) { return { ok: false, error: '创建 .env 失败：' + (e && e.message) }; }
  }
  const res = writeFileSafely(file, lines.join('\n'));
  return res.ok ? { ok: true, updated: existed } : res;
}

/** 清空一个密钥（置空而不是删行，保持 .env 结构稳定）。 */
function clearSecret(name) {
  const key = String(name || '').trim();
  if (!key) return { ok: false, error: '缺少环境变量名' };
  const file = envPath();
  if (!fs.existsSync(file)) return { ok: true, changed: false };
  let lines;
  try { lines = fs.readFileSync(file, 'utf8').split('\n'); }
  catch (e) { return { ok: false, error: '读取 .env 失败：' + (e && e.message) }; }
  let hit = false;
  for (let i = 0; i < lines.length; i++) {
    const m = /^(\s*)([A-Za-z_][A-Za-z0-9_]*)(\s*=\s*)(.*)$/.exec(lines[i]);
    if (m && m[2] === key) { lines[i] = m[1] + m[2] + m[3] + '""'; hit = true; break; }
  }
  if (!hit) return { ok: true, changed: false };
  const res = writeFileSafely(file, lines.join('\n'));
  return res.ok ? { ok: true, changed: true } : res;
}

/** 读官方控制 API 的 /status（异步，失败返回 available:false 并带原因）。 */
function fetchControlStatus(addr, tokenEnv, timeoutMs = 2000) {
  return new Promise((resolve) => {
    const target = String(addr || '127.0.0.1:37913');
    const idx = target.lastIndexOf(':');
    const host = idx > 0 ? target.slice(0, idx) : target;
    const port = Number(idx > 0 ? target.slice(idx + 1) : '37913') || 37913;
    const env = readEnvFile();
    const token = env[String(tokenEnv || 'REASONIX_BOT_CONTROL_TOKEN')] || '';
    const req = http.request({
      host, port, path: '/status', method: 'GET',
      timeout: timeoutMs,
      headers: token ? { Authorization: 'Bearer ' + token } : {},
    }, (res) => {
      let body = '';
      res.on('data', (c) => { body += c; });
      res.on('end', () => {
        if (res.statusCode !== 200) {
          return resolve({ available: false, error: `控制 API 返回 HTTP ${res.statusCode}`, body: body.slice(0, 400) });
        }
        let json = null;
        try { json = JSON.parse(body); } catch { /* 非 JSON：原样返回 */ }
        resolve({ available: true, statusCode: 200, status: json, body: json ? undefined : body.slice(0, 400) });
      });
    });
    req.on('timeout', () => { req.destroy(); resolve({ available: false, error: `连接控制 API 超时（${timeoutMs}ms）` }); });
    req.on('error', (e) => resolve({ available: false, error: '连接控制 API 失败：' + (e && e.message) }));
    req.end();
  });
}

module.exports = {
  reasonixHome, configPath, envPath,
  readEnvFile, readBotSettings, writeBotSettings, patchConfigText,
  setSecret, clearSecret, fetchControlStatus,
  parseBotRaw, toFrontendBot,
  _internal: { tableNameOf, splitComment, parseScalar, literalOf, findBotRegion, writeFileSafely },
};
