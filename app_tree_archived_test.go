package main

// app_tree_archived_test.go — 项目树归档过滤测试。
// 事故背景：DSH 归档只是记 id，session.list 仍返回该会话；项目树路径漏了过滤，
// 导致已清理的测试会话长期挂在侧栏（"001" 项目）。

import "testing"

func TestFilterArchivedSessions(t *testing.T) {
	items := []dshSession{{SessionID: "a"}, {SessionID: "b"}, {SessionID: "c"}}
	got := filterArchivedSessions(items, map[string]bool{"b": true})
	if len(got) != 2 || got[0].SessionID != "a" || got[1].SessionID != "c" {
		t.Fatalf("归档过滤不符: %+v", got)
	}
	if n := len(filterArchivedSessions(items, nil)); n != 3 {
		t.Fatalf("空归档集合应原样返回 3 条，实际 %d", n)
	}
	if n := len(filterArchivedSessions(items, map[string]bool{})); n != 3 {
		t.Fatalf("空 map 应原样返回 3 条，实际 %d", n)
	}
	if n := len(filterArchivedSessions(nil, map[string]bool{"a": true})); n != 0 {
		t.Fatalf("空输入应为空，实际 %d", n)
	}
	all := filterArchivedSessions(items, map[string]bool{"a": true, "b": true, "c": true})
	if len(all) != 0 {
		t.Fatalf("全部归档应为空，实际 %d", len(all))
	}
}
