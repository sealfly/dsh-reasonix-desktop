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
