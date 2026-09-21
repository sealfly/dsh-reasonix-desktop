package main

// App 的会话恢复桥方法（点击会话后加载消息历史）。
// 前端点击会话 → ResumeSessionPageForTab/ResumeSessionPage → ResumeSession(sessionId)，
// 期待返回消息数组 [{role, content}]。这里透传 DSH session.history，
// 把 assistant/message 事件折叠成消息（含工具调用），符合项目原则：桥只做展示适配，不限制 DSH。

import (
	"encoding/base64"
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
	// 前端把 cursor 当**不透明串**原样回传（见 mockHistorySlice 的 cursor 处理），
	// 我们用它承载 DSH 的 beforeSeq，从而真的能翻到更早的提问。
	before := decodeSeqCursor(fmt.Sprint(req["cursor"]))
	w, err := a.fetchHistoryWindow(sid, before)
	if err != nil || w == nil {
		resumeLog("HistorySliceForTab sid=%q 拉取失败: %v", sid, err)
		return map[string]any{"entries": []any{}, "nextCursor": "", "hasOlder": false,
			"totalTurns": 0, "startTurn": 0, "endTurn": 0, "stale": false, "revisionKnown": false, "source": "dsh",
			"error": fmt.Sprint(err)}
	}
	usersInPage := countUserMessages(w.Msgs)
	total := w.TotalTurns
	if total < usersInPage {
		total = usersInPage
	}
	if total == 0 {
		// 既没有 projections 又没有 user 消息（极短会话/只有摘要）→ 退回消息条数，避免空白
		total = len(w.Msgs)
	}
	// 本页的提问是**会话末尾**的 usersInPage 个 → 全局起始序号 = total - usersInPage
	startTurn := total - usersInPage
	if startTurn < 0 {
		startTurn = 0
	}
	resumeLog("HistorySliceForTab sid=%q 本页消息=%d 提问=%d total=%d startTurn=%d hasMore=%v beforeSeq=%d",
		sid, len(w.Msgs), usersInPage, total, startTurn, w.HasMore, before)

	entries := make([]any, 0, len(w.Msgs))
	question := startTurn
	for i, m := range w.Msgs {
		msg := map[string]any{
			"role":      m.Role,
			"content":   m.Content,
			"reasoning": "",
		}
		turnNo := m.Turn
		if m.Role == "user" {
			question++
			turnNo = question // 全局问题号（1-based），与点位 data-turn = 序号-1 对齐
		}
		entry := map[string]any{
			"entryId": fmt.Sprintf("dsh-%s:t%d:m%d", sid, turnNo, i),
			"turn":    turnNo,
			"order":   i,
			"message": msg,
			"refs":    []any{},
			"note":    nil,
		}
		// Reasonix 历史协议：user 条目需要顶层 role/content/checkpointTurn
		// （问题导航据此构建「第 n 个问题」列表，缺失则全部显示「点击加载」）。
		if m.Role == "user" {
			entry["role"] = "user"
			entry["content"] = m.Content
			entry["checkpointTurn"] = turnNo
			if m.SubmitText != "" {
				entry["submitText"] = m.SubmitText
			}
			if m.CreatedAt > 0 {
				entry["createdAt"] = m.CreatedAt
			}
		}
		entries = append(entries, entry)
	}
	nextCursor := ""
	hasOlder := w.HasMore
	if hasOlder && w.MinSeq > 0 {
		nextCursor = encodeSeqCursor(w.MinSeq)
	}
	return map[string]any{
		"entries":       entries,
		"nextCursor":    nextCursor,
		"hasOlder":      hasOlder,
		"totalTurns":    total,
		"startTurn":     startTurn,
		"endTurn":       total,
		"stale":         false,
		"revision":      0,
		"revisionKnown": false,
		"digest":        "",
		"source":        "dsh",
		"error":         "",
	}
}

// encodeSeqCursor / decodeSeqCursor：把 DSH 的 beforeSeq 装进前端的不透明 cursor。
// 形式与前端 mock 的 cursor 一致（base64 的小 JSON 对象），前端只做透传。
func encodeSeqCursor(seq int64) string {
	if seq <= 0 {
		return ""
	}
	raw, _ := json.Marshal(map[string]any{"seq": seq})
	return base64.StdEncoding.EncodeToString(raw)
}

