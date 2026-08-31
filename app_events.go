package main

// 通路 A：DSH 实时事件流 → EventsEmit("agent:event")。
// Reasonix 前端有两条内容通路：A. agent:event 事件流（实时/历史回放，前端从流重建会话）；
// B. topic:activation → hydrate → HistorySliceForTab 拉历史。Wails 版此前只有 B。
// 这里补齐 A：连接 DSH WebSocket 事件流(ws://127.0.0.1:3080/api/events.mux)，
// 把事件帧转成 Reasonix WireEvent 后经 EventsEmit("agent:event") 推给前端，
// 对齐 Electron 旧版 main.js(dsh:event) → preload(agent:event) 的链路。

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

var eventStreamOnce sync.Once

// ===== DSH 询问/审批 pending 表（回答 AnswerQuestionForTab / ApproveTab 用）=====
// DSH 在 events.mux 上以 server-request 帧发出 ask 询问（approval/requested、question/requested），
// 帧顶层 rpcId 是回答时的回显令牌：客户端回答时 POST /api/respond 带 {type:"client-response",
// rpcId:<回显>, result:{ok:true, value:{...}}}。这里按 sessionId 缓存最近一个 pending，
// 前端回答（AnswerQuestionForTab / ApproveTab）时据此构造 client-response。

type pendingAsk struct {
	RPCID      string
	SessionID  string
	ApprovalID string // approval/requested 专用
	ToolName   string
	CallID     string
	Questions  []map[string]any // question/requested 专用
}

var (
	pendingAskMu sync.Mutex
	pendingAsks  = map[string]pendingAsk{} // key: sessionId
)

// cachePendingAsk 记录一个可回答的 server-request 帧（approval/question）。
func cachePendingAsk(frame struct {
	RPCID  string `json:"rpcId"`
	Method string `json:"method"`
	Payload struct {
		SessionID  string           `json:"sessionId"`
		ApprovalID string           `json:"approvalId"`
		ToolName   string           `json:"toolName"`
		CallID     string           `json:"callId"`
		Questions  []map[string]any `json:"questions"`
	} `json:"payload"`
}) bool {
	if frame.Payload.SessionID == "" {
		return false
	}
	switch frame.Method {
	case "approval/requested", "question/requested":
		pendingAskMu.Lock()
		pendingAsks[frame.Payload.SessionID] = pendingAsk{
			RPCID:      frame.RPCID,
			SessionID:  frame.Payload.SessionID,
			ApprovalID: frame.Payload.ApprovalID,
			ToolName:   frame.Payload.ToolName,
			CallID:     frame.Payload.CallID,
			Questions:  frame.Payload.Questions,
		}
		pendingAskMu.Unlock()
		resumeLog("cached pending ask method=%s session=%s rpcId=%s", frame.Method, frame.Payload.SessionID, frame.RPCID)
		return true
	}
	return false
}

// takePendingAsk 取走并清除某会话的 pending 询问（回答一次后即失效）。
func takePendingAsk(sessionID string) (pendingAsk, bool) {
	pendingAskMu.Lock()
	defer pendingAskMu.Unlock()
	p, ok := pendingAsks[sessionID]
	if ok {
		delete(pendingAsks, sessionID)
	}
	return p, ok
}

// startEventStream 启动 DSH 实时事件流转发（幂等，只启动一个连接循环）。
func (a *App) startEventStream() {
	eventStreamOnce.Do(func() {
		go a.eventStreamLoop()
	})
}

// eventStreamLoop 连接/重连 DSH 事件流，读帧转发（DSH 未启动时循环重试）。
func (a *App) eventStreamLoop() {
	for {
		if a.ctx == nil {
			time.Sleep(2 * time.Second)
			continue
		}
		conn, _, err := websocket.DefaultDialer.Dial("ws://127.0.0.1:3080/api/events.mux", nil)
		if err != nil {
			time.Sleep(3 * time.Second)
			continue
		}
		resumeLog("agent:event stream connected (events.mux)")
		ok := true
		for ok {
			_, data, err := conn.ReadMessage()
			if err != nil {
				ok = false
				continue
			}
			a.handleEventFrame(data)
		}
		_ = conn.Close()
		resumeLog("agent:event stream disconnected, retrying")
		time.Sleep(3 * time.Second)
	}
}

