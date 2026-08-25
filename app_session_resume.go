package main

// App 的会话恢复桥方法（点击会话后加载消息历史）。
// 前端点击会话 → ResumeSessionPageForTab/ResumeSessionPage → ResumeSession(sessionId)，
// 期待返回消息数组 [{role, content}]。这里透传 DSH session.history，
// 把 assistant/message 事件折叠成消息（含工具调用），符合项目原则：桥只做展示适配，不限制 DSH。

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// DiagStore 前端调试用：把前端捕获的错误/调用写入文件（排查桥接问题）。
func (a *App) DiagStore(msg string) {
	resumeLog("DiagStore: %s", msg)
}

// sessionTabMeta 从 DSH session.list 取指定会话的 tabMeta（找不到返回 nil）。
func (a *App) sessionTabMeta(sessionID string) map[string]any {
	if a.dsh == nil {
		return nil
	}
	raw, err := a.dsh.RPC("session.list", map[string]any{})
	if err != nil {
		return nil
	}
	var list struct {
		Items []dshSession `json:"items"`
	}
	if err := DecodeRPC(raw, &list); err != nil {
		return nil
	}
	for i, s := range list.Items {
		if s.SessionID == sessionID {
			return a.tabMeta(s, i)
		}
	}
	return nil
}

// StartTopicActivationImpl 前端"打开会话"的核心入口（Reasonix 的 topic 激活）。
// 前端期待返回 { requestId, tabId, meta }，meta 就是 tab 对象（含 id/sessionPath/topicId）。
// 原桩返回 error（nil → JS null），导致前端 _.meta 崩溃 → "无法打开会话"。
func (a *App) StartTopicActivationImpl(req map[string]any) map[string]any {
	requestID := ""
	if s, ok := req["requestId"].(string); ok {
		requestID = s
	}
	// 解析会话：优先 topicId（=sessionId），其次 sessionPath（=sessionId.jsonl）
	sid := ""
	if s, ok := req["topicId"].(string); ok {
		sid = cleanSessionID(s)
	}
	if sid == "" {
		if s, ok := req["sessionPath"].(string); ok {
			sid = cleanSessionID(s)
		}
	}
	var tab map[string]any
	if sid != "" {
		tab = a.sessionTabMeta(sid)
	}
	if tab == nil {
		// 无会话标识 → 创建新会话（OpenGlobalTab 语义）
		tab = a.NewSessionForTab("")
	}
	tabID, _ := tab["id"].(string)
	resumeLog("StartTopicActivation -> sid=%q tabId=%q requestId=%q", sid, tabID, requestID)
	// 推 topic:activation 事件（原版 Electron Go 推送 starting→ready；DSH 无此事件，
	// 前端 onTopicActivation 用 window.runtime.EventsOn("topic:activation") 订阅，
	// 不推则前端 on 处理器永不触发 → hydrate 永不执行 → 会话历史空白）。
	if a.ctx != nil && tabID != "" {
		wruntime.EventsEmit(a.ctx, "topic:activation", map[string]any{
			"requestId": requestID, "tabId": tabID, "phase": "starting",
		})
		wruntime.EventsEmit(a.ctx, "topic:activation", map[string]any{
			"requestId": requestID, "tabId": tabID, "phase": "ready",
		})
		resumeLog("topic:activation emitted starting+ready tabId=%q", tabID)
	}
	return map[string]any{
		"requestId": requestID,
		"tabId":     tabID,
		"meta":      tab,
	}
}

// historySliceForTabImpl 前端 hydrate 加载历史的入口（transcript store 的 fetchSlice）。
// 前端期待 { entries:[{entryId,turn,order,message:{role,content}}], nextCursor, hasOlder,
// totalTurns, startTurn, endTurn, stale, revision, revisionKnown, digest, source, error }。
func (a *App) historySliceForTabImpl(tabID string, req map[string]any) map[string]any {
	sid := toSessionID(tabID)
	if sid == "" {
		if p, ok := req["sessionPath"].(string); ok {
			sid = cleanSessionID(p)
		}
	}
	msgs := a.sessionMessages(sid)
	resumeLog("HistorySliceForTab sid=%q msgs=%d req=%v", sid, len(msgs), req)
	entries := make([]any, 0, len(msgs))
	for i, m := range msgs {
		msg := map[string]any{
			"role":      m.Role,
			"content":   m.Content,
			"reasoning": "",
		}
		entries = append(entries, map[string]any{
			"entryId": fmt.Sprintf("dsh-%s:m%d:o0", sid, i),
			"turn":    i,
			"order":   i,
			"message": msg,
			"refs":    []any{},
			"note":    nil,
		})
	}
	return map[string]any{
		"entries":       entries,
		"nextCursor":    "",
		"hasOlder":      false,
		"totalTurns":    len(msgs),
		"startTurn":     0,
		"endTurn":       len(msgs),
		"stale":         false,
		"revision":      0,
		"revisionKnown": false,
		"digest":        "",
		"source":        "dsh",
		"error":         "",
	}
}

