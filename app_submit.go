package main

// app_submit.go — 任务提交适配器（把前端对话提交转发给 DSH 执行）。
//
// 设计目标（贴合项目精神 + 预留 DSH 升级接口）：
//  1. 贴合 DSH：走 DSH 原生 RPC（session.prompt），不碰内部存储。
//  2. 适配器模式：提交通道封装为多通道链，DSH 协议升级时只改适配器，
//     前端桥方法（SubmitToTabWithID 等）保持不变。
//  3. 多通道回退（原则 3：失败留痕、兜底不崩溃）：
//       HTTP 主通道 → HTTP 备用通道（同一 DSH 的其他网络路径 localhost/[::1]）
//       → 持久化兜底队列（DSH 恢复后自动补发，用户输入不丢） → 日志兜底
//  4. 可配置：endpoint/payload 模板可配（DSH 变更时改配置即可接入）。
//
// 关于 WebSocket 上行：DSH 0.1.1-rc.2 的两条 WebSocket（/api/events.mux、
// /api/events.host）均以 registerDownlink 注册为**仅下行**，向其写 client-request
// 会被服务端以 close 1008 "policy violation: downlink only" 断开（已实测，
// 见 ws_probe_test.go）。因此上行只有 HTTP POST /api/<method>（服务端 assertChannel
// 校验通道名）；submitViaWS 保留为能力探测接口（未来 DSH 支持上行 WS 时可启用）。

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ===== 提交通道配置（DSH 升级时调整这里）=====

// dshPromptEndpoint DSH 提交 prompt 的 RPC 方法名。
// DSH 不同版本可能用 session.prompt / agent.prompt / session.submit 等，
// 升级时改这一个常量即可，前端无需改动。
const dshPromptEndpoint = "session.prompt"

// submitQueueMax 兜底队列上限（防无限增长）。
const submitQueueMax = 100

// submitConfig 提交配置（预留，可扩展为读配置文件）。
type submitConfig struct {
	// 尝试顺序：http → http-alt → queue → log
	UseHTTP     bool
	UseHTTPAlt  bool
	UseQueue    bool
	HTTPTimeout time.Duration
	// 是否记录提交日志（兜底）
	LogFallback bool
}

// defaultSubmitConfig 默认提交配置。
func defaultSubmitConfig() submitConfig {
	return submitConfig{
		UseHTTP:     true,
		UseHTTPAlt:  true,
		UseQueue:    true,
		HTTPTimeout: 15 * time.Second,
		LogFallback: true,
	}
}

// defaultAltDshClients 备用上行通道：同一 DSH 实例的其他网络路径。
// 主通道用配置的 host（通常 127.0.0.1）；备用补上 localhost 与 IPv6 环回——
// 网络栈/DNS 差异（IPv4 栈异常、hosts 映射、代理拦截单一地址）下仍能到达同一后端。
func defaultAltDshClients(host string, port int) []*DshClient {
	seen := map[string]bool{}
	var alts []*DshClient
	for _, h := range []string{"127.0.0.1", "localhost", "::1"} {
		if strings.EqualFold(h, host) || seen[h] {
			continue
		}
		seen[h] = true
		alts = append(alts, NewDshClientAt(h, port))
	}
	return alts
}

// ===== 提交结果 =====

// SubmitResult 提交结果（前端读取 ok/error/channel）。
type SubmitResult struct {
	OK      bool   `json:"ok"`
	Channel string `json:"channel"` // http/http-alt/queue/ws/log
	Error   string `json:"error,omitempty"`
}

// ===== 核心：提交 prompt 到 DSH =====