// handleEventFrame 解析一帧 DSH 事件并转发为 WireEvent。
func (a *App) handleEventFrame(data []byte) {
	// 先识别可回答帧（approval/requested、question/requested）→ 缓存 pending 供回答。
	var frame struct {
		RPCID  string `json:"rpcId"`
		Method string `json:"method"`
		Payload struct {
			SessionID  string           `json:"sessionId"`
			ApprovalID string           `json:"approvalId"`
			ToolName   string           `json:"toolName"`
			CallID     string           `json:"callId"`
			Questions  []map[string]any `json:"questions"`
		} `json:"payload"`
	}
	if json.Unmarshal(data, &frame) == nil && cachePendingAsk(frame) {
		return
	}
	wire := parseEventFrame(data)
	if wire == nil {
		return
	}
	if a.ctx != nil {
		wruntime.EventsEmit(a.ctx, "agent:event", wire)
	}
	// 低频日志：text/reasoning 高频帧只计数，其余帧（turn/tool 等）逐条记录。
	kind, _ := wire["kind"].(string)
	if kind != "text" && kind != "reasoning" {
		resumeLog("agent:event emit kind=%q tabId=%v", kind, wire["tabId"])
	} else {
		n := eventFrameCounter.Add(1)
		if n%50 == 1 {
			resumeLog("agent:event emit %s x%d (tabId=%v)", kind, n, wire["tabId"])
		}
	}
}

// parseEventFrame 解析 events.mux 帧 → WireEvent。
// 帧结构：{type:"server-request", rpcId, method, payload}；内容事件的
// payload = {sessionId, event:{type, seq, data}}（对齐旧版 subscribe 透传 payload 后
// dshEventToWire(frame) 读取 frame.sessionId + frame.event 的约定；
// session/subscribed、session/jobs 等无 event 字段的帧返回 nil 被过滤）。
func parseEventFrame(data []byte) map[string]any {
	var frame struct {
		Method  string `json:"method"`
		Payload struct {
			SessionID string `json:"sessionId"`
			Event     struct {
				Type string          `json:"type"`
				Seq  int64           `json:"seq"`
				Data json.RawMessage `json:"data"`
			} `json:"event"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(data, &frame); err != nil {
		return nil
	}
	return dshFrameToWire(frame.Payload.SessionID, frame.Payload.Event.Type, frame.Payload.Event.Seq, frame.Payload.Event.Data)
}

var eventFrameCounter atomic.Int64

// normEventType 对齐旧版：turn/started → turn/start；turn/done → turn/end。
func normEventType(t string) string {
	if strings.HasSuffix(t, "/started") {
		return t[:len(t)-len("/started")] + "/start"
	}
	if strings.HasSuffix(t, "/done") {
		return t[:len(t)-len("/done")] + "/end"
	}
	return t
}

// dshFrameToWire 对齐旧版 main.js dshEventToWire：DSH 事件帧 → Reasonix WireEvent。
// 帧结构 {sessionId, event:{type, seq, data}}；wire 结构 {kind, tabId, runtimeEpoch, ...}。
func dshFrameToWire(sessionID, evType string, seq int64, dataJSON json.RawMessage) map[string]any {
	if sessionID == "" || evType == "" {
		return nil
	}
	base := map[string]any{"tabId": sessionID, "runtimeEpoch": seq}
	withKind := func(m map[string]any, k string) map[string]any {
		m["kind"] = k
		return m
	}
	var d struct {
		Chunk *struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"chunk"`
		Tool *struct {
			Name   string `json:"name"`
			CallID string `json:"callId"`
		} `json:"tool"`
		Name     string `json:"name"`
		ToolName string `json:"toolName"`
		CallID   string `json:"callId"`
		Result   any    `json:"result"`
	}
	_ = json.Unmarshal(dataJSON, &d)
	toolName := ""
	callID := ""
	if d.Tool != nil {
		toolName = d.Tool.Name
		callID = d.Tool.CallID
	}
	if toolName == "" {
		toolName = d.Name
	}
	if toolName == "" {
		toolName = d.ToolName
	}
	if toolName == "" {
		toolName = "tool"
	}
	if callID == "" {
		callID = d.CallID
	}
	switch normEventType(evType) {
	case "turn/start":
		return withKind(base, "turn_started")
	case "assistant/chunk":
		if d.Chunk == nil {
			return nil
		}
		switch d.Chunk.Type {
		case "reasoning-delta":
			base["text"] = d.Chunk.Text
			base["reasoning"] = d.Chunk.Text
			return withKind(base, "reasoning")
		case "text-delta":
			base["text"] = d.Chunk.Text
			return withKind(base, "text")
		case "block-start":
			base["text"] = ""
			return withKind(base, "text")
		}
		return nil
	case "tool/call":
		base["tool"] = map[string]any{"name": toolName, "callId": callID}
		return withKind(base, "tool_dispatch")
	case "tool/result":
		base["tool"] = map[string]any{"name": toolName, "callId": callID}
		if d.Result != nil {
			detail := fmt.Sprintf("%v", d.Result)
			if len(detail) > 500 {
				detail = detail[:500]
			}
			base["detail"] = detail
		}
		return withKind(base, "tool_result")
	case "turn/end", "assistant/end":
		return withKind(base, "turn_done")
	default:
		return nil
	}
}
