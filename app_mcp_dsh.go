package main

// app_mcp_dsh.go — MCP 服务器配置的 **DSH 真实生效**实现。
//
// 背景（2026-09-18 完善）：
// 旧实现把 MCP 配置写进 ~/.reasonix/mcp-servers.json —— 那份文件 **DSH 永远不会读**，
// 所以"设置-MCP与工具"页里加多少服务器都对 DSH 毫无影响（工具数恒为 0）。
// 实测确认：DSH 的 MCP 支持由 @deepseek-ai/dsh-mcp-client 插件提供，**每个 MCP 服务器
// = profile 配置里的一个插件实例**，且该插件支持 HMR 热替换（改配置即断线重连，无需重启）：
//
//	# ~/.dsh/profiles/<profile>/cordis.patch.yml
//	- id: mcp-github                          # 插件实例 id（本文件内唯一）
//	  name: '@deepseek-ai/dsh-mcp-client'
//	  disabled: true                          # ← 启停开关（HMR 即时生效）
//	  config:
//	    serverName: github                    # 模型看到的工具前缀 mcp__<serverName>__<tool>
//	    transport: stdio                      # stdio | streamable-http
//	    command: npx
//	    args: ['-y', '@modelcontextprotocol/server-github']
//	    env:
//	      GITHUB_TOKEN: xxx
//
// 因此本文件把 MCP 页接到 profile 的 patch 层：
//   - 读：解析 patch 层（+ cordis.yml）里的 mcp-client 实例 → ServerView[]
//   - 写：**只替换 MCP 块**，其余行（注释、别的插件、用户手写内容）逐字节保留；
//         写前备份、写后重解析校验、原子替换
//   - 启停：设/清 `disabled:`（DSH HMR 立即重连/断开）
//   - 重连：重写该块（mtime 变化触发 HMR 重连）
//
// 诚实降级：DSH 未暴露任何 MCP 状态 RPC（mcp.*/plugin.* 实测 404），logs 目录也为空，
// 因此**不伪造 connected**：启用中报 `deferred`（DSH 按需加载），禁用报 `disabled`。

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// mcpPluginName 是 DSH 侧提供 MCP 能力的插件包名。
const mcpPluginName = "@deepseek-ai/dsh-mcp-client"

// mcpDSHServer 是 profile 配置里的一个 MCP 插件实例。
type mcpDSHServer struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"` // = config.serverName
	Disabled  bool              `json:"disabled"`
	Transport string            `json:"transport"`
	Command   string            `json:"command"`
	Args      []string          `json:"args"`
	Env       map[string]string `json:"env"`
	URL       string            `json:"url"`
	Headers   map[string]string `json:"headers"`
	// lineStart/lineEnd 是该块在 patch 文件里的行区间（0-based，含首行、不含末行）
	lineStart int
	lineEnd   int
}

// mcpProfilePaths 返回 DSH profile 的配置路径。
//
// profile 解析顺序：环境变量 DSH_PROFILE → 正在运行的 dsh 进程命令行（bin.js <profile>）
// → ~/.dsh/profiles 下唯一/含 web 的目录 → "web"。
func mcpProfilePaths() (profile string, patchPath string, cordisPath string, err error) {
	home := os.Getenv("DSH_HOME")
	if strings.TrimSpace(home) == "" {
		if h, e := os.UserHomeDir(); e == nil {
			home = filepath.Join(h, ".dsh")
		}
	}
	profile = strings.TrimSpace(os.Getenv("DSH_PROFILE"))
	if profile == "" {
		profile = detectRunningDSHProfile()
	}
	profilesDir := filepath.Join(home, "profiles")
	if profile == "" {
		if entries, e := os.ReadDir(profilesDir); e == nil {
			names := []string{}
			for _, e := range entries {
				if e.IsDir() {
					names = append(names, e.Name())
				}
			}
			sort.Strings(names)
			for _, n := range names {
				if n == "web" {
					profile = n
					break
				}
			}
			if profile == "" && len(names) > 0 {
				profile = names[0]
			}
		}
	}
	if profile == "" {
		profile = "web"
	}
	dir := filepath.Join(profilesDir, profile)
	return profile, filepath.Join(dir, "cordis.patch.yml"), filepath.Join(dir, "cordis.yml"), nil
}

