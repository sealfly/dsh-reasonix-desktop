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

// ResizeTerminalForTab 调整终端尺寸。
//
// **说明（诚实边界）**：本项目的终端是无 PTY 实现（terminal.go 顶部有说明：stdout/stderr 走管道、
// 无 ANSI 控制），因此 cols/rows 对远端 shell 没有意义 —— 这里按前端契约接收 4 个实参（tabID、
// sessionID、cols、rows）并如实忽略，而不是假装成功。
// 之前的签名是 4 个 any 且注释写"前端无参数调用"，与真机不符：前端 TerminalTransport.resize
// 明确按 (tabId, sessionId, cols, rows) 调用，参数不符会被 Wails 绑定直接拒绝。
func (a *App) ResizeTerminalForTab(_tabID string, _sessionID string, _cols int, _rows int) {}

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
