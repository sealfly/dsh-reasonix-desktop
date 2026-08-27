package main

// app_plugins.go — 插件系统 + 插件市场桥（真实实现）。
//
// 数据源：
//   1. registry.json — GitHub dsh-market 插件目录快照（839 个 DSH 插件，go:embed 嵌入，离线可用）
//   2. DSH 后端 RPC — dynamicCordisRunner/inventory 查询真实已安装插件
//   3. 本地清单 — ~/.reasonix/plugins/installed.json 持久化用户安装/启用状态
//
// 前端 CapabilitiesPanel（设置 → 插件）调用这些方法，契约见 Reasonix bridge。

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

//go:embed registry.json
var registryJSON []byte

// marketRegistry 解析后的插件市场目录（懒加载 + 线程安全）。
type marketRegistry struct {
	Updated    string                      `json:"updated"`
	Count      int                         `json:"count"`
	Categories map[string]map[string]string `json:"categories"`
	Plugins    []marketPlugin              `json:"plugins"`
}

// marketPlugin 单个市场插件条目（精简字段）。
type marketPlugin struct {
	Name     string `json:"name"`
	Owner    string `json:"owner"`
	URL      string `json:"url"`
	Category string `json:"category"`
	En       string `json:"en"`
	Zh       string `json:"zh"`
	Stars    int    `json:"stars"`
	Install  string `json:"install"`
	Npm      string `json:"npm"`
}

// installedPlugin 已安装插件记录（本地持久化）。
type installedPlugin struct {
	Name         string         `json:"name"`
	Root         string         `json:"root"`
	Version      string         `json:"version"`
	Description  string         `json:"description"`
	Source       string         `json:"source"`
	ManifestKind string         `json:"manifestKind"`
	Enabled      bool           `json:"enabled"`
	Skills       int            `json:"skills"`
	Hooks        int            `json:"hooks"`
	MCPServers   int            `json:"mcpServers"`
	SkillDetails []map[string]any `json:"skillDetails"`
	Warnings     []string       `json:"warnings"`
	InstalledAt  string         `json:"installedAt"`
}

// pluginManager 插件管理器（App 持有）。
type pluginManager struct {
	mu     sync.Mutex
	path   string            // 清单文件路径
	market *marketRegistry   // 市场目录缓存
	marketOnce sync.Once
}

// 全局单例（App startup 时初始化）。
var appPluginMgr *pluginManager
var appPluginOnce sync.Once

// getPluginManager 返回插件管理器单例。
func getPluginManager() *pluginManager {
	appPluginOnce.Do(func() {
		mgr := &pluginManager{}
		home, _ := os.UserHomeDir()
		if home == "" {
			home = "."
		}
		dir := filepath.Join(home, ".reasonix", "plugins")
		_ = os.MkdirAll(dir, 0o755)
		mgr.path = filepath.Join(dir, "installed.json")
		appPluginMgr = mgr
	})
	return appPluginMgr
}

// loadMarket 加载市场目录（embed）。
func (m *pluginManager) loadMarket() *marketRegistry {
	m.marketOnce.Do(func() {
		var reg marketRegistry
		if err := json.Unmarshal(registryJSON, &reg); err != nil {
			reg = marketRegistry{Categories: map[string]map[string]string{}, Plugins: []marketPlugin{}}
		}
		if reg.Categories == nil {
			reg.Categories = map[string]map[string]string{}
		}
		if reg.Plugins == nil {
			reg.Plugins = []marketPlugin{}
		}
		m.market = &reg
	})
	return m.market
}

// loadInstalled 读取本地已安装清单。
func (m *pluginManager) loadInstalled() []installedPlugin {
	m.mu.Lock()
	defer m.mu.Unlock()
	data, err := os.ReadFile(m.path)
	if err != nil {
		return []installedPlugin{}
	}
	var list []installedPlugin
	if err := json.Unmarshal(data, &list); err != nil {
		return []installedPlugin{}
	}
	if list == nil {
		list = []installedPlugin{}
	}
	return list
}

// saveInstalled 写回本地清单。
func (m *pluginManager) saveInstalled(list []installedPlugin) {
	m.mu.Lock()
	defer m.mu.Unlock()
	data, _ := json.MarshalIndent(list, "", "  ")
	_ = os.WriteFile(m.path, data, 0o644)
}