// detectRunningDSHProfile 从正在运行的 dsh 进程命令行里取 profile（`node bin.js web`）。
func detectRunningDSHProfile() string {
	out, err := hiddenCmd("powershell.exe", "-NoProfile", "-Command",
		"(Get-CimInstance Win32_Process -Filter \"Name='node.exe'\" | Where-Object { $_.CommandLine -like '*dsh*bin.js*' } | Select-Object -First 1).CommandLine").Output()
	if err != nil {
		return ""
	}
	re := regexp.MustCompile(`bin\.js["']?\s+([A-Za-z0-9_-]+)`)
	if m := re.FindStringSubmatch(string(out)); len(m) == 2 {
		return strings.TrimSpace(m[1])
	}
	return ""
}

// parseMCPBlockFields 从块文本里抽出 MCP 实例字段（只认我们自己写出的规范形式 + README 里的常见形式）。
func parseMCPBlockFields(block []string) (mcpDSHServer, bool) {
	s := mcpDSHServer{Env: map[string]string{}, Headers: map[string]string{}}
	joined := strings.Join(block, "\n")
	if !strings.Contains(joined, "dsh-mcp-client") {
		return s, false
	}
	insideConfig := false
	section := "" // env | headers | args
	for _, raw := range block {
		line := raw
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		switch {
		case indent == 0 && strings.HasPrefix(trimmed, "- "):
			// 块首行：- id: xxx
			if v, ok := mcpYAMLScalar(strings.TrimPrefix(trimmed, "- "), "id"); ok {
				s.ID = v
			}
		case indent <= 2 && !insideConfig && strings.HasPrefix(trimmed, "disabled:"):
			s.Disabled = strings.TrimSpace(strings.TrimPrefix(trimmed, "disabled:")) == "true"
		case indent <= 2 && strings.HasPrefix(trimmed, "config:"):
			insideConfig = true
		case insideConfig:
			if indent <= 2 {
				insideConfig = false
				continue
			}
			// config 内：缩进 ≥4
			if indent == 4 {
				section = ""
				switch {
				case strings.HasPrefix(trimmed, "serverName:"):
					s.Name = strings.Trim(strings.TrimSpace(strings.TrimPrefix(trimmed, "serverName:")), `'"`)
				case strings.HasPrefix(trimmed, "transport:"):
					s.Transport = strings.Trim(strings.TrimSpace(strings.TrimPrefix(trimmed, "transport:")), `'"`)
				case strings.HasPrefix(trimmed, "command:"):
					s.Command = strings.Trim(strings.TrimSpace(strings.TrimPrefix(trimmed, "command:")), `'"`)
				case strings.HasPrefix(trimmed, "url:"):
					s.URL = strings.Trim(strings.TrimSpace(strings.TrimPrefix(trimmed, "url:")), `'"`)
				case strings.HasPrefix(trimmed, "env:"):
					section = "env"
				case strings.HasPrefix(trimmed, "headers:"):
					section = "headers"
				case strings.HasPrefix(trimmed, "args:"):
					section = "args"
					s.Args = parseInlineList(strings.TrimSpace(strings.TrimPrefix(trimmed, "args:")))
				}
				continue
			}
			switch section {
			case "env":
				if k, v, ok := yamlKeyValue(trimmed); ok {
					s.Env[k] = v
				}
			case "headers":
				if k, v, ok := yamlKeyValue(trimmed); ok {
					s.Headers[k] = v
				}
			case "args":
				if strings.HasPrefix(trimmed, "- ") {
					s.Args = append(s.Args, strings.Trim(strings.TrimSpace(strings.TrimPrefix(trimmed, "- ")), `'"`))
				}
			}
		}
	}
	if s.ID == "" && s.Name == "" {
		return s, false
	}
	if s.Name == "" {
		s.Name = s.ID
	}
	return s, true
}

// yamlScalar 从 `key: value` 片段里取 key 对应的值。
func mcpYAMLScalar(line, key string) (string, bool) {
	prefix := key + ":"
	if !strings.HasPrefix(strings.TrimSpace(line), prefix) {
		return "", false
	}
	return strings.Trim(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), prefix)), `'"`), true
}

