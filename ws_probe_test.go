package main

// WS 双向通道协议探针：验证 DSH events.mux WebSocket 是否接受 client-request 帧
// 并以 client-response 回包（这是 app_submit.go submitViaWS 备用通道的前提）。
//
// 用只读方法 session.list 探测，不创建任何会话；DSH 未运行时自动 skip。

import (
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func dshReachable() bool {
	c, err := net.DialTimeout("tcp", "127.0.0.1:3080", 2*time.Second)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}

// TestWsProbeProtocol 是**上行能力哨兵**：固化 DSH 当前的实测约束——
// 两条 WebSocket（events.mux / events.host）均为仅下行（服务端 registerDownlink），
// 向其写 client-request 会被 close 1008 "policy violation: downlink only" 断开，
// 因此提交上行只有 HTTP POST /api/<method>（见 app_submit.go 的通道链）。
//
// 测试语义：DSH 若**未来开放上行 WS**（本测试开始收到 client-response），
// 这里会失败 → 提醒在 submitViaWS 中接上该通道（升级韧性，原则 6）。
func TestWsProbeProtocol(t *testing.T) {
	if !dshReachable() {
		t.Skip("DSH 3080 未运行，跳过 WS 协议探针")
	}
	conn, _, err := websocket.DefaultDialer.Dial("ws://127.0.0.1:3080/api/events.mux", nil)
	if err != nil {
		t.Fatalf("dial events.mux: %v", err)
	}
	defer conn.Close()

	frame := map[string]any{
		"type":    "client-request",
		"rpcId":   "wsprobe-session-list",
		"method":  "session.list",
		"payload": map[string]any{},
	}
	raw, _ := json.Marshal(frame)
	if err := conn.WriteMessage(websocket.TextMessage, raw); err != nil {
		t.Fatalf("write client-request: %v", err)
	}
	t.Logf("sent: %s", raw)

	deadline := time.Now().Add(8 * time.Second)
	_ = conn.SetReadDeadline(deadline)
	gotResponse := false
	downlinkOnly := false
	for time.Now().Before(deadline) {
		_, data, err := conn.ReadMessage()
		if err != nil {
			t.Logf("read ended: %v", err)
			if strings.Contains(err.Error(), "downlink only") {
				downlinkOnly = true
			}
			break
		}
		var head struct {
			Type  string `json:"type"`
			RPCID string `json:"rpcId"`
		}
		_ = json.Unmarshal(data, &head)
		if head.Type == "client-response" && head.RPCID == "wsprobe-session-list" {
			gotResponse = true
			break
		}
	}
	switch {
	case gotResponse:
		t.Errorf("DSH 已支持上行 WebSocket！请在 submitViaWS 中接上该通道（备用通道升级，原则 6），并更新本哨兵测试")
	case downlinkOnly:
		t.Log("确认：events.mux 为仅下行通道（close 1008 downlink only）——上行走 HTTP POST /api/<method>，符合当前通道设计")
	default:
		t.Log("未观察到 client-response，也未见 downlink-only 关闭帧（DSH 行为可能变化，观察即可）")
	}
}
