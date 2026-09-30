// bot-config 自测：解析 / 格式保留写入 / 幂等 / 连接块增删 / 空保存不写盘
// 运行： node test/bot-config.test.js
// 只读真实 config.toml 做输入，所有写入都在内存里验证，绝不碰真文件。
'use strict';
const fs = require('fs');
const os = require('os');
const path = require('path');
const bc = require('../src/bot-config.js');

const real = bc.configPath();
const text = fs.readFileSync(real, 'utf8');
let fail = 0;
const ok = (name, cond, extra) => {
  if (!cond) fail++;
  console.log((cond ? '  [OK]   ' : '  [FAIL] ') + name + (extra ? '  ' + extra : ''));
};

console.log('配置文件: ' + real);
console.log('字节数: ' + Buffer.byteLength(text) + '  行数: ' + text.split('\n').length);
console.log('CRLF 行数: ' + (text.match(/\r\n/g) || []).length);
console.log('');

// ---------- 1. 解析 ----------
const raw = bc.parseBotRaw(text);
ok('找到 [bot] 段', raw.present);
console.log('  base.enabled = ' + raw.base.enabled + '  max_steps = ' + raw.base.max_steps + '  queue_mode = ' + raw.base.queue_mode);
console.log('  control = ' + JSON.stringify(raw.control));
console.log('  pairing = ' + JSON.stringify(raw.pairing));
console.log('  channels = ' + Object.keys(raw.channels).join(','));
console.log('  allowlist 键数 = ' + Object.keys(raw.allowlist).length);
console.log('  connections = ' + raw.connections.length + '  routes = ' + raw.routes.length);
ok('base.enabled 解析为 false', raw.base.enabled === false);
ok('max_steps 解析为 25', raw.base.max_steps === 25);
ok('control.addr 解析正确', raw.control.addr === '127.0.0.1:37913');
ok('allowlist 拿到 qq_users', Array.isArray(raw.allowlist.qq_users));
ok('channels 含 dingtalk（未被前端模型覆盖）', Boolean(raw.channels.dingtalk));
console.log('  dingtalk = ' + JSON.stringify(raw.channels.dingtalk));
console.log('');

// ---------- 2. 转前端形状 ----------
const bot = bc.toFrontendBot(raw);
ok('前端形状 enabled=false', bot.enabled === false);
ok('前端形状 allowlist.qqUsers 是数组', Array.isArray(bot.allowlist.qqUsers));
ok('前端形状 qq.appSecretEnv 有默认值', bot.qq.appSecretEnv === 'QQ_BOT_APP_SECRET');
ok('前端形状 control.addr', bot.control.addr === '127.0.0.1:37913');
console.log('');

// ---------- 2b. 关键不变量：未改动的 draft 保存必须是无操作 ----------
const noop = bc.patchConfigText(text, bot);
ok('未改动 draft 保存 → changed=false（不写盘）', noop.ok && noop.changed === false);
if (noop.ok && noop.changed) {
  const a = text.split('\n');
  const b2 = noop.text.split('\n');
  const diff = [];
  for (let i = 0; i < Math.max(a.length, b2.length); i++) {
    if (a[i] !== b2[i]) diff.push((i + 1) + ': ' + JSON.stringify(a[i]) + '  ->  ' + JSON.stringify(b2[i]));
  }
  console.log('    差异 ' + diff.length + ' 行，前 12 行:');
  for (const d of diff.slice(0, 12)) console.log('      ' + d);
}
console.log('');

// ---------- 3. 格式保留写入 ----------
const draft = JSON.parse(JSON.stringify(bot));
draft.enabled = true;
draft.maxSteps = 30;
draft.queueMode = 'interrupt';
draft.qq.appId = '102000001';
draft.allowlist.qqUsers = ['user-a', 'user-b'];
draft.selfUserIds.feishu = ['ou_self'];

const patched = bc.patchConfigText(text, draft);
ok('patchConfigText 成功', patched.ok, patched.error || '');
if (!patched.ok) process.exit(1);
ok('patch 报告有变化', patched.changed === true);

const before = text.split('\n');
const after = patched.text.split('\n');
const region = bc._internal.findBotRegion(before);
console.log('  bot 段行区间: ' + (region.start + 1) + ' - ' + region.end + '（1-based）');
const afterRegion = bc._internal.findBotRegion(after);
// 段外逐行比对（起始行数应一致）
const headSame = before.slice(0, region.start).join('\n') === after.slice(0, afterRegion.start).join('\n');
ok('bot 段之前的所有字节完全未变', headSame);
const tailBefore = before.slice(region.end).join('\n');
const tailAfter = after.slice(afterRegion.end).join('\n');
ok('bot 段之后的所有字节完全未变', tailBefore === tailAfter);
console.log('  段前 ' + region.start + ' 行, 段后 ' + (before.length - region.end) + ' 行均逐字节一致');
console.log('');