// yamlKeyValue 解析 `KEY: value`（env / headers 项）。
func yamlKeyValue(line string) (string, string, bool) {
	idx := strings.Index(line, ":")
	if idx <= 0 {
		return "", "", false
	}
	k := strings.TrimSpace(line[:idx])
	v := strings.Trim(strings.TrimSpace(line[idx+1:]), `'"`)
	if k == "" || strings.ContainsAny(k, "[]{}") {
		return "", "", false
	}
	return k, v, true
}

// parseInlineList 解析 `['a','b']` / `["a"]` / `[a, b]` 形式的行内数组。
func parseInlineList(s string) []string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "[") || !strings.HasSuffix(s, "]") {
		return nil
	}
	inner := strings.TrimSpace(s[1 : len(s)-1])
	if inner == "" {
		return nil
	}
	out := []string{}
	for _, part := range strings.Split(inner, ",") {
		v := strings.Trim(strings.TrimSpace(part), `'"`)
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}

// readProfileMCPServers 解析 profile 配置里的全部 MCP 插件实例。
func readProfileMCPServers(path string) ([]mcpDSHServer, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []mcpDSHServer{}, nil
		}
		return nil, err
	}
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	out := []mcpDSHServer{}
	i := 0
	for i < len(lines) {
		line := lines[i]
		if !strings.HasPrefix(line, "- ") && line != "-" {
			i++
			continue
		}
		start := i
		j := i + 1
		for j < len(lines) && !(strings.HasPrefix(lines[j], "- ") || lines[j] == "-") {
			j++
		}
		if s, ok := parseMCPBlockFields(lines[start:j]); ok {
			s.lineStart = start
			s.lineEnd = j
			out = append(out, s)
		}
		i = j
	}
	return out, nil
}

// renderMCPBlock 生成一个 MCP 插件实例的规范 YAML 文本（写回时使用）。
func renderMCPBlock(s mcpDSHServer) string {
	var b strings.Builder
	b.WriteString("- id: " + yamlQuote(s.ID) + "\n")
	b.WriteString("  name: '" + mcpPluginName + "'\n")
	if s.Disabled {
		b.WriteString("  disabled: true\n")
	}
	b.WriteString("  config:\n")
	b.WriteString("    serverName: " + yamlQuote(s.Name) + "\n")
	transport := s.Transport
	if transport == "" {
		transport = "stdio"
	}
	b.WriteString("    transport: " + yamlQuote(transport) + "\n")
	if transport == "stdio" {
		if s.Command != "" {
			b.WriteString("    command: " + yamlQuote(s.Command) + "\n")
		}
		if len(s.Args) > 0 {
			b.WriteString("    args:\n")
			for _, a := range s.Args {
				b.WriteString("      - " + yamlQuote(a) + "\n")
			}
		}
	} else if s.URL != "" {
		b.WriteString("    url: " + yamlQuote(s.URL) + "\n")
	}
	if len(s.Env) > 0 {
		b.WriteString("    env:\n")
		for _, k := range sortedKeys(s.Env) {
			b.WriteString("      " + k + ": " + yamlQuote(s.Env[k]) + "\n")
		}
	}
	if len(s.Headers) > 0 {
		b.WriteString("    headers:\n")
		for _, k := range sortedKeys(s.Headers) {
			b.WriteString("      " + k + ": " + yamlQuote(s.Headers[k]) + "\n")
		}
	}
	return b.String()
}

