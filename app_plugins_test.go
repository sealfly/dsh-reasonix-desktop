// app_plugins_test.go — 插件桥方法结构测试（防返回空导致前端插件页崩溃）。

package main

import (
	"os"
	"strings"
	"testing"
)

// 测试强制离线：PluginMarket 不触发 imsai 网络请求（走 embed 回退，稳定可复现）。
func init() {
	_ = os.Setenv("DSH_MARKET_OFFLINE", "1")
}

func TestPluginsReturnsArray(t *testing.T) {
	a := newTestApp()
	v := a.Plugins()
	if v == nil {
		t.Fatalf("Plugins() 返回 nil")
	}
}

func TestPluginMarketSearch(t *testing.T) {
	a := newTestApp()
	v := a.PluginMarket("sidebar", "")
	if v == nil {
		t.Fatalf("PluginMarket 返回 nil")
	}
	items, ok := v["items"].([]any)
	if !ok || items == nil {
		t.Fatalf("PluginMarket items 缺失: %v", v)
	}
	if len(items) == 0 {
		t.Fatalf("PluginMarket 搜索 sidebar 应命中插件")
	}
	if v["categories"] == nil {
		t.Fatalf("PluginMarket categories 缺失")
	}
	if v["total"].(int) < 100 {
		t.Fatalf("PluginMarket total 应 >= 100 (实际 %v)", v["total"])
	}
}

func TestPluginMarketCategoryFilter(t *testing.T) {
	a := newTestApp()
	v := a.PluginMarket("", "theme")
	items := v["items"].([]any)
	for _, it := range items {
		m := it.(map[string]any)
		if m["category"] != "theme" {
			t.Fatalf("分类过滤失效: %v", m["category"])
		}
	}
	if len(items) == 0 {
		t.Fatalf("theme 分类应有插件")
	}
}

func TestInstallPluginRoundTrip(t *testing.T) {
	a := newTestApp()
	plan, err := a.InstallPlugin("DSH-Right-Sidebar", map[string]any{})
	if err != nil {
		t.Fatalf("InstallPlugin 错误: %v", err)
	}
	if !strings.Contains(plan, "DSH-Right-Sidebar") {
		t.Fatalf("InstallPlugin 返回计划缺名字: %s", plan)
	}
	// 已安装列表应包含它
	found := false
	for _, p := range a.Plugins() {
		m := p.(map[string]any)
		if m["name"] == "DSH-Right-Sidebar" {
			found = true
		}
	}
	if !found {
		t.Fatalf("安装后 Plugins() 应包含该插件")
	}
	// 卸载
	if err := a.RemovePlugin("DSH-Right-Sidebar"); err != nil {
		t.Fatalf("RemovePlugin 错误: %v", err)
	}
	for _, p := range a.Plugins() {
		m := p.(map[string]any)
		if m["name"] == "DSH-Right-Sidebar" {
			t.Fatalf("卸载后仍存在")
		}
	}
}

func TestSetPluginEnabled(t *testing.T) {
	a := newTestApp()
	_, _ = a.InstallPlugin("dsh-explorer", map[string]any{})
	if err := a.SetPluginEnabled("dsh-explorer", false); err != nil {
		t.Fatalf("SetPluginEnabled 错误: %v", err)
	}
	for _, p := range a.Plugins() {
		m := p.(map[string]any)
		if m["name"] == "dsh-explorer" && m["enabled"] != false {
			t.Fatalf("SetPluginEnabled 未生效")
		}
	}
	_ = a.RemovePlugin("dsh-explorer")
}

func TestPluginDoctor(t *testing.T) {
	a := newTestApp()
	v := a.PluginDoctor("nonexistent-plugin-xyz")
	if v["error"] == nil || v["error"] == "" {
		t.Fatalf("PluginDoctor 对未安装插件应返回 error 字段")
	}
}

// ===== dsh-std inventory 解析 =====

