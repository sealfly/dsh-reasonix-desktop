package main

// 提交多通道回退测试：HTTP 主通道失败 → 备用通道 → 持久化队列 → 恢复补发。
// 用 httptest 模拟 DSH 端点，不依赖真实 DSH。

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// clientFor 从 httptest server 造一个 DshClient。
func clientFor(t *testing.T, srv *httptest.Server) *DshClient {
	t.Helper()
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatalf("parse port: %v", err)
	}
	return NewDshClientAt(u.Hostname(), port)
}

// okDshServer 返回 accepted:true 的 DSH 桩。
func okDshServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":{"ok":true,"value":{"accepted":true}}}`))
	}))
}

// failDshServer 始终 500 的 DSH 桩。
func failDshServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
}

func newSubmitTestApp(t *testing.T) *App {
	t.Helper()
	return &App{submitQueuePath: filepath.Join(t.TempDir(), "submit-queue.json")}
}

// 主通道正常 → channel=http。
func TestSubmitPrimaryHTTP(t *testing.T) {
	srv := okDshServer(t)
	defer srv.Close()
	a := newSubmitTestApp(t)
	a.dsh = clientFor(t, srv)

	res := a.submitPrompt("session-1", "hello")
	if !res.OK || res.Channel != "http" {
		t.Fatalf("want ok/http got %+v", res)
	}
	if n := a.SubmitQueueStatus()["count"].(int); n != 0 {
		t.Errorf("queue should stay empty, got %d", n)
	}
}

// 主通道 500 → 备用通道成功 → channel=http-alt。
func TestSubmitFallbackToAltChannel(t *testing.T) {
	bad := failDshServer(t)
	defer bad.Close()
	good := okDshServer(t)
	defer good.Close()

	a := newSubmitTestApp(t)
	a.dsh = clientFor(t, bad)                 // 主通道坏
	a.dshAlts = []*DshClient{clientFor(t, good)} // 备用通道好

	res := a.submitPrompt("session-2", "hello alt")
	if !res.OK || res.Channel != "http-alt" {
		t.Fatalf("want ok/http-alt got %+v", res)
	}
}

// 主 + 备用全失败 → 入兜底队列（ok=false, channel=queue），输入不丢。
func TestSubmitQueuesWhenAllChannelsFail(t *testing.T) {
	bad1 := failDshServer(t)
	defer bad1.Close()
	bad2 := failDshServer(t)
	defer bad2.Close()

	a := newSubmitTestApp(t)
	a.dsh = clientFor(t, bad1)
	a.dshAlts = []*DshClient{clientFor(t, bad2)}

	res := a.submitPrompt("session-3", "queue me")
	if res.OK || res.Channel != "queue" {
		t.Fatalf("want fail/queue got %+v", res)
	}
	if res.Error == "" {
		t.Error("queue result should carry a user-visible explanation")
	}
	st := a.SubmitQueueStatus()
	if st["count"].(int) != 1 {
		t.Fatalf("queue count want 1 got %v", st["count"])
	}
	items := st["items"].([]any)
	first := items[0].(map[string]any)
	if first["sessionId"] != "session-3" || first["preview"] != "queue me" {
		t.Errorf("queued item wrong: %+v", first)
	}
}

// DSH 恢复后补发：flushed 条目从队列移除。
func TestSubmitQueueFlushOnRecovery(t *testing.T) {
	bad1 := failDshServer(t)
	defer bad1.Close()
	bad2 := failDshServer(t)
	defer bad2.Close()

	a := newSubmitTestApp(t)
	a.dsh = clientFor(t, bad1)
	a.dshAlts = []*DshClient{clientFor(t, bad2)}
	if res := a.submitPrompt("session-4", "recover me"); res.Channel != "queue" {
		t.Fatalf("setup failed: %+v", res)
	}
	if a.SubmitQueueStatus()["count"].(int) != 1 {
		t.Fatal("setup: expected 1 queued item")
	}

	// DSH 恢复
	good := okDshServer(t)
	defer good.Close()

	var gotPrompt string
	hits := 0
	rec := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method  string `json:"method"`
			Payload struct {
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
			} `json:"payload"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		hits++
		if len(req.Payload.Content) > 0 {
			gotPrompt = req.Payload.Content[0].Text
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":{"ok":true,"value":{"accepted":true}}}`))
	}))
	defer rec.Close()
	a.dsh = clientFor(t, rec)

	sent := a.flushSubmitQueue()
	if sent != 1 {
		t.Fatalf("flush want 1 sent got %d", sent)
	}
	if gotPrompt != "recover me" {
		t.Errorf("recovered prompt = %q want %q", gotPrompt, "recover me")
	}
	if a.SubmitQueueStatus()["count"].(int) != 0 {
		t.Errorf("queue should be empty after flush: %v", a.SubmitQueueStatus())
	}
}

// 队列超上限 → 丢最旧，防无限增长。
func TestSubmitQueueCapsSize(t *testing.T) {
	a := newSubmitTestApp(t)
	for i := 0; i < submitQueueMax+5; i++ {
		if err := a.enqueuePrompt("s", "p", "boom"); err != nil {
			t.Fatalf("enqueue: %v", err)
		}
	}
	st := a.SubmitQueueStatus()
	if st["count"].(int) != submitQueueMax {
		t.Fatalf("queue cap: want %d got %v", submitQueueMax, st["count"])
	}
}

// WS 通道按 DSH 实测能力返回明确诊断（不实际发送）。
func TestSubmitViaWSReportsDownlinkOnly(t *testing.T) {
	a := newSubmitTestApp(t)
	res := a.submitViaWS("session-x", "hello")
	if res.OK {
		t.Fatal("WS submit must not report success on downlink-only DSH")
	}
	if res.Channel != "ws" || res.Error == "" {
		t.Fatalf("WS probe should explain why: %+v", res)
	}
}

// 备用通道默认生成：排除主 host 本身，补 localhost/IPv6 环回。
func TestDefaultAltDshClients(t *testing.T) {
	alts := defaultAltDshClients("127.0.0.1", 3080)
	if len(alts) != 2 {
		t.Fatalf("want 2 alts (localhost, ::1) got %d", len(alts))
	}
	for _, c := range alts {
		if c.port != 3080 {
			t.Errorf("alt port = %d want 3080", c.port)
		}
		if c.host == "127.0.0.1" {
			t.Error("alt must not duplicate the primary host")
		}
	}
	// DSH 短超时不受影响；备用客户端也应配置超时
	if alts[0].http == nil || alts[0].http.Timeout <= 0 {
		t.Error("alt client needs an http timeout")
	}
	_ = time.Second
}