// yamlQuote 给标量加**必要**的引号（能不加就不加，保持 profile 配置可读）。
//
// YAML 里这些情况必须加引号：空值；首尾空格；以指示符开头（- ? : , [ ] { } # & * ! | > ' " % @ `）；
// 含 ": "（冒号+空格）、" #"、或流式集合符号。其余（如 tok-123、https://h:3000/mcp、
// streamable-http）保持裸标量即可 —— 它们本就是合法 plain scalar。
func yamlQuote(v string) string {
	if v == "" {
		return "''"
	}
	needQuote := v != strings.TrimSpace(v) ||
		strings.Contains(v, ": ") || strings.Contains(v, " #") ||
		strings.ContainsAny(v, "[]{},&*!|>%@`\"'#") ||
		strings.ContainsAny(v[:1], "-?:,[]{}#&*!|>'\"%@`")
	if !needQuote {
		return v
	}
	return "'" + strings.ReplaceAll(v, "'", "''") + "'"
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// writeProfileMCPServers 用给定的 MCP 实例集合重写 profile 配置里的 **MCP 块**。
//
// 只替换 MCP 块：文件里的其它行（注释、别的插件、用户手写内容）逐字节保留。
// 写前备份（.bak-dsh-mcp-<时间戳>）、写后重解析校验、原子替换（tmp + rename）、失败即停。
func writeProfileMCPServers(path string, servers []mcpDSHServer) error {
	var original string
	if data, err := os.ReadFile(path); err == nil {
		original = strings.ReplaceAll(string(data), "\r\n", "\n")
	} else if !os.IsNotExist(err) {
		return err
	}
	lines := strings.Split(original, "\n")

	// 收集所有 MCP 块的行区间（含它前面的空行/注释归属：这里只取块本身，注释留在原位）
	type span struct{ start, end int }
	spans := []span{}
	i := 0
	for i < len(lines) {
		if !strings.HasPrefix(lines[i], "- ") && lines[i] != "-" {
			i++
			continue
		}
		j := i + 1
		for j < len(lines) && !(strings.HasPrefix(lines[j], "- ") || lines[j] == "-") {
			j++
		}
		if _, ok := parseMCPBlockFields(lines[i:j]); ok {
			spans = append(spans, span{i, j})
		}
		i = j
	}

	// 逐字节保留非 MCP 行
	keep := make([]bool, len(lines))
	for k := range keep {
		keep[k] = true
	}
	for _, sp := range spans {
		for k := sp.start; k < sp.end; k++ {
			keep[k] = false
		}
	}
	out := []string{}
	end := len(lines)
	if end > 0 && lines[end-1] == "" {
		end--
	}
	for k := 0; k < end; k++ {
		if keep[k] {
			out = append(out, lines[k])
		}
	}
	// 追加新的 MCP 块
	for _, s := range servers {
		block := strings.TrimRight(renderMCPBlock(s), "\n")
		out = append(out, strings.Split(block, "\n")...)
	}
	text := strings.Join(out, "\n")
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}

	if text == original {
		return nil // 无变化（幂等）
	}
	if original != "" {
		bak := fmt.Sprintf("%s.bak-dsh-mcp-%s", path, timeStampCompact())
		if err := os.WriteFile(bak, []byte(original), 0o644); err != nil {
			return fmt.Errorf("备份失败，已放弃写入: %w", err)
		}
	}
	tmp := path + ".tmp-dsh-mcp"
	if err := os.WriteFile(tmp, []byte(text), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	// 写后校验：能重新解析出期望的服务器集合
	back, err := readProfileMCPServers(path)
	if err != nil {
		return fmt.Errorf("写入后校验失败: %w", err)
	}
	got := map[string]bool{}
	for _, s := range back {
		got[s.Name] = true
	}
	for _, s := range servers {
		if !got[s.Name] {
			return fmt.Errorf("写入后校验失败：配置里找不到 MCP 服务器 %q", s.Name)
		}
	}
	return nil
}

// timeStampCompact 生成紧凑时间戳（用于备份文件名）。
func timeStampCompact() string {
	return time.Now().Format("20060102-150405")
}

// ===== 桥方法：MCP 服务器（DSH profile 配置驱动）=====

// mcpServerFromInput 把前端 MCPServerInput 归一成 profile 实例。
func mcpServerFromInput(in map[string]any) mcpDSHServer {
	s := mcpDSHServer{Env: map[string]string{}, Headers: map[string]string{}}
	if in == nil {
		return s
	}
	get := func(k string) string {
		if v, ok := in[k].(string); ok {
			return strings.TrimSpace(v)
		}
		return ""
	}
	s.Name = get("name")
	s.Transport = normalizeMCPTransport(get("transport"))
	s.Command = get("command")
	s.URL = get("url")
	if raw, ok := in["args"].([]any); ok {
		for _, a := range raw {
			if v, ok := a.(string); ok && strings.TrimSpace(v) != "" {
				s.Args = append(s.Args, strings.TrimSpace(v))
			}
		}
	}
	if raw, ok := in["env"].(map[string]any); ok {
		for k, v := range raw {
			if sv, ok := v.(string); ok && sv != "" {
				s.Env[k] = sv
			}
		}
	}
	if raw, ok := in["headers"].(map[string]any); ok {
		for k, v := range raw {
			if sv, ok := v.(string); ok && sv != "" {
				s.Headers[k] = sv
			}
		}
	}
	if s.Transport == "" {
		if s.URL != "" {
			s.Transport = "streamable-http"
		} else {
			s.Transport = "stdio"
		}
	}
	s.ID = "mcp-" + sanitizeMCPName(s.Name)
	return s
}

// sanitizeMCPName 生成安全的插件实例 id（[A-Za-z0-9_-]）。
func sanitizeMCPName(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		out = "server"
	}
	return out
}

