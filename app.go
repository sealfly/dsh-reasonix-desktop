package main

// App — Wails 绑定的应用结构体。
// 它的导出方法会被 Wails 绑定到前端 window.go.main.App（前端 bridge.ts 直接调用）。
// 方法签名必须与 Reasonix v1.29.0 前端 bridge.ts 的接口契约一致（方法名/参数/返回）。

import (
	"context"
	"runtime"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// App 是桥的核心：持有 DSH 客户端 + 桌面设置 + 终端会话。
type App struct {
	ctx context.Context
	dsh *DshClient
	st  *Settings
	term *TerminalManager
}

// NewApp 创建 App（main.go 里调用）。
func NewApp() *App {
	return &App{}
}

// startup 在 Wails 启动时调用（窗口创建前）。
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.dsh = NewDshClient(3080)
	a.st = NewSettings()
	a.term = NewTerminalManager()
}

// domReady 在前端 DOM 就绪后调用。
func (a *App) domReady(ctx context.Context) {
	a.ctx = ctx
}

// shutdown 在应用关闭时调用（清理终端子进程）。
func (a *App) shutdown(ctx context.Context) {
	if a.term != nil {
		a.term.CloseAll()
	}
}

// Platform 返回当前平台（前端用）。
func (a *App) Platform() string { return runtime.GOOS }

// Version 返回应用版本（前端"关于"用）。
func (a *App) Version() string { return "0.0.0" }

// ===== 窗口控制（前端 WindowsWindowControls 调用）=====

// MinimiseMainWindow 最小化主窗口。
func (a *App) MinimiseMainWindow() {
	if a.ctx == nil {
		return
	}
	wruntime.WindowMinimise(a.ctx)
}

// ToggleMaximiseMainWindow 最大化/还原主窗口。
func (a *App) ToggleMaximiseMainWindow() {
	if a.ctx == nil {
		return
	}
	wruntime.WindowToggleMaximise(a.ctx)
}

// CloseMainWindow 关闭主窗口（= 退出应用；closeBehavior 的 background 模式暂简化为 quit）。
func (a *App) CloseMainWindow() {
	if a.ctx == nil {
		return
	}
	wruntime.Quit(a.ctx)
}

// IsMainWindowMaximised 查询主窗口是否最大化（前端同步按钮状态）。
func (a *App) IsMainWindowMaximised() bool {
	if a.ctx == nil {
		return false
	}
	return wruntime.WindowIsMaximised(a.ctx)
}
