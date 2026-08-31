package main

// app_submit_test.go — 任务提交通道测试。
// 单测覆盖提取/回退逻辑；集成测试(TestSubmitViaHTTPIntegration)创建临时会话
// 走真实 DSH 提交，验证 accepted:true（不打扰当前运行中的会话）。

import (
  "testing"
)

// extractPromptText: display 优先，其次 input.text，再 input.prompt，最后序列化。
func TestExtractPromptText(t *testing.T) {
  if got := extractPromptText("hello", nil); got != "hello" {
    t.Fatalf("display 应优先: %q", got)
  }
  if got := extractPromptText("", map[string]any{"text": "inner"}); got != "inner" {
    t.Fatalf("input.text 兜底失败: %q", got)
  }
  if got := extractPromptText("", map[string]any{"prompt": "pp"}); got != "pp" {
    t.Fatalf("input.prompt 兜底失败: %q", got)
  }
  if got := extractPromptText("", map[string]any{"x": 1}); got == "" {
    t.Fatalf("序列化兜底失败: %q", got)
  }
}

// 空文本提交必须返回错误而非崩溃。
func TestSubmitToTabWithIDEmpty(t *testing.T) {
  a := newTestApp()
  r := a.SubmitToTabWithID("tab-1", "   ", nil)
  if r["ok"] != false {
    t.Fatalf("空文本应失败: %v", r)
  }
}

// 集成测试：创建临时会话 → session.prompt 提交 → 验证 accepted:true。
// 需要 DSH 运行在 127.0.0.1:3080；不可用时跳过。
func TestSubmitViaHTTPIntegration(t *testing.T) {
  a := newTestApp()
  a.dsh = NewDshClient(3080)
  // 创建临时会话（不打扰当前运行会话，测试结束自动清理目录）
  sid := createTempSession(t)

  // 提交 prompt（正确 payload: mode+content）
  res := a.submitViaHTTP(sid, "测试桌面客户端提交通道, 请只回复 OK", defaultSubmitConfig())
  if !res.OK {
    t.Fatalf("submitViaHTTP 失败: %+v", res)
  }
  if res.Channel != "http" {
    t.Fatalf("通道应为 http: %+v", res)
  }
}

// SubmitToTabWithID 真实路径（经 activeSessionID 解析）: 提交到临时会话。
func TestSubmitToTabWithIDIntegration(t *testing.T) {
  a := newTestApp()
  a.dsh = NewDshClient(3080)
  // 创建临时会话并自动清理
  sid := createTempSession(t)
  r := a.SubmitToTabWithID(sid, "集成测试: 请回复 OK", nil)
  if r["ok"] != true {
    t.Fatalf("SubmitToTabWithID 失败: %v", r)
  }
}