func resumeLog(format string, args ...any) {
	f, err := os.OpenFile(os.TempDir()+"\\resume-debug.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s: %s\n", time.Now().Format("15:04:05.000"), fmt.Sprintf(format, args...))
}

// resumeEvent 匹配 DSH session.history 的事件帧（只取需要的字段）。
type resumeEvent struct {
	Event struct {
		Type string `json:"type"`
		Data struct {
			Turn    int `json:"turn"`
			Step    int `json:"step"`
			Message struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		} `json:"data"`
		Seq int64 `json:"seq"`
	} `json:"event"`
}

// resumeMessage 是前端期待的消息条目。
type resumeMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	Seq     int64  `json:"seq,omitempty"`
}

// historyPayload 是 session.history 的返回结构。
type historyPayload struct {
	Events    []resumeEvent `json:"events"`
	HasMore   bool          `json:"hasMore"`
	Projections map[string]any `json:"projections"`
}

// fetchHistory 调用 DSH session.history 拉取会话事件。
func (a *App) fetchHistory(sessionID string) (*historyPayload, error) {
	if a.dsh == nil {
		resumeLog("fetchHistory: dsh nil")
		return nil, fmt.Errorf("dsh client not ready")
	}
	raw, err := a.dsh.RPC("session.history", map[string]any{"sessionId": sessionID})
	if err != nil {
		resumeLog("fetchHistory %q err=%v", sessionID, err)
		return nil, err
	}
	var hp historyPayload
	if err := DecodeRPC(raw, &hp); err != nil {
		return nil, err
	}
	return &hp, nil
}

// resumeEventText 从 message.content（[{type:text,text},...] 或纯字符串）提取纯文本。
func resumeEventText(content json.RawMessage) string {
	if len(content) == 0 || string(content) == "null" {
		return ""
	}
	// 纯字符串形式
	var s string
	if err := json.Unmarshal(content, &s); err == nil {
		return s
	}
	// 块数组形式 [{type:"text",text:"..."},...]
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(content, &blocks); err == nil {
		var b strings.Builder
		for _, blk := range blocks {
			if blk.Type == "text" && blk.Text != "" {
				if b.Len() > 0 {
					b.WriteString("\n")
				}
				b.WriteString(blk.Text)
			}
		}
		return b.String()
	}
	return ""
}

// sessionMessages 把 session.history 的 assistant/message 事件折叠成消息数组。
// 按 seq 升序；assistant/message 一条对应一轮 assistant 输出。
// 用户消息不在事件流里（DSH 事件只有 assistant 侧），前端对空历史会显示会话标题占位。
func (a *App) sessionMessages(sessionID string) []resumeMessage {
	hp, err := a.fetchHistory(sessionID)
	if err != nil || hp == nil {
		return []resumeMessage{}
	}
	out := make([]resumeMessage, 0, len(hp.Events))
	for _, ev := range hp.Events {
		if ev.Event.Type != "assistant/message" {
			continue
		}
		role := ev.Event.Data.Message.Role
		if role == "" {
			role = "assistant"
		}
		text := resumeEventText(ev.Event.Data.Message.Content)
		if text == "" {
			continue
		}
		out = append(out, resumeMessage{
			Role:    role,
			Content: text,
			Seq:     ev.Event.Seq,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Seq < out[j].Seq })
	return out
}

// ResumeSession 返回会话的消息数组（前端 bridge 期待 [{role, content}]）。
func (a *App) ResumeSession(sessionID any) []any {
	sid := toSessionID(sessionID)
	resumeLog("ResumeSession called sessionID=%q(cleaned=%q)", sessionID, sid)
	if sid == "" {
		resumeLog("ResumeSession: empty after clean")
		return []any{}
	}
	msgs := a.sessionMessages(sid)
	resumeLog("ResumeSession: %d messages", len(msgs))
	out := make([]any, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, map[string]any{
			"role":    m.Role,
			"content": m.Content,
		})
	}
	return out
}

