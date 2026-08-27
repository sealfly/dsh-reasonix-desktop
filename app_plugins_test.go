// app_plugins_test.go — 插件桥方法结构测试（防返回空导致前端插件页崩溃）。

package main

import (
	"strings"
	"testing"
)

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
