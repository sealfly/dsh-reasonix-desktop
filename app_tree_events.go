package main

// app_tree_events.go — 项目树事件：让前端在**不整页重载**的情况下刷新项目树。
//
// 背景（2026-09-21 真机）：前端项目树只在挂载时调用一次 GetProjectTreeSnapshot，
// 之后靠事件刷新。它订阅的是：
//
//	project-tree:changed / project-tree:changed-v2   ← 外壳树（项目/会话节点），payload {revision, reason}
//	project-tree:runtime-changed                     ← 运行态装饰（open/running/status），payload {revision, topics}
//
// 本项目此前**从未发过这两个事件**，于是"应用先于 DSH 后端启动"时项目树会一直停在空态
// —— 实测 DSH 里有 12 个项目 / 39 个会话，界面却显示「还没有项目」，手动刷新页面才恢复。
// 现在：会话列表变化、连接状态变为已连接、成功启动后端时都会发事件，前端据此自行刷新。
//
// 另外修掉一个隐患：GetProjectTreeSnapshot 曾经**恒定返回 revision=1**。前端会把收到的
// revision 记在 Ce.current 里（只增不减），一旦事件带了更大的 revision，常数 1 的快照就会被
// 判定为"不新鲜"而丢弃。现在快照与事件共用同一个单调递增的 revision。

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"sort"
	"sync"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

var (
	treeRevMu sync.Mutex
	treeRev   int64 = 1

	connStateMu   sync.Mutex
	connWasOnline bool

	sessionSigMu   sync.Mutex
	sessionSigHash string

	// 发事件计数（ctx 为空也计数）：便于日志与单测确认"该发的时候发了、不多发"。
	treeEmitMu       sync.Mutex
	treeEmitN        int64
	treeRuntimeEmitN int64
)

// nextTreeRevision 返回下一个（单调递增的）树版本号。
func nextTreeRevision() int64 {
	treeRevMu.Lock()
	defer treeRevMu.Unlock()
	treeRev++
	return treeRev
}

// currentTreeRevision 返回当前树版本号（快照与事件共用，保证"快照不旧于事件"）。
func currentTreeRevision() int64 {
	treeRevMu.Lock()
	defer treeRevMu.Unlock()
	return treeRev
}

// treeEmitCounts 返回 (外壳树事件数, 运行态事件数)。
func treeEmitCounts() (int64, int64) {
	treeEmitMu.Lock()
	defer treeEmitMu.Unlock()
	return treeEmitN, treeRuntimeEmitN
}

// emitProjectTreeChanged 通知前端"项目树（项目/会话结构）变了"。
// reason 与前端约定：metadata（元数据变化）/ connected / launch / manual …
func (a *App) emitProjectTreeChanged(reason string) {
	treeEmitMu.Lock()
	treeEmitN++
	treeEmitMu.Unlock()
	if a == nil || a.ctx == nil {
		return
	}
	rev := nextTreeRevision()
	payload := map[string]any{"revision": rev, "reason": reason}
	// 两个名字都发：v2 是当前前端用的，原名兼容旧订阅
	wruntime.EventsEmit(a.ctx, "project-tree:changed-v2", payload)
	wruntime.EventsEmit(a.ctx, "project-tree:changed", payload)
	resumeLog("tree: emit project-tree:changed reason=%s revision=%d", reason, rev)
}

// emitProjectTreeRuntimeChanged 通知前端"会话运行态变了"（open/running/status 装饰）。
// payload 与 GetProjectTreeRuntimeSnapshot() 同形：{revision, topics:[{node:{...}}]}。
func (a *App) emitProjectTreeRuntimeChanged() {
	treeEmitMu.Lock()
	treeRuntimeEmitN++
	treeEmitMu.Unlock()
	if a == nil || a.ctx == nil {
		return
	}
	snap := a.GetProjectTreeRuntimeSnapshot()
	wruntime.EventsEmit(a.ctx, "project-tree:runtime-changed", snap)
	resumeLog("tree: emit project-tree:runtime-changed revision=%v topics=%v", snap["revision"], len(snap["topics"].([]any)))
}

// noteSessionListSignature 在每次拉取会话列表后调用：**只有内容真的变了**才发事件，
// 避免前端每 3 秒的轮询把事件变成噪声。返回是否发生变化。
//
// 签名对**顺序不敏感**：session.list 的返回顺序偶尔会变，若直接按顺序拼接会把"换个顺序"
// 误判成"内容变化"，于是每次轮询都发事件。
func (a *App) noteSessionListSignature(items []dshSession) bool {
	lines := make([]string, 0, len(items))
	for _, s := range items {
		lines = append(lines, fmt.Sprintf("%s|%v|%v|%v", s.SessionID, s.UpdatedAt, s.Running, s.Cwd))
	}
	sort.Strings(lines)
	h := sha1.New()
	for _, l := range lines {
		_, _ = fmt.Fprintf(h, "%s;", l)
	}
	sig := hex.EncodeToString(h.Sum(nil))
	sessionSigMu.Lock()
	prev := sessionSigHash
	// 首次（prev 为空）也算"变化"：会话列表从"拿不到"变成"拿到了"必须让前端刷新一次。
	changed := prev != sig
	sessionSigHash = sig
	sessionSigMu.Unlock()
	return changed
}

// markConnectedTransition 记录连接状态，返回是否发生"离线 → 在线"跃迁。
func markConnectedTransition(online bool) (transition bool) {
	connStateMu.Lock()
	defer connStateMu.Unlock()
	transition = online && !connWasOnline
	connWasOnline = online
	return transition
}
