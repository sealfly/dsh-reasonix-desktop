#!/usr/bin/env node
'use strict';
// sync-remotes.js — 双端（GitHub / CNB）同步状态核对与执行：PRINCIPLES 原则 9 的固化。
//
// 为什么需要它：这台机器到 github.com:443 的 **读取**（fetch / ls-remote）经常失败，
// 而 **写入**（push）基本能成 —— 只靠 git fetch 判断"远程有没有新提交"会得到假阴性
// （本地 origin/master ref 停留在上次 push 的结果，看起来"没有差异"，其实无从确认）。
// 所以本工具按「fetch → ls-remote → HTTP API」三级回退取证，并且**只在确认有差异时**
// 才尝试拉取或推送。
//
// 用法：
//   node scripts/sync-remotes.js                  # 只核对并打印判定（默认 dry-run）
//   node scripts/sync-remotes.js --apply          # 按判定执行（ff / push / force-with-lease）
//   node scripts/sync-remotes.js --remote cnb     # 只看某个远程
//   node scripts/sync-remotes.js --json           # 机器可读输出
//
// 判定与动作（与原则 9 一致）：
//   equal      → 无需动作
//   behind     → git merge --ff-only <ref>          （本地落后，可快进）
//   ahead      → git push <remote> <branch>          （本地领先）
//   diverged   → 内容等价（git diff --name-only 为空）才 push --force-with-lease；
//                内容不等价 → **拒绝自动处理**，只报告（必须人工合并，绝不盲目强推）

const { execFileSync } = require('child_process');
const https = require('https');
const path = require('path');

const REPO = path.resolve(__dirname, '..');
const argv = process.argv.slice(2);
const has = (f) => argv.includes(f);
const val = (f, d) => {
  const i = argv.indexOf(f);
  return i >= 0 && i + 1 < argv.length ? argv[i + 1] : d;
};

const APPLY = has('--apply');
const AS_JSON = has('--json');
const ONLY = val('--remote', null);
const FETCH_ATTEMPTS = Math.max(1, parseInt(val('--fetch-attempts', '1'), 10) || 1);
// 进程级超时：连接阶段卡住时 git 自己的超时是 21s，LOW_SPEED 参数管不到 —— 必须硬杀。
const TIMEOUT_MS = Math.max(3000, parseInt(val('--timeout', '12000'), 10) || 12000);

const REMOTES = [
  { name: 'origin', label: 'GitHub', apiRepo: 'sealfly/dsh-reasonix-desktop' },
  { name: 'cnb', label: 'CNB', apiRepo: null }, // CNB 无公开匿名 API，靠 fetch/ls-remote
].filter((r) => !ONLY || r.name === ONLY);

// 让 git 快速失败：低速 10 秒即放弃，避免每次卡满 21 秒默认超时
const GIT_ENV = {
  ...process.env,
  GIT_HTTP_LOW_SPEED_LIMIT: '1000',
  GIT_HTTP_LOW_SPEED_TIME: '10',
  GIT_TERMINAL_PROMPT: '0',
};

function git(args, { allowFail = false, timeout = TIMEOUT_MS } = {}) {
  try {
    return { ok: true, out: execFileSync('git', args, { cwd: REPO, env: GIT_ENV, encoding: 'utf8', maxBuffer: 1 << 28, timeout }).trim() };
  } catch (err) {
    if (!allowFail) throw err;
    const killed = err.killed || err.signal === 'SIGTERM' || /ETIMEDOUT|timed out/i.test(String(err.message || ''));
    const msg = ((err.stderr || '') + (err.stdout || '') + (err.message || '')).toString().trim().split('\n')[0];
    return { ok: false, out: '', timeout: killed, error: (killed ? '[超时 ' + timeout + 'ms] ' : '') + msg.slice(0, 160) };
  }
}

function httpJson(url) {
  return new Promise((resolve) => {
    const req = https.get(url, { headers: { 'User-Agent': 'dsh-sync-remotes', Accept: 'application/vnd.github+json' }, timeout: 20000 }, (res) => {
      let body = '';
      res.on('data', (c) => { body += c; });
      res.on('end', () => {
        try { resolve(JSON.parse(body)); } catch { resolve(null); }
      });
    });
    req.on('timeout', () => { req.destroy(); resolve(null); });
    req.on('error', () => resolve(null));
  });
}

async function probe(remote) {
  const branch = git(['rev-parse', '--abbrev-ref', 'HEAD']).out;
  const local = git(['rev-parse', 'HEAD']).out;
  const out = { name: remote.name, label: remote.label, branch, local, sha: null, via: null, ref: null, note: '' };

  // 1) fetch（成功即拿到 refs/remotes/<name>/<branch>，可做完整祖先关系判定）
  for (let i = 1; i <= FETCH_ATTEMPTS; i++) {
    const r = git(['fetch', remote.name, '--prune'], { allowFail: true });
    if (r.ok) {
      const ref = remote.name + '/' + branch;
      const rr = git(['rev-parse', ref], { allowFail: true });
      if (rr.ok) { out.sha = rr.out; out.via = 'fetch'; out.ref = ref; return out; }
    } else if (i === FETCH_ATTEMPTS) {
      out.note = 'fetch 失败: ' + (r.error || '未知');
    }
  }

  // 2) 回退 ls-remote（比 fetch 轻，常能成功；只有 SHA，不能做合并）
  const ls = git(['ls-remote', remote.name, 'refs/heads/' + branch], { allowFail: true });
  if (ls.ok && ls.out) { out.sha = ls.out.split(/\s+/)[0]; out.via = 'ls-remote'; return out; }

  // 3) 再回退 HTTP API（目前仅 GitHub 提供）
  if (remote.apiRepo) {
    const j = await httpJson('https://api.github.com/repos/' + remote.apiRepo + '/commits/' + branch);
    if (j && j.sha) { out.sha = j.sha; out.via = 'api'; return out; }
  }
  return out;
}