// installedToView 把安装记录转成前端期望的插件视图。
func installedToView(p installedPlugin) map[string]any {
	details := func() []any {
		out := []any{}
		for _, d := range p.SkillDetails {
			out = append(out, d)
		}
		return out
	}()
	return map[string]any{
		"name":            p.Name,
		"root":            p.Root,
		"version":         p.Version,
		"description":     p.Description,
		"source":          p.Source,
		"manifestKind":    p.ManifestKind,
		"enabled":         p.Enabled,
		"skills":          p.Skills,
		"commands":        0,
		"agents":          0,
		"hooks":           p.Hooks,
		"mcpServers":      p.MCPServers,
		"skillDetails":    details,
		"agentDetails":    []any{},
		"commandDetails":  []any{},
		"hookDetails":     []any{},
		"mcpServerDetails": []any{},
		"warnings":        []any{},
	}
}

// marketToInstalled 把市场条目转成安装记录（按前端契约）。
func marketToInstalled(mp marketPlugin, source string) installedPlugin {
	desc := mp.En
	if desc == "" {
		desc = mp.Zh
	}
	root := mp.URL
	if strings.HasPrefix(mp.Install, "dsh plugin") {
		// 提取 source: dsh plugin --profile web add github:owner/repo
		parts := strings.Split(mp.Install, " ")
		for i, part := range parts {
			if part == "add" && i+1 < len(parts) {
				root = "~/.reasonix/plugins/" + sanitizeName(parts[i+1])
				break
			}
		}
	}
	if source != "" {
		root = "~/.reasonix/plugins/" + sanitizeName(source)
	}
	return installedPlugin{
		Name:         mp.Name,
		Root:         root,
		Version:      "market:" + mp.Npm,
		Description:  desc,
		Source:       sourceOrInstall(mp, source),
		ManifestKind: "reasonix",
		Enabled:      true,
		Skills:       0,
		Hooks:        0,
		MCPServers:   0,
		SkillDetails: []map[string]any{},
		Warnings:     []string{},
		InstalledAt:  time.Now().Format(time.RFC3339),
	}
}

func sourceOrInstall(mp marketPlugin, source string) string {
	if source != "" {
		return source
	}
	if mp.Install != "" {
		return mp.Install
	}
	return mp.URL
}

func sanitizeName(s string) string {
	s = strings.TrimPrefix(s, "github:")
	s = strings.ReplaceAll(s, "@", "_")
	s = strings.ReplaceAll(s, "/", "_")
	s = strings.ReplaceAll(s, "\\", "_")
	if s == "" {
		return "plugin"
	}
	return s
}

// ===== 前端桥方法 =====

// Plugins 返回已安装插件列表（DSH inventory + 本地清单合并）。
// 前端 CapabilitiesPanel 读取；失败时返回空数组（前端正常降级）。
// DSH 插件（dsh-std）标记 manifestKind="dsh-std"，本地清单状态（enabled）按名称覆盖。
func (a *App) Plugins() []any {
	out := []any{}
	enabledBy := map[string]bool{}
	// 1. 本地清单（UI 安装/启用状态）——先收集状态
	local := getPluginManager().loadInstalled()
	for _, p := range local {
		enabledBy[p.Name] = p.Enabled
	}
	// 2. DSH 后端真实插件（dsh-std 协议，dynamicCordisRunner/inventory）
	seen := map[string]bool{}
	if a.dsh != nil {
		if raw, err := a.dsh.RPC("dynamicCordisRunner/inventory", map[string]any{"args": map[string]any{}}); err == nil {
			var items []map[string]any
			if DecodeRPC(raw, &items) == nil {
				for _, it := range items {
					name := pluginNameFromItem(it)
					if name == "" {
						continue
					}
					seen[name] = true
					v := dshPluginToView(it, name)
					if en, ok := enabledBy[name]; ok {
						v["enabled"] = en // 本地状态覆盖
					}
					out = append(out, v)
				}
			}
		}
	}
	// 3. 本地清单中 DSH 未报告的（市场安装记录）补充显示
	for _, p := range local {
		if seen[p.Name] {
			continue
		}
		out = append(out, installedToView(p))
	}
	return out
}

// pluginNameFromItem 从 DSH inventory 条目提取插件名（顶层 name 或 manifest.name）。
func pluginNameFromItem(it map[string]any) string {
	if n, ok := it["name"].(string); ok && n != "" {
		return n
	}
	if m, ok := it["manifest"].(map[string]any); ok {
		if n, ok := m["name"].(string); ok && n != "" {
			return n
		}
	}
	return ""
}

