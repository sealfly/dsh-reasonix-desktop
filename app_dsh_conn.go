package main

// app_dsh_conn.go — DSH 连接管理桥：用户可自选 DSH 后端地址（host:port），
// 应用启动自动检测，未连接时前端引导启动/配置。
//
// 持久化到 ~/.reasonix/dsh-config.json（默认 127.0.0.1:3080）。
// 原则：DSH 是共享后端绝不主动关它；启动器只在 3080 空闲时才拉起 dsh web。

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	dshDefaultHost = "127.0.0.1"
	dshDefaultPort = 3080
	dshConfigName  = "dsh-config.json"
)

// dshConnConfig 用户自选的 DSH 后端连接配置。
type dshConnConfig struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}

// dshConfigPath 返回连接配置文件路径（~/.reasonix/dsh-config.json）。
func dshConfigPath() string {
	return filepath.Join(reasonixDataDir(), dshConfigName)
}

// reasonixDataDir 返回用户数据目录（~/.reasonix，原则：展示与持久化适配）。
func reasonixDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".reasonix"
	}
	return filepath.Join(home, ".reasonix")
}

// loadDshConnConfig 读连接配置（缺省/损坏回退默认 127.0.0.1:3080）。
func loadDshConnConfig() dshConnConfig {
	cfg := dshConnConfig{Host: dshDefaultHost, Port: dshDefaultPort}
	data, err := os.ReadFile(dshConfigPath())
	if err != nil {
		return cfg
	}
	var c dshConnConfig
	if json.Unmarshal(data, &c) != nil {
		return cfg
	}
	if strings.TrimSpace(c.Host) != "" {
		cfg.Host = strings.TrimSpace(c.Host)
	}
	if c.Port > 0 && c.Port < 65536 {
		cfg.Port = c.Port
	}
	return cfg
}

