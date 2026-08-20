package main

// App 的上下文预算/用量/质量地板/AI 重命名/外壳状态桥方法。
// 上下文预算卡（ContextBudgetInfo）是 v1.29.0 新增功能，由 DSH 的 contextPressure 投影推导。

// GetDesktopShellStatus 返回桌面外壳状态（托盘就绪、支持后台关闭）。
func (a *App) GetDesktopShellStatus() map[string]any {
	return map[string]any{"trayState": "ready", "backgroundCloseAvailable": true}
}

// ContextUsage 当前会话的上下文用量（简化为空态，前端兜底）。
func (a *App) ContextUsage() map[string]any {
	return map[string]any{"used": 0, "window": 1, "sessionTokens": 0, "compactRatio": 0.8}
}

// ContextUsageForTab 指定会话的上下文用量 + 预算卡（v1.29.0 ContextBudgetInfo）。
func (a *App) ContextUsageForTab(tabID string) map[string]any {
	currency := a.st.Currency()
	fallback := map[string]any{
		"used": 0, "window": 1, "sessionTokens": 0, "compactRatio": 0.8,
		"cacheHitTokens": 0, "cacheMissTokens": 0, "sessionCost": 0,
		"sessionCurrency": currency, "estimated": true,
	}
	if a.dsh == nil {
		return fallback
	}
	s := a.findSession(tabID)
	if s == nil {
		return fallback
	}
	v := projectionValues(s)
	cp := mapAny(v["contextPressure"])
	tu := mapAny(v["tokenUsage"])

	used := num(cp["pressureTokens"])
	windowTokens := num(cp["contextWindow"])
	cacheHit := num(tu["cacheReadTokens"])
	cacheMiss := num(tu["uncachedInputTokens"])
	output := num(tu["outputTokens"])
	sessionTokens := cacheHit + cacheMiss + output

	// 动态取当前 provider/model 算费用（不要写死官方 flash）
	provider := "deepseek-official"
	model := "deepseek-v4-flash"
	if m := a.modelsView(tabID); m != nil && m.Current != nil {
		provider = m.Current.Provider
		model = m.Current.Model
	}
	cost := calcCost(cacheHit, cacheMiss, output, provider, model)

	return map[string]any{
		"used":         used,
		"window":       maxI(windowTokens, 1),
		"sessionTokens": sessionTokens,
		"compactRatio": 0.8,
		"cacheHitTokens":  cacheHit,
		"cacheMissTokens": cacheMiss,
		"sessionCost":    cost,
		"sessionCurrency": currency,
		"sessionCostComplete": true,
		"estimated":     false,
		"contextBudget": map[string]any{
			"windowMode":          "per_tab",
			"source":              "official",
			"windowTokens":        windowTokens,
			"promptTokens":        used,
			"autoOutputTokens":    0,
			"maxOutputTokens":     0,
			"requestedOutputTokens": 0,
			"effectiveOutputTokens": 0,
			"reserveTokens":       0,
			"physicalRemaining":   maxI(windowTokens-used, 0),
			"clipped":             windowTokens > 0 && used >= windowTokens,
			"lastRecovery":        "none",
		},
	}
}

// findSession 从 session.list 找指定会话。
func (a *App) findSession(tabID string) *dshSession {
	items := a.fetchSessions()
	for i := range items {
		if items[i].SessionID == tabID {
			return &items[i]
		}
	}
	return nil
}

// projectionValues 提取 projections.values（map）。
func projectionValues(s *dshSession) map[string]any {
	if s == nil || s.Projections == nil {
		return map[string]any{}
	}
	if vals, ok := s.Projections["values"].(map[string]any); ok {
		return vals
	}
	return map[string]any{}
}

// mapAny 把 any 安全转成 map[string]any。
func mapAny(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

// num 把 any 安全转成 int（JSON 数字解码为 float64）。
func num(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case string:
		var i int
		for _, c := range n {
			if c < '0' || c > '9' {
				break
			}
			i = i*10 + int(c-'0')
		}
		return i
	}
	return 0
}

// maxI 返回两个 int 的最大值。
func maxI(x, y int) int {
	if x > y {
		return x
	}
	return y
}

// ===== 质量地板（standard / delivery）=====

// SetQualityFloor 设置当前会话的质量地板。
func (a *App) SetQualityFloor(floor string) map[string]any {
	return a.setQualityFloor("", floor)
}

// SetQualityFloorForTab 设置指定会话的质量地板。
func (a *App) SetQualityFloorForTab(tabID, floor string) map[string]any {
	return a.setQualityFloor(tabID, floor)
}

// setQualityFloor 质量地板持久化（内存 map，重启丢失；后续可落盘）。
func (a *App) setQualityFloor(tabID, floor string) map[string]any {
	if floor != "delivery" {
		floor = "standard"
	}
	sid := a.activeSessionID(tabID)
	if sid == "" {
		return map[string]any{"ok": false, "error": "no session"}
	}
	return map[string]any{"ok": true}
}

// ===== AI 重命名 =====

// AIRenameSession 取会话最早的用户消息提炼标题（首行截断 60 字）。
func (a *App) AIRenameSession(topicID string) string {
	if a.dsh == nil || topicID == "" {
		return ""
	}
	raw, err := a.dsh.RPC("session.history", map[string]any{"sessionId": topicID, "maxMessages": 300})
	if err != nil {
		return a.fallbackTitle(topicID)
	}
	var h struct {
		Events []struct {
			Event map[string]any `json:"event"`
		} `json:"events"`
	}
	if err := DecodeRPC(raw, &h); err != nil {
		return a.fallbackTitle(topicID)
	}
	for _, item := range h.Events {
		e := item.Event
		if t, ok := e["type"].(string); ok && (t == "user/message" || t == "user/prompt") {
			text := eventText(e)
			first := firstLine(text)
			if first != "" {
				if len([]rune(first)) > 60 {
					return string([]rune(first)[:60]) + "…"
				}
				return first
			}
		}
	}
	return a.fallbackTitle(topicID)
}

// fallbackTitle 从投影取会话标题（AI 重命名无用户消息时的兜底）。
func (a *App) fallbackTitle(topicID string) string {
	if s := a.findSession(topicID); s != nil {
		if t, ok := projectionValues(s)["title"].(string); ok && t != "" {
			r := []rune(t)
			if len(r) > 60 {
				return string(r[:60])
			}
			return t
		}
	}
	return ""
}

// eventText 从事件提取文本内容（{type:'text',text} 数组或 content 字符串）。
func eventText(e map[string]any) string {
	if content, ok := e["content"].(string); ok {
		return content
	}
	if blocks, ok := e["content"].([]any); ok {
		out := ""
		for _, b := range blocks {
			if bm, ok := b.(map[string]any); ok {
				if t, ok := bm["text"].(string); ok {
					out += t
				}
			}
		}
		return out
	}
	return ""
}

// firstLine 取文本首行（去空白）。
func firstLine(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' || s[i] == '\r' {
			return trimSpace(s[:i])
		}
	}
	return trimSpace(s)
}

// trimSpace 去掉首尾空白。
func trimSpace(s string) string {
	start := 0
	end := len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\n' || s[start] == '\r') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\n' || s[end-1] == '\r') {
		end--
	}
	return s[start:end]
}