// submitPrompt 把用户输入提交给 DSH 执行（多通道回退）。
// sessionId 为当前会话，text 为用户输入（含图片引用等 markdown）。
func (a *App) submitPrompt(sessionId, text string) SubmitResult {
	cfg := defaultSubmitConfig()
	text = strings.TrimSpace(text)
	if text == "" {
		return SubmitResult{OK: false, Error: "empty prompt"}
	}

	// 0. 先补发历史兜底队列（DSH 可能已恢复；失败不阻塞本次提交）
	a.flushSubmitQueue()

	// 1. HTTP 主通道
	var lastErr string
	if cfg.UseHTTP && a.dsh != nil {
		res := a.submitViaHTTP(sessionId, text, cfg)
		if res.OK {
			return res
		}
		lastErr = res.Error
		resumeLog("submitPrompt http failed: %s", res.Error)
	}

	// 2. HTTP 备用通道（同一 DSH 的其他网络路径）
	if cfg.UseHTTPAlt {
		if res := a.submitViaAlt(sessionId, text, cfg); res.OK {
			return res
		} else if res.Error != "" {
			lastErr = res.Error
		}
	}

	// 3. 持久化兜底队列（DSH 恢复后自动补发，输入不丢）
	if cfg.UseQueue {
		if err := a.enqueuePrompt(sessionId, text, lastErr); err == nil {
			resumeLog("submitPrompt queued: session=%s prompt=%q", sessionId, truncate(text, 80))
			return SubmitResult{
				OK:      false,
				Channel: "queue",
				Error:   "DSH 暂不可用，消息已暂存，恢复后自动发送",
			}
		} else {
			resumeLog("submitPrompt queue write failed: %v", err)
		}
	}

	// 4. 日志兜底（不崩溃）
	if cfg.LogFallback {
		resumeLog("submitPrompt fallback: session=%s prompt=%q", sessionId, truncate(text, 80))
		return SubmitResult{OK: false, Channel: "log", Error: "no channel available; logged only"}
	}
	return SubmitResult{OK: false, Error: "no submit channel"}
}

// submitViaHTTP 通过 HTTP RPC 提交（主通道，含一次瞬时故障重试）。
// payload 结构（已在 DSH 源码 rpc-schemas.spec.ts 验证）：
//
//	{ sessionId, mode:"queue"|"steer", content:[{type:"text",text}], clientTimeZone? }
//
// 字段名是 content（不是 prompt），必须带 mode —— 这是历史 bad-request 的根源。
// 返回的 value 是 { accepted:true, command? }。
func (a *App) submitViaHTTP(sessionId, text string, cfg submitConfig) SubmitResult {
	res := a.submitToClient(a.dsh, sessionId, text, "http")
	if res.OK {
		return res
	}
	// 瞬时故障（DSH 短暂 GC/连接重置）重试一次
	time.Sleep(200 * time.Millisecond)
	retry := a.submitToClient(a.dsh, sessionId, text, "http")
	if retry.OK {
		resumeLog("submitPrompt http retry succeeded after: %s", res.Error)
		return retry
	}
	return retry
}

// submitViaAlt 依次尝试备用通道（localhost / [::1]），任一成功即返回。
func (a *App) submitViaAlt(sessionId, text string, cfg submitConfig) SubmitResult {
	var last string
	for _, alt := range a.dshAlts {
		res := a.submitToClient(alt, sessionId, text, "http-alt")
		if res.OK {
			resumeLog("submitPrompt alt channel (%s:%d) succeeded", alt.host, alt.port)
			return res
		}
		last = res.Error
	}
	if last == "" {
		last = "no alt channel configured"
	}
	resumeLog("submitPrompt alt channels failed: %s", last)
	return SubmitResult{OK: false, Channel: "http-alt", Error: last}
}

// submitToClient 用指定客户端提交（HTTP 信封 + 结果解析）。
func (a *App) submitToClient(client *DshClient, sessionId, text, channel string) SubmitResult {
	if client == nil {
		return SubmitResult{OK: false, Channel: channel, Error: "no dsh client"}
	}
	payload := map[string]any{
		"sessionId": sessionId,
		"mode":      "queue",
		"content": []any{map[string]any{
			"type": "text",
			"text": text,
		}},
		"clientTimeZone": "Asia/Shanghai",
	}
	raw, err := client.RPC(dshPromptEndpoint, payload)
	if err != nil {
		return SubmitResult{OK: false, Channel: channel, Error: err.Error()}
	}
	return parseSubmitResult(raw, channel)
}

