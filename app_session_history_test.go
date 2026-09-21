package main

// app_session_history_test.go — 锁住「问题导航（导轨）」依赖的两件事：
//
//	1) DSH 的 user/message 事件把内容放在 **data.content**（不是 data.message.content）——
//	   读错位置会让每条用户提问被丢弃，问题导航于是把 totalTurns 退化成"消息条数"、
//	   并把 hasOlder 报成 false，界面上表现为：点位全是「第 n 个问题（点击加载）」且**点了没反应**。
//	2) 用户提问必须按**顺序问题号**编号（1..N），而不是 DSH 的会话级 turn（实测同一 payload
//	   里全是 207/208…），否则点位 data-turn（0..N-1）与问题索引对不上，全部显示"未加载"。
//
// 用 httptest 伪造 session.history，不依赖真实 DSH。

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeHistoryServer 返回一个伪造的 session.history 端点（信封与真实一致：{result:{ok,value}}）。
// totalTurns 通过 projections.sessionStats.turns 提供（真实 DSH 就是这么给的）。
func fakeHistoryServer(t *testing.T, events []map[string]any, hasMore bool) *httptest.Server {
	t.Helper()
	return fakeHistoryServerWithTurns(t, events, hasMore, 0)
}

func fakeHistoryServerWithTurns(t *testing.T, events []map[string]any, hasMore bool, turns int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		value := map[string]any{
			"events":      events,
			"hasMore":     hasMore,
			"projections": map[string]any{"asOfSeq": 999, "values": map[string]any{"sessionStats": map[string]any{"turns": turns}}},
		}
		resp := map[string]any{"result": map[string]any{"ok": true, "value": value}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

// userEvent 造一条**真实形状**的 user/message 事件（内容在 data.content）。
func userEvent(seq int, text string) map[string]any {
	return map[string]any{"event": map[string]any{
		"type": "user/message",
		"seq":  seq,
		"time": 1789381378015,
		"data": map[string]any{
			"content": []any{map[string]any{"type": "text", "text": text}},
			"role":    "user",
			"id":      "q-" + text,
		},
	}}
}

// assistantEvent 造一条 assistant/message（内容在 data.message.content，并带 DSH 的会话级 turn）。
func assistantEvent(seq int, turn int, text string) map[string]any {
	return map[string]any{"event": map[string]any{
		"type": "assistant/message",
		"seq":  seq,
		"time": 1789380198042,
		"data": map[string]any{
			"turn": turn,
			"step": 1,
			"message": map[string]any{
				"role":    "assistant",
				"content": []any{map[string]any{"type": "text", "text": text}},
			},
		},
	}}
}

// toolOnlyAssistant 造一条只有工具调用、没有文本的 assistant/message（应被跳过）。
func toolOnlyAssistant(seq int, turn int) map[string]any {
	return map[string]any{"event": map[string]any{
		"type": "assistant/message",
		"seq":  seq,
		"data": map[string]any{
			"turn": turn,
			"message": map[string]any{
				"role":    "assistant",
				"content": []any{map[string]any{"type": "tool-call", "name": "read"}},
			},
		},
	}}
}

func turnStartEvent(seq int, turn int) map[string]any {
	return map[string]any{"event": map[string]any{"type": "turn/start", "seq": seq, "data": map[string]any{"turn": turn}}}
}

func TestSessionMessagesRecognizesUserContentAtDataLevel(t *testing.T) {
	srv := fakeHistoryServer(t, []map[string]any{
		turnStartEvent(1, 207),
		userEvent(2, "第一个问题"),
		assistantEvent(3, 207, "回答一"),
		toolOnlyAssistant(4, 207),
		turnStartEvent(5, 208),
		userEvent(6, "第二个问题"),
		assistantEvent(7, 208, "回答二"),
	}, false)
	defer srv.Close()

	a := &App{dsh: clientFor(t, srv)}
	msgs := a.sessionMessages("session-test")

	users := 0
	for _, m := range msgs {
		if m.Role == "user" {
			users++
		}
	}
	if users != 2 {
		t.Fatalf("应识别出 2 条用户提问（内容在 data.content），实际 %d；msgs=%+v", users, msgs)
	}

	// 问题号必须是 1..N（顺序），不是 DSH 的 207/208
	idx := 0
	for _, m := range msgs {
		if m.Role != "user" {
			continue
		}
		idx++
		if m.Turn != idx {
			t.Fatalf("第 %d 条提问的 turn 应为 %d（顺序问题号），实际 %d", idx, idx, m.Turn)
		}
		if m.CheckpointTurn != idx {
			t.Fatalf("第 %d 条提问的 checkpointTurn 应为 %d，实际 %d", idx, idx, m.CheckpointTurn)
		}
	}
	// 无文本的 tool-only assistant 不应产出条目
	for _, m := range msgs {
		if strings.Contains(m.Content, "tool-call") {
			t.Fatalf("tool-only 消息不应成为条目：%+v", m)
		}
	}
}

func TestHistorySliceReportsTrueTotalsAndCursor(t *testing.T) {
	// 真实情形：会话共 211 轮，而 DSH 尾部一页只给 5 条提问，hasMore=true。
	// 期望：totalTurns=211（导轨据此画 211 个点位）、startTurn=206（本页是最后 5 个提问）、
	//       hasOlder=true 且给出 nextCursor（客户端据此能翻到更早的提问）。
	var events []map[string]any
	seq := 1000
	for i := 1; i <= 5; i++ {
		events = append(events, userEvent(seq, "问题"+string(rune('0'+i))))
		seq++
		events = append(events, assistantEvent(seq, 200+i, "回答"+string(rune('0'+i))))
		seq++
	}
	srv := fakeHistoryServerWithTurns(t, events, true, 211)
	defer srv.Close()

	a := &App{dsh: clientFor(t, srv)}
	got := a.HistorySliceForTab("session-test", map[string]any{})

	if total, _ := got["totalTurns"].(int); total != 211 {
		t.Fatalf("totalTurns 应取会话真实轮次 211（旧实现拿尾页条数当总数，导轨只画 5 个点位），实际 %v", got["totalTurns"])
	}
	if st, _ := got["startTurn"].(int); st != 206 {
		t.Fatalf("startTurn 应为 211-5=206（本页为最后 5 个提问），实际 %v", got["startTurn"])
	}
	if ho, _ := got["hasOlder"].(bool); !ho {
		t.Fatalf("hasMore=true 时 hasOlder 必须为 true（否则「点击加载」点不动），实际 %v", got["hasOlder"])
	}
	cursor, _ := got["nextCursor"].(string)
	if cursor == "" {
		t.Fatal("应给出 nextCursor 以便翻更早的提问")
	}
	if decodeSeqCursor(cursor) != 1000 {
		t.Fatalf("cursor 应承载本页最早事件序号 1000，实际 %d", decodeSeqCursor(cursor))
	}

	// user 条目的 turn 应是**全局问题号**（207..211），与导轨点位一一对应
	entries, _ := got["entries"].([]any)
	var turns []int
	for _, e := range entries {
		m, _ := e.(map[string]any)
		if m["role"] == "user" {
			if n, ok := m["turn"].(int); ok {
				turns = append(turns, n)
			}
		}
	}
	if len(turns) != 5 || turns[0] != 207 || turns[4] != 211 {
		t.Fatalf("user 条目的 turn 应为全局问题号 207..211，实际 %v", turns)
	}
}

func TestHistorySliceCursorRoundTrip(t *testing.T) {
	// 带上一页的 cursor 再请求时，应把 beforeSeq 传给 DSH（本测试的假 DSH 只校验能正常返回）
	var seenBefore []any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Payload map[string]any `json:"payload"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		seenBefore = append(seenBefore, req.Payload["beforeSeq"])
		value := map[string]any{"events": []any{userEvent(500, "更早的问题")}, "hasMore": false,
			"projections": map[string]any{"values": map[string]any{"sessionStats": map[string]any{"turns": 211}}}}
		_ = json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"ok": true, "value": value}})
	}))
	defer srv.Close()

	a := &App{dsh: clientFor(t, srv)}
	got := a.HistorySliceForTab("session-test", map[string]any{"cursor": encodeSeqCursor(777)})
	if len(seenBefore) != 1 || seenBefore[0] == nil {
		t.Fatalf("应把 cursor 里的 seq 作为 beforeSeq 传给 DSH，实际 %v", seenBefore)
	}
	if v, _ := seenBefore[0].(float64); int64(v) != 777 {
		t.Fatalf("beforeSeq 应为 777，实际 %v", seenBefore[0])
	}
	if ho, _ := got["hasOlder"].(bool); ho {
		t.Fatalf("hasMore=false 时 hasOlder 应为 false，实际 %v", got["hasOlder"])
	}
	if c, _ := got["nextCursor"].(string); c != "" {
		t.Fatalf("没有更早数据时不应给 cursor，实际 %q", c)
	}
}

func TestHistorySliceWithoutUserMessagesStillLoads(t *testing.T) {
	// 极端情况：历史里没有任何 user/message（只有摘要）→ 不能报 totalTurns=0 而让导轨空白
	srv := fakeHistoryServer(t, []map[string]any{
		assistantEvent(1, 1, "只有助手摘要"),
	}, false)
	defer srv.Close()

	a := &App{dsh: clientFor(t, srv)}
	got := a.HistorySliceForTab("session-test", map[string]any{})
	if total, _ := got["totalTurns"].(int); total != 1 {
		t.Fatalf("无 user 消息时应退回按消息条数计（1），实际 %v", got["totalTurns"])
	}
	if ho, _ := got["hasOlder"].(bool); ho {
		t.Fatalf("只有一条消息时 hasOlder 应为 false，实际 %v", got["hasOlder"])
	}
}

func TestHistoryQuestionWindowBounds(t *testing.T) {
	msgs := []resumeMessage{
		{Role: "user", Content: "q1"}, {Role: "assistant", Content: "a1"},
		{Role: "user", Content: "q2"}, {Role: "assistant", Content: "a2"},
		{Role: "user", Content: "q3"}, {Role: "assistant", Content: "a3"},
	}
	// 取最近 1 个提问
	from, to, total, hasOlder := historyQuestionWindow(msgs, 0, 1)
	if total != 3 || !hasOlder {
		t.Fatalf("total/hasOlder 错误：total=%d hasOlder=%v", total, hasOlder)
	}
	if from != 4 || to != len(msgs) {
		t.Fatalf("窗口应为 [4,%d)，实际 [%d,%d)", len(msgs), from, to)
	}
	// limit 大于总数 → 全量、无更早
	from2, to2, _, hasOlder2 := historyQuestionWindow(msgs, 0, 99)
	if from2 != 0 || to2 != len(msgs) || hasOlder2 {
		t.Fatalf("全量窗口错误：from=%d to=%d hasOlder=%v", from2, to2, hasOlder2)
	}
	// limit<=0 → 默认 60（全量）
	from3, to3, _, _ := historyQuestionWindow(msgs, 0, 0)
	if from3 != 0 || to3 != len(msgs) {
		t.Fatalf("默认 limit 应取全量：from=%d to=%d", from3, to3)
	}
}
