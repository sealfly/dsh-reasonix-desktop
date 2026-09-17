#!/usr/bin/env node
/*
 * fake-openai-provider.js — 供 UI/桥测试用的假 OpenAI 兼容端点。
 *
 * 用途：验证「刷新模型 → 勾选 → 保存」链路时，不能依赖真实供应商端点（要 key、会变、会限流）。
 * 起一个本机端点返回固定模型列表，测试即可完全确定。
 *
 * 用法：
 *   node scripts/fake-openai-provider.js [port]
 * 端点：
 *   GET /v1/models   → {"object":"list","data":[{"id":"fake-alpha"},{"id":"fake-beta"},{"id":"fake-vl-vision"}]}
 */
"use strict";

const http = require("http");

const port = Number(process.argv[2] || 9411);
const MODELS = ["fake-alpha", "fake-beta", "fake-vl-vision"];

const server = http.createServer((req, res) => {
  const url = req.url || "/";
  if (url.startsWith("/v1/models") || url.startsWith("/models")) {
    res.writeHead(200, {
      "Content-Type": "application/json",
      // 即使前端理论上走桥拉取，也带上 CORS 头，避免将来走浏览器 fetch 时假失败
      "Access-Control-Allow-Origin": "*",
    });
    res.end(JSON.stringify({ object: "list", data: MODELS.map((id) => ({ id, object: "model" })) }));
    return;
  }
  res.writeHead(404, { "Content-Type": "application/json" });
  res.end(JSON.stringify({ error: { message: "not found" } }));
});

server.listen(port, "127.0.0.1", () => {
  console.log(`[fake-openai] listening on http://127.0.0.1:${port}/v1  models=${MODELS.join(",")}`);
});

process.on("SIGINT", () => server.close(() => process.exit(0)));
process.on("SIGTERM", () => server.close(() => process.exit(0)));
