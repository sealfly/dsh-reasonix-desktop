package main

// app_tree_events_test.go — 项目树事件相关的不变量：
//   1) revision 单调递增（快照与事件共用，避免快照被判"不新鲜"而丢弃）；
//   2) 会话列表签名只在内容变化时为 true（否则 3s 轮询会变成事件噪声）；
//   3) 连接跃迁只在"离线 → 在线"时为 true。

import (
	"testing"
)

// 测试用的状态复位（包内可直接访问那两个互斥锁保护的变量）
func resetSessionSignature() {
	sessionSigMu.Lock()
	sessionSigHash = ""
	sessionSigMu.Unlock()
}

func resetConnState() {
	connStateMu.Lock()
	connWasOnline = false
	connStateMu.Unlock()
}

func TestTreeRevisionMonotonic(t *testing.T) {
	start := currentTreeRevision()
	a := nextTreeRevision()
	b := nextTreeRevision()
	if a <= start || b <= a {
		t.Fatalf("revision 必须单调递增：start=%d a=%d b=%d", start, a, b)
	}
	if currentTreeRevision() != b {
		t.Fatalf("currentTreeRevision 应等于最后一次 next 的值（%d）", b)
	}
	// 快照用的是同一个计数：不能被事件"甩开"
	if got := (&App{}).GetProjectTreeSnapshot()["revision"]; got != currentTreeRevision() {
		t.Fatalf("快照 revision(%v) 应与事件计数一致(%d)", got, currentTreeRevision())
	}
}

func TestSessionListSignatureDetectsChange(t *testing.T) {
	resetSessionSignature()
	base := []dshSession{
		{SessionID: "s1", Cwd: "C:\\a", UpdatedAt: float64(100), Running: false},
		{SessionID: "s2", Cwd: "C:\\b", UpdatedAt: float64(200), Running: false},
	}
	app := &App{}
	if !app.noteSessionListSignature(base) {
		t.Fatal("首次拿到会话列表应视为变化（后端刚可用时必须让前端刷新一次）")
	}
	if app.noteSessionListSignature(base) {
		t.Fatal("内容相同时不应重复报变化（否则 3s 轮询会把事件变成噪声）")
	}
	// 运行态变化 → 变化
	changedState := []dshSession{
		{SessionID: "s1", Cwd: "C:\\a", UpdatedAt: float64(100), Running: true},
		{SessionID: "s2", Cwd: "C:\\b", UpdatedAt: float64(200), Running: false},
	}
	if !app.noteSessionListSignature(changedState) {
		t.Fatal("running 变化应视为变化")
	}
	// 新增会话 → 变化
	withNew := append(append([]dshSession{}, base...), dshSession{SessionID: "s3", Cwd: "C:\\c"})
	if !app.noteSessionListSignature(withNew) {
		t.Fatal("新增会话应视为变化")
	}
	// 顺序无关（同一批会话换个顺序不该反复发事件）
	reordered := []dshSession{withNew[2], withNew[1], withNew[0]}
	if app.noteSessionListSignature(reordered) {
		t.Fatal("仅顺序变化不应视为内容变化（session.list 顺序会变）")
	}
}

func TestConnectedTransitionSemantics(t *testing.T) {
	resetConnState()
	if !markConnectedTransition(true) {
		t.Fatal("首次在线应视为跃迁（应用先于后端启动时靠它让前端自愈）")
	}
	if markConnectedTransition(true) {
		t.Fatal("持续在线不应重复跃迁")
	}
	if markConnectedTransition(false) {
		t.Fatal("掉线不是跃迁")
	}
	if !markConnectedTransition(true) {
		t.Fatal("掉线后再上线应再次跃迁")
	}
}

func TestEmitTreeEventsWithoutContextDoesNotPanic(t *testing.T) {
	a := &App{} // ctx == nil（未 Start）
	a.emitProjectTreeChanged("test")
	a.emitProjectTreeRuntimeChanged()
}

func TestNoteConnectedForTreeEmitsOncePerTransition(t *testing.T) {
	resetConnState()
	a := &App{}
	base, baseRt := treeEmitCounts()

	a.noteConnectedForTree(true) // 跃迁 → 应发一次外壳树 + 一次运行态
	c1, r1 := treeEmitCounts()
	if c1 != base+1 || r1 != baseRt+1 {
		t.Fatalf("首次上线应各发一次：changed %d->%d, runtime %d->%d", base, c1, baseRt, r1)
	}

	a.noteConnectedForTree(true) // 持续在线 → 不再发
	c2, _ := treeEmitCounts()
	if c2 != c1 {
		t.Fatalf("持续在线不应重复发事件：%d -> %d", c1, c2)
	}

	a.noteConnectedForTree(false) // 掉线不发
	c3, _ := treeEmitCounts()
	if c3 != c2 {
		t.Fatalf("掉线不应发项目树事件：%d -> %d", c2, c3)
	}

	a.noteConnectedForTree(true) // 再次上线 → 再发
	c4, _ := treeEmitCounts()
	if c4 != c3+1 {
		t.Fatalf("再次上线应再发一次：%d -> %d", c3, c4)
	}
}