// mockHistoryPage 复刻前端 bridge 的分页逻辑（index 页 + 条数窗口）。
// 前端按 user 轮次计数；DSH 事件流没有 user 消息时退化为按消息条数分页。
func mockHistoryPage(msgs []resumeMessage, t int, o int) map[string]any {
	userCount := 0
	for _, m := range msgs {
		if m.Role == "user" {
			userCount++
		}
	}
	limit := o
	if limit <= 0 {
		limit = 60
	}
	if limit > 200 {
		limit = 200
	}
	n := userCount
	// DSH 事件流只有 assistant/message，没有 user 消息时按消息总数分页
	if n == 0 && len(msgs) > 0 {
		n = len(msgs)
	}
	start := t
	if t > 0 && t <= n {
		start = t
	} else if t <= 0 {
		start = n
	}
	from := start - limit
	if from < 0 {
		from = 0
	}
	// 按轮次窗口过滤
	sel := make([]map[string]any, 0, len(msgs))
	if userCount == 0 {
		// 无 user 消息：直接按消息索引窗口
		for i, m := range msgs {
			if i < from || i >= start {
				continue
			}
			sel = append(sel, map[string]any{"role": m.Role, "content": m.Content})
		}
	} else {
		seen := -1
		for _, m := range msgs {
			if m.Role == "user" {
				seen++
			}
			if seen < from || seen >= start {
				continue
			}
			sel = append(sel, map[string]any{"role": m.Role, "content": m.Content})
		}
	}
	return map[string]any{
		"messages":   sel,
		"startTurn":  from,
		"endTurn":    start,
		"totalTurns": n,
		"hasOlder":   from > 0,
	}
}

// toLimit 把前端的 limit 参数（数字或 useRef 对象）解析成 int。
// 前端实际传的是 useRef 对象（如 {current:null}），Wails 绑定 any 不会转换失败。
func toLimit(v any) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case int:
		return t
	case string:
		var n int
		if _, err := fmt.Sscanf(t, "%d", &n); err == nil {
			return n
		}
	case map[string]any:
		if c, ok := t["current"].(float64); ok {
			return int(c)
		}
	}
	return 60
}

// cleanSessionID 容错解析会话标识：去掉 .jsonl / 路径前缀，提取 session-xxx。
func cleanSessionID(s string) string {
	s = strings.TrimSpace(s)
	// 去掉路径（Windows/Unix 分隔符后的最后一段）
	if i := strings.LastIndexAny(s, "/\\"); i >= 0 {
		s = s[i+1:]
	}
	// 去掉 .jsonl 后缀
	s = strings.TrimSuffix(s, ".jsonl")
	return s
}

// toSessionID 把前端的会话标识参数（字符串/对象/路径）统一解析成 sessionId 字符串。
// 前端可能传：sessionId 字符串、"xxx.jsonl" 路径、或会话对象 {id/sessionId/tabId/path/topicId}。
func toSessionID(v any) string {
	switch t := v.(type) {
	case string:
		return cleanSessionID(t)
	case map[string]any:
		for _, k := range []string{"sessionId", "id", "tabId", "topicId", "path"} {
			if s, ok := t[k].(string); ok && s != "" {
				return cleanSessionID(s)
			}
		}
	case nil:
		return ""
	}
	return ""
}

// ResumeSessionPage 恢复会话历史页（前端 bridge: ResumeSessionPage(e, t=60)）。
func (a *App) ResumeSessionPage(sessionID any, limit any) map[string]any {
	sid := toSessionID(sessionID)
	resumeLog("ResumeSessionPage called sessionID=%q(cleaned=%q) limit=%v", sessionID, sid, limit)
	if sid == "" {
		resumeLog("ResumeSessionPage: empty after clean")
		return map[string]any{"messages": []any{}, "startTurn": 0, "endTurn": 0, "totalTurns": 0, "hasOlder": false}
	}
	return mockHistoryPage(a.sessionMessages(sid), 0, toLimit(limit))
}

// ResumeSessionPageForTab 恢复指定 tab 的会话历史页（前端 bridge: ResumeSessionPageForTab(e, t, o=60)）。
func (a *App) ResumeSessionPageForTab(tabID, sessionID any, limit any) map[string]any {
	sid := toSessionID(sessionID)
	resumeLog("ResumeSessionPageForTab called tabID=%v sessionID=%q(cleaned=%q) limit=%v", tabID, sessionID, sid, limit)
	return a.ResumeSessionPage(sid, limit)
}

// ResumeSessionForTab 恢复指定 tab 的会话消息（前端 bridge: ResumeSessionForTab(e, t)）。
func (a *App) ResumeSessionForTab(tabID, sessionID any) []any {
	return a.ResumeSession(sessionID)
}