// mcpServerView 把 profile 实例转成前端 ServerView。
//
// 状态诚实映射（DSH 无 MCP 状态 RPC，实测 mcp.*/plugin.* 404，logs 目录为空）：
//   - 禁用 → "disabled"；启用 → "deferred"（DSH 按需连接，我们不伪造 connected）
func mcpServerView(s mcpDSHServer, source string) map[string]any {
	status := "deferred"
	availability := "available_on_demand"
	if s.Disabled {
		status = "disabled"
		availability = "disabled"
	}
	view := map[string]any{
		"name": s.Name, "transport": s.Transport, "status": status,
		"enabled": !s.Disabled, "installed": true, "configured": true,
		"source": source, "configSource": source, "availability": availability,
		"autoStart": !s.Disabled, "action": "none",
		"envKeys": sortedKeys(s.Env), "headerKeys": sortedKeys(s.Headers),
		"tools": 0, "toolCount": 0, "prompts": 0, "resources": 0,
		"toolList": []any{}, "authStatus": "none",
	}
	if s.Command != "" {
		view["command"] = s.Command
	}
	if len(s.Args) > 0 {
		view["args"] = s.Args
	}
	if s.URL != "" {
		view["url"] = s.URL
	}
	return view
}

// MCPServers MCP 服务器列表（读 DSH profile 配置；并把旧本地 JSON 里的历史配置一并列出）。
func (a *App) MCPServers() []any {
	_, patch, _, _ := mcpProfilePaths()
	servers, err := readProfileMCPServers(patch)
	out := []any{}
	if err == nil {
		for _, s := range servers {
			out = append(out, mcpServerView(s, "plugin"))
		}
	}
	// 历史配置（旧实现写在 ~/.reasonix/mcp-servers.json，DSH 从不读取）：
	// 仍然列出来并标成 legacy，让用户看得到、可以迁到 profile 里或删掉。
	seen := map[string]bool{}
	for _, s := range servers {
		seen[s.Name] = true
	}
	for _, e := range getMCPManager().load() {
		if seen[e.Name] {
			continue
		}
		legacy := mcpDSHServer{Name: e.Name, Transport: e.Transport, Command: e.Command,
			Args: e.Args, URL: e.URL, Env: e.Env, Headers: e.Headers}
		view := mcpServerView(legacy, "legacy-local")
		view["status"] = "disabled"
		view["enabled"] = false
		view["availability"] = "disabled"
		view["action"] = "retry"
		out = append(out, view)
	}
	return out
}

// AddMCPServer 添加 MCP 服务器：写入 DSH profile 的 patch 层（HMR 热生效），返回工具数。
//
// 工具数在添加时无法确定（DSH 按需连接后才注册 mcp__<name>__<tool>），因此返回 0 并
// 由页面的 InstallMCPServer 给出说明；不伪造数字。
func (a *App) AddMCPServer(in map[string]any) int {
	entry := mcpServerFromInput(in)
	if entry.Name == "" {
		return 0
	}
	if err := a.upsertMCPProfileServer(entry); err != nil {
		resumeLog("mcp: add %s failed: %v", entry.Name, err)
		return 0
	}
	// 若同名存在于旧本地 JSON，迁移后清理，避免页面出现重复项
	a.removeLegacyMCPEntry(entry.Name)
	return 0
}

// upsertMCPProfileServer 写入/更新 profile 里的 MCP 实例（保留 enabled 状态）。
func (a *App) upsertMCPProfileServer(entry mcpDSHServer) error {
	_, patch, _, err := mcpProfilePaths()
	if err != nil {
		return err
	}
	servers, err := readProfileMCPServers(patch)
	if err != nil {
		return err
	}
	replaced := false
	for i := range servers {
		if servers[i].Name == entry.Name {
			entry.Disabled = servers[i].Disabled
			servers[i] = entry
			replaced = true
			break
		}
	}
	if !replaced {
		servers = append(servers, entry)
	}
	if err := writeProfileMCPServers(patch, servers); err != nil {
		return err
	}
	resumeLog("mcp: %s 已写入 DSH profile 配置（%s）", entry.Name, patch)
	return nil
}