func TestPluginNameFromItem(t *testing.T) {
	if got := pluginNameFromItem(map[string]any{"name": "mcp-server"}); got != "mcp-server" {
		t.Fatalf("top-level name -> %q", got)
	}
	if got := pluginNameFromItem(map[string]any{"manifest": map[string]any{"name": "tool-plugin"}}); got != "tool-plugin" {
		t.Fatalf("manifest name -> %q", got)
	}
	if got := pluginNameFromItem(map[string]any{"description": "x"}); got != "" {
		t.Fatalf("no name -> %q, want empty", got)
	}
	if got := pluginNameFromItem(map[string]any{"manifest": "not-a-map"}); got != "" {
		t.Fatalf("malformed manifest -> %q", got)
	}
}

func TestDSHPluginToView(t *testing.T) {
	it := map[string]any{
		"name":        "dsh-tools",
		"description": "DSH 工具插件",
		"version":     "1.2.0",
		"enabled":     false,
		"manifest": map[string]any{
			"apiVersion":  "tool.dsh/v1",
			"kind":        "Tool",
			"description": "manifest desc",
			"version":     "9.9.9",
		},
	}
	v := dshPluginToView(it, "dsh-tools")
	if v["name"] != "dsh-tools" || v["manifestKind"] != "dsh-std" {
		t.Fatalf("name/manifestKind wrong: %v %v", v["name"], v["manifestKind"])
	}
	if v["enabled"] != false {
		t.Fatalf("enabled = %v, want false", v["enabled"])
	}
	if v["apiVersion"] != "tool.dsh/v1" || v["kind"] != "Tool" {
		t.Fatalf("apiVersion/kind = %v/%v", v["apiVersion"], v["kind"])
	}
	if v["description"] != "DSH 工具插件" {
		t.Fatalf("description = %v", v["description"])
	}
	if v["version"] != "1.2.0" {
		t.Fatalf("version = %v", v["version"])
	}
}

func TestDSHPluginToViewMissingManifest(t *testing.T) {
	v := dshPluginToView(map[string]any{"name": "bare"}, "bare")
	if v["manifestKind"] != "dsh-std" {
		t.Fatalf("manifestKind = %v", v["manifestKind"])
	}
	if len(v["warnings"].([]any)) == 0 {
		t.Fatalf("expected missing-manifest warning")
	}
}

// ===== imsai 动态源（app_market_api.go）=====

func TestRiskLevel(t *testing.T) {
	cases := []struct {
		text string
		want string
	}{
		{"deletes files with rm -rf", "high"},
		{"root access and credential storage", "high"},
		{"fetches remote data over http", "medium"},
		{"network upload via ssh tunnel", "medium"},
		{"shows a sidebar panel for todos", "low"},
		{"", "low"},
	}
	for _, c := range cases {
		if got := riskLevel(c.text); got != c.want {
			t.Fatalf("riskLevel(%q) = %q, want %q", c.text, got, c.want)
		}
	}
}

func TestImsaiToItem(t *testing.T) {
	it := imsaiToItem(imsaiPlugin{
		ID: "owner/repo", Name: "demo", Owner: "owner",
		URL: "https://github.com/owner/repo", Category: "ui",
		Description: "a sidebar panel", Install: "dsh plugin --profile web add demo", Stars: 5,
	})
	if it["name"] != "demo" || it["owner"] != "owner" {
		t.Fatalf("name/owner wrong: %v %v", it["name"], it["owner"])
	}
	if it["risk"] != "low" {
		t.Fatalf("risk = %v, want low", it["risk"])
	}
	if it["install"] != "dsh plugin --profile web add demo" {
		t.Fatalf("install 未透传")
	}
	// name 缺失时回退 ID
	it2 := imsaiToItem(imsaiPlugin{ID: "owner/repo/sub"})
	if it2["name"] != "owner/repo/sub" {
		t.Fatalf("name 回退 ID 失败: %v", it2["name"])
	}
}

