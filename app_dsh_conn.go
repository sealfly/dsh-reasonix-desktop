package main

// app_dsh_conn.go — DSH 连接管理桥：用户可自选 DSH 后端地址（host:port），
// 应用启动自动检测，未连接时前端引导启动/配置。
//
// 持久化到 ~/.reasonix/dsh-config.json（默认 127.0.0.1:3080）。
// 原则：DSH 是共享后端绝不主动关它；启动器只在 3080 空闲时才拉起 dsh web。

import (
	"encoding/json"
	"fmt"
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
// 返回 {configured:{host,port}, connected:bool, detected:{host,port}, error?}。
func (a *App) DshConnStatus() map[string]any {
	cfg := loadDshConnConfig()
	status := map[string]any{
		"configured": map[string]any{"host": cfg.Host, "port": cfg.Port},
		"connected":  false,
		"detected":   map[string]any{"host": "", "port": 0},
	}
	// 1. 试配置地址
	if err := pingDsh(cfg.Host, cfg.Port); err == nil {
		status["connected"] = true
		status["detected"] = map[string]any{"host": cfg.Host, "port": cfg.Port}
		return status
	}
	// 2. 配置非默认时回退探测默认 127.0.0.1:3080
	if cfg.Host != dshDefaultHost || cfg.Port != dshDefaultPort {
		if err := pingDsh(dshDefaultHost, dshDefaultPort); err == nil {
			status["connected"] = true
			status["detected"] = map[string]any{"host": dshDefaultHost, "port": dshDefaultPort}
			return status
		}
	}
	return status
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
// 只在 3080 空闲时拉起：先探测 dsh 命令，后台执行 dsh web，返回启动结果。
// 绝不干扰已运行的 DSH 实例（共享后端原则）。
func (a *App) DshLaunch() map[string]any {
	// 已运行则直接返回
	if err := pingDsh(dshDefaultHost, dshDefaultPort); err == nil {
		return map[string]any{"ok": true, "alreadyRunning": true, "host": dshDefaultHost, "port": dshDefaultPort}
	}
	// 找 dsh 命令
	dshCmd := "dsh"
	if _, err := exec.LookPath("dsh"); err != nil {
		// 常见 npm 全局位置
		candidates := []string{
			filepath.Join(os.Getenv("APPDATA"), "npm", "dsh.cmd"),
			filepath.Join(os.Getenv("ProgramFiles"), "nodejs", "dsh.cmd"),
		}
		found := false
		for _, c := range candidates {
			if c != "" {
				if _, err := os.Stat(c); err == nil {
					dshCmd = c
					found = true
					break
				}
			}
		}
		if !found {
			return map[string]any{"ok": false, "error": "未找到 dsh 命令。请先安装: npm install -g @deepseek-ai/dsh"}
		}
	}
	// 后台启动 dsh web（不阻塞应用）
	cmd := exec.Command(dshCmd, "web")
	if err := cmd.Start(); err != nil {
		return map[string]any{"ok": false, "error": "启动 DSH 失败: " + err.Error()}
	}
	go func() {
		_ = cmd.Wait() // 让出进程；DSH web 常驻
	}()
	return map[string]any{"ok": true, "started": true, "host": dshDefaultHost, "port": dshDefaultPort, "note": "DSH 正在启动，初始化可能需要几秒"}
}

// dshLaunchText 供前端按钮文案（单点维护）。
func dshLaunchText() string { return fmt.Sprintf("启动 DSH 后端 (%s:%d)", dshDefaultHost, dshDefaultPort) }