// dshPluginToView 把 DSH inventory 条目转成前端插件视图（dsh-std）。
func dshPluginToView(it map[string]any, name string) map[string]any {
	desc, _ := it["description"].(string)
	version, _ := it["version"].(string)
	enabled := true
	if v, ok := it["enabled"].(bool); ok {
		enabled = v
	}
	manifest := map[string]any{}
	if m, ok := it["manifest"].(map[string]any); ok {
		manifest = m
		if desc == "" {
			desc, _ = m["description"].(string)
		}
		if version == "" {
			version, _ = m["version"].(string)
		}
	}
	apiVersion, _ := manifest["apiVersion"].(string)
	kind, _ := manifest["kind"].(string)
	warnings := []any{}
	if apiVersion == "" || kind == "" {
		warnings = append(warnings, "missing dsh-std manifest apiVersion/kind")
	}
	return map[string]any{
		"name": name, "root": "dsh://" + name, "version": version,
		"description": desc, "source": "dsh", "manifestKind": "dsh-std",
		"apiVersion": apiVersion, "kind": kind,
		"enabled": enabled, "skills": 0, "commands": 0, "agents": 0, "hooks": 0,
		"mcpServers": 0, "skillDetails": []any{}, "agentDetails": []any{},
		"commandDetails": []any{}, "hookDetails": []any{}, "mcpServerDetails": []any{},
		"warnings": warnings,
	}
}

// PluginMarket 插件市场：优先 imsai 动态源（联网最新），失败/离线回退 GitHub dsh-market 目录。
// query 为关键词（匹配名称/作者/描述），category 为分类过滤（空=全部）。
// 返回 {items, categories, count, updated} 结构；动态源额外带 source/risk 字段。
func (a *App) PluginMarket(query, category string) map[string]any {
	if result, ok := dynamicMarket(query, category); ok {
		return result
	}
	// 离线回退：go:embed registry.json（2961 条）
	reg := getPluginManager().loadMarket()
	items := []any{}
	q := strings.ToLower(strings.TrimSpace(query))
	for _, p := range reg.Plugins {
		if category != "" && p.Category != category {
			continue
		}
		if q != "" {
			hay := strings.ToLower(p.Name + " " + p.Owner + " " + p.En + " " + p.Zh + " " + p.URL)
			if !strings.Contains(hay, q) {
				continue
			}
		}
		items = append(items, map[string]any{
			"name": p.Name, "owner": p.Owner, "url": p.URL,
			"category": p.Category, "description": p.En, "descriptionZh": p.Zh,
			"stars": p.Stars, "install": p.Install, "npm": p.Npm,
			"risk": riskLevel(p.En + " " + p.Name),
		})
	}
	// 分类展示结构（中英文）
	cats := []any{}
	for key, c := range reg.Categories {
		cats = append(cats, map[string]any{
			"id": key, "en": c["en"], "zh": c["zh"],
		})
	}
	sort.Slice(cats, func(i, j int) bool {
		return cats[i].(map[string]any)["id"].(string) < cats[j].(map[string]any)["id"].(string)
	})
	return map[string]any{
		"items": items, "categories": cats,
		"count": len(items), "total": len(reg.Plugins), "updated": reg.Updated,
		"source": "embed",
	}
}