// removeLegacyMCPEntry 从旧本地 JSON 里删除同名条目（迁移用）。
func (a *App) removeLegacyMCPEntry(name string) {
	mgr := getMCPManager()
	list := mgr.load()
	out := list[:0]
	changed := false
	for _, e := range list {
		if e.Name == name {
			changed = true
			continue
		}
		out = append(out, e)
	}
	if changed {
		mgr.save(out)
	}
}

// UpdateMCPServer 更新 MCP 服务器（name 为稳定身份，不支持改名，与官方语义一致）。
func (a *App) UpdateMCPServer(name string, in map[string]any) error {
	entry := mcpServerFromInput(in)
	trimmed := strings.TrimSpace(name)
	if entry.Name != "" && entry.Name != trimmed {
		return fmt.Errorf("renaming MCP servers is not supported; remove and add a new server")
	}
	if trimmed == "" {
		return fmt.Errorf("no configured MCP server name")
	}
	entry.Name = trimmed
	entry.ID = "mcp-" + sanitizeMCPName(trimmed)
	_, patch, _, _ := mcpProfilePaths()
	servers, err := readProfileMCPServers(patch)
	if err != nil {
		return err
	}
	for i := range servers {
		if servers[i].Name == trimmed {
			entry.Disabled = servers[i].Disabled
			servers[i] = entry
			return writeProfileMCPServers(patch, servers)
		}
	}
	return fmt.Errorf("no configured MCP server named %q", name)
}

// RemoveMCPServer 删除 MCP 服务器（profile 配置 + 旧本地 JSON 都清）。
func (a *App) RemoveMCPServer(name string) error {
	_, patch, _, _ := mcpProfilePaths()
	servers, err := readProfileMCPServers(patch)
	if err != nil {
		return err
	}
	out := []mcpDSHServer{}
	found := false
	for _, s := range servers {
		if s.Name == name {
			found = true
			continue
		}
		out = append(out, s)
	}
	if found {
		if err := writeProfileMCPServers(patch, out); err != nil {
			return err
		}
	}
	a.removeLegacyMCPEntry(name)
	if !found {
		// 只在 profile 里也没有、且本地也没有时报错
		for _, e := range getMCPManager().load() {
			if e.Name == name {
				return nil
			}
		}
		return fmt.Errorf("no configured MCP server named %q", name)
	}
	return nil
}

// SetMCPServerEnabled 启用/禁用 MCP 服务器（写 `disabled:`，DSH HMR 即时生效）。
func (a *App) SetMCPServerEnabled(name string, enabled bool) error {
	_, patch, _, _ := mcpProfilePaths()
	servers, err := readProfileMCPServers(patch)
	if err != nil {
		return err
	}
	for i := range servers {
		if servers[i].Name == name {
			servers[i].Disabled = !enabled
			if err := writeProfileMCPServers(patch, servers); err != nil {
				return err
			}
			resumeLog("mcp: %s enabled=%v（HMR 热生效）", name, enabled)
			return nil
		}
	}
	// 旧本地配置：迁移进 profile 后再启用（这样"启用"才真的有意义）
	for _, e := range getMCPManager().load() {
		if e.Name == name {
			migrated := mcpDSHServer{Name: e.Name, Transport: e.Transport, Command: e.Command,
				Args: e.Args, URL: e.URL, Env: e.Env, Headers: e.Headers, Disabled: !enabled}
			if err := a.upsertMCPProfileServerKeepState(migrated); err != nil {
				return err
			}
			a.removeLegacyMCPEntry(name)
			return nil
		}
	}
	return fmt.Errorf("no configured MCP server named %q", name)
}

// upsertMCPProfileServerKeepState 写入 profile 实例并显式保留 Disabled 状态（迁移用）。
func (a *App) upsertMCPProfileServerKeepState(entry mcpDSHServer) error {
	_, patch, _, _ := mcpProfilePaths()
	servers, err := readProfileMCPServers(patch)
	if err != nil {
		return err
	}
	replaced := false
	for i := range servers {
		if servers[i].Name == entry.Name {
			servers[i] = entry
			replaced = true
			break
		}
	}
	if !replaced {
		servers = append(servers, entry)
	}
	return writeProfileMCPServers(patch, servers)
}

