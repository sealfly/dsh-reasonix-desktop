package main

// App 的终端桥方法（对应前端 bridge.ts 的 terminal 相关调用）。

// TerminalWorkspaceForTab 返回终端的会话清单（前端终端面板打开时调用）。
func (a *App) TerminalWorkspaceForTab(tabID string) map[string]any {
	return a.term.Workspace(tabID)
}

// CreateTerminalForTab 创建一个终端会话（spawn shell）。
func (a *App) CreateTerminalForTab(tabID, relativePath, shellID string) (map[string]any, error) {
	return a.term.Create(a, tabID, relativePath, shellID)
}

// WriteTerminalForTab 向终端写输入。
func (a *App) WriteTerminalForTab(_tabID, sessionID, data string) error {
	return a.term.Write(sessionID, data)
}

// TerminateTerminalForTab 终止终端（kill 进程）。
func (a *App) TerminateTerminalForTab(_tabID, sessionID string) error {
	return a.term.Close(sessionID)
}

// CloseTerminalForTab 关闭终端（同 Terminate）。
func (a *App) CloseTerminalForTab(_tabID, sessionID string) error {
	return a.term.Close(sessionID)
}

// ResizeTerminalForTab 调整终端尺寸（无 PTY，忽略；前端无参数调用）。
func (a *App) ResizeTerminalForTab() {}

// ListTerminalSessionsForTab 列出终端会话（同 Workspace）。
func (a *App) ListTerminalSessionsForTab(tabID string) map[string]any {
	return a.term.Workspace(tabID)
}

// RenameTerminalForTab 重命名终端会话。
func (a *App) RenameTerminalForTab(_tabID, sessionID, title string) error {
	a.term.mu.Lock()
	defer a.term.mu.Unlock()
	if s := a.term.sessions[sessionID]; s != nil {
		s.Title = title
	}
	return nil
}

// SetTerminalThemeForTab 设置终端主题（无 PTY，前端自己配色，忽略）。
func (a *App) SetTerminalThemeForTab(_tabID, _theme string) {}
