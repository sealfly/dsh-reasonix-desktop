package main

// app_acp.go — ACP (Agent Client Protocol) 预留接口。
//
// 背景：项目走 dsh-std Host + HTTP RPC 连共享 DSH（127.0.0.1:3080），当前不依赖 ACP。
// 为未来留出接口（沙箱独立 agent / 隔离任务 / 对接 @deepseek-ai/dsh-acp 自动化服务器），
// 这里暴露桥方法，签名对准 ACP 协议方法（session/new, session/prompt, session/cancel...）。
//
// 原则：预留 = 结构完整 + 安全兜底——方法存在、返回清晰 reserved 状态，前端可显示"预留"；
// 不假装可用、不干扰现有 HTTP RPC。未来启用时：探测 dsh → spawn ACP 组合(stdio) → 转发。

import (
	"os"
	"os/exec"
	"path/filepath"
)

// acpReservedNote 每个 reserved 响应都带（前端可据此显示"预留/未启用"）。
// 所有 ACP 桥方法当前只返回 reserved 状态：结构先就位、不假装可用，
// 也不干扰项目主通道（HTTP RPC 连共享 DSH 3080）。
const acpReservedNote = "ACP reserved: wire dsh ACP server (stdio) later; HTTP RPC unaffected"

// AcpAvailable 探测 DSH 是否具备 ACP 能力（本机 dsh 命令存在）。
// 返回里 available/dshDetected 表示"机器上找得到 dsh"（未来能 spawn ACP），
// enabled=false + stage="reserved" 表示功能本身还没启用——前端据此显示"预留"。
func (a *App) AcpAvailable() map[string]any {
	dshDetected := dshCmdExists()
	return map[string]any{
		"available":   dshDetected,
		"dshDetected": dshDetected,
		"enabled":     false,
		"stage":       "reserved",
		"note":        acpReservedNote,
	}
}

// AcpStart 预留：启动一个 ACP 会话（未来 spawn dsh ACP 组合并保持 stdio）。
// config 可含 {cwd, provider, model}。当前返回 reserved。
func (a *App) AcpStart(config map[string]any) map[string]any {
	return map[string]any{
		"ok": false, "stage": "reserved",
		"error":  "ACP session start not enabled yet",
		"config": config,
		"note":   acpReservedNote,
	}
}

// AcpPrompt 预留：向 ACP 会话发 prompt（对应 ACP session/prompt）。
// sessionId 为本接口自管的会话标识（与 DSH 3080 会话无关）。
func (a *App) AcpPrompt(sessionId, prompt string) map[string]any {
	return map[string]any{
		"ok": false, "stage": "reserved",
		"sessionId": sessionId,
		"error":     "ACP prompt not enabled yet",
		"note":      acpReservedNote,
	}
}

// AcpCancel 预留：取消 ACP 会话进行中请求（ACP session/cancel）。
func (a *App) AcpCancel(sessionId string) map[string]any {
	return map[string]any{
		"ok": false, "stage": "reserved",
		"sessionId": sessionId,
		"error":     "ACP cancel not enabled yet",
		"note":      acpReservedNote,
	}
}

// AcpStop 预留：关闭 ACP 会话（断开 stdio）。
func (a *App) AcpStop(sessionId string) map[string]any {
	return map[string]any{
		"ok": false, "stage": "reserved",
		"sessionId": sessionId,
		"error":     "ACP stop not enabled yet",
		"note":      acpReservedNote,
	}
}

// dshCmdExists 检测 dsh 命令是否存在（PATH 直查 + npm 全局常见落点兜底）。
// 用于 AcpAvailable 判断"机器上是否具备未来 spawn dsh ACP 组合的条件"。
func dshCmdExists() bool {
	// 1) PATH 里直接找 dsh（npm -g 安装后通常已进 PATH）
	if p, err := exec.LookPath("dsh"); err == nil && p != "" {
		return true
	}
	// 2) npm 全局常见落点（PATH 可能不含）：
	//    %APPDATA%\npm\dsh.cmd、%ProgramFiles%\nodejs\dsh.cmd、
	//    %APPDATA%\npm\node_modules\@deepseek-ai\dsh（免 cmd 的安装检测）
	seen := map[string]bool{}
	roots := []string{
		os.Getenv("APPDATA"),
		os.Getenv("ProgramFiles"),
		filepath.Join(os.Getenv("ProgramFiles"), "nodejs"),
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		roots = append(roots, filepath.Join(home, "AppData", "Roaming", "npm"))
	}
	for _, r := range roots {
		if r == "" || seen[r] {
			continue // 空路径/已查过去重
		}
		seen[r] = true
		// 命中其一即认为 dsh 已安装：命令入口(dsh.cmd) 或 包本体(带 bin 的 package.json)
		for _, c := range []string{filepath.Join(r, "dsh.cmd"), filepath.Join(r, "node_modules", "@deepseek-ai", "dsh", "package.json")} {
			if _, err := os.Stat(c); err == nil {
				return true
			}
		}
	}
	return false
}
