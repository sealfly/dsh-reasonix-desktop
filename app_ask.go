package main

// app_ask.go — ask 询问回答 + 目标模式提交（DSH 原生机制）。
//
// 1) AnswerQuestionForTab / ApproveTab：回答 DSH 的 ask 询问。DSH 在 events.mux 上以
//    server-request 帧发 approval/requested（权限审批）与 question/requested（提问），
//    帧顶层 rpcId 是回答回显令牌。app_events.go 已把这类帧缓存进 pendingAsks；
//    前端回答后这里构造 client-response POST /api/respond 回传，DSH 继续执行。
// 2) SubmitInitialGoalToTabWithID：目标模式首条提交——先 goal.create 注册总目标，
//    再提交首条消息（有工具调用则提交调用，否则提交普通文本）。

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// respondRPC 向 DSH 回答一个 pending server-request（client-response → /api/respond）。
// rpcID 必须回显 server-request 帧的 rpcId；value 是业务回答 payload。
func (a *App) respondRPC(rpcID string, value any) error {
	if a.dsh == nil {
		return fmt.Errorf("dsh client not initialized")
	}
	body, err := json.Marshal(map[string]any{
		"type":   "client-response",
		"rpcId":  rpcID,
		"result": map[string]any{"ok": true, "value": value},
	})
	if err != nil {
		return err
	}
	url := fmt.Sprintf("http://127.0.0.1:%d/api/respond", a.dsh.port)
	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("respond: %w", err)
	}
	defer resp.Body.Close()
	var out struct {
		Accepted bool   `json:"accepted"`
		Reason   string `json:"reason"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return fmt.Errorf("respond decode: %w", err)
	}
	if !out.Accepted {
		return fmt.Errorf("respond rejected: %s", out.Reason)
	}
	return nil
}

// AnswerQuestionForTab 回答 DSH 的 ask 提问（question/requested）。
// tabID 即 DSH sessionId；answers 为前端回传的回答数组，元素形如
// {questionId, selected:[...], custom?:"..."}（questionId 即 DSH question.id）。
func (a *App) AnswerQuestionForTab(tabID string, answers []any, _ any) error {
	sid := a.activeSessionID(tabID)
	if sid == "" {
		sid = tabID
	}
	p, ok := takePendingAsk(sid)
	if !ok || p.RPCID == "" {
		return fmt.Errorf("no pending question for session %s", sid)
	}
	// 归一化前端回答 → DSH answer.answers（{id, selected, custom?}）
	norm := make([]map[string]any, 0, len(answers))
	for _, ans := range answers {
		m, ok := ans.(map[string]any)
		if !ok {
			continue
		}
		qid, _ := m["questionId"].(string)
		if qid == "" {
			qid, _ = m["id"].(string)
		}
		sel, _ := m["selected"].([]any)
		custom, _ := m["custom"].(string)
		entry := map[string]any{"id": qid, "selected": sel}
		if custom != "" {
			entry["custom"] = custom
		}
		norm = append(norm, entry)
	}
	if len(norm) == 0 {
		return fmt.Errorf("no valid answers for session %s", sid)
	}
	payload := map[string]any{
		"sessionId": sid,
		"answer":    map[string]any{"answers": norm},
	}
	if err := a.respondRPC(p.RPCID, payload); err != nil {
		return err
	}
	resumeLog("answered question session=%s rpcId=%s answers=%d", sid, p.RPCID, len(norm))
	return nil
}

// ApproveTab 回答 DSH 的权限审批（approval/requested）。
// approve=true → allowed-once；approve=false → rejected。grantSession/grantSaved 为前端
// 扩展选项（本次会话有效 / 保存为规则），DSH approval 回答只接受 allowed-once/rejected，
// 这里映射为 allowed-once 并记日志（长期规则由权限设置区块管理）。
func (a *App) ApproveTab(tabID string, approve bool, grantSession bool, grantSaved bool, _ any) error {
	sid := a.activeSessionID(tabID)
	if sid == "" {
		sid = tabID
	}
	p, ok := takePendingAsk(sid)
	if !ok || p.RPCID == "" {
		return fmt.Errorf("no pending approval for session %s", sid)
	}
	outcome := "rejected"
	if approve {
		outcome = "allowed-once"
	}
	payload := map[string]any{
		"sessionId":  sid,
		"approvalId": p.ApprovalID,
		"outcome":    outcome,
	}
	if err := a.respondRPC(p.RPCID, payload); err != nil {
		return err
	}
	resumeLog("answered approval session=%s tool=%s outcome=%s grantSession=%v grantSaved=%v",
		sid, p.ToolName, outcome, grantSession, grantSaved)
	return nil
}

// SubmitInitialGoalToTabWithID 目标模式首条提交：先注册总目标，再提交首条消息。
// 参数：tabID=会话, goal=目标文本, display=显示文本, input=结构化输入, invocations=工具调用。
// 语义对齐前端 mock：goal 非空 → goal.create；invocations 非空 → 提交调用；否则提交 display。
func (a *App) SubmitInitialGoalToTabWithID(tabID string, goal string, display string, input map[string]any, invocations []any, _ any, _ any, _ any) error {
	sid := a.activeSessionID(tabID)
	if sid == "" {
		sid = tabID
	}
	// 1. 注册目标（goal.create）
	goal = strings.TrimSpace(goal)
	if goal != "" {
		if a.dsh == nil {
			return fmt.Errorf("dsh client not initialized")
		}
		if _, err := a.dsh.RPC("goal.create", map[string]any{
			"sessionId": sid,
			"objective": goal,
		}); err != nil {
			return fmt.Errorf("goal.create: %w", err)
		}
		resumeLog("goal.create session=%s objective=%q", sid, truncate(goal, 60))
	}
	// 2. 提交首条消息（桥方法返回 map，这里把 ok 状态转成 error）
	var r map[string]any
	if len(invocations) > 0 {
		r = a.SubmitInvocationsToTabWithID(sid, display, invocations, input)
	} else {
		r = a.SubmitDisplayToTabWithID(sid, display, input)
	}
	if ok, _ := r["ok"].(bool); !ok {
		if e, _ := r["error"].(string); e != "" {
			return fmt.Errorf("submit: %s", e)
		}
		return fmt.Errorf("submit failed")
	}
	return nil
}