func decodeSeqCursor(cursor string) int64 {
	c := strings.TrimSpace(cursor)
	if c == "" || c == "<nil>" {
		return 0
	}
	raw, err := base64.StdEncoding.DecodeString(c)
	if err != nil {
		return 0
	}
	var v struct {
		Seq int64 `json:"seq"`
	}
	if json.Unmarshal(raw, &v) != nil {
		return 0
	}
	return v.Seq
}

// historyQuestionWindow 按「提问序号」把消息切成窗口，返回字节区间 [from,to)、提问总数与是否还有更早的提问。
//
// 语义与前端 mockHistoryPage 一致：一"轮" = 一条 user 消息；limit<=0 取默认 60；
// 窗口取**最近 limit 个提问**（含其后的 assistant 消息）。
func historyQuestionWindow(msgs []resumeMessage, startHint int, limit int) (from int, to int, total int, hasOlder bool) {
	if limit <= 0 {
		limit = 60
	}
	if limit > 200 {
		limit = 200
	}
	total = countUserMessages(msgs)
	// 无 user 消息（极短会话/只有摘要）时退回按消息条数计，避免空白
	counted := total
	if counted == 0 {
		counted = len(msgs)
	}
	start := startHint
	if start <= 0 || start > counted {
		start = counted
	}
	begin := start - limit
	if begin < 0 {
		begin = 0
	}
	if total == 0 {
		return begin, start, counted, begin > 0
	}
	// 有 user：把"第 n 个提问"翻译成消息下标
	seen := 0
	fromIdx, toIdx := -1, len(msgs)
	for i, m := range msgs {
		if m.Role != "user" {
			continue
		}
		if seen == begin && fromIdx < 0 {
			fromIdx = i
		}
		if seen == start {
			toIdx = i
			break
		}
		seen++
	}
	if fromIdx < 0 {
		fromIdx = 0
	}
	return fromIdx, toIdx, total, begin > 0
}

