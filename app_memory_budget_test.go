package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMemoryBudgetRoundTrip 验证 SetMemoryBudget 的底层逻辑：
// patchMemoryRowConfig 写 config 段 + memoryPatchRows 读回，
// 且不破坏 disabled 状态与其他行。
func TestMemoryBudgetRoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	webDir := filepath.Join(home, ".dsh", "profiles", "web")
	if err := os.MkdirAll(webDir, 0755); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(webDir, "cordis.patch.yml")
	initial := "# comment\n- id: openviking-memory\n  disabled: true\n- id: memos-local-memory\n  disabled: true\n"
	if err := os.WriteFile(root, []byte(initial), 0644); err != nil {
		t.Fatal(err)
	}

	// 写预算（openviking）
	if err := patchMemoryRowConfig("openviking-memory", map[string]any{"recallTokenBudget": 800, "profileTokenBudget": 5000}); err != nil {
		t.Fatalf("patchMemoryRowConfig: %v", err)
	}
	// 写预算（memos 按需检索）
	if err := patchMemoryRowConfig("memos-local-memory", map[string]any{"recallEnabled": false, "contextMaxChars": 3000}); err != nil {
		t.Fatalf("patchMemoryRowConfig memos: %v", err)
	}

	content, _ := os.ReadFile(root)
	s := string(content)
	// disabled 状态保留
	if !strContains(s, "- id: openviking-memory\n  disabled: true") {
		t.Fatalf("disabled 状态被破坏:\n%s", s)
	}
	// config 段写入
	if !strContains(s, "recallTokenBudget: 800") || !strContains(s, "profileTokenBudget: 5000") {
		t.Fatalf("openviking config 未写入:\n%s", s)
	}
	if !strContains(s, "recallEnabled: false") || !strContains(s, "contextMaxChars: 3000") {
		t.Fatalf("memos config 未写入:\n%s", s)
	}

	// 读回
	rows, err := memoryPatchRows()
	if err != nil {
		t.Fatalf("memoryPatchRows: %v", err)
	}
	got := map[string]map[string]any{}
	for _, r := range rows {
		got[r["id"].(string)] = r["config"].(map[string]any)
	}
	if got["openviking-memory"]["recallTokenBudget"] != 800 {
		t.Fatalf("readback recallTokenBudget = %v", got["openviking-memory"]["recallTokenBudget"])
	}
	if got["memos-local-memory"]["recallEnabled"] != false {
		t.Fatalf("readback recallEnabled = %v", got["memos-local-memory"]["recallEnabled"])
	}
	if got["memos-local-memory"]["contextMaxChars"] != 3000 {
		t.Fatalf("readback contextMaxChars = %v", got["memos-local-memory"]["contextMaxChars"])
	}

	// 二次写（覆盖预算）——config 段应被替换而非重复
	if err := patchMemoryRowConfig("openviking-memory", map[string]any{"recallTokenBudget": 500}); err != nil {
		t.Fatal(err)
	}
	content2, _ := os.ReadFile(root)
	s2 := string(content2)
	if strCount(s2, "recallTokenBudget:") != 1 {
		t.Fatalf("config 段重复（应有 1 处 recallTokenBudget）:\n%s", s2)
	}
	if !strContains(s2, "recallTokenBudget: 500") {
		t.Fatalf("预算未更新:\n%s", s2)
	}
}

func strContains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func strCount(s, sub string) int {
	n := 0
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			n++
			i += len(sub) - 1
		}
	}
	return n
}
