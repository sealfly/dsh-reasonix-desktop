package main

// App 的会话桥方法（DSH session.* 透传 + 转换成前端 TabMeta）。
// 项目原则：不限制 DSH 能力——DSH 的方法经 dsh.RPC 通用透传调用。

import "fmt"

// dshSession 是 DSH session.list 返回的会话对象（部分字段）。
type dshSession struct {
	SessionID   string         `json:"sessionId"`
	Cwd         string         `json:"cwd"`
	Running     bool           `json:"running"`
	AgentPreset string         `json:"agentPreset"`
	UpdatedAt   any            `json:"updatedAt"`
	Projections map[string]any `json:"projections"`
}

// tabMeta 是前端期望的 TabMeta 结构（部分字段，前端用到的）。
func (a *App) tabMeta(s dshSession, idx int) map[string]any {
	title := "未命名会话"
	cwd := s.Cwd
	if cwd == "" {
		cwd = homeDir()
	}
	if s.Projections != nil {
		if v, ok := s.Projections["values"].(map[string]any); ok {
			if t, ok := v["title"].(string); ok && t != "" {
				title = t
			}
		}
	}
	return map[string]any{
		"id":            s.SessionID,
		"tabId":         s.SessionID,
		"title":         title,
		"cwd":           cwd,
		"workspaceRoot": cwd,
		"running":       s.Running,
		"readOnly":      false,
		"active":        idx == 0,
		"order":         idx,
	}
}

// Tabs 返回当前所有会话的 TabMeta 列表（前端启动/刷新时读）。
func (a *App) Tabs() []any {
	if a.dsh == nil {
		return []any{}
	}
	raw, err := a.dsh.RPC("session.list", map[string]any{})
	if err != nil {
		return []any{}
	}
	var list struct {
		Items []dshSession `json:"items"`
	}
	if err := DecodeRPC(raw, &list); err != nil {
		return []any{}
	}
	tabs := make([]any, 0, len(list.Items))
	for i, s := range list.Items {
		tabs = append(tabs, a.tabMeta(s, i))
	}
	return tabs
}

// ListTabs 同 Tabs（兼容前端 bridge.ts 的方法名）。
func (a *App) ListTabs() []any { return a.Tabs() }

// ListSessions 同 Tabs（部分前端路径用 ListSessions）。
func (a *App) ListSessions() []any { return a.Tabs() }

// CreateSession 创建会话（DSH session.create 透传）。
func (a *App) CreateSession(workspaceRoot, preset string) (map[string]any, error) {
	if a.dsh == nil {
		return nil, fmt.Errorf("dsh not ready")
	}
	payload := map[string]any{}
	if workspaceRoot != "" {
		payload["cwd"] = workspaceRoot
	}
	if preset != "" {
		payload["agentPreset"] = preset
	}
	raw, err := a.dsh.RPC("session.create", payload)
	if err != nil {
		return nil, err
	}
	var res struct {
		SessionID string `json:"sessionId"`
	}
	if err := DecodeRPC(raw, &res); err != nil {
		return nil, err
	}
	return a.tabMeta(dshSession{SessionID: res.SessionID, Cwd: workspaceRoot}, 0), nil
}

// Prompt 向会话发送提示（DSH session.prompt 透传）。
// tabID 是 sessionId，input 是用户输入文本。
func (a *App) Prompt(tabID, input string) error {
	if a.dsh == nil {
		return fmt.Errorf("dsh not ready")
	}
	_, err := a.dsh.RPC("session.prompt", map[string]any{
		"sessionId": tabID,
		"input":     input,
	})
	return err
}

// RenameSession 重命名会话（DSH session.rename 透传）。
func (a *App) RenameSession(sessionID, title string) error {
	if a.dsh == nil {
		return fmt.Errorf("dsh not ready")
	}
	_, err := a.dsh.RPC("session.rename", map[string]any{
		"sessionId": sessionID,
		"title":     title,
	})
	return err
}

// DeleteSession 删除会话（DSH 有 workspace.archiveSession 或 session 删除；透传 archive）。
func (a *App) DeleteSession(sessionID string) error {
	if a.dsh == nil {
		return fmt.Errorf("dsh not ready")
	}
	_, err := a.dsh.RPC("workspace.archiveSession", map[string]any{"sessionId": sessionID})
	return err
}

// Cancel 取消运行中的会话（DSH session.cancel 透传）。
func (a *App) Cancel(tabID string) error {
	if a.dsh == nil {
		return fmt.Errorf("dsh not ready")
	}
	_, err := a.dsh.RPC("session.cancel", map[string]any{"sessionId": tabID})
	return err
}

// CancelForTab 同 Cancel。
func (a *App) CancelForTab(tabID string) error { return a.Cancel(tabID) }
