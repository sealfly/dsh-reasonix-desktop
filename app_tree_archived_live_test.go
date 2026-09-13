package main

// app_tree_archived_live_test.go — 对运行中的 DSH 做只读回归：
// 项目树/会话列表都不得出现已归档会话（事故：集成测试残留的 "001" 项目长期挂在侧栏）。
// 本测试只读，不创建会话，不产生任何残留。

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestProjectTreeExcludesArchivedSessions(t *testing.T) {
	a := newTestApp()
	a.dsh = NewDshClient(3080)
	if _, err := a.dsh.RPC("session.list", map[string]any{}); err != nil {
		t.Skipf("DSH 不可用，跳过: %v", err)
	}
	archived := a.fetchArchivedSessionIDs()
	if len(archived) == 0 {
		t.Skip("归档集合为空，跳过")
	}
	sessions := a.fetchSessions()
	for _, s := range sessions {
		if archived[s.SessionID] {
			t.Fatalf("fetchSessions 未过滤归档会话: %s (cwd=%s)", s.SessionID, s.Cwd)
		}
	}
	t.Logf("session.list 原始 %d 条；fetchSessions 过滤后 %d 条；归档集合 %d 个",
		len(sessions), len(sessions), len(archived))

	// 打印项目树实际内容，便于人工核对"侧栏到底挂了哪些项目"
	tree := a.buildProjectTree()
	names := make([]string, 0, len(tree.Projects))
	for _, p := range tree.Projects {
		m, ok := p.(map[string]any)
		if !ok {
			continue
		}
		label, _ := m["label"].(string)
		root, _ := m["root"].(string)
		kids, _ := m["children"].([]any)
		names = append(names, fmt.Sprintf("%s(%d 会话) <- %s", label, len(kids), root))
	}
	t.Logf("项目树 %d 个项目:\n    %s", len(names), strings.Join(names, "\n    "))

	// 项目树与 Tab 列表里不得再出现测试临时工作区
	for _, probe := range []struct {
		name string
		val  any
	}{
		{"buildProjectTree", a.buildProjectTree()},
		{"Tabs", a.Tabs()},
	} {
		b, err := json.Marshal(probe.val)
		if err != nil {
			t.Fatalf("%s 序列化失败: %v", probe.name, err)
		}
		js := string(b)
		for _, bad := range []string{"reasonix-session-tmp", "\\\\Temp\\\\Test", "TestSubmitViaHTTPIntegration", "TestSetGoalForTabIntegration"} {
			if strings.Contains(js, bad) {
				t.Fatalf("%s 仍含测试工作区残留（匹配 %q）", probe.name, bad)
			}
		}
	}
}
