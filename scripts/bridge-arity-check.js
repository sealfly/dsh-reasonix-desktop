#!/usr/bin/env node
/**
 * bridge-arity-check.js — 桥方法「前端调用实参个数 ↔ Go 签名形参个数」契约检查。
 *
 * 为什么需要它：Wails 绑定按**参数个数**校验（多/少一个都直接抛
 * `error parsing arguments: received N arguments to method 'main.App.X', expected M`），
 * 而前端是懒加载的多个 chunk（面板组件不在主 bundle 里）。曾经就是这样踩到：
 * 「设置-远程SSH」配好主机后，右栏出现上游自带的「远程」页签，但远程文件树恒报
 *   `received 2 arguments to method 'main.App.ListRemoteDir', expected 0`
 * —— Go 侧那四个方法还是零参占位桩，前端却按 2/4 个参数调用。
 *
 * 用法：node scripts/bridge-arity-check.js [repoRoot]
 * 退出码：0 = 无错位；1 = 存在错位（可接进 CI/发布前检查）。
 */
"use strict";

const fs = require("fs");
const path = require("path");

const root = process.argv[2] || path.join(__dirname, "..");

/* ---------- 1) 解析 Go 侧签名 ---------- */

/**
 * 顶层逗号切分。
 *
 * mode="go"：把 `<` `>` 当括号（Go 泛型 / map[string]any 等）；
 * mode="js"：**不能**把 `<` `>` 当括号 —— 否则箭头函数 `e=>e.name` 和比较运算会把深度算错，
 *            实测会把 `f(a.providers.map(e=>e.name), e)` 误判成 1 个实参（真值 2），
 *            进而产出**假错位**并诱使人去"修"本来正确的代码。
 * 两种模式都跳过字符串字面量与箭头函数体。
 */
function splitTopLevel(s, sep = ",", mode = "go") {
  const out = [];
  let depth = 0, cur = "";
  let quote = null;
  for (let i = 0; i < s.length; i++) {
    const ch = s[i];
    if (quote) {
      cur += ch;
      if (ch === "\\") { cur += s[++i] ?? ""; continue; }
      if (ch === quote) quote = null;
      continue;
    }
    if (ch === '"' || ch === "'" || ch === "`") { quote = ch; cur += ch; continue; }
    if (mode === "go" ? "([{<".includes(ch) : "([{".includes(ch)) depth++;
    else if (mode === "go" ? ")]}>".includes(ch) : ")]}".includes(ch)) depth--;
    if (ch === sep && depth === 0) { out.push(cur); cur = ""; continue; }
    cur += ch;
  }
  if (cur.trim()) out.push(cur);
  return out;
}

// 一个形参组 `a, b string` 展开为 2 个形参；`_ string` 为 1 个。
function countParams(paramList) {
  const t = paramList.trim();
  if (!t) return 0;
  let n = 0;
  for (const group of splitTopLevel(t)) {
    const g = group.trim();
    if (!g) continue;
    // 去掉类型部分：取最后一个"类型起始"之前的名字列表
    const m = g.match(/^([\s\S]*?)\s+([A-Za-z_][\w.]*(?:\[\])?[\w.\[\]*{}]*)$/);
    if (!m) { n += 1; continue; }
    const names = splitTopLevel(m[1]);
    n += names.length;
  }
  return n;
}

function goSignatures() {
  const files = fs.readdirSync(root).filter((f) => f.endsWith(".go"));
  const map = new Map();
  for (const f of files) {
    let src;
    try { src = fs.readFileSync(path.join(root, f), "utf8"); } catch { continue; }
    const re = /func \(a \*App\) (\w+)\(([^)]*)\)/g;
    let m;
    while ((m = re.exec(src))) {
      // 同名多签名（重载式）罕见，这里若出现则记成冲突
      if (map.has(m[1]) && map.get(m[1]).argc !== countParams(m[2])) {
        map.set(m[1], { argc: countParams(m[2]), file: f, dup: true });
      } else if (!map.has(m[1])) {
        map.set(m[1], { argc: countParams(m[2]), file: f });
      }
    }
  }
  return map;
}

/* ---------- 2) 解析前端调用点 ---------- */

function argCountAt(src, openParenIdx) {
  let depth = 0, end = -1;
  for (let i = openParenIdx; i < Math.min(src.length, openParenIdx + 800); i++) {
    const ch = src[i];
    if (ch === "(") depth++;
    else if (ch === ")") { depth--; if (depth === 0) { end = i; break; } }
  }
  if (end < 0) return null;
  const args = src.slice(openParenIdx + 1, end);
  if (!args.trim()) return 0;
  return splitTopLevel(args, ",", "js").length;
}

