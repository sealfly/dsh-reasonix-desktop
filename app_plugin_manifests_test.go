package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestPluginManifestsAdmit 回归保护：随包分发的配套 dsh-plugin.json
// （build/windows/installer/plugin-manifests/*/dsh-plugin.json）必须始终
// ①可解析（valid=true，无 error 级 issue）②可准入（compatible=true，允许 degraded）。
//
// 这些 manifest 是"声明层 dsh-std 化"的产物：插件本体是 cordis 形态，
// 由本项目为其补一份符合 Community v0.15 schema 的能力声明，
// 使准入器（PluginDshStdAdmit）能读到 manifest 做五态评估。
func TestPluginManifestsAdmit(t *testing.T) {
	root := filepath.Join("build", "windows", "installer", "plugin-manifests")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("plugin-manifests dir missing: %v", err)
	}
	seen := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		path := filepath.Join(root, name, "dsh-plugin.json")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s: read manifest: %v", name, err)
		}
		parsed := ParseDshPluginManifest(data)
		if v, _ := parsed["valid"].(bool); !v {
			t.Fatalf("%s: manifest invalid: %v", name, parsed["issues"])
		}
		// 声明的权限必须全部在 Host 权限注册表内（否则解析器会给出 unregistered-permission
		// warning）——网络能力用扩展开的 net.dsh.connect，不得出现未注册权限。
		for _, iss := range parsed["issues"].([]any) {
			m := iss.(map[string]any)
			if m["code"] == "unregistered-permission" {
				t.Fatalf("%s: %v", name, m["message"])
			}
		}
		state, compatible, issues := AdmitPlugin(parsed)
		if !compatible {
			t.Fatalf("%s: not compatible (state=%s issues=%v)", name, state, issues)
		}
		m := parsed["manifest"].(map[string]any)
		t.Logf("%-24s id=%-38s version=%-8s state=%s compatible=%v",
			name, m["id"], m["version"], state, compatible)
		seen++
	}
	if seen == 0 {
		t.Fatal("no plugin manifests found")
	}
	t.Logf("validated %d companion manifests", seen)
}

// TestPluginManifestsCoverSeedList 配套 manifest 必须覆盖 seed 默认附加清单里的插件：
// 清单每加一款插件，就必须补一份配套 manifest（原则 8：集成即随包 + dsh-std 合规）。
func TestPluginManifestsCoverSeedList(t *testing.T) {
	// 与 build/windows/installer/prepare-plugin-offline.ps1 的 $plugins 一一对应
	want := map[string]string{
		"@openviking/dsh-memory-plugin":          "dsh-memory-plugin",
		"@vectorize-io/hindsight-coding-agents":  "hindsight-coding-agents",
		"@memtensor/memos-local-plugin":          "memos-local-plugin",
		"@nanmicoder/dsh-agent-teams":            "dsh-agent-teams",
	}
	root := filepath.Join("build", "windows", "installer", "plugin-manifests")
	for pkg, dir := range want {
		path := filepath.Join(root, dir, "dsh-plugin.json")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s: companion manifest missing (%v)", pkg, err)
		}
		parsed := ParseDshPluginManifest(data)
		m, _ := parsed["manifest"].(map[string]any)
		if m == nil {
			t.Fatalf("%s: manifest unparsable", pkg)
		}
		if got, _ := m["name"].(string); got != pkg {
			t.Fatalf("%s: manifest name=%q does not match package", pkg, got)
		}
	}
}
