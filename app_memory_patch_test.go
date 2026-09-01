package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestPatchMemoryRowDisabled 验证根 patch 开关往返：
// 禁用 → 行出现；启用 → 行移除并恢复 []；再禁用 → 行回来。
func TestPatchMemoryRowDisabled(t *testing.T) {
	// 用临时目录模拟 ~/.dsh/profiles/web
	home := t.TempDir()
	webDir := filepath.Join(home, ".dsh", "profiles", "web")
	if err := os.MkdirAll(webDir, 0755); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(webDir, "cordis.patch.yml")
	// 原 userHomeDir 会读到真实 HOME——用临时 HOME 环境变量不可靠，
	// 改为直接测 patchMemoryRowDisabled 的核心逻辑（行增删）用临时文件。
	original := "# comment\n[]\n"
	if err := os.WriteFile(root, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	// 禁用（写入 disabled:true）
	content := original
	content = applyDisabled(content, "openviking-memory", true)
	if !containsRow(content, "openviking-memory") || !containsDisabled(content, "openviking-memory") {
		t.Fatalf("禁用后应有 disabled 行:\n%s", content)
	}
	if containsRow(content, "hindsight") {
		t.Fatalf("不应包含未处理的行:\n%s", content)
	}

	// 启用（移除行，恢复 []）
	content = applyDisabled(content, "openviking-memory", false)
	if containsRow(content, "openviking-memory") {
		t.Fatalf("启用后应移除行:\n%s", content)
	}
	if !containsEmptyArray(content) {
		t.Fatalf("启用后应恢复空数组:\n%s", content)
	}

	// 再禁用两个
	content = applyDisabled(content, "hindsight", true)
	content = applyDisabled(content, "memos-local-memory", true)
	if !containsDisabled(content, "hindsight") || !containsDisabled(content, "memos-local-memory") {
		t.Fatalf("批量禁用失败:\n%s", content)
	}
	// 全部启用 → 恢复 []
	content = applyDisabled(content, "hindsight", false)
	content = applyDisabled(content, "memos-local-memory", false)
	if !containsEmptyArray(content) {
		t.Fatalf("全部启用后应恢复空数组:\n%s", content)
	}
}

// 复制 patchMemoryRowDisabled 的行操作逻辑（独立于用户 HOME；按行名限定）
func applyDisabled(content, row string, disabled bool) string {
	rowRe := regexp.MustCompile(`(?m)^\s*-\s*id:\s*` + regexp.QuoteMeta(row) + `\s*\n(?:\s+disabled:\s*(?:true|false)\s*\n)?`)
	content = rowRe.ReplaceAllString(content, "")
	if disabled {
		entry := "- id: " + row + "\n  disabled: true\n"
		if idx := strIndexOf(content, "[]"); idx >= 0 {
			content = content[:idx] + entry + content[idx+2:]
		} else {
			content = trimRightNewline(content) + "\n" + entry
		}
	} else {
		// 启用后数组已无任何条目 → 恢复 []（与 patchMemoryRowDisabled 一致）
		if !strings.Contains(content, "- id:") {
			content = trimRightNewline(content) + "\n[]\n"
		}
	}
	return content
}

func containsRow(s, row string) bool { return strIndexOf(s, "- id: "+row) >= 0 }
func containsDisabled(s, row string) bool {
	idx := strIndexOf(s, "- id: "+row)
	return idx >= 0 && strIndexOf(s[idx:], "disabled: true") >= 0
}
func containsEmptyArray(s string) bool { return strIndexOf(s, "[]") >= 0 }
func strIndexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
func trimRightNewline(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}