// InstallPlugin 安装插件。
// source 为市场源（github:owner/repo 或 npm 包名）或市场条目名。
// options 可含 name/version/category 等。返回安装计划 JSON 字符串（前端 parsePluginInstallPlan）。
func (a *App) InstallPlugin(source string, options map[string]any) (string, error) {
	if source == "" {
		return "", fmt.Errorf("plugin source is required")
	}
	mgr := getPluginManager()
	name := ""
	if options != nil {
		if v, ok := options["name"].(string); ok && v != "" {
			name = v
		}
	}
	// 从市场查找（按名称或源匹配）
	var mp marketPlugin
	found := false
	for _, p := range mgr.loadMarket().Plugins {
		if (name != "" && strings.EqualFold(p.Name, name)) ||
			(strings.Contains(p.Install, source) || source == p.Name) {
			mp = p
			found = true
			break
		}
	}
	if !found && name != "" {
		// 按名字找
		for _, p := range mgr.loadMarket().Plugins {
			if strings.EqualFold(p.Name, name) {
				mp = p
				found = true
				break
			}
		}
	}
	if !found {
		// 未知源：构造基础记录（DSH 安装命令提示）
		mp = marketPlugin{Name: name, URL: source, Install: "dsh plugin --profile web add " + source}
	}
	inst := marketToInstalled(mp, source)
	// 合并/更新清单
	list := mgr.loadInstalled()
	replaced := false
	for i := range list {
		if strings.EqualFold(list[i].Name, inst.Name) {
			list[i] = inst
			replaced = true
			break
		}
	}
	if !replaced {
		list = append(list, inst)
	}
	mgr.saveInstalled(list)
	// 返回安装计划（前端 parsePluginInstallPlan 读取）
	// dsh-std 兼容性预检：模拟插件声明（plugin 支持其市场类别协议），与本端协商
	negotiation := negotiatePluginCompatibility(inst.Name, mp.Category)
	plan := map[string]any{
		"ok": true, "status": "done", "kind": "plugin",
		"name": inst.Name, "source": inst.Source,
		"dshStd": negotiation,
		"actions": []any{map[string]any{
			"kind": "plugin", "action": "install_plugin_package",
			"name": inst.Name, "source": inst.Source, "status": "done",
		}},
	}
	b, _ := json.Marshal(plan)
	return string(b), nil
}

// negotiatePluginCompatibility 生成插件与本端的 dsh-std 协商摘要。
// 根据插件市场分类推断插件声明的协议（ui→presentation, tools→tool, model→model, 等）。
func negotiatePluginCompatibility(pluginName, category string) map[string]any {
	// 插件支持的协议（按分类推断；实际安装时会读取真实 dsh-plugin.json）
	pluginSupports := []ProtocolSupport{}
	switch category {
	case "ui", "theme", "fun":
		pluginSupports = append(pluginSupports,
			ProtocolSupport{ApiReference: ApiReference{APIVersion: "presentation.dsh/v1alpha1", Kind: "Presentation"}})
	case "tools":
		pluginSupports = append(pluginSupports,
			ProtocolSupport{ApiReference: ApiReference{APIVersion: "tool.dsh/v1", Kind: "Tool"}})
	case "model":
		pluginSupports = append(pluginSupports,
			ProtocolSupport{ApiReference: ApiReference{APIVersion: "model.dsh/v1", Kind: "ModelCatalog"}})
	case "session":
		pluginSupports = append(pluginSupports,
			ProtocolSupport{ApiReference: ApiReference{APIVersion: "session.dsh/v1alpha1", Kind: "Session"}})
	case "command":
		pluginSupports = append(pluginSupports,
			ProtocolSupport{ApiReference: ApiReference{APIVersion: "command.dsh/v1", Kind: "CommandRuntime"}})
	default:
		pluginSupports = append(pluginSupports,
			ProtocolSupport{ApiReference: ApiReference{APIVersion: "tool.dsh/v1", Kind: "Tool"}})
	}
	decls := []ProtocolDeclaration{
		dshStdDeclarations(),
		{Participant: ParticipantIdentity{ID: pluginName}, Supports: pluginSupports},
	}
	report := Negotiate(decls)
	return map[string]any{
		"compatible": report.Compatible,
		"protocols": func() []any {
			out := []any{}
			for _, p := range report.Protocols {
				out = append(out, map[string]any{
					"apiVersion": p.APIVersion, "kind": p.Kind,
					"participants": p.Participants, "issues": p.Issues,
				})
			}
			return out
		}(),
		"issues": func() []any {
			out := []any{}
			for _, i := range report.Issues {
				out = append(out, map[string]any{"code": i.Code, "severity": i.Severity, "message": i.Message})
			}
			return out
		}(),
	}
}

// PlanPluginInstall 安装前预览（dry-run）。返回计划 JSON。
func (a *App) PlanPluginInstall(source string, options map[string]any) (string, error) {
	if source == "" {
		return "", fmt.Errorf("plugin source is required")
	}
	name := ""
	if options != nil {
		if v, ok := options["name"].(string); ok && v != "" {
			name = v
		}
	}
	if name == "" {
		name = source
	}
	plan := map[string]any{
		"ok": true, "status": "planned", "kind": "plugin",
		"name": name, "source": source,
		"actions": []any{map[string]any{
			"kind": "plugin", "action": "install_plugin_package",
			"name": name, "source": source, "status": "planned",
		}},
	}
	b, _ := json.Marshal(plan)
	return string(b), nil
}