// saveDshConnConfig 持久化连接配置。
func saveDshConnConfig(cfg dshConnConfig) error {
	if err := os.MkdirAll(reasonixDataDir(), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(dshConfigPath(), data, 0o644)
}

// pingDsh 测试 host:port 是否有 DSH 服务（session.list 轻量探测，3 秒超时）。
func pingDsh(host string, port int) error {
	d := NewDshClientAt(host, port)
	d.http.Timeout = 3 * time.Second
	_, err := d.RPC("session.list", map[string]any{})
	return err
}

// DshConnStatus 返回当前 DSH 连接状态（前端启动时调用）。
// 返回 {configured:{host,port}, connected:bool, detected:{host,port}, version?, error?}。
// version 为探测到的本地 DSH 版本（dshCoreVersion 读 npm 全局/源码/桌面版），
// 前端横幅可显示"已连接 DSH x.y.z"。
func (a *App) DshConnStatus() map[string]any {
	cfg := loadDshConnConfig()
	status := map[string]any{
		"configured":  map[string]any{"host": cfg.Host, "port": cfg.Port},
		"connected":   false,
		"detected":    map[string]any{"host": "", "port": 0},
		"version":     dshCoreVersion(),
		"installRoot": dshInstallRoot(),
	}
	// 1. 试配置地址
	if err := pingDsh(cfg.Host, cfg.Port); err == nil {
		status["connected"] = true
		status["detected"] = map[string]any{"host": cfg.Host, "port": cfg.Port}
		a.noteConnectedForTree(true)
		return status
	}
	// 2. 配置非默认时回退探测默认 127.0.0.1:3080
	if cfg.Host != dshDefaultHost || cfg.Port != dshDefaultPort {
		if err := pingDsh(dshDefaultHost, dshDefaultPort); err == nil {
			status["connected"] = true
			status["detected"] = map[string]any{"host": dshDefaultHost, "port": dshDefaultPort}
			a.noteConnectedForTree(true)
			return status
		}
	}
	a.noteConnectedForTree(false)
	return status
}

// noteConnectedForTree 记录连接状态；发生"离线 → 在线"跃迁时通知前端刷新项目树。
//
// 为什么在这里发：前端项目树只在挂载时取一次数，之后靠 project-tree:changed 事件刷新。
// 应用先于后端启动时，那次取数是空的 —— 本应用此前从不发这个事件，于是侧栏一直空着
// （真机实测：DSH 里 12 个项目 / 39 个会话，界面显示「还没有项目」）。连接一旦恢复就发一次，
// 前端即可自愈，**不必整页重载**。
func (a *App) noteConnectedForTree(online bool) {
	if markConnectedTransition(online) {
		a.emitProjectTreeChanged("connected")
		a.emitProjectTreeRuntimeChanged()
	}
}

// TestDshConn 测试指定 host:port 连接（前端"测试连接"按钮）。
func (a *App) TestDshConn(host string, port int) map[string]any {
	if strings.TrimSpace(host) == "" {
		host = dshDefaultHost
	}
	if port <= 0 || port >= 65536 {
		port = dshDefaultPort
	}
	if err := pingDsh(strings.TrimSpace(host), port); err != nil {
		return map[string]any{"ok": false, "host": strings.TrimSpace(host), "port": port, "error": err.Error()}
	}
	return map[string]any{"ok": true, "host": strings.TrimSpace(host), "port": port}
}

// SetDshConn 保存用户自选的 DSH 后端地址并测试（前端"保存"按钮）。
func (a *App) SetDshConn(host string, port int) map[string]any {
	host = strings.TrimSpace(host)
	if host == "" {
		host = dshDefaultHost
	}
	if port <= 0 || port >= 65536 {
		port = dshDefaultPort
	}
	cfg := dshConnConfig{Host: host, Port: port}
	if err := saveDshConnConfig(cfg); err != nil {
		return map[string]any{"ok": false, "error": "保存配置失败: " + err.Error()}
	}
	// 保存后测试
	if err := pingDsh(host, port); err != nil {
		return map[string]any{"ok": true, "connected": false, "host": host, "port": port, "warning": "已保存，但当前无法连接: " + err.Error()}
	}
	return map[string]any{"ok": true, "connected": true, "host": host, "port": port}
}

// DshLaunch 启动 DSH 后端（前端"启动 DSH"按钮）。
//
// 2026-09-21 真机修复（旧实现有三个硬伤，导致"启动 DSH"点了没用）：
//
//	1. **写死 127.0.0.1:3080**：用户在「连接设置」里改成别的端口（如 3092）后，
//	   这里仍然去起 3080 —— 配置与行为不一致。现在尊重配置的 host:port。
//	2. **不检查端口占用**：3080 被另一个 DSH 实例占着时照样 `dsh web`，
//	   结果是 EADDRINUSE 或者起出一个本应用**用不了**的后端（实测：占用者要求 token，
//	   而 DshClient 没有任何 token 支持 → session.list 返回 unauthorized）。
//	   现在端口被占且 ping 不通时**明确报错**并给出处置建议，而不是起个注定失败的进程。
//	3. **不等待就绪、不回报原因**：现在拉起后轮询 ping，成功才回报；失败则带上子进程
//	   stderr 尾部（例如 `cannot resolve profile bundle "dsh-tauri"` 这种一眼能看懂的原因）。
//
// 启动形态：优先 Desktop 内置 dsh（与前端版本匹配、通常无需 token），
// 参数 `--profile <p> --no-open --port <port>`；profile 先试 web，
// 若该 profile 缺 bundle（本机实测 `~/.dsh/profiles/web` 里 dsh-tauri* 是指向
// 已删除的 Harness Desktop resources 的死链，任何 `dsh web` 都起不来），
// 自动回退到最小可用 profile（tauri = dsh-base + dsh-web-app）。
//
// 共享后端原则不变：绝不主动关停已在运行、且可用的 DSH。
func (a *App) DshLaunch() map[string]any {
	cfg := loadDshConnConfig()
	host, port := cfg.Host, cfg.Port

	// 已可用 → 直接返回（不重复拉起）。顺便发一次树事件：调用方多半是"启动后端"的用户动作，
	// 此时发一次能让挂在空态的项目树自愈。
	if err := pingDsh(host, port); err == nil {
		a.emitProjectTreeChanged("launch")
		a.emitProjectTreeRuntimeChanged()
		return map[string]any{"ok": true, "alreadyRunning": true, "host": host, "port": port}
	}
	// 端口被占但 ping 不通：占用者不是本应用能用的 DSH，别去起
	if tcpPortInUse(host, port) {
		msg := fmt.Sprintf("%s:%d 已被占用，但该后端不接受本应用的调用"+
			"（常见原因：占用者是另一个要求 token 鉴权的 DSH 实例，而本应用客户端只发裸 RPC）。"+
			"请在「连接设置」里换一个端口（例如 3092）后再启动。", host, port)
		resumeLog("dsh: DshLaunch 中止 —— %s", msg)
		return map[string]any{"ok": false, "host": host, "port": port, "error": msg, "portBusy": true}
	}

	exe, prefix, ok := dshInvocation()
	if !ok {
		// 兜底：npm 全局装一个（保持原行为）
		if npmPath, err := exec.LookPath("npm"); err == nil {
			inst := hiddenCmd(npmPath, "install", "-g", "@deepseek-ai/dsh")
			if err := inst.Start(); err == nil {
				go func() { _ = inst.Wait() }()
				return map[string]any{"ok": true, "installing": true,
					"note": "未找到 dsh, 已开始自动安装 @deepseek-ai/dsh (npm), 完成后点启动"}
			}
		}
		return map[string]any{"ok": false, "error": "未找到 dsh 且自动安装不可用。请手动安装: npm install -g @deepseek-ai/dsh"}
	}

	var lastErr string
	for _, profile := range dshLaunchProfiles() {
		args := append(append([]string{}, prefix...), dshLaunchArgs(profile, port)...)
		if err := launchDshAndWait(exe, args, host, port, dshLaunchWait); err != nil {
			lastErr = fmt.Sprintf("profile=%s: %v", profile, err)
			resumeLog("dsh: DshLaunch %s 失败 —— %v", profile, err)
			continue
		}
		resumeLog("dsh: DshLaunch 成功 profile=%s %s:%d", profile, host, port)
		a.noteConnectedForTree(true) // 记连接跃迁（下次状态查询不会重复发）
		a.emitProjectTreeChanged("launch")
		a.emitProjectTreeRuntimeChanged()
		return map[string]any{"ok": true, "started": true, "host": host, "port": port, "profile": profile,
			"note": "DSH 已启动并就绪"}
	}
	msg := "启动 DSH 失败"
	if lastErr != "" {
		msg += "：" + lastErr
	}
	return map[string]any{"ok": false, "host": host, "port": port, "error": msg}
}

// dshLaunchWait 启动后等待就绪的上限。
const dshLaunchWait = 25 * time.Second

// dshLaunchProfiles 依次尝试的 profile（web 是常规入口；tauri 是 dsh-base + dsh-web-app 的最小组合，
// 用于 web profile 缺 bundle 时兜底）。可用 DSH_LAUNCH_PROFILES 覆盖（逗号分隔，便于排障）。
func dshLaunchProfiles() []string {
	if v := strings.TrimSpace(os.Getenv("DSH_LAUNCH_PROFILES")); v != "" {
		parts := strings.Split(v, ",")
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			if p = strings.TrimSpace(p); p != "" {
				out = append(out, p)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return []string{"web", "tauri"}
}

// dshLaunchArgs 构造 dsh 启动参数（--no-open 避免弹浏览器；--port 用用户配置的端口）。
func dshLaunchArgs(profile string, port int) []string {
	return []string{"--profile", profile, "--no-open", "--port", fmt.Sprint(port)}
}

// tcpPortInUse 判断 host:port 是否已被监听（短超时，仅用于给出可读的错误提示）。
func tcpPortInUse(host string, port int) bool {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, fmt.Sprint(port)), 800*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// launchDshAndWait 拉起 dsh 并轮询等待可用；失败时杀进程并返回**子进程 stderr 尾部**作为原因。
func launchDshAndWait(exe string, args []string, host string, port int, wait time.Duration) error {
	cmd := hiddenCmd(exe, args...)
	var errBuf bytes.Buffer
	cmd.Stderr = &errBuf
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动失败: %w", err)
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()

	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		select {
		case err := <-exited:
			return fmt.Errorf("进程提前退出: %v%s", err, errTail(&errBuf))
		default:
		}
		if pingDsh(host, port) == nil {
			go func() { <-exited }() // 让出僵尸，DSH 常驻
			return nil
		}
		time.Sleep(600 * time.Millisecond)
	}
	_ = cmd.Process.Kill()
	return fmt.Errorf("等待 %s 未就绪%s", wait, errTail(&errBuf))
}

// errTail 取子进程 stderr 的末几行，便于把失败原因直接呈现给用户。
func errTail(buf *bytes.Buffer) string {
	s := strings.TrimSpace(buf.String())
	if s == "" {
		return ""
	}
	lines := strings.Split(s, "\n")
	if len(lines) > 4 {
		lines = lines[len(lines)-4:]
	}
	return " —— " + strings.Join(lines, " ")
}

// dshLaunchText 供前端按钮文案（单点维护）。
func dshLaunchText() string { return fmt.Sprintf("启动 DSH 后端 (%s:%d)", dshDefaultHost, dshDefaultPort) }
