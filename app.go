package main

// App — Wails 绑定的应用结构体。
// 它的导出方法会被 Wails 绑定到前端 window.go.main.App（前端 bridge.ts 直接调用）。
// 方法签名必须与 Reasonix v1.29.0 前端 bridge.ts 的接口契约一致（方法名/参数/返回）。

import (
	"context"
	"runtime"

	"golang.org/x/sys/windows"

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
	cfg := loadDshConnConfig()
	a.dsh = NewDshClientAt(cfg.Host, cfg.Port)
	a.st = NewSettings()
	a.term = NewTerminalManager()
	a.startEventStream()
	a.startShowEventListener()
	// 预装记忆插件（首次启动后台自动安装，幂等；不阻塞启动、失败可重试）
	go a.preinstallMemoryPlugins()
	// 记忆插件默认关闭（根 patch disabled，仅首次应用一次）
	go ensureMemoryDefaultOff()
	// 预热会话列表缓存（DSH session.list 投影计算慢，预热后左侧任务栏秒开）
	go a.warmTabsCache()
}

// startShowEventListener 监听命名事件 "Local\DSH-ReasonixUI-Show"：
// background 模式下另一实例运行时触发该事件，本实例收到后显示并聚焦窗口。
func (a *App) startShowEventListener() {
	go func() {
		name, err := windows.UTF16PtrFromString(`Local\DSH-ReasonixUI-Show`)
		if err != nil {
			return
		}
		h, err := windows.CreateEvent(nil, 0, 0, name)
		if err != nil {
			resumeLog("show-event create failed: %v", err)
			return
		}
		for {
			if _, werr := windows.WaitForSingleObject(h, 0xFFFFFFFF); werr != nil {
				return
			}
			if a.ctx != nil {
				wruntime.WindowShow(a.ctx)
				wruntime.WindowUnminimise(a.ctx)
				wruntime.WindowExecJS(a.ctx, "window.focus()")
			}
		}
	}()
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
func (a *App) Version() string { return "0.1.0" }

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
// CloseMainWindow 关闭主窗口：按 closeBehavior 决定 quit（退出）或 background（隐藏保持后台）。
func (a *App) CloseMainWindow() {
	if a.ctx == nil {
		return
	}
	if a.st != nil && a.st.CloseBehavior() == "background" {
		// background：隐藏窗口，进程与 DSH 保持运行；再次运行 exe（单实例）触发显示事件恢复窗口。
		resumeLog("close: background mode -> hide window (rerun exe to restore)")
		wruntime.WindowHide(a.ctx)
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
