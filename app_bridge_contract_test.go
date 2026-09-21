package main

// app_bridge_contract_test.go — 桥方法返回值契约的回归守卫。
//
// 背景（2026-09-21 真机事故）：`GetSessionCatalogStatus` 是零值桩 `return nil`，
// 前端在 project-tree:changed 事件路径上 `GetSessionCatalogStatus().then(Me)` 把结果
// 直接存进 catalog 状态，随后 useMemo 读 `catalog.state` →
//   TypeError: Cannot read properties of null (reading 'state')
// → React 错误边界把整个界面替换成错误页。
// 这类"返回 null 但前端立刻取属性"的桩是隐雷：调用点在被事件/用户操作真正触发前都不报错。
// 本测试把"这些结果必须非 nil 且带必要字段"固定下来。

import "testing"

func TestDereferencedBridgeResultsAreNotNull(t *testing.T) {
	a := &App{}

	// 1) 会话目录状态：前端直接 Me(结果) 并随后读 catalog.state / canRebuild
	cat := a.GetSessionCatalogStatus()
	if cat == nil {
		t.Fatal("GetSessionCatalogStatus 不得返回 nil（前端会读 .state，null 会崩渲染）")
	}
	if _, ok := cat["state"]; !ok {
		t.Fatalf("catalog 必须含 state 字段：%v", cat)
	}
	if _, ok := cat["canRebuild"]; !ok {
		t.Fatalf("catalog 必须含 canRebuild 字段（前端据此决定是否显示重建按钮）：%v", cat)
	}

	// 2) 项目树快照里的 catalog 同样必须非 nil（同一条渲染路径）
	snap := a.GetProjectTreeSnapshot()
	if snap == nil {
		t.Fatal("GetProjectTreeSnapshot 不得返回 nil")
	}
	if snap["catalog"] == nil {
		t.Fatal("快照里的 catalog 不得为 nil")
	}
	if _, ok := snap["revision"]; !ok {
		t.Fatal("快照必须含 revision")
	}

	// 3) 工作区冲突：前端立刻读 e.state（"none" === e.state ? null : e）
	conflict := a.WorkspaceConflictForTab("tab-test")
	if conflict == nil {
		t.Fatal("WorkspaceConflictForTab 不得返回 nil（前端会读 .state）")
	}
	if conflict["state"] != "none" {
		t.Fatalf("未做冲突检测时应如实返回 state=none，实际 %v", conflict["state"])
	}

	// 4) 运行态快照必须与外壳树同源（revision 单调计数），且 topics 可迭代
	rt := a.GetProjectTreeRuntimeSnapshot()
	if rt == nil {
		t.Fatal("GetProjectTreeRuntimeSnapshot 不得返回 nil")
	}
	if _, ok := rt["revision"]; !ok {
		t.Fatal("运行态快照必须含 revision（前端据此丢弃过期快照）")
	}
	if _, ok := rt["topics"].([]any); !ok {
		t.Fatalf("运行态快照的 topics 必须是数组：%T", rt["topics"])
	}
}