function frontendCalls() {
  const dir = path.join(root, "frontend", "dist", "assets");
  const calls = new Map(); // name -> [{file, argc, snippet}]
  if (!fs.existsSync(dir)) return calls;
  const files = fs.readdirSync(dir, { recursive: true }).filter((f) => /\.(js|mjs)$/.test(String(f)));
  for (const rel of files) {
    const full = path.join(dir, String(rel));
    let src;
    try { src = fs.readFileSync(full, "utf8"); } catch { continue; }
    // 调用形态：<obj>.<Name>(…)；名字以大写开头（Wails 桥方法都是导出名）
    const re = /\.([A-Z]\w*)\(/g;
    let m;
    while ((m = re.exec(src))) {
      const name = m[1];
      const open = m.index + m[0].length - 1; // '(' 位置
      const argc = argCountAt(src, open);
      if (argc === null) continue;
      if (!calls.has(name)) calls.set(name, []);
      const arr = calls.get(name);
      if (!arr.some((x) => x.file === String(rel) && x.argc === argc) && arr.length < 6) {
        arr.push({ file: String(rel), argc, snippet: src.slice(Math.max(0, m.index - 40), open + 60).replace(/\s+/g, " ") });
      }
    }
  }
  return calls;
}

/* ---------- 3) 比对 ---------- */

const go = goSignatures();
const calls = frontendCalls();

const mismatches = [];
const unknown = [];
for (const [name, sites] of calls) {
  if (!go.has(name)) { unknown.push({ name, sites }); continue; }
  const expected = go.get(name).argc;
  for (const s of sites) {
    if (s.argc !== expected) mismatches.push({ name, expected, got: s.argc, ...s, goFile: go.get(name).file });
  }
}

console.log(`扫描：Go 桥方法 ${go.size} 个；前端调用点覆盖 ${calls.size} 个方法`);
console.log(`错位：${mismatches.length} 处\n`);

const byName = new Map();
for (const m of mismatches) {
  if (!byName.has(m.name)) byName.set(m.name, []);
  byName.get(m.name).push(m);
}

// 调用点分类：开发用 mock 桥（bridge-*.js，其 arity 与真实绑定同源，仍具参考性）
// 与真实 UI chunk 的调用点要分开看 —— 后者才是用户真会走到的路径。
const isMock = (f) => /^bridge-/.test(f);

/**
 * 有意保留的例外：**只被开发态 mock 桥调用、真实 UI 从不调用**的方法。
 * 这里必须写清原因——例外表是"已审阅"的记号，不是忽略错误的开关。
 */
const INTENTIONAL = new Map([
  ["Cancel", "真实 UI 从不调 Cancel()；Go 侧 Cancel(tabID) 是 CancelForTab 的实现基础，改成 0 参会破坏按标签页取消。mock 桥的 this.Cancel() 只是假实现。"],
]);

const realOnes = [];
const mockOnly = [];
const excused = [];
for (const [name, list] of byName.entries()) {
  const hasReal = list.some((x) => !isMock(x.file));
  if (hasReal) { realOnes.push([name, list]); continue; }
  if (INTENTIONAL.has(name)) { excused.push([name, list]); continue; }
  mockOnly.push([name, list]);
}
realOnes.sort(); mockOnly.sort(); excused.sort();

function dump(list, title) {
  console.log(`\n${title}（${list.length} 个方法）`);
  for (const [name, l] of list.sort()) {
    const g = go.get(name);
    const real = l.find((x) => !isMock(x.file));
    const pick = real || l[0];
    console.log(`  ✗ ${name}  Go 形参 ${g.argc}（${g.file}）  前端实参 ${[...new Set(l.map((x) => x.argc))].join("/")}`);
    console.log(`      调用点: ${pick.file}  …${pick.snippet.slice(-80)}`);
  }
}
dump(realOnes, "真实 UI 调用点错位（用户可达路径，优先修）");
dump(mockOnly, "仅 mock 桥调用点错位（开发态；影响 dev mock 一致性）");

if (excused.length) {
  console.log(`\n有意保留的例外（${excused.length} 个，已审阅）`);
  for (const [name] of excused) console.log(`  · ${name} —— ${INTENTIONAL.get(name)}`);
}

if (process.argv.includes("--list-unknown") && unknown.length) {
  console.log(`\n前端调用但 Go 无同名方法（${unknown.length}）——多为前端内部同名函数，仅供参考：`);
  for (const u of unknown.slice(0, 40)) console.log(`  ? ${u.name}  ${u.sites[0].file} argc=${u.sites[0].argc}`);
}

console.log(`\n结论：真实 UI 错位 ${realOnes.length} 个、仅 mock 错位 ${mockOnly.length} 个、已审阅例外 ${excused.length} 个`);
// 退出码只由**真实 UI 错位**决定：mock 桥与已审阅例外不应让检查长期红灯（否则会被当成噪声忽略）。
process.exit(realOnes.length ? 1 : 0);
