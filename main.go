// Command dsh-reasonix-wails is the Wails shell hosting the Reasonix v1.29.0
// frontend, bridged to the DSH backend (127.0.0.1:3080).
//
// 为什么用 Wails 而不是 Electron（Why Wails instead of Electron）：
//   Reasonix 前端是为 Wails 写的（依赖 window.go.main.App + window.runtime +
//   --wails-draggable 原生拖拽）。之前的 Electron 桥把 --wails-draggable 映射成
//   -webkit-app-region: drag，触发了 Chromium 的合成层行为，导致布局热切换时
//   侧栏 logo 叠影（旧帧残留）。Wails 的拖拽是原生层的，不经过渲染合成，根除叠影。
//
// 项目原则（PRINCIPLES.md）：
//   - 前端 dist 零改动：直接 go:embed frontend/dist。
//   - 一切适配走 Go 桥：App 方法把前端调用转成 DSH RPC（通用透传，不设白名单）。
//   - 不限制 DSH 原生能力：DshClient 是通用 RPC 透传，任何 DSH 方法可调。
package main

import (
	"embed"
	"os"

	gwsys "golang.org/x/sys/windows"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

// 单实例互斥体与显示事件（closeBehavior=background 时：关窗隐藏进程保留，
// 再次运行 exe 会触发旧实例显示窗口并退出新实例）。
const (
	showEventName = `Local\DSH-ReasonixUI-Show`
	mutexName     = `Local\DSH-ReasonixUI-Single`
)

// alreadyRunning 检查是否已有实例在运行（创建互斥体，ERROR_ALREADY_EXISTS 即已有）。
func alreadyRunning() bool {
	name, err := gwsys.UTF16PtrFromString(mutexName)
	if err != nil {
		return false
	}
	_, err = gwsys.CreateMutex(nil, false, name)
	if err == gwsys.ERROR_ALREADY_EXISTS {
		return true
	}
	// 首次创建成功：句柄由进程持有至退出（不关闭），保证互斥。
	return false
}

// requestShow 触发旧实例的显示事件（background 恢复窗口用）。
func requestShow() {
	name, err := gwsys.UTF16PtrFromString(showEventName)
	if err != nil {
		return
	}
	if h, err := gwsys.CreateEvent(nil, 0, 0, name); err == nil && h != 0 {
		_ = gwsys.SetEvent(h)
		_ = gwsys.CloseHandle(h)
	}
}

// assets embeds the built frontend (Reasonix v1.29.0 dist, copied from the
// Electron project's renderer/dist). 前端构建产物零改动地嵌入。
//
//go:embed all:frontend/dist
var assets embed.FS

func main() {
	app := NewApp()

	// 单实例：已有实例（background 模式隐藏）时请求其显示窗口后退出本实例。
	if alreadyRunning() {
		requestShow()
		return
	}

	err := wails.Run(&options.App{
		Title:     "DSH-ReasonixUI",
		Width:     1480,
		Height:    920,
		MinWidth:  760,
		MinHeight: 480,
		// Frameless：前端用 --wails-draggable 标记拖拽区，Wails runtime 原生处理
		// （不经过渲染合成，无叠影）。frameless + 原生拖拽 = 官方 Reasonix 同款。
		Frameless: true,
		// 深色窗口背景，避免 webview 首帧白闪（与前端暗色主题匹配）。
		BackgroundColour: &options.RGBA{R: 9, G: 10, B: 12, A: 255},
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		OnStartup:  app.startup,
		OnDomReady: app.domReady,
		OnShutdown: app.shutdown,
		// Bind：把 App 的导出方法绑定到 window.go.main.App（前端 bridge.ts 直接调用）。
		Bind: []any{app},

		Windows: &windows.Options{
			// 跟随系统主题（浅色/深色），不锁死。
			Theme: windows.SystemDefault,
		},
	})
	if err != nil {
		println("Error:", err.Error())
		_ = os.WriteFile("wails-error.log", []byte(err.Error()), 0o644)
	}
}
