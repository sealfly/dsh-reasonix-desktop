package main

// App 的项目树桥方法（DSH session.list 按 cwd 分组 → 前端 ProjectTree 结构）。
// 侧栏项目树是核心 UI：会话按工作目录分组为"项目"，每个项目下是"会话(topic)"节点。

import (
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// dshSessionList 是 session.list 的返回。
type dshSessionList struct {
	Items []dshSession `json:"items"`
}

// sessionsCache 是 session.list 原始结果的短期缓存。
//
// 为什么需要（2026-09-17 实测）：fetchSessions 被 8 处调用（项目树、历史、上下文用量、
// MetaForTab…），而 DSH 的 session.list 对巨型会话要 0.5~1.3s。
// 用户实际能感觉到的例子：Composer 里切换审批模式（询问/自动/yolo）时，
// 前端接着会调 MetaForTab + ContextUsageForTab，两者都走 findSession→fetchSessions，
// 于是点一次按钮要等 1~3.6s（实测 MetaForTab 553~1569ms、ContextUsageForTab 495~2069ms，
// 而按钮自身的桥方法只要 2~4ms）。
//
// 与 tabsCache 同款策略：TTL + 会话写操作失效。TTL 取 3s —— 空闲点击秒回；
// 回合进行中最多 3s 陈旧（界面本来就在按事件流持续刷新，可接受），同时把 DSH 的
// 重复计算压到每 3s 一次。
var (
	sessionsCacheMu sync.Mutex
	sessionsCache   []dshSession
	sessionsCacheAt time.Time
)

const sessionsCacheTTL = 3 * time.Second

// invalidateSessionsCache 清空会话原始列表缓存。
func invalidateSessionsCache() {
	sessionsCacheMu.Lock()
	sessionsCache = nil
	sessionsCacheAt = time.Time{}
	sessionsCacheMu.Unlock()
}

// fetchSessions 读取 session.list，并按官方语义过滤归档会话（带短期缓存）。
//
// ★ 归档过滤必须在这里做：DSH 的 archive 只是把 sessionId 记进 workspace.list 的
// archivedSessionIds，session.list 与 DSH 内存态仍然返回该会话。任何"列会话"的路径
// 漏掉过滤，已归档/已清理的会话就会一直挂在侧栏项目树里。
// 实测事故：集成测试留下的 14 个 Temp\...\TestXxx\001 会话，磁盘目录早已清干净，
// 项目树里却长期显示 6 个 "001" 项目（fetchTabs 过滤了，这条路径没有）。
func (a *App) fetchSessions() []dshSession {
	if a.dsh == nil {
		return nil
	}
	sessionsCacheMu.Lock()
	if sessionsCache != nil && time.Since(sessionsCacheAt) < sessionsCacheTTL {
		cached := sessionsCache
		sessionsCacheMu.Unlock()
		return cached
	}
	sessionsCacheMu.Unlock()

	raw, err := a.dsh.RPC("session.list", map[string]any{})
	if err != nil {
		return nil
	}
	var list dshSessionList
	if err := DecodeRPC(raw, &list); err != nil {
		return nil
	}
	items := filterArchivedSessions(list.Items, a.fetchArchivedSessionIDs())
	sessionsCacheMu.Lock()
	sessionsCache = items
	sessionsCacheAt = time.Now()
	sessionsCacheMu.Unlock()
	// 会话列表**内容变化**时通知前端刷新项目树（签名去重，避免把 3s 轮询变成事件噪声）。
	// 这也是"应用先于后端启动"场景的兜底：后端一可用，这里就会发一次 project-tree:changed。
	if a.noteSessionListSignature(items) {
		a.emitProjectTreeChanged("metadata")
		a.emitProjectTreeRuntimeChanged()
	}
	return items
}

// filterArchivedSessions 按归档集合过滤会话（纯函数，便于单测）。
// archived 为空 = 归档读取失败或确实没有归档：宁可多显示，
// 也不因为一次读取失败把整个会话列表吞掉。
func filterArchivedSessions(items []dshSession, archived map[string]bool) []dshSession {
	if len(archived) == 0 {
		return items
	}
	out := make([]dshSession, 0, len(items))
	for _, s := range items {
		if archived[s.SessionID] {
			continue
		}
		out = append(out, s)
	}
	return out
}

// projectTree 是 buildProjectTree 的返回结构（前端 ProjectTree 期望）。
type projectTree struct {
	Revision  int            `json:"revision"`
	Projects  []any          `json:"projects"`
	Catalog   map[string]any `json:"catalog"`
	Indexed   int            `json:"indexed"`
	Total     int            `json:"total"`
	IndexingDone bool        `json:"indexingDone"`
}

// buildProjectTree 把 session.list 按 cwd 分组为项目树。
func (a *App) buildProjectTree() projectTree {
	items := a.fetchSessions()
	byRoot := map[string][]any{}
	for _, s := range items {
		v := map[string]any{}
		if s.Projections != nil {
			if vals, ok := s.Projections["values"].(map[string]any); ok {
				v = vals
			}
		}
		root := s.Cwd
		if root == "" {
			root = "C:\\"
		}
		title, _ := v["title"].(string)
		if title == "" {
			title = "未命名会话"
		}
		byRoot[root] = append(byRoot[root], map[string]any{
			"key":    s.SessionID,
			"kind":   "topic", // 'topic' 而非 'session'：前端只给 topic 节点渲染行级操作
			"label":  title,
			"root":   root,
			"topicId": s.SessionID,
			"sessionPath": s.SessionID + ".jsonl",
			"turns":  turnsOf(v),
			"turnsState": "valid",
			"health": "ok",
			"lastActivityAt": s.UpdatedAt,
			"open":   true,
			"running": s.Running,
			"pinned": false,
			"children": []any{},
		})
	}
	roots := make([]string, 0, len(byRoot))
	for root := range byRoot {
		roots = append(roots, root)
	}
	sort.Strings(roots)

	projects := make([]any, 0, len(roots))
	for _, root := range roots {
		name := filepath.Base(root)
		if name == "" || name == "." || name == "\\" {
			name = "workspace"
		}
		projects = append(projects, map[string]any{
			"key":     "p:" + root,
			"kind":    "project",
			"label":   name,
			"root":    root,
			"pinned":  false,
			"open":    true,
			"children": byRoot[root],
		})
	}
	return projectTree{
		Revision:     int(currentTreeRevision()),
		Projects:     projects,
		Catalog:      a.sessionCatalogStatus(),
		Indexed:      len(items),
		Total:        len(items),
		IndexingDone: true,
	}
}

// GetSessionCatalogStatus 返回会话目录状态（桥方法）。
//
// 前端在 project-tree:changed / -v2 事件路径上会调用它并把结果直接存进 catalog 状态；
// 必须返回非 nil 对象（旧零值桩 return nil 会崩渲染，见 sessionCatalogStatus 的说明）。
func (a *App) GetSessionCatalogStatus() map[string]any {
	return a.sessionCatalogStatus()
}

// sessionCatalogStatus 返回会话目录（catalog）状态。
//
// ⚠️ 必须返回**非 nil** 对象：前端在收到 project-tree:changed 事件后会
// `GetSessionCatalogStatus().then(Me)`，把结果直接存进 catalog 状态；随后
// useMemo 会读 `catalog.state`。旧实现是 `return nil`（app_stubs2.go 的零值桩），
// 一旦事件真的发出来就会 `Cannot read properties of null (reading 'state')`
// → React 错误边界把整个界面替换成错误页（2026-09-21 实测踩到）。
func (a *App) sessionCatalogStatus() map[string]any {
	n := len(a.fetchSessions())
	return map[string]any{
		"state":                "ready",
		"mode":                 "memory",
		"revision":             currentTreeRevision(),
		"indexed":              n,
		"total":                n,
		"repairPending":        0,
		"sourceCount":          n,
		"unindexedTargetCount": 0,
		"canRebuild":           false,
	}
}

// turnsOf 提取会话的 turns 数（projections.values.sessionStats.turns）。
func turnsOf(v map[string]any) int {
	if ss, ok := v["sessionStats"].(map[string]any); ok {
		if t, ok := ss["turns"].(float64); ok {
			return int(t)
		}
	}
	return 0
}

// GetProjectTreeSnapshot 返回项目树快照（前端侧栏渲染）。
//
// revision 必须与 project-tree:changed 事件**共用同一个单调递增计数**：
// 前端会把收到的 revision 记在 Ce.current（只增不减），若快照恒为 1，等事件带了更大的
// revision 之后，快照就会被判成"不新鲜"而丢弃（2026-09-21 修复）。
func (a *App) GetProjectTreeSnapshot() map[string]any {
	t := a.buildProjectTree()
	return map[string]any{
		"revision": currentTreeRevision(), "projects": t.Projects, "catalog": t.Catalog,
		"indexed": t.Indexed, "total": t.Total, "indexingDone": true,
	}
}

// GetProjectTreeRuntimeSnapshot 运行时投影（会话运行状态叠加）。
//
// revision 与事件共用同一单调计数：前端 projectTreeRuntime 模块**只接受 revision 不小于
// 当前值**的快照（`e.revision < c.revision` 即丢弃）。旧实现用"会话条数"当 revision，
// 条数不变而运行态变化（如某个会话开始运行）时更新会被静默忽略。
func (a *App) GetProjectTreeRuntimeSnapshot() map[string]any {
	items := a.fetchSessions()
	topics := make([]any, 0, len(items))
	for _, s := range items {
		status := "idle"
		if s.Running {
			status = "running"
		}
		root := s.Cwd
		if root == "" {
			root = "C:\\"
		}
		topics = append(topics, map[string]any{
			"node": map[string]any{
				"topicId": s.SessionID, "scope": "project", "workspaceRoot": root,
				"sessionPath": s.SessionID + ".jsonl", "open": true,
				"running": s.Running, "status": status, "children": []any{},
			},
		})
	}
	return map[string]any{"revision": currentTreeRevision(), "topics": topics}
}

// ListProjectTree 返回项目 root 列表。
func (a *App) ListProjectTree() []any {
	t := a.buildProjectTree()
	roots := make([]any, 0, len(t.Projects))
	for _, p := range t.Projects {
		if pm, ok := p.(map[string]any); ok {
			if root, ok := pm["root"].(string); ok {
				roots = append(roots, root)
			}
		}
	}
	return roots
}

// ListProjectTopics 分页返回某项目的会话(topic)列表。
func (a *App) ListProjectTopics(req map[string]any) map[string]any {
	t := a.buildProjectTree()
	scope, _ := req["scope"].(string)
	workspaceRoot, _ := req["workspaceRoot"].(string)

	var children []any
	for _, p := range t.Projects {
		pm, _ := p.(map[string]any)
		root, _ := pm["root"].(string)
		if scope == "global" {
			// DSH 没有 global folder，global 作用域返回空
			continue
		}
		if root == workspaceRoot {
			if c, ok := pm["children"].([]any); ok {
				children = c
			}
			break
		}
	}
	start := 0
	if c, ok := req["cursor"].(string); ok {
		start, _ = strconv.Atoi(c)
	}
	if start < 0 {
		start = 0
	}
	limit := 50
	if l, ok := req["limit"].(float64); ok {
		limit = int(l)
	}
	if limit < 1 {
		limit = 1
	}
	if limit > 200 {
		limit = 200
	}
	end := start + limit
	if end > len(children) {
		end = len(children)
	}
	items := children[start:end]
	var nextCursor any
	if end < len(children) {
		nextCursor = strconv.Itoa(end)
	}
	return map[string]any{
		"items": items, "nextCursor": nextCursor, "revision": 1,
		"complete": true, "readyDirectories": 1, "pendingDirectories": 0, "failedDirectories": 0,
	}
}

// ListWorkspaces 返回工作区列表（session.list 的 cwd 去重）。
func (a *App) ListWorkspaces() []any {
	items := a.fetchSessions()
	seen := map[string]bool{}
	out := []any{}
	for _, s := range items {
		root := s.Cwd
		if root == "" {
			root = "C:\\"
		}
		if seen[root] {
			continue
		}
		seen[root] = true
		name := filepath.Base(root)
		if name == "" || name == "." {
			name = "workspace"
		}
		out = append(out, map[string]any{
			"root": root, "name": name, "workspaceRoot": root,
		})
	}
	return out
}

// normalizeRoot 规范化 Windows 路径分隔符（供其它方法复用）。
func normalizeRoot(root string) string {
	if root == "" {
		return "C:\\"
	}
	return strings.ReplaceAll(root, "/", "\\")
}
