package main

// app_mcp.go — MCP 服务器配置的本地持久化桥（DSH 语义）。
//
// 探测确认 DSH 后端无 MCP 管理 RPC（mcp.* / cap / plugins 均 404，session.* 正常）。
// 按项目原则 1（桥做"展示与持久化适配"）：MCP 服务器配置持久化到
// ~/.reasonix/mcp-servers.json，前端 v1.31.4 的 MCP 设置页可添加/编辑/删除。
// DSH 侧当前不加载 MCP（工具数 0，诚实降级），配置留待 DSH 支持 MCP 时直接消费。

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// mcpServerEntry 持久化的 MCP 服务器配置（对齐官方 MCPServerInput 字段）。
type mcpServerEntry struct {
	Name               string            `json:"name"`
	Transport          string            `json:"transport"`
	Command            string            `json:"command,omitempty"`
	Args               []string          `json:"args,omitempty"`
	URL                string            `json:"url,omitempty"`
	Env                map[string]string `json:"env,omitempty"`
	Headers            map[string]string `json:"headers,omitempty"`
	AutoStart          *bool             `json:"autoStart,omitempty"`
	CallTimeoutSeconds *int              `json:"callTimeoutSeconds,omitempty"`
	ToolTimeoutSeconds map[string]int    `json:"toolTimeoutSeconds,omitempty"`
}

// mcpManager 本地 MCP 配置管理器（单例，仿 pluginManager 模式）。
type mcpManager struct {
	mu   sync.Mutex
	path string
}

var (
	appMCPMgr  *mcpManager
	appMCPOnce sync.Once
)

func getMCPManager() *mcpManager {
	appMCPOnce.Do(func() {
		home, _ := os.UserHomeDir()
		if home == "" {
			home = "."
		}
		dir := filepath.Join(home, ".reasonix")
		_ = os.MkdirAll(dir, 0o755)
		appMCPMgr = &mcpManager{path: filepath.Join(dir, "mcp-servers.json")}
	})
	return appMCPMgr
}

func (m *mcpManager) load() []mcpServerEntry {
	m.mu.Lock()
	defer m.mu.Unlock()
	data, err := os.ReadFile(m.path)
	if err != nil {
		return []mcpServerEntry{}
	}
	var list []mcpServerEntry
	if json.Unmarshal(data, &list) != nil || list == nil {
		return []mcpServerEntry{}
	}
	return list
}

func (m *mcpManager) save(list []mcpServerEntry) {
	m.mu.Lock()
	defer m.mu.Unlock()
	data, _ := json.MarshalIndent(list, "", "  ")
	_ = os.WriteFile(m.path, data, 0o644)
}

// normalizeMCPTransport 规范化传输类型（官方语义：stdio / sse / streamable-http）。
func normalizeMCPTransport(t string) string {
	switch strings.ToLower(strings.TrimSpace(t)) {
	case "sse":
		return "sse"
	case "http", "streamable-http", "streamablehttp":
		return "streamable-http"
	default:
		return "stdio"
	}
}

// mcpServerInputFromMap 从前端 JSON 提取 MCPServerInput 字段。
func mcpServerInputFromMap(in map[string]any) mcpServerEntry {
	var e mcpServerEntry
	e.Name = strings.TrimSpace(strOf(in["name"]))
	e.Transport = normalizeMCPTransport(strOf(in["transport"]))
	e.Command = strings.TrimSpace(strOf(in["command"]))
	e.URL = strings.TrimSpace(strOf(in["url"]))
	if a, ok := in["args"].([]any); ok {
		for _, v := range a {
			e.Args = append(e.Args, strOf(v))
		}
	}
	if env, ok := in["env"].(map[string]any); ok {
		e.Env = map[string]string{}
		for k, v := range env {
			e.Env[k] = strOf(v)
		}
	}
	if hd, ok := in["headers"].(map[string]any); ok {
		e.Headers = map[string]string{}
		for k, v := range hd {
			e.Headers[k] = strOf(v)
		}
	}
	if b, ok := in["autoStart"].(bool); ok {
		e.AutoStart = &b
	}
	if n, ok := intOf(in["callTimeoutSeconds"]); ok {
		e.CallTimeoutSeconds = &n
	}
	if tm, ok := in["toolTimeoutSeconds"].(map[string]any); ok {
		e.ToolTimeoutSeconds = map[string]int{}
		for k, v := range tm {
			if n, ok := intOf(v); ok {
				e.ToolTimeoutSeconds[k] = n
			}
		}
	}
	return e
}