// ReconnectMCPServer 触发重连：重写该块（mtime 变化 → DSH HMR 断线重连）。
func (a *App) ReconnectMCPServer(name string) error {
	_, patch, _, _ := mcpProfilePaths()
	servers, err := readProfileMCPServers(patch)
	if err != nil {
		return err
	}
	for i := range servers {
		if servers[i].Name == name {
			if err := writeProfileMCPServers(patch, servers); err != nil {
				return err
			}
			// 强制 mtime 变化（内容相同则不落盘，这里显式 touch）
			now := time.Now()
			_ = os.Chtimes(patch, now, now)
			resumeLog("mcp: %s 已请求重连（touch profile 配置触发 HMR）", name)
			return nil
		}
	}
	return fmt.Errorf("no configured MCP server named %q", name)
}

// InstallMCPServer 添加并返回安装结果（前端用它给出"已就绪/需要操作"的反馈）。
//
// 说明：MCP 服务器是外部进程（npx/python/…）或 HTTP 端点，**不需要 DSH 侧安装**；
// 这里做的是"写入 profile 配置 + 报告状态"，不谎报已连接的工具数。
func (a *App) InstallMCPServer(in map[string]any) map[string]any {
	entry := mcpServerFromInput(in)
	if entry.Name == "" {
		return map[string]any{"name": "", "state": "issue", "toolCount": 0,
			"action": "retry", "message": "缺少服务器名称"}
	}
	if err := a.upsertMCPProfileServer(entry); err != nil {
		return map[string]any{"name": entry.Name, "state": "issue", "toolCount": 0,
			"action": "retry", "message": err.Error()}
	}
	a.removeLegacyMCPEntry(entry.Name)
	msg := "已写入 DSH profile 配置；启用后 DSH 会按需连接，模型可用的工具名为 mcp__" + entry.Name + "__<工具>"
	return map[string]any{"name": entry.Name, "state": "ready", "toolCount": 0,
		"action": "none", "message": msg}
}

// AuthenticateMCPServer DSH 的 mcp-client 不支持 OAuth（实测其实现里没有授权流程），
// 因此这里如实返回"不支持"，而不是假装成功。
func (a *App) AuthenticateMCPServer(name string) error {
	return fmt.Errorf("DSH 的 MCP 客户端不支持交互式授权；如需鉴权请在服务器配置的 env/headers 里填令牌")
}

// ClearMCPServerAuthentication 同上：没有可清除的授权态。
func (a *App) ClearMCPServerAuthentication(name string) error {
	return nil
}

// MCPCapabilityMatrix 返回 MCP 能力矩阵（当前实现下：列出已配置服务器的连接方式与工具前缀）。
func (a *App) MCPCapabilityMatrix() map[string]any {
	_, patch, _, _ := mcpProfilePaths()
	servers, _ := readProfileMCPServers(patch)
	rows := []any{}
	enabled := 0
	for _, s := range servers {
		if !s.Disabled {
			enabled++
		}
		rows = append(rows, map[string]any{
			"name": s.Name, "transport": s.Transport, "enabled": !s.Disabled,
			"toolPrefix": "mcp__" + s.Name + "__",
		})
	}
	return map[string]any{
		"servers": rows, "configured": len(servers), "enabled": enabled,
		"toolPrefixPattern": "mcp__<serverName>__<tool>",
		"note": "DSH 未暴露 MCP 状态 RPC：启用中的服务器按需连接，工具在连接后注册",
	}
}

// AnswerMCPInteractionForTab MCP 交互（elicitation）应答。
// DSH 的 mcp-client 未实现 elicitation 交互通道，这里如实降级：不排队、不伪造成功。
func (a *App) AnswerMCPInteractionForTab(tabID string, interactionID string, answers map[string]any) error {
	return fmt.Errorf("当前 DSH MCP 客户端不支持交互式应答")
}

// mcpYAMLQuoteForTest 暴露给测试的引号函数（测试与实现保持一致）。
var mcpYAMLQuoteForTest = yamlQuote