// historyPageFromWindow 把一页窗口整理成前端 ResumeSessionPage 期望的形状。
//
// totalTurns 取**会话真实轮次**（projections.sessionStats.turns）：尾页只有 5 条提问时，
// 旧实现把 totalTurns 报成 5（甚至报成消息条数），问题导航因此只画出 5 个点位、
// 与真实 211 次提问完全对不上。
func historyPageFromWindow(w *historyWindow, limit int) map[string]any {
	if w == nil {
		return map[string]any{"messages": []any{}, "startTurn": 0, "endTurn": 0, "totalTurns": 0, "hasOlder": false}
	}
	from, to, total, _ := historyQuestionWindow(w.Msgs, 0, limit)
	msgs := w.Msgs[from:to]
	usersInPage := countUserMessages(msgs)
	startTurn := total - usersInPage
	if startTurn < 0 {
		startTurn = 0
	}
	out := make([]any, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, map[string]any{"role": m.Role, "content": m.Content})
	}
	return map[string]any{
		"messages":   out,
		"startTurn":  startTurn,
		"endTurn":    total,
		"totalTurns": total,
		"hasOlder":   w.HasMore || startTurn > 0,
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
//
// ⚠️ 字段位置差异（2026-09-21 真机踩到，直接导致「问题导航」失效）：
// user/message 事件把内容放在 **data.content**（同级还有 data.role / data.id），
// 而 assistant/message 放在 data.message.content。旧结构只读 data.message.content，
// 于是每条用户提问都被 resumeEventText 判成空串而 continue 掉 →
//   1) mockHistoryPage 的 userCount 恒为 0 → totalTurns 退化成"消息条数"（如 25），
//      问题导航据此画出 25 个点位，却没有任何一条对应真实提问；
//   2) hasOlder 恒为 false → 点位全显示「第 n 个问题（点击加载）」且**点了毫无反应**
//      （前端要点位未加载就会走 requestOlder 分页，但 hasOlder=false 直接返回 false）。
// 修复后 user 条目能正确产出、并带上顺序问题号，导航即可定位与跳转。
type resumeEvent struct {
	Event struct {
		Type string `json:"type"`
		Data struct {
			Turn    int             `json:"turn"`
			Step    int             `json:"step"`
			Role    string          `json:"role"`
			ID      string          `json:"id"`
			Content json.RawMessage `json:"content"`
			Message struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		} `json:"data"`
		Seq  int64 `json:"seq"`
		Time int64 `json:"time"`
	} `json:"event"`
}

// resumeMessage 是前端期待的消息条目。
// Turn/CheckpointTurn 是问题导航(questionNav)的关键：user 条目提供问题边界，
// 前端据此构建「第 n 个问题」导航与跳转；缺失时全部显示「点击加载」且无法跳转。
type resumeMessage struct {
	Role           string `json:"role"`
	Content        string `json:"content"`
	Seq            int64  `json:"seq,omitempty"`
	Turn           int    `json:"turn,omitempty"`
	CheckpointTurn int    `json:"checkpointTurn,omitempty"`
	SubmitText     string `json:"submitText,omitempty"`
	CreatedAt      int64  `json:"createdAt,omitempty"`
}

// historyPayload 是 session.history 的返回结构。
type historyPayload struct {
	Events    []resumeEvent `json:"events"`
	HasMore   bool          `json:"hasMore"`
	Projections map[string]any `json:"projections"`
}

// fetchHistory 调用 DSH session.history 拉取会话事件。
//
// beforeSeq>0 时只取**该序号之前**的一页（实测有效：不带参数返回尾部 34248 事件，
// 带 beforeSeq 返回更早的一页已换窗口）。这是问题导航"点击加载更早提问"的基础。
func (a *App) fetchHistory(sessionID string, beforeSeq int64) (*historyPayload, error) {
	if a.dsh == nil {
		resumeLog("fetchHistory: dsh nil")
		return nil, fmt.Errorf("dsh client not ready")
	}
	payload := map[string]any{"sessionId": sessionID}
	if beforeSeq > 0 {
		payload["beforeSeq"] = beforeSeq
	}
	raw, err := a.dsh.RPC("session.history", payload)
	if err != nil {
		resumeLog("fetchHistory %q beforeSeq=%d err=%v", sessionID, beforeSeq, err)
		return nil, err
	}
	var hp historyPayload
	if err := DecodeRPC(raw, &hp); err != nil {
		return nil, err
	}
	return &hp, nil
}

// sessionTotalTurns 从 projections.sessionStats.turns 取会话真实轮次（= 提问数）。
//
// 为什么不能数消息：DSH 一次只回尾部一页，尾页里的 user/message 条数（实测 5）
// 远小于会话真实轮次（实测 211）；拿它当总数会让问题导航只画出 5 个点位。
func (hp *historyPayload) sessionTotalTurns() int {
	if hp == nil || hp.Projections == nil {
		return 0
	}
	values, _ := hp.Projections["values"].(map[string]any)
	if values == nil {
		return 0
	}
	stats, _ := values["sessionStats"].(map[string]any)
	if stats == nil {
		return 0
	}
	switch t := stats["turns"].(type) {
	case float64:
		return int(t)
	case int:
		return t
	}
	return 0
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

// sessionMessages 把 session.history 的事件折叠成消息数组（按 seq 升序）。
// 关键：DSH 事件流里有 user/message 事件（用户问题），是问题导航的问题边界；
// turn/start 事件提供真实轮次号。assistant/message 一条对应一轮 assistant 输出。
func (a *App) sessionMessages(sessionID string) []resumeMessage {
	w, err := a.fetchHistoryWindow(sessionID, 0)
	if err != nil || w == nil {
		return []resumeMessage{}
	}
	return w.Msgs
}

// historyWindow 是一个 DSH 历史窗口的解析结果。
//
// 为什么需要"窗口"概念：DSH 的 session.history 一次只返回**尾部一页**（实测 34248 事件、
// hasMore=true），要拿更早的提问必须带 `beforeSeq` 再拉一页（实测 beforeSeq 能改变窗口范围）。
// 而会话真实轮次要另取 projections.sessionStats.turns（实测该会话 211 轮，而尾页里只有 5 条
// user/message）—— 旧实现把"尾页消息条数"当总轮次，问题导航于是只画 5 个点位且与真实提问对不上。
type historyWindow struct {
	Msgs       []resumeMessage
	HasMore    bool  // DSH 说还有更早的事件
	MinSeq     int64 // 本页最早事件序号（下一页的 beforeSeq）
	TotalTurns int   // 会话真实轮次（提问数），来自 projections.sessionStats.turns
}

// fetchHistoryWindow 拉取一页历史（beforeSeq>0 表示要更早的一页）并解析。
func (a *App) fetchHistoryWindow(sessionID string, beforeSeq int64) (*historyWindow, error) {
	hp, err := a.fetchHistory(sessionID, beforeSeq)
	if err != nil || hp == nil {
		return nil, err
	}
	w := &historyWindow{
		Msgs:       parseHistoryMessages(hp.Events),
		HasMore:    hp.HasMore,
		TotalTurns: hp.sessionTotalTurns(),
	}
	for _, ev := range hp.Events {
		if ev.Event.Seq > 0 && (w.MinSeq == 0 || ev.Event.Seq < w.MinSeq) {
			w.MinSeq = ev.Event.Seq
		}
	}
	if w.TotalTurns < countUserMessages(w.Msgs) {
		w.TotalTurns = countUserMessages(w.Msgs)
	}
	return w, nil
}

// countUserMessages 统计提问条数。
func countUserMessages(msgs []resumeMessage) int {
	n := 0
	for _, m := range msgs {
		if m.Role == "user" {
			n++
		}
	}
	return n
}

// parseHistoryMessages 把事件流折叠成消息（按 seq 升序，并给提问编顺序号）。
func parseHistoryMessages(events []resumeEvent) []resumeMessage {
	out := make([]resumeMessage, 0, len(events))
	currentTurn := 0
	for _, ev := range events {
		switch ev.Event.Type {
		case "turn/start":
			if ev.Event.Data.Turn > 0 {
				currentTurn = ev.Event.Data.Turn
			}
		case "user/message":
			// 内容在 data.content（不是 data.message.content）—— 见 resumeEvent 上的说明。
			text := resumeEventText(ev.Event.Data.Content)
			if text == "" {
				text = resumeEventText(ev.Event.Data.Message.Content)
			}
			if text == "" {
				continue
			}
			out = append(out, resumeMessage{
				Role:       "user",
				Content:    text,
				Seq:        ev.Event.Seq,
				SubmitText: text,
				CreatedAt:  ev.Event.Time,
			})
		case "assistant/message":
			role := ev.Event.Data.Message.Role
			if role == "" {
				role = "assistant"
			}
			text := resumeEventText(ev.Event.Data.Message.Content)
			if text == "" {
				continue
			}
			turn := ev.Event.Data.Turn
			if turn <= 0 {
				turn = currentTurn
			}
			out = append(out, resumeMessage{
				Role:    role,
				Content: text,
				Seq:     ev.Event.Seq,
				Turn:    turn,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Seq < out[j].Seq })
	// 给每条用户提问编**顺序问题号**（1..N）。
	//
	// 为什么不是 DSH 的 turn：前端问题导航把「第 n 个问题」按 turn 建索引（点位 data-turn = turn-1），
	// 而 DSH 的 turn 是会话级轮次号（本机实测同一 payload 里全是 207/208…），
	// 与点位 0..N-1 完全对不上 → 所有点位都会显示"未加载"且无法跳转。
	// 按提问先后顺序编号后，点位、问题文本、跳转三者才对得上。
	question := 0
	for i := range out {
		if out[i].Role != "user" {
			continue
		}
		question++
		out[i].Turn = question
		if out[i].CheckpointTurn == 0 {
			out[i].CheckpointTurn = question
		}
	}
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
// 前端按 user 轮次计数（user/message 事件即问题边界）。
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
	// 历史极短（如首次会话只有 assistant 摘要）时按消息总数分页兜底
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
	w, err := a.fetchHistoryWindow(sid, 0)
	if err != nil {
		resumeLog("ResumeSessionPage %q 拉取失败: %v", sid, err)
		return map[string]any{"messages": []any{}, "startTurn": 0, "endTurn": 0, "totalTurns": 0, "hasOlder": false}
	}
	page := historyPageFromWindow(w, toLimit(limit))
	resumeLog("ResumeSessionPage %q → messages=%d totalTurns=%v startTurn=%v hasOlder=%v",
		sid, len(page["messages"].([]any)), page["totalTurns"], page["startTurn"], page["hasOlder"])
	return page
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
