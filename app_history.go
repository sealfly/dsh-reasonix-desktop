package main

// App 的历史目录桥方法（历史面板的会话列表/内容搜索/上下文）。
// 依赖 DSH session.list（会话元数据）+ session.history（消息内容，用于搜索）。

import (
	"strconv"
	"strings"
	"time"
)

// historyMeta 把 session 转成前端历史面板的 SessionMeta 条目。
func (a *App) historyMeta() []map[string]any {
	items := a.fetchSessions()
	now := time.Now().UnixMilli()
	out := make([]map[string]any, 0, len(items))
	for _, s := range items {
		v := projectionValues(&s)
		root := s.Cwd
		last := ""
		if t, ok := v["lastMessage"].(string); ok && t != "" {
			last = t
		} else if t, ok := v["lastUserMessage"].(string); ok && t != "" {
			last = t
		} else if t, ok := v["title"].(string); ok {
			last = t
		}
		at := timeVal(s.UpdatedAt, now)
		created := timeVal(s.CreatedAt, at)
		title, _ := v["title"].(string)
		turns := turnsOf(v)

		scope := "global"
		if root != "" {
			scope = "project"
		}
		m := map[string]any{
			"path":           s.SessionID + ".jsonl",
			"preview":        truncateRunes(last, 200),
			"title":          title,
			"turns":          turns,
			"turnsState":     "valid",
			"createdAt":      created,
			"lastActivityAt": at,
			"modTime":        at,
			"current":        false,
			"open":           true,
			"scope":          scope,
			"workspaceRoot":  root,
			"topicId":        s.SessionID,
			"topicTitle":     title,
			"running":        s.Running,
		}
		out = append(out, m)
	}
	return out
}

// timeVal 把 any 转成毫秒时间戳（字符串/数字/ISO 时间）。
func timeVal(v any, fallback int64) int64 {
	switch t := v.(type) {
	case float64:
		return int64(t)
	case int64:
		return t
	case string:
		if t == "" {
			return fallback
		}
		if ts, err := time.Parse(time.RFC3339, t); err == nil {
			return ts.UnixMilli()
		}
	}
	return fallback
}

// truncateRunes 按 rune 截断字符串（中文安全）。
func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// ListHistorySessions 历史面板会话列表（分页 + scope/状态/时间/关键字过滤）。
func (a *App) ListHistorySessions(req map[string]any) map[string]any {
	all := a.historyMeta()
	query := strings.ToLower(strings.TrimSpace(strOf(req["query"])))
	scope, _ := req["scope"].(string)
	status, _ := req["status"].(string)
	timeFilter, _ := req["timeFilter"].(string)

	now := time.Now()
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).UnixMilli()

	filtered := make([]map[string]any, 0, len(all))
	for _, m := range all {
		if scope != "" && scope != "all" && m["scope"] != scope {
			continue
		}
		if status == "current" && m["current"] != true {
			continue
		}
		if status == "open" && m["open"] != true {
			continue
		}
		lastAt := toInt64(m["lastActivityAt"])
		if timeFilter == "today" && lastAt < dayStart {
			continue
		}
		if timeFilter == "yesterday" && !(lastAt >= dayStart-86400000 && lastAt < dayStart) {
			continue
		}
		if timeFilter == "older" && lastAt >= dayStart-86400000 {
			continue
		}
		if query != "" {
			match := false
			for _, k := range []string{"title", "preview", "topicTitle", "workspaceRoot"} {
				if strings.Contains(strings.ToLower(strOf(m[k])), query) {
					match = true
					break
				}
			}
			if !match {
				continue
			}
		}
		filtered = append(filtered, m)
	}
	start := toInt(req["cursor"])
	if start < 0 {
		start = 0
	}
	limit := toInt(req["limit"])
	if limit < 1 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	end := start + limit
	if end > len(filtered) {
		end = len(filtered)
	}
	items := filtered[start:end]
	nextCursor := ""
	if end < len(filtered) {
		nextCursor = itoa(end)
	}
	return map[string]any{"items": items, "nextCursor": nextCursor, "revision": 1, "partial": false, "staleCursor": false}
}