// intOf 安全取 int（float64/int/int64/string）。
func intOf(v any) (int, bool) {
	switch t := v.(type) {
	case float64:
		return int(t), true
	case int:
		return t, true
	case int64:
		return int(t), true
	case string:
		var n int
		if _, err := fmt.Sscanf(t, "%d", &n); err == nil {
			return n, true
		}
	}
	return 0, false
}

// mapKeys 取 map 键（排序，前端 envKeys/headerKeys 显示用）。
func mapKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// AddMCPServer 添加 MCP 服务器配置（DSH 语义：本地持久化，DSH 不加载）。
// 返回工具数（DSH 未消费 MCP → 0，诚实降级）。
func (a *App) AddMCPServer(in map[string]any) int {
	mgr := getMCPManager()
	entry := mcpServerInputFromMap(in)
	if entry.Name == "" {
		return 0
	}
	list := mgr.load()
	for i := range list {
		if list[i].Name == entry.Name {
			list[i] = entry
			mgr.save(list)
			return 0
		}
	}
	list = append(list, entry)
	mgr.save(list)
	return 0
}

// MCPServers MCP 服务器列表（本地持久化配置，DSH 未加载，工具数 0）。
func (a *App) MCPServers() []any {
	mgr := getMCPManager()
	list := mgr.load()
	out := make([]any, 0, len(list))
	for _, e := range list {
		autoStart := false
		if e.AutoStart != nil {
			autoStart = *e.AutoStart
		}
		m := map[string]any{
			"name": e.Name, "transport": e.Transport, "status": "configured",
			"enabled": autoStart, "installed": true, "configured": true,
			"source": "user-config", "configSource": "user-config",
			"command": e.Command, "args": e.Args, "url": e.URL,
			"envKeys": mapKeys(e.Env), "headerKeys": mapKeys(e.Headers),
			"tools": 0, "toolCount": 0, "prompts": 0, "resources": 0,
			"toolList": []any{}, "authStatus": "none",
		}
		if e.CallTimeoutSeconds != nil {
			m["callTimeoutSeconds"] = *e.CallTimeoutSeconds
		}
		if e.ToolTimeoutSeconds != nil {
			m["toolTimeoutSeconds"] = e.ToolTimeoutSeconds
		}
		out = append(out, m)
	}
	return out
}

// UpdateMCPServer 更新 MCP 服务器配置（name 为稳定身份，不支持改名，与官方语义一致）。
func (a *App) UpdateMCPServer(name string, in map[string]any) error {
	mgr := getMCPManager()
	entry := mcpServerInputFromMap(in)
	trimmed := strings.TrimSpace(name)
	if entry.Name != "" && entry.Name != trimmed {
		return fmt.Errorf("renaming MCP servers is not supported; remove and add a new server")
	}
	entry.Name = trimmed
	if entry.Name == "" {
		return fmt.Errorf("no configured MCP server name")
	}
	list := mgr.load()
	for i := range list {
		if list[i].Name == entry.Name {
			list[i] = entry
			mgr.save(list)
			return nil
		}
	}
	return fmt.Errorf("no configured MCP server named %q", name)
}

// RemoveMCPServer 删除 MCP 服务器配置。
func (a *App) RemoveMCPServer(name string) error {
	mgr := getMCPManager()
	list := mgr.load()
	out := list[:0]
	found := false
	for _, e := range list {
		if e.Name == name {
			found = true
			continue
		}
		out = append(out, e)
	}
	if !found {
		return fmt.Errorf("no configured MCP server named %q", name)
	}
	mgr.save(out)
	return nil
}
