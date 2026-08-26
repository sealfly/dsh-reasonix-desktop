package main

import (
	"encoding/json"
	"testing"
)

// TestNormEventType 验证事件名归一化（/started→/start、/done→/end）。
func TestNormEventType(t *testing.T) {
	cases := map[string]string{
		"turn/started":   "turn/start",
		"turn/done":      "turn/end",
		"assistant/chunk": "assistant/chunk",
		"turn/start":     "turn/start",
		"tool/call":      "tool/call",
	}
	for in, want := range cases {
		if got := normEventType(in); got != want {
			t.Errorf("normEventType(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestHandleEventFrameParsing 验证 events.mux 帧解析：
// {type:"server-request", method, payload:{sessionId, event:{type, seq, data}}}。
func TestHandleEventFrameParsing(t *testing.T) {
	frame := `{"type":"server-request","rpcId":"x","method":"assistant/chunk",
		"payload":{"sessionId":"sess-9","event":{"type":"assistant/chunk","seq":77,
		"data":{"chunk":{"type":"text-delta","text":"实时内容"}}}}}`
	got := parseEventFrame([]byte(frame))
	if got == nil {
		t.Fatalf("want wire, got nil")
	}
	if got["kind"] != "text" || got["text"] != "实时内容" {
		t.Errorf("wire = %v", got)
	}
	if got["tabId"] != "sess-9" || got["runtimeEpoch"] != int64(77) {
		t.Errorf("base fields mismatch: %v", got)
	}
	// 无 event 的帧（session/subscribed 等）应被过滤
	if w := parseEventFrame([]byte(`{"type":"server-request","method":"session/subscribed","payload":{"type":"session/subscribed","sessionId":"s","lastSeq":1}}`)); w != nil {
		t.Errorf("session/subscribed should be filtered, got %v", w)
	}
	// 坏 JSON 应安全忽略
	if w := parseEventFrame([]byte(`{bad`)); w != nil {
		t.Errorf("bad json should yield nil, got %v", w)
	}
}

// TestDSHFrameToWire 验证 DSH 事件帧 → WireEvent 转换（对齐旧版 dshEventToWire）。
func TestDSHFrameToWire(t *testing.T) {
	mustJSON := func(v any) json.RawMessage {
		b, _ := json.Marshal(v)
		return b
	}
	cases := []struct {
		name   string
		evType string
		data   json.RawMessage
		kind   string
		check  func(t *testing.T, w map[string]any)
	}{
		{
			name: "turn_started", evType: "turn/started", data: mustJSON(map[string]any{}),
			kind: "turn_started",
		},
		{
			name: "turn_start", evType: "turn/start", data: mustJSON(map[string]any{}),
			kind: "turn_started",
		},
		{
			name: "text_delta", evType: "assistant/chunk",
			data: mustJSON(map[string]any{"chunk": map[string]any{"type": "text-delta", "text": "你好"}}),
			kind: "text",
			check: func(t *testing.T, w map[string]any) {
				if w["text"] != "你好" {
					t.Errorf("text = %v, want 你好", w["text"])
				}
			},
		},
		{
			name: "reasoning_delta", evType: "assistant/chunk",
			data: mustJSON(map[string]any{"chunk": map[string]any{"type": "reasoning-delta", "text": "思考中"}}),
			kind: "reasoning",
			check: func(t *testing.T, w map[string]any) {
				if w["text"] != "思考中" || w["reasoning"] != "思考中" {
					t.Errorf("reasoning text mismatch: %v", w)
				}
			},
		},
		{
			name: "tool_dispatch", evType: "tool/call",
			data: mustJSON(map[string]any{"tool": map[string]any{"name": "shell", "callId": "c1"}}),
			kind: "tool_dispatch",
			check: func(t *testing.T, w map[string]any) {
				tool := w["tool"].(map[string]any)
				if tool["name"] != "shell" || tool["callId"] != "c1" {
					t.Errorf("tool = %v", w["tool"])
				}
			},
		},
		{
			name: "tool_result", evType: "tool/result",
			data: mustJSON(map[string]any{"name": "shell", "callId": "c1", "result": "ok"}),
			kind: "tool_result",
			check: func(t *testing.T, w map[string]any) {
				if w["detail"] != "ok" {
					t.Errorf("detail = %v, want ok", w["detail"])
				}
			},
		},
		{
			name: "turn_done", evType: "turn/done", data: mustJSON(map[string]any{}),
			kind: "turn_done",
		},
		{
			name: "user_prompt_ignored", evType: "user/prompt", data: mustJSON(map[string]any{}),
			kind: "",
		},
		{
			name: "unknown_ignored", evType: "whatever/x", data: mustJSON(map[string]any{}),
			kind: "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := dshFrameToWire("sess-1", c.evType, 42, c.data)
			if c.kind == "" {
				if w != nil {
					t.Errorf("want nil, got %v", w)
				}
				return
			}
			if w == nil {
				t.Fatalf("want kind %q, got nil", c.kind)
			}
			if w["kind"] != c.kind {
				t.Errorf("kind = %v, want %q", w["kind"], c.kind)
			}
			if w["tabId"] != "sess-1" {
				t.Errorf("tabId = %v, want sess-1", w["tabId"])
			}
			if w["runtimeEpoch"] != int64(42) {
				t.Errorf("runtimeEpoch = %v, want 42", w["runtimeEpoch"])
			}
			if c.check != nil {
				c.check(t, w)
			}
		})
	}
}