function classify(remote) {
  if (!remote.sha) return { verdict: 'unknown', ref: null };
  if (remote.sha === remote.local) return { verdict: 'equal', ref: remote.ref };
  // 有 SHA 但没 fetch 到 ref → 关系未知，必须拿到 ref 才能继续
  if (!remote.ref) return { verdict: 'remote-has-new', ref: null };
  const cnt = git(['rev-list', '--left-right', '--count', remote.ref + '...HEAD'], { allowFail: true });
  if (!cnt.ok) return { verdict: 'unknown', ref: remote.ref };
  const [behind, ahead] = cnt.out.split(/\s+/).map((n) => parseInt(n, 10));
  if (behind > 0 && ahead === 0) return { verdict: 'behind', ref: remote.ref, behind };
  if (ahead > 0 && behind === 0) return { verdict: 'ahead', ref: remote.ref, ahead };
  if (ahead > 0 && behind > 0) {
    // 并行链：内容等价才允许 force-with-lease
    const diff = git(['diff', '--name-only', remote.ref, 'HEAD'], { allowFail: true });
    const equivalent = diff.ok && diff.out === '';
    return { verdict: equivalent ? 'diverged-equivalent' : 'diverged-conflict', ref: remote.ref, ahead, behind };
  }
  return { verdict: 'equal', ref: remote.ref };
}

const ACTIONS = {
  equal: (r) => '无需动作',
  behind: (r) => 'git merge --ff-only ' + r.ref,
  ahead: (r) => 'git push ' + r.name + ' ' + r.branch,
  'diverged-equivalent': (r) => 'git push --force-with-lease ' + r.name + ' ' + r.branch + '（内容等价，统一 SHA）',
  'diverged-conflict': () => '⚠ 内容不等价，拒绝自动处理：请人工合并后再推',
  'remote-has-new': (r) => '⚠ 远程有新提交但 fetch 不通：重试 fetch 或稍后再同步',
  unknown: () => '⚠ 无法判定（fetch/ls-remote/API 全部失败）',
};

function run(remote, state) {
  const v = state.verdict;
  if (v === 'behind') return git(['merge', '--ff-only', state.ref], { allowFail: true });
  if (v === 'ahead') return git(['push', remote.name, remote.branch], { allowFail: true });
  if (v === 'diverged-equivalent') return git(['push', '--force-with-lease', remote.name, remote.branch], { allowFail: true });
  return { ok: false, out: '', error: 'no action for verdict ' + v };
}

(async () => {
  const results = [];
  for (const remote of REMOTES) {
    const probed = await probe(remote);
    const state = classify(probed);
    results.push({ ...probed, ...state });
  }

  if (AS_JSON) {
    console.log(JSON.stringify({ apply: APPLY, results }, null, 2));
  } else {
    console.log('[sync-remotes] 仓库 ' + REPO);
    for (const r of results) {
      console.log('');
      console.log('  ' + r.label + ' (' + r.name + ')');
      console.log('    取证方式  : ' + (r.via || '全部失败') + (r.via === 'fetch' ? '（可做完整祖先判定）' : r.via ? '（仅 SHA）' : ''));
      console.log('    本地 HEAD : ' + r.local.slice(0, 12));
      console.log('    远程 HEAD : ' + (r.sha ? r.sha.slice(0, 12) : '(未知)'));
      console.log('    判定      : ' + r.verdict + (r.ahead !== undefined ? '  (领先 ' + (r.ahead || 0) + ' / 落后 ' + (r.behind || 0) + ')' : ''));
      console.log('    建议动作  : ' + ACTIONS[r.verdict](r));
      if (r.note) console.log('    备注      : ' + r.note);

      if (APPLY) {
        if (!['behind', 'ahead', 'diverged-equivalent'].includes(r.verdict)) {
          console.log('    → 跳过（' + r.verdict + ' 不自动处理）');
          continue;
        }
        const res = run(r, r);
        console.log(res.ok ? '    → 已执行并成功' : '    → 执行失败: ' + (res.error || '未知'));
      }
    }
    const allEqual = results.every((r) => r.verdict === 'equal');
    console.log('');
    console.log(allEqual
      ? '[sync-remotes] ✓ 三方一致，无需同步'
      : '[sync-remotes] 存在差异' + (APPLY ? '（已按 --apply 处理，见上）' : '（dry-run：加 --apply 执行）'));
  }
  process.exit(0);
})();
