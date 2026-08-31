package main

// app_compat_test.go — 插件兼容性标志测试。

import "testing"

func TestCompatLevel(t *testing.T) {
	cases := []struct {
		p    marketPlugin
		want string
	}{
		// 后端工具类 → native
		{marketPlugin{Name: "web-search", Category: "tools", En: "Search the web"}, "native"},
		{marketPlugin{Name: "dsh-skills", Category: "skill"}, "native"},
		{marketPlugin{Name: "workflow-orchestrator", Category: "workflow"}, "native"},
		{marketPlugin{Name: "model-provider", Category: "model"}, "native"},
		// 主题/皮肤/UI 类 → partial
		{marketPlugin{Name: "dsh-deep-whale", Category: "theme"}, "partial"},
		{marketPlugin{Name: "dsh-liang-skin", Category: "theme"}, "partial"},
		{marketPlugin{Name: "catppuccin-theme", Category: "theme"}, "partial"},
		{marketPlugin{Name: "sidebar-enhancer", Category: "ui"}, "partial"},
		{marketPlugin{Name: "theme-manager", Category: "dev", En: "manage themes and skins"}, "partial"},
		// 明确面向 DSH Web UI 结构 → incompatible
		{marketPlugin{Name: "dsh-client-ui-conversation", Category: "ui"}, "incompatible"},
		{marketPlugin{Name: "dsh-client-ui-skin-denia", Category: "theme"}, "incompatible"},
		{marketPlugin{Name: "webui-tweaks", Category: "ui"}, "incompatible"},
		{marketPlugin{Name: "dsh-mobile", Category: "ui"}, "incompatible"},
		// client-ui 但 tools 类(工具)不算 UI 结构
		{marketPlugin{Name: "client-ui-helper-tool", Category: "tools"}, "native"},
	}
	for _, c := range cases {
		if got := compatLevel(c.p); got != c.want {
			t.Errorf("compatLevel(%q/%s) = %q, want %q", c.p.Name, c.p.Category, got, c.want)
		}
	}
}

func TestCompatNote(t *testing.T) {
	for level, want := range map[string]string{
		"native":       "桌面端可正常使用",
		"partial":      "后端可安装",
		"incompatible": "面向 DSH Web 界面",
	} {
		note := compatNote(level)
		if len(note) == 0 {
			t.Fatalf("compatNote(%q) 为空", level)
		}
		// 说明应包含对应关键词
		if !containsAny(note, want) {
			t.Errorf("compatNote(%q) = %q, 应含 %q", level, note, want)
		}
	}
}

// 集成：市场返回的每个插件都带 compat 字段。
func TestPluginMarketCompatField(t *testing.T) {
	a := newTestApp()
	v := a.PluginMarket("", "")
	items, ok := v["items"].([]any)
	if !ok || len(items) == 0 {
		t.Fatalf("items 为空: %v", v)
	}
	seen := map[string]bool{}
	for _, it := range items {
		m, _ := it.(map[string]any)
		compat, _ := m["compat"].(string)
		if compat != "native" && compat != "partial" && compat != "incompatible" {
			t.Fatalf("compat 字段非法: %q", compat)
		}
		if note, _ := m["compatNote"].(string); note == "" {
			t.Fatalf("compatNote 为空: %v", m["name"])
		}
		seen[compat] = true
	}
	// 三种级别都应出现（124 个 theme + 大量 ui 插件）
	if !seen["partial"] || !seen["native"] {
		t.Fatalf("应同时出现 partial 和 native: %v", seen)
	}
}
