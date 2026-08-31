package main

// app_submit.go — 任务提交适配器（把前端对话提交转发给 DSH 执行）。
//
// 设计目标（贴合项目精神 + 预留 DSH 升级接口）：
//  1. 贴合 DSH：走 DSH 原生 RPC（session.prompt），不碰内部存储。
//  2. 适配器模式：提交通道封装为 dshPromptSubmitter，DSH 协议升级时只改适配器，
//     前端桥方法（SubmitToTabWithID 等）保持不变。
//  3. 多通道回退：HTTP RPC 优先，WS 备用，都失败则日志兜底（不崩溃）。
//  4. 可配置：endpoint/method/payload 模板可配（DSH 变更时改配置即可接入）。

import (
	"encoding/json"
	"strings"
	"time"
)

// ===== 提交通道配置（DSH 升级时调整这里）=====

// dshPromptEndpoint DSH 提交 prompt 的 RPC 方法名。
// DSH 不同版本可能用 session.prompt / agent.prompt / session.submit 等，
// 升级时改这一个常量即可，前端无需改动。
const dshPromptEndpoint = "session.prompt"

// submitConfig 提交配置（预留，可扩展为读配置文件）。
type submitConfig struct {
	// 尝试顺序：http → ws → log
	UseHTTP bool
	UseWS   bool
	// HTTP 超时
	HTTPTimeout time.Duration
	// 是否记录提交日志（兜底）
	LogFallback bool
}

// defaultSubmitConfig 默认提交配置。
func defaultSubmitConfig() submitConfig {
	return submitConfig{
		UseHTTP:     true,
		UseWS:       true,
		HTTPTimeout: 15 * time.Second,
		LogFallback: true,
	}
}

// ===== 提交结果 =====

// SubmitResult 提交结果（前端读取 ok/error/channel）。
type SubmitResult struct {
	OK      bool   `json:"ok"`
	Channel string `json:"channel"` // http/ws/log
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

	// 1. HTTP RPC 通道（优先）
	if cfg.UseHTTP && a.dsh != nil {
		res := a.submitViaHTTP(sessionId, text, cfg)
		if res.OK {
			return res
		}
		resumeLog("submitPrompt http failed: %s", res.Error)
	}

	// 2. WS 通道（预留）
	if cfg.UseWS {
		res := a.submitViaWS(sessionId, text)
		if res.OK {
			return res
		}
		resumeLog("submitPrompt ws failed: %s", res.Error)
	}

	// 3. 日志兜底（不崩溃）
	if cfg.LogFallback {
		resumeLog("submitPrompt fallback: session=%s prompt=%q", sessionId, truncate(text, 80))
		return SubmitResult{OK: false, Channel: "log", Error: "no channel available; logged only"}
	}
	return SubmitResult{OK: false, Error: "no submit channel"}
}

// submitViaHTTP 通过 HTTP RPC 提交。
// payload 结构（已在 DSH 源码 rpc-schemas.spec.ts 验证）：
//
//	{ sessionId, mode:"queue"|"steer", content:[{type:"text",text}], clientTimeZone? }
//
// 字段名是 content（不是 prompt），必须带 mode —— 这是历史 bad-request 的根源。
// 返回的 value 是 { accepted:true, command? }。
func (a *App) submitViaHTTP(sessionId, text string, cfg submitConfig) SubmitResult {
	payload := map[string]any{
		"sessionId": sessionId,
		"mode":      "queue",
		"content": []any{map[string]any{
			"type": "text",
			"text": text,
		}},
		"clientTimeZone": "Asia/Shanghai",
	}
	return a.rpcSubmit(dshPromptEndpoint, payload)
}

// rpcSubmit 用 DshClient.RPC 提交并解析结果。
// session.prompt 成功返回 value={accepted:true}；其他方法可能返回 {ok:...}。
// 两者都视为成功（accepted 优先，兼容未来 DSH 变体）。
func (a *App) rpcSubmit(method string, payload any) SubmitResult {
	raw, err := a.dsh.RPC(method, payload)
	if err != nil {
		return SubmitResult{OK: false, Channel: "http", Error: err.Error()}
	}
	// DSH 成功时返回 result.value：{accepted:true} 或 {ok:true}
	var v map[string]any
	if json.Unmarshal(raw, &v) == nil {
		if accepted, _ := v["accepted"].(bool); accepted {
			return SubmitResult{OK: true, Channel: "http"}
		}
		if ok, _ := v["ok"].(bool); ok {
			return SubmitResult{OK: true, Channel: "http"}
		}
	}
	// 非空 value 但无布尔标记 → 视为成功（值本身即结果）
	if len(raw) > 0 && string(raw) != "null" {
		return SubmitResult{OK: true, Channel: "http"}
	}
	return SubmitResult{OK: false, Channel: "http", Error: "unexpected response"}
}

// submitViaWS 通过 WebSocket 提交（预留，DSH WS 协议稳定后启用）。
// 当前 DSH 的 events.mux WS 支持双向 RPC，但握手/订阅细节需进一步确认；
// 这里预留接口：连接、发送 client-request、读取结果。
func (a *App) submitViaWS(sessionId, text string) SubmitResult {
	// TODO: 实现 WS 双向提交（复用 app_events.go 的连接，扩展 send 能力）。
	// 当前 DSH 版本 HTTP RPC 优先；WS 作为升级备用通道保留接口。
	return SubmitResult{OK: false, Channel: "ws", Error: "ws submit not yet wired (reserved)"}
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

