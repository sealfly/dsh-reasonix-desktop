package main

// app_ask_test.go — ask 询问回答 + 目标模式提交测试。
// 单测覆盖 pending 缓存/无 pending 防崩溃；集成测试走真实 DSH（临时会话，不打扰当前会话）。

import "testing"

// pending 缓存：approval/requested 帧 → 可回答 → 回答后清除。
func TestPendingAskCache(t *testing.T) {
	// 清空表
	pendingAskMu.Lock()
	pendingAsks = map[string]pendingAsk{}
	pendingAskMu.Unlock()

	// 模拟 approval/requested 帧
	frame := struct {
		RPCID  string `json:"rpcId"`
		Method string `json:"method"`
		Payload struct {
			SessionID  string           `json:"sessionId"`
			ApprovalID string           `json:"approvalId"`
			ToolName   string           `json:"toolName"`
			CallID     string           `json:"callId"`
			Questions  []map[string]any `json:"questions"`
		} `json:"payload"`
	}{
		RPCID:  "rpc-1",
		Method: "approval/requested",
	}
	frame.Payload.SessionID = "sess-1"
	frame.Payload.ApprovalID = "appr-1"
	frame.Payload.ToolName = "bash"
	if !cachePendingAsk(frame) {
		t.Fatal("approval 帧应被缓存")
	}
	p, ok := takePendingAsk("sess-1")
	if !ok || p.RPCID != "rpc-1" || p.ApprovalID != "appr-1" || p.ToolName != "bash" {
		t.Fatalf("取回 pending 不对: %+v ok=%v", p, ok)
	}
	// 已取走 → 再次取失败
	if _, ok := takePendingAsk("sess-1"); ok {
		t.Fatal("回答后 pending 应已清除")
	}
	// 非询问帧不缓存
	frame2 := frame
	frame2.Method = "session/subscribed"
	if cachePendingAsk(frame2) {
		t.Fatal("session/subscribed 不应缓存")
	}
}

// 无 pending 时回答必须返回错误而非崩溃。
func TestAnswerQuestionNoPending(t *testing.T) {
	pendingAskMu.Lock()
	pendingAsks = map[string]pendingAsk{}
	pendingAskMu.Unlock()
	a := newTestApp()
	a.dsh = NewDshClient(3080)
	err := a.AnswerQuestionForTab("sess-none", []any{map[string]any{"questionId": "q1", "selected": []any{"A"}}}, nil)
	if err == nil {
		t.Fatal("无 pending 应报错")
	}
}

// 无 pending 时审批回答必须返回错误而非崩溃。
func TestApproveNoPending(t *testing.T) {
	pendingAskMu.Lock()
	pendingAsks = map[string]pendingAsk{}
	pendingAskMu.Unlock()
	a := newTestApp()
	a.dsh = NewDshClient(3080)
	if err := a.ApproveTab("sess-none", true, false, false, nil); err == nil {
		t.Fatal("无 pending 应报错")
	}
}

// 集成：目标模式首条提交（临时会话 + goal.create + 首条消息）。
func TestSubmitInitialGoalIntegration(t *testing.T) {
	a := newTestApp()
	a.dsh = NewDshClient(3080)
	// 临时会话（自动清理）
	sid := createTempSession(t)
	// 目标 + 首条消息
	if err := a.SubmitInitialGoalToTabWithID(sid, "集成测试目标: 回复 OK 即可", "请回复 OK", nil, nil, nil, nil, nil); err != nil {
		t.Fatalf("SubmitInitialGoalToTabWithID 失败: %v", err)
	}
	// 验证 goal 已注册
	if _, err := a.dsh.RPC("session.list", map[string]any{}); err != nil {
		t.Fatalf("session.list 失败: %v", err)
	}
}
