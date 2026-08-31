package main

// app_composer_test.go — Composer 执行方式方法测试。
// 单测覆盖 goal ref 缓存；集成测试走真实 DSH（临时会话）。

import "testing"

// goal ref 缓存读写清除。
func TestGoalRefCache(t *testing.T) {
	clearGoalRef("sess-g1")
	if _, ok := getGoalRef("sess-g1"); ok {
		t.Fatal("应无缓存")
	}
	setGoalRef("sess-g1", dshGoalRef{ID: "goal-1", Revision: 2})
	r, ok := getGoalRef("sess-g1")
	if !ok || r.ID != "goal-1" || r.Revision != 2 {
		t.Fatalf("缓存错误: %+v ok=%v", r, ok)
	}
	clearGoalRef("sess-g1")
	if _, ok := getGoalRef("sess-g1"); ok {
		t.Fatal("清除失败")
	}
}

// 空目标 → 本地清除不崩溃。
func TestSetGoalForTabEmpty(t *testing.T) {
	a := newTestApp()
	a.dsh = NewDshClient(3080)
	if err := a.SetGoalForTab("tab-none", ""); err != nil {
		t.Fatalf("空目标清除不应报错: %v", err)
	}
}

// 协作/审批模式不崩溃。
func TestComposerModesNoCrash(t *testing.T) {
	a := newTestApp()
	if err := a.SetCollaborationModeForTab("tab-1", "goal"); err != nil {
		t.Fatalf("collab mode: %v", err)
	}
	if err := a.SetToolApprovalModeForTab("tab-1", "yolo"); err != nil {
		t.Fatalf("approval mode: %v", err)
	}
	if a.st.DefaultToolApprovalMode() != "yolo" {
		t.Fatalf("approval mode 未持久化: %q", a.st.DefaultToolApprovalMode())
	}
	if err := a.SetToolApprovalModeForTab("tab-1", "bogus"); err != nil {
		t.Fatalf("非法模式不应报错: %v", err)
	}
}

// 集成：真实 DSH 设置目标 → goal.create 生效。
func TestSetGoalForTabIntegration(t *testing.T) {
	a := newTestApp()
	a.dsh = NewDshClient(3080)
	sid := createTempSession(t)
	// 设置目标
	if err := a.SetGoalForTab(sid, "集成测试: 请回复 OK"); err != nil {
		t.Fatalf("SetGoalForTab 失败: %v", err)
	}
	// ref 已缓存
	if _, ok := getGoalRef(sid); !ok {
		t.Fatal("goal ref 未缓存")
	}
	// 清除目标
	if err := a.ClearGoalForTab(sid); err != nil {
		t.Fatalf("ClearGoalForTab 失败: %v", err)
	}
	if _, ok := getGoalRef(sid); ok {
		t.Fatal("清除后 ref 应移除")
	}
}

// 集成：Composer profile 组合设置。
func TestSetComposerProfileIntegration(t *testing.T) {
	a := newTestApp()
	a.dsh = NewDshClient(3080)
	sid := createTempSession(t)
	if err := a.SetComposerProfileForTab(sid, "goal", "auto", "组合测试目标"); err != nil {
		t.Fatalf("SetComposerProfileForTab 失败: %v", err)
	}
	if a.st.DefaultToolApprovalMode() != "auto" {
		t.Fatalf("approval 模式: %q", a.st.DefaultToolApprovalMode())
	}
}