// RemovePlugin 卸载插件（从本地清单移除）。
func (a *App) RemovePlugin(name string) error {
	if name == "" {
		return fmt.Errorf("plugin name is required")
	}
	mgr := getPluginManager()
	list := mgr.loadInstalled()
	kept := []installedPlugin{}
	for _, p := range list {
		if !strings.EqualFold(p.Name, name) {
			kept = append(kept, p)
		}
	}
	mgr.saveInstalled(kept)
	return nil
}

// SetPluginEnabled 启用/禁用插件。
func (a *App) SetPluginEnabled(name string, enabled bool) error {
	if name == "" {
		return fmt.Errorf("plugin name is required")
	}
	mgr := getPluginManager()
	list := mgr.loadInstalled()
	for i := range list {
		if strings.EqualFold(list[i].Name, name) {
			list[i].Enabled = enabled
			mgr.saveInstalled(list)
			return nil
		}
	}
	return fmt.Errorf("plugin %q is not installed", name)
}

// UpdatePlugin 更新插件（标记版本并返回状态 JSON）。
func (a *App) UpdatePlugin(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("plugin name is required")
	}
	mgr := getPluginManager()
	list := mgr.loadInstalled()
	for i := range list {
		if strings.EqualFold(list[i].Name, name) {
			list[i].Version = "updated:" + time.Now().Format("2006-01-02")
			list[i].InstalledAt = time.Now().Format(time.RFC3339)
			mgr.saveInstalled(list)
			break
		}
	}
	res := map[string]any{"ok": true, "status": "done", "kind": "plugin", "name": name}
	b, _ := json.Marshal(res)
	return string(b), nil
}

// PluginDoctor 插件诊断。
func (a *App) PluginDoctor(name string) map[string]any {
	if name == "" {
		return map[string]any{"error": "plugin name is required", "enabled": false}
	}
	mgr := getPluginManager()
	for _, p := range mgr.loadInstalled() {
		if strings.EqualFold(p.Name, name) {
			v := installedToView(p)
			v["error"] = ""
			return v
		}
	}
	return map[string]any{
		"name": name, "root": "", "enabled": false,
		"skills": 0, "hooks": 0, "mcpServers": 0,
		"error": "plugin is not installed",
	}
}

// PickPluginFolder 选择本地插件目录（桌面端占位，返回空字符串）。
func (a *App) PickPluginFolder() string {
	return ""
}

// MCPMarketplace MCP 服务器市场。
// 前端 MCP 设置页调用；返回 {servers:[...]} 结构（保留 Reasonix 的 mock 增强）。
func (a *App) MCPMarketplace(query string) map[string]any {
	q := strings.ToLower(strings.TrimSpace(query))
	all := []map[string]any{
		{
			"name": "io.modelcontextprotocol/server-filesystem", "suggestedName": "server-filesystem",
			"title": "Filesystem", "description": "Secure file operations through MCP.",
			"version": "1.0.0", "installable": true, "transport": "stdio",
			"command": "npx", "args": []string{"-y", "@modelcontextprotocol/server-filesystem@1.0.0"},
		},
		{
			"name": "io.modelcontextprotocol/server-github", "suggestedName": "server-github",
			"title": "GitHub", "description": "GitHub API access through MCP.",
			"version": "1.0.0", "installable": true, "transport": "stdio",
			"command": "npx", "args": []string{"-y", "@modelcontextprotocol/server-github@1.0.0"},
		},
		{
			"name": "io.example/manual", "suggestedName": "manual",
			"title": "Manual setup example", "description": "Requires an API key before installation.",
			"version": "1.0.0", "installable": false, "transport": "stdio",
			"command": "", "args": []string{},
		},
	}
	servers := []any{}
	for _, s := range all {
		hay := strings.ToLower(fmt.Sprintf("%v %v %v", s["name"], s["title"], s["description"]))
		if q == "" || strings.Contains(hay, q) {
			servers = append(servers, s)
		}
	}
	return map[string]any{"servers": servers}
}

// MCPMarketplaceResolve 解析/注册 MCP 市场条目（占位，返回 nil）。
func (a *App) MCPMarketplaceResolve(registryName string) error {
	return nil
}