// parseSubmitResult 解析 DSH 提交响应（HTTP/其他通道共用）。
// session.prompt 成功返回 value={accepted:true}；其他方法可能返回 {ok:...}。
// 两者都视为成功（accepted 优先，兼容未来 DSH 变体）。
func parseSubmitResult(raw json.RawMessage, channel string) SubmitResult {
	var v map[string]any
	if json.Unmarshal(raw, &v) == nil {
		if accepted, _ := v["accepted"].(bool); accepted {
			return SubmitResult{OK: true, Channel: channel}
		}
		if ok, _ := v["ok"].(bool); ok {
			return SubmitResult{OK: true, Channel: channel}
		}
	}
	// 非空 value 但无布尔标记 → 视为成功（值本身即结果）
	if len(raw) > 0 && string(raw) != "null" {
		return SubmitResult{OK: true, Channel: channel}
	}
	return SubmitResult{OK: false, Channel: channel, Error: "unexpected response"}
}

// submitViaWS 通道能力探测（当前 DSH 不支持上行 WS）。
// 实测证据：向 /api/events.mux 写 client-request 帧，服务端回
// close 1008 "policy violation: downlink only"（app_events.go 的两条 WS 均为
// registerDownlink 注册）。因此这里不做实际发送，只返回明确诊断；
// 未来 DSH 若开放上行 WS，在此实现发送 + rpcId 匹配 client-response 即可。
func (a *App) submitViaWS(_sessionId, _text string) SubmitResult {
	return SubmitResult{
		OK:      false,
		Channel: "ws",
		Error:   "DSH events.mux is downlink-only (server closes with 1008 policy violation); uplink is HTTP POST /api/<method>",
	}
}

// ===== 兜底队列（持久化，DSH 恢复后自动补发）=====

// queuedPrompt 队列条目。
type queuedPrompt struct {
	ID        string `json:"id"`
	SessionID string `json:"sessionId"`
	Text      string `json:"text"`
	CreatedAt int64  `json:"createdAt"`
	Attempts  int    `json:"attempts"`
	LastError string `json:"lastError,omitempty"`
}

var submitQueueMu sync.Mutex

// queuePath 队列文件路径（默认 ~/.reasonix/submit-queue.json）。
func (a *App) queuePath() string {
	if a.submitQueuePath != "" {
		return a.submitQueuePath
	}
	return filepath.Join(reasonixDataDir(), "submit-queue.json")
}

// loadSubmitQueue 读队列（不存在/损坏 → 空）。
func (a *App) loadSubmitQueue() []queuedPrompt {
	submitQueueMu.Lock()
	defer submitQueueMu.Unlock()
	return a.loadSubmitQueueLocked()
}

func (a *App) loadSubmitQueueLocked() []queuedPrompt {
	data, err := os.ReadFile(a.queuePath())
	if err != nil {
		return nil
	}
	var q []queuedPrompt
	if json.Unmarshal(data, &q) != nil {
		return nil
	}
	return q
}

