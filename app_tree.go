package main

// App 的项目树桥方法（DSH session.list 按 cwd 分组 → 前端 ProjectTree 结构）。
// 侧栏项目树是核心 UI：会话按工作目录分组为"项目"，每个项目下是"会话(topic)"节点。

import (
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// dshSessionList 是 session.list 的返回。
type dshSessionList struct {
	Items []dshSession `json:"items"`
}

// fetchSessions 读取 session.list（带缓存语义，这里简单直接调用）。
func (a *App) fetchSessions() []dshSession {
	if a.dsh == nil {
		return nil
	}
	raw, err := a.dsh.RPC("session.list", map[string]any{})
	if err != nil {
		return nil
	}
	var list dshSessionList
	if err := DecodeRPC(raw, &list); err != nil {
		return nil
	}
	return list.Items
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
		Revision: 1,
		Projects: projects,
		Catalog:  map[string]any{"state": "ready", "mode": "memory", "revision": 1, "indexed": len(items), "total": len(items), "repairPending": 0},
		Indexed:  len(items),
		Total:    len(items),
		IndexingDone: true,
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
func (a *App) GetProjectTreeSnapshot() map[string]any {
	t := a.buildProjectTree()
	return map[string]any{
		"revision": 1, "projects": t.Projects, "catalog": t.Catalog,
		"indexed": t.Indexed, "total": t.Total, "indexingDone": true,
	}
}

// GetProjectTreeRuntimeSnapshot 运行时投影（会话运行状态叠加）。
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
	return map[string]any{"revision": len(items), "topics": topics}
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