func TestPluginMarketOfflineHasSource(t *testing.T) {
	a := newTestApp()
	v := a.PluginMarket("", "")
	if v["source"] != "embed" {
		t.Fatalf("离线模式 source = %v, want embed", v["source"])
	}
	items := v["items"].([]any)
	if len(items) == 0 {
		t.Fatalf("离线市场为空")
	}
	if m := items[0].(map[string]any); m["risk"] == nil {
		t.Fatalf("离线条目缺 risk 字段")
	}
}

// TestPluginMarketInstallFlow 模拟前端市场 UI 完整闭环：
// 浏览市场 → 选一个 install 字段完整的插件 → 安装 → 已安装列表出现 → 卸载。
func TestPluginMarketInstallFlow(t *testing.T) {
	a := newTestApp()
	// 1. 浏览市场（前端"浏览市场"按钮 → PluginMarket("","")）
	market := a.PluginMarket("", "")
	if market == nil {
		t.Fatalf("PluginMarket 返回 nil")
	}
	items := market["items"].([]any)
	if len(items) == 0 {
		t.Fatalf("市场为空")
	}
	// 2. 挑一个 install 字段完整的（前端注入代码用 it.install || it.url || it.name）
	var picked map[string]any
	for _, it := range items {
		m := it.(map[string]any)
		if src, ok := m["install"].(string); ok && src != "" {
			picked = m
			break
		}
	}
	if picked == nil {
		t.Fatalf("市场条目均无 install 字段")
	}
	name, _ := picked["name"].(string)
	src, _ := picked["install"].(string)
	t.Logf("选中: %s  src=%s", name, src)
	// 3. 安装（前端"安装"按钮 → InstallPlugin(src, {name}))
	plan, err := a.InstallPlugin(src, map[string]any{"name": name})
	if err != nil {
		t.Fatalf("InstallPlugin 失败: %v", err)
	}
	if !strings.Contains(plan, name) {
		t.Fatalf("安装计划不含插件名: %s", plan)
	}
	// 4. 已安装列表出现（前端 reload → Plugins()）
	found := false
	for _, p := range a.Plugins() {
		m := p.(map[string]any)
		if m["name"] == name {
			found = true
			if m["enabled"] != true {
				t.Fatalf("安装后应默认启用")
			}
		}
	}
	if !found {
		t.Fatalf("安装后 Plugins() 不含 %s", name)
	}
	// 5. 卸载（前端"卸载"）
	if err := a.RemovePlugin(name); err != nil {
		t.Fatalf("RemovePlugin 失败: %v", err)
	}
	for _, p := range a.Plugins() {
		if m := p.(map[string]any); m["name"] == name {
			t.Fatalf("卸载后仍存在: %s", name)
		}
	}
}

// TestPluginMarketItemsComplete 市场条目关键字段完整性（前端安装依赖 install/url/name）。
func TestPluginMarketItemsComplete(t *testing.T) {
	a := newTestApp()
	market := a.PluginMarket("", "")
	items := market["items"].([]any)
	if len(items) == 0 {
		t.Fatalf("市场为空")
	}
	noName, noInstall := 0, 0
	for _, it := range items {
		m := it.(map[string]any)
		if n, _ := m["name"].(string); n == "" {
			noName++
		}
		if s, _ := m["install"].(string); s == "" {
			noInstall++
		}
	}
	total := len(items)
	if noName > 0 {
		t.Fatalf("%d/%d 条目缺 name", noName, total)
	}
	// install 缺失的条目必须能靠 url/name 兜底
	for _, it := range items {
		m := it.(map[string]any)
		if s, _ := m["install"].(string); s == "" {
			u, _ := m["url"].(string)
			n, _ := m["name"].(string)
			if u == "" && n == "" {
				t.Fatalf("条目既无 install 也无 url/name 兜底: %v", m)
			}
		}
	}
	t.Logf("总数=%d install缺失=%d（可兜底）", total, noInstall)
}
