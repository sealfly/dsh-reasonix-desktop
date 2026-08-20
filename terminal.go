package main

// TerminalManager — 本地终端（方案 A：Go 端 os/exec spawn cmd/pwsh）。
// 对应 Electron 版 main.js 的终端管理器：无 PTY，stdout/stderr 经 runtime.EventsEmit
// 推给前端（事件名 terminal:output / terminal:exit）。前端 store 用 window.runtime.EventsOn 订阅。

import (
	"bufio"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// terminalSession 是一个终端会话（对应一个 shell 进程）。
type terminalSession struct {
	ID        string
	TabID     string
	Title     string
	Shell     string
	Cwd       string
	CreatedAt int64
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	done      chan struct{}
}

// TerminalManager 管理所有终端会话。
type TerminalManager struct {
	mu       sync.Mutex
	sessions map[string]*terminalSession
	seq      int
}

// NewTerminalManager 创建终端管理器。
func NewTerminalManager() *TerminalManager {
	return &TerminalManager{sessions: make(map[string]*terminalSession)}
}

// CloseAll 关闭所有终端（shutdown 时清理子进程）。
func (t *TerminalManager) CloseAll() {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, s := range t.sessions {
		if s.cmd != nil && s.cmd.Process != nil {
			_ = s.cmd.Process.Kill()
		}
	}
	t.sessions = make(map[string]*terminalSession)
}

// Workspace 返回终端的会话清单（前端 TerminalWorkspaceForTab 调用）。
func (t *TerminalManager) Workspace(tabID string) map[string]any {
	t.mu.Lock()
	defer t.mu.Unlock()
	sessions := []any{}
	for _, s := range t.sessions {
		if s.TabID == tabID {
			sessions = append(sessions, map[string]any{
				"id": s.ID, "tabId": s.TabID, "title": s.Title, "shell": s.Shell,
				"cwd": s.Cwd, "createdAt": s.CreatedAt, "running": true,
			})
		}
	}
	return map[string]any{
		"available": true,
		"readOnly":  false,
		"sessions":  sessions,
		"shells": []any{
			map[string]any{"id": "default", "label": "cmd.exe（默认）"},
			map[string]any{"id": "powershell", "label": "PowerShell"},
		},
	}
}

// Create 创建一个终端会话（spawn shell）。
func (t *TerminalManager) Create(app *App, tabID, relativePath, shellID string) (map[string]any, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.seq++
	id := fmt.Sprintf("term-%d-%d", t.seq, time.Now().UnixMilli())
	cwd := t.cwdForTab(app, tabID)
	if relativePath != "" && relativePath != "." {
		cwd = filepath.Join(cwd, relativePath)
	}

	shell := "default"
	if shellID == "powershell" {
		shell = "powershell"
	}

	var cmd *exec.Cmd
	if shell == "powershell" {
		cmd = exec.Command("powershell", "-NoLogo")
	} else {
		cmd = exec.Command("cmd.exe", "/Q")
	}
	cmd.Dir = cwd

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	s := &terminalSession{
		ID: id, TabID: tabID, Title: shell, Shell: shell, Cwd: cwd,
		CreatedAt: time.Now().UnixMilli(), cmd: cmd, stdin: stdin, done: make(chan struct{}),
	}
	t.sessions[id] = s

	// 启动后切 UTF-8 代码页，防中文乱码（cmd 默认 GBK）。
	if shell == "default" {
		_, _ = stdin.Write([]byte("chcp 65001>nul\r\n"))
	}

	// stdout/stderr 读取 → EventsEmit("terminal:output", {id, data})。
	go t.pump(app, s, stdout, false)
	go t.pump(app, s, stderr, true)

	// 等待进程退出 → EventsEmit("terminal:exit", {id, exitCode})。
	go func() {
		err := cmd.Wait()
		code := 0
		if err != nil {
			if ee, ok := err.(*exec.ExitError); ok {
				code = ee.ExitCode()
			} else {
				code = -1
			}
		}
		close(s.done)
		wruntime.EventsEmit(app.ctx, "terminal:exit", map[string]any{"id": id, "exitCode": code})
		t.mu.Lock()
		delete(t.sessions, id)
		t.mu.Unlock()
	}()

	return map[string]any{
		"id": id, "tabId": tabID, "title": shell, "shell": shell,
		"cwd": cwd, "createdAt": s.CreatedAt, "running": true,
	}, nil
}

// pump 读取 shell 输出并推给前端（无 PTY：无 ANSI 色彩控制，按行缓冲）。
func (t *TerminalManager) pump(app *App, s *terminalSession, r io.Reader, isErr bool) {
	br := bufio.NewReader(r)
	for {
		line, err := br.ReadString('\n')
		if len(line) > 0 {
			// 前端 xterm 期望 \r\n 换行；保持字节流原样，只补 \r\n。
			data := line
			if !strings.HasSuffix(data, "\n") {
				data += "\r\n"
			} else if !strings.HasSuffix(data, "\r\n") {
				data = strings.TrimSuffix(data, "\n") + "\r\n"
			}
			wruntime.EventsEmit(app.ctx, "terminal:output", map[string]any{"id": s.ID, "data": data})
		}
		if err != nil {
			return
		}
	}
}

// Write 写 stdin（前端 WriteTerminalForTab 调用）。
// cmd 管道模式需要 \r\n 才执行；把 \r 规范成 \r\n（保留已有 \r\n）。
func (t *TerminalManager) Write(sessionID, data string) error {
	t.mu.Lock()
	s := t.sessions[sessionID]
	t.mu.Unlock()
	if s == nil || s.stdin == nil {
		return fmt.Errorf("terminal session not found: %s", sessionID)
	}
	normalized := strings.ReplaceAll(data, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\r\n")
	_, err := s.stdin.Write([]byte(normalized))
	return err
}

// Close 关闭终端（kill 进程 + 推 exit 事件 removed）。
func (t *TerminalManager) Close(sessionID string) error {
	t.mu.Lock()
	s := t.sessions[sessionID]
	delete(t.sessions, sessionID)
	t.mu.Unlock()
	if s == nil {
		return nil
	}
	if s.cmd != nil && s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
	return nil
}

// cwdForTab 取会话的工作目录（从 DSH session.list 查 cwd；失败回退主目录）。
func (t *TerminalManager) cwdForTab(app *App, tabID string) string {
	home := homeDir()
	if app == nil || app.dsh == nil {
		return home
	}
	raw, err := app.dsh.RPC("session.list", map[string]any{})
	if err != nil {
		return home
	}
	var list struct {
		Items []struct {
			SessionID string `json:"sessionId"`
			Cwd       string `json:"cwd"`
		} `json:"items"`
	}
	if err := DecodeRPC(raw, &list); err != nil {
		return home
	}
	for _, it := range list.Items {
		if it.SessionID == tabID && it.Cwd != "" {
			return it.Cwd
		}
	}
	return home
}