// saveSubmitQueue 写队列（原子替换）。
func (a *App) saveSubmitQueueLocked(q []queuedPrompt) error {
	path := a.queuePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(q, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// enqueuePrompt 把提交失败的消息存入兜底队列（超过上限丢最旧）。
func (a *App) enqueuePrompt(sessionId, text, lastErr string) error {
	submitQueueMu.Lock()
	defer submitQueueMu.Unlock()
	q := a.loadSubmitQueueLocked()
	q = append(q, queuedPrompt{
		ID:        fmt.Sprintf("q-%d", time.Now().UnixNano()),
		SessionID: sessionId,
		Text:      text,
		CreatedAt: time.Now().UnixMilli(),
		LastError: lastErr,
	})
	if len(q) > submitQueueMax {
		q = q[len(q)-submitQueueMax:]
	}
	return a.saveSubmitQueueLocked(q)
}

// flushSubmitQueue 尝试补发队列（DSH 恢复后调用；成功条目移除，失败保留并累计次数）。
// 返回成功补发条数。无队列时零开销直接返回。
func (a *App) flushSubmitQueue() int {
	submitQueueMu.Lock()
	q := a.loadSubmitQueueLocked()
	submitQueueMu.Unlock()
	if len(q) == 0 {
		return 0
	}
	var remain []queuedPrompt
	sent := 0
	for _, item := range q {
		res := a.submitToClient(a.dsh, item.SessionID, item.Text, "http")
		if !res.OK {
			for _, alt := range a.dshAlts {
				if r := a.submitToClient(alt, item.SessionID, item.Text, "http-alt"); r.OK {
					res = r
					break
				}
			}
		}
		if res.OK {
			sent++
			continue
		}
		item.Attempts++
		item.LastError = res.Error
		remain = append(remain, item)
	}
	submitQueueMu.Lock()
	// 保留补发期间新入队的条目
	current := a.loadSubmitQueueLocked()
	if len(current) > len(q) {
		remain = append(remain, current[len(q):]...)
	}
	_ = a.saveSubmitQueueLocked(remain)
	submitQueueMu.Unlock()
	if sent > 0 {
		resumeLog("submitQueue flushed: sent=%d remain=%d", sent, len(remain))
	}
	return sent
}

// SubmitQueueStatus 队列状态（诊断用：前端/日志可查还有多少条待补发）。
func (a *App) SubmitQueueStatus() map[string]any {
	q := a.loadSubmitQueue()
	items := make([]any, 0, len(q))
	for _, it := range q {
		items = append(items, map[string]any{
			"id":        it.ID,
			"sessionId": it.SessionID,
			"attempts":  it.Attempts,
			"createdAt": it.CreatedAt,
			"lastError": it.LastError,
			"preview":   truncate(it.Text, 60),
		})
	}
	return map[string]any{"count": len(q), "items": items, "path": a.queuePath()}
}

// ===== 前端桥方法 =====

// SubmitToTabWithID 普通对话提交（前端 Composer 主通道）。
// 参数：_a1=tabId, _a2=display(显示文本), _a3=input(结构化输入)。
// 返回：{ok, channel, error}，前端据此提示。
func (a *App) SubmitToTabWithID(tabID string, display string, input map[string]any) map[string]any {
	text := extractPromptText(display, input)
	sid := a.activeSessionID(tabID)
	if sid == "" {
		sid = tabID
	}
	res := a.submitPrompt(sid, text)
	return map[string]any{"ok": res.OK, "channel": res.Channel, "error": res.Error}
}

// SubmitInvocationsToTabWithID 工具调用提交（把结构化调用转成文本提交）。
func (a *App) SubmitInvocationsToTabWithID(tabID string, display string, invocations []any, input map[string]any) map[string]any {
	text := extractPromptText(display, input)
	sid := a.activeSessionID(tabID)
	if sid == "" {
		sid = tabID
	}
	res := a.submitPrompt(sid, text)
	return map[string]any{"ok": res.OK, "channel": res.Channel, "error": res.Error}
}

// SubmitDisplayToTabWithID 显示提交（预览确认后提交）。
func (a *App) SubmitDisplayToTabWithID(tabID string, display string, input map[string]any) map[string]any {
	return a.SubmitToTabWithID(tabID, display, input)
}

// ===== 辅助 =====

// extractPromptText 从 display/input 提取用户文本。
func extractPromptText(display string, input map[string]any) string {
	if strings.TrimSpace(display) != "" {
		return display
	}
	if input != nil {
		if t, ok := input["text"].(string); ok && t != "" {
			return t
		}
		if t, ok := input["prompt"].(string); ok && t != "" {
			return t
		}
		// 序列化其余字段（兜底）
		if b, err := json.Marshal(input); err == nil {
			return string(b)
		}
	}
	return display
}

// truncate 截断日志用。
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