// ---------- 4. 回读校验 ----------
const raw2 = bc.parseBotRaw(patched.text);
ok('回读 enabled=true', raw2.base.enabled === true);
ok('回读 max_steps=30', raw2.base.max_steps === 30);
ok('回读 queue_mode=interrupt', raw2.base.queue_mode === 'interrupt');
ok('回读 qq.app_id', raw2.channels.qq.app_id === '102000001');
ok('回读 allowlist.qq_users', JSON.stringify(raw2.allowlist.qq_users) === JSON.stringify(['user-a', 'user-b']));
ok('回读 self_user_ids.feishu', JSON.stringify(raw2.selfUserIds.feishu) === JSON.stringify(['ou_self']));
ok('回读 dingtalk 仍未被破坏', JSON.stringify(raw2.channels.dingtalk) === JSON.stringify(raw.channels.dingtalk));
console.log('');

// ---------- 5. 行尾注释是否保留 ----------
const commentLine = after.find((l) => /tool_approval_mode\s*=/.test(l) && l.includes('#'));
ok('就地替换保留了行尾注释', Boolean(commentLine), commentLine ? commentLine.trim() : '');
const commentLineOther = after.find((l) => /queue_drop\s*=/.test(l) && l.includes('#'));
ok('未改动的键注释也在', Boolean(commentLineOther));
console.log('');

// ---------- 6. 幂等 ----------
const again = bc.patchConfigText(patched.text, draft);
ok('幂等：同样的 draft 再 patch 一次报告 changed=false', again.ok && again.changed === false);
console.log('');

// ---------- 7. 连接块增删 ----------
const withConn = JSON.parse(JSON.stringify(draft));
withConn.connections = [{
  id: 'feishu-lark', provider: 'feishu', domain: 'lark', label: '工作飞书', enabled: true,
  model: '', toolApprovalMode: 'ask', workspaceRoot: 'D:\\proj',
  credential: { appId: 'cli_xxx', appSecretEnv: 'FEISHU_BOT_APP_SECRET', accountId: '', tokenEnv: '', secretSet: false },
  access: { enabled: true, allowAll: false, pairingEnabled: true, users: ['ou_a'], groups: [], approvers: [], admins: [] },
  sessionMappings: [], lastError: '', createdAt: '', updatedAt: '',
}];
const p3 = bc.patchConfigText(text, withConn);
ok('插入 connections 成功', p3.ok);
const raw3 = bc.parseBotRaw(p3.text);
ok('回读 connections 数量=1', raw3.connections.length === 1, JSON.stringify(raw3.connections[0] || {}).slice(0, 200));
const c0 = raw3.connections[0] || {};
ok('回读 connection.provider', c0.provider === 'feishu');
ok('回读 connection.domain', c0.domain === 'lark');
ok('回读 connection.credential.app_id', c0.credential && c0.credential.app_id === 'cli_xxx');
ok('回读 connection.access.users', c0.access && JSON.stringify(c0.access.users) === JSON.stringify(['ou_a']));
const f3 = bc.toFrontendBot(raw3);
ok('前端形状 connections[0].id', f3.connections[0].id === 'feishu-lark');
ok('前端形状 connections[0].credential.secretSet=false', f3.connections[0].credential.secretSet === false);
ok('段外仍未被破坏（插入 connections 后）', (() => {
  const r3 = bc._internal.findBotRegion(p3.text.split('\n'));
  const a3 = p3.text.split('\n');
  return a3.slice(0, r3.start).join('\n') === before.slice(0, region.start).join('\n')
      && a3.slice(r3.end).join('\n') === tailBefore;
})());
// 幂等（带 connections）
const p4 = bc.patchConfigText(p3.text, withConn);
ok('幂等（带 connections）', p4.ok && p4.changed === false);
// 删除 connections
const noConn = JSON.parse(JSON.stringify(withConn));
noConn.connections = [];
const p5 = bc.patchConfigText(p3.text, noConn);
ok('删除 connections 成功', p5.ok);
ok('删除后回读为 0 条', bc.parseBotRaw(p5.text).connections.length === 0);
ok('删除后段外仍未被破坏', (() => {
  const r5 = bc._internal.findBotRegion(p5.text.split('\n'));
  const a5 = p5.text.split('\n');
  return a5.slice(0, r5.start).join('\n') === before.slice(0, region.start).join('\n')
      && a5.slice(r5.end).join('\n') === tailBefore;
})());
console.log('');

// ---------- 8. 拒绝破坏：没有 [bot] 段就不写 ----------
const refused = bc.patchConfigText('[other]\nx = 1\n', draft);
ok('无 [bot] 段时拒绝写入', refused.ok === false, refused.error || '');
console.log('');

console.log(fail === 0 ? '全部通过（0 项失败）' : ('有 ' + fail + ' 项失败'));
process.exit(fail === 0 ? 0 : 1);
