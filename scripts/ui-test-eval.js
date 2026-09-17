#!/usr/bin/env node
/*
 * ui-test-eval.js — 通过 UI 测试钩子执行一段 JS（调试/探针用）。
 *
 * 用法：
 *   node scripts/ui-test-eval.js "document.title"
 *   node scripts/ui-test-eval.js --hook 9310 --file expr.js
 *   node scripts/ui-test-eval.js --click ".some-selector"
 */
"use strict";

const args = process.argv.slice(2);
function argValue(flag, fallback) {
  const i = args.indexOf(flag);
  return i >= 0 && args[i + 1] ? args[i + 1] : fallback;
}
const HOOK = argValue("--hook", process.env.DSH_UI_TEST_PORT || "9310");
const file = argValue("--file", "");
const clickSel = argValue("--click", "");
const expr = file ? require("fs").readFileSync(file, "utf8") : args.filter((a) => !a.startsWith("--")).join(" ");
const path = clickSel ? "/click" : "/eval";

(async () => {
  const payload = clickSel ? { selector: clickSel } : { js: expr };
  const res = await fetch(`http://127.0.0.1:${HOOK}${path}`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
  const body = await res.json();
  console.log(JSON.stringify(body, null, 2));
  process.exit(body.ok ? 0 : 1);
})().catch((err) => {
  console.error(`FATAL: ${err.message}`);
  process.exit(1);
});
