package main

// app_plugin_admit_test.go — 插件 dsh-std 准入测试。

import (
	"os"
	"path/filepath"
	"testing"
)

// PluginDshStdAdmit：不存在插件 → manifestFound=false, 不崩溃。
func TestPluginDshStdAdmitNotFound(t *testing.T) {
	a := &App{}
	v := a.PluginDshStdAdmit("definitely-not-installed-plugin-xyz")
	if v["ok"] != true {
		t.Fatalf("ok 应为 true: %v", v)
	}
	if v["manifestFound"] != false {
		t.Fatalf("manifestFound 应为 false: %v", v)
	}
	if v["state"] != "unknown" {
		t.Fatalf("state 应为 unknown: %v", v)
	}
}

// 空 name → ok=false。
func TestPluginDshStdAdmitEmptyName(t *testing.T) {
	a := &App{}
	if v := a.PluginDshStdAdmit(""); v["ok"] != false {
		t.Fatalf("空 name 应失败: %v", v)
	}
}

// sanitizePluginDir 归一化。
func TestSanitizePluginDir(t *testing.T) {
	cases := map[string]string{
		"@openviking/dsh-memory-plugin": "openviking/dsh-memory-plugin",
		"github:foo/bar":               "foo/bar",
		"plain-name":                   "plain-name",
	}
	for in, want := range cases {
		if got := sanitizePluginDir(in); got != want {
			t.Errorf("sanitizePluginDir(%q)=%q, want %q", in, got, want)
		}
	}
}

// 在临时 ~/.dsh 布局下验证 locate 能找到 manifest（隔离 HOME 影响）。
// dshProfileModulesDir 固定 ~/.dsh；此处直接验证 locateDshPluginManifest 对已存在目录的探测
// —— 用真实 ~/.dsh(本机开发环境)若装过插件会命中; 不强依赖, 只保证不 panic。
func TestLocateNoPanic(t *testing.T) {
	_ = locateDshPluginManifest("")
	_ = locateDshPluginManifest("@a/b")
	_ = dshProfileModulesDir()
}

// 用构造的 manifest 验证五态准入路径(临时文件 → 手动定位 → 解析)。
func TestAdmitSampleManifest(t *testing.T) {
	// 造一个合法 v0.15 清单临时文件（官方形态：无 supports/description，facets.host 必填，
	// 顶层 permissions/contributes/subscriptions 必填）
	dir := t.TempDir()
	mf := `{"$schema":"https://dsh-std.dev/schemas/dsh-plugin-0.15.schema.json","manifestVersion":"0.15","id":"com.example.test-plugin","name":"Test","version":"0.1.0","facets":{"host":{"entry":"index.js","apiVersion":"v1alpha1"}},"requires":{"contracts":[{"apiVersion":"core.dsh/v1alpha1","kind":"Negotiation"}]},"permissions":[],"contributes":{"commands":[]},"subscriptions":[]}`
	p := filepath.Join(dir, "dsh-plugin.json")
	if err := os.WriteFile(p, []byte(mf), 0o644); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	parsed := ParseDshPluginManifest(raw)
	if valid, _ := parsed["valid"].(bool); !valid {
		t.Fatalf("合法 v0.15 清单应 valid: %v", parsed["issues"])
	}
	man, _ := parsed["manifest"].(map[string]any)
	if man == nil || man["manifestVersion"] != "0.15" {
		t.Fatalf("manifestVersion 解析错: %v", man)
	}
	state, compatible, _ := AdmitPlugin(parsed)
	if !compatible {
		t.Fatalf("合法 v0.15 清单应 compatible, got state=%v", state)
	}
}

// AdmitInstalledPlugins 空/异常不崩溃。
func TestAdmitInstalledPluginsNoPanic(t *testing.T) {
	a := &App{}
	_ = a.AdmitInstalledPlugins()
}