// GetHistoryIndexStatus 历史索引状态（DSH 无独立索引，用 session 数量）。
func (a *App) GetHistoryIndexStatus() map[string]any {
	n := len(a.fetchSessions())
	return map[string]any{"state": "ready", "mode": "memory", "revision": 1, "indexed": n, "total": n, "pending": 0, "failed": 0}
}

// RebuildHistoryIndex 重建索引（DSH 无索引，空操作）。
func (a *App) RebuildHistoryIndex() {}

// SearchHistoryContent 内容搜索（有界扫描：前 10 个会话，各取最近 60 条消息）。
func (a *App) SearchHistoryContent(req map[string]any) map[string]any {
	query := strings.ToLower(strings.TrimSpace(strOf(req["query"])))
	status := a.GetHistoryIndexStatus()
	if query == "" {
		return map[string]any{"items": []any{}, "nextCursor": "", "revision": 1, "partial": false, "staleCursor": false, "status": status}
	}
	limit := toInt(req["limit"])
	if limit < 1 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	items := a.fetchSessions()
	if len(items) > 10 {
		items = items[:10]
	}
	hits := []any{}
	for _, s := range items {
		v := projectionValues(&s)
		events := []struct {
			Event map[string]any `json:"event"`
		}{}
		if raw, err := a.dsh.RPC("session.history", map[string]any{"sessionId": s.SessionID, "maxMessages": 60}); err == nil {
			var h struct {
				Events []struct {
					Event map[string]any `json:"event"`
				} `json:"events"`
			}
			if DecodeRPC(raw, &h) == nil {
				events = h.Events
			}
		}
		for idx, item := range events {
			e := item.Event
			text := eventText(e)
			if !strings.Contains(strings.ToLower(text), query) {
				continue
			}
			role := "tool"
			if t, _ := e["type"].(string); t != "" {
				if strings.HasPrefix(t, "user") {
					role = "user"
				} else if strings.HasPrefix(t, "assistant") {
					role = "assistant"
				}
			}
			title, _ := v["title"].(string)
			hits = append(hits, map[string]any{
				"sessionPath": s.SessionID + ".jsonl", "sessionId": s.SessionID,
				"source": "session", "messageIndex": idx, "role": role, "kind": "message",
				"snippet": truncateRunes(text, 300), "score": 1,
				"sessionTitle": title, "workspaceRoot": s.Cwd,
				"lastActivityAt": timeVal(s.UpdatedAt, time.Now().UnixMilli()),
				"open": true, "running": s.Running, "current": false,
			})
			if len(hits) >= limit {
				break
			}
		}
		if len(hits) >= limit {
			break
		}
	}
	return map[string]any{"items": hits, "nextCursor": "", "revision": 1, "partial": true, "staleCursor": false, "status": status}
}

// GetHistorySearchContext 搜索命中的上下文行（前后原文片段）。
func (a *App) GetHistorySearchContext(req map[string]any) map[string]any {
	sid := strings.TrimSuffix(strOf(req["sessionPath"]), ".jsonl")
	raw, err := a.dsh.RPC("session.history", map[string]any{"sessionId": sid, "maxMessages": 60})
	if err != nil {
		return map[string]any{"before": []any{}, "after": []any{}}
	}
	var h struct {
		Events []struct {
			Event map[string]any `json:"event"`
		} `json:"events"`
	}
	if DecodeRPC(raw, &h) != nil {
		return map[string]any{"before": []any{}, "after": []any{}}
	}
	msgIdx := toInt(req["messageIndex"])
	before := []any{}
	after := []any{}
	for i, item := range h.Events {
		text := eventText(item.Event)
		if i < msgIdx {
			before = append(before, text)
		} else if i > msgIdx {
			after = append(after, text)
		}
	}
	return map[string]any{"before": before, "after": after}
}

// --- 工具函数 ---

func strOf(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func toInt(v any) int {
	return num(v)
}

func toInt64(v any) int64 {
	switch t := v.(type) {
	case float64:
		return int64(t)
	case int64:
		return t
	case int:
		return int64(t)
	}
	return 0
}

func itoa(v int) string {
	return strconv.Itoa(v)
}
