package main

import (
	"encoding/json"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"
)

// 子代理面板（右侧栏「子代理」页）数据源。
//
// 数据分两块：
//  1. 子智能体：DSH `subagent.list {parentSessionId}` 为权威来源（返回
//     kind/mode/label/activity/hasChildren），再用 session.list 的会话信息（running/cwd/title）
//     补齐状态；若 subagent.list 不可用（旧版 DSH），退回用 session.list 的
//     parentSessionId 过滤兜底。
//  2. 后台进程：DSH 未暴露任何进程/任务 RPC（job.list / process.list 等实测 404），
//     故由桥在 Windows 侧枚举进程（Win32_Process），按"与本项目/DSH 相关"的规则过滤：
//     命令行含 dsh / deepseek-harness / mcp / dsh-reasonix / agent-teams 关键词，
//     或其父进程属于相关集合（一轮传播）。

type panelSession struct {
	SessionID       string `json:"sessionId"`
	ParentSessionID string `json:"parentSessionId"`
	Running         bool   `json:"running"`
	Cwd             string `json:"cwd"`
	AgentPreset     string `json:"agentPreset"`
	UpdatedAt       int64  `json:"updatedAt"`
	Projections     struct {
		Values struct {
			Title string `json:"title"`
		} `json:"values"`
	} `json:"projections"`
}

type subagentEntry struct {
	Kind        string `json:"kind"`
	ID          string `json:"id"`
	Mode        string `json:"mode"`
	Label       string `json:"label"`
	Activity    string `json:"activity"`
	HasChildren bool   `json:"hasChildren"`
}

type procRow struct {
	PID      int     `json:"pid"`
	PPID     int     `json:"ppid"`
	Name     string  `json:"name"`
	MemMB    float64 `json:"memMb"`
	Cmd      string  `json:"cmd"`
	Category string  `json:"category"`
}

var (
	procCacheMu  sync.Mutex
	procCache    []map[string]any
	procCacheAt  time.Time
	procCacheTTL = 4 * time.Second
)

// LogFromFrontend 前端注入脚本的诊断上报。
//
// 生产构建的 WebView2 不带远程调试端口（CDP 不可用），注入脚本一旦探测不到宿主容器
// 就会静默失效；因此脚本把关键诊断（是否找到 tab 栏、注入结果、异常）回报到桥，
// 由桥写进 %TEMP%\resume-debug.log，便于事后排查（失败留痕原则）。
func (a *App) LogFromFrontend(msg string) {
	resumeLog("frontend: %s", msg)
}

// SubagentPanel 返回「子代理」页数据。sessionId 为空时自动选最近更新的父会话。
func (a *App) SubagentPanel(sessionId string) map[string]any {
	out := map[string]any{
		"ok":         true,
		"sessionId":  "",
		"subagents":  []any{},
		"processes":  []any{},
		"sources":    map[string]any{"subagents": "subagent.list", "processes": "win32-process"},
		"notes":      []any{},
		"fetchedAt":  time.Now().UnixMilli(),
	}
	if a.dsh == nil {
		out["ok"] = false
		out["notes"] = []any{"DSH 未连接"}
		return out
	}

	sessions := a.fetchPanelSessions()
	sid := strings.TrimSpace(sessionId)
	if sid == "" {
		sid = pickActiveSession(sessions)
	}
	out["sessionId"] = sid
	out["parentTitle"] = sessionTitle(sessions, sid)

	entries := a.fetchSubagentEntries(sid)
	rows := mergeSubagentRows(entries, sessions, sid)
	if len(entries) == 0 {
		// 退回：session.list 里 parent 指向本会话的会话（含插件生成的成员会话）
		rows = fallbackSubagentsFromSessions(sessions, sid)
		if len(rows) > 0 {
			out["sources"].(map[string]any)["subagents"] = "session.list(parentSessionId)"
		}
	}
	out["subagents"] = rows
	out["counts"] = map[string]any{
		"subagents": len(rows),
		"active":    countActive(rows),
	}

	procs := cachedRelatedProcesses()
	out["processes"] = procs
	out["counts"].(map[string]any)["processes"] = len(procs)

	out["notes"] = []any{
		"子智能体来自 DSH subagent.list（parentSessionId=当前会话）；插件（如 dsh-agent-teams）派生的成员会话同样登记在此",
		"后台进程为本机 Win32_Process 中与本项目/DSH 相关的进程（命令行关键词 dsh / deepseek-harness / mcp / dsh-reasonix / agent-teams，含一轮父子传播）",
		"DSH 进程内的后台任务（如 bash 后台 job）不产生独立进程，故不出现在进程列表",
	}
	return out
}

func (a *App) fetchPanelSessions() []panelSession {
	raw, err := a.dsh.RPC("session.list", map[string]any{})
	if err != nil {
		resumeLog("subagentPanel session.list err=%v", err)
		return nil
	}
	var list struct {
		Items []panelSession `json:"items"`
	}
	if err := DecodeRPC(raw, &list); err != nil {
		resumeLog("subagentPanel session.list decode err=%v", err)
		return nil
	}
	return list.Items
}

// pickActiveSession 选"最近更新的父会话"（无 parentSessionId 的会话），
// 前端未传 sessionId 时用它作为当前会话。
func pickActiveSession(sessions []panelSession) string {
	best := ""
	var bestAt int64 = -1
	for _, s := range sessions {
		if strings.TrimSpace(s.ParentSessionID) != "" {
			continue // 子会话不参与
		}
		if s.UpdatedAt > bestAt {
			bestAt = s.UpdatedAt
			best = s.SessionID
		}
	}
	return best
}

func sessionTitle(sessions []panelSession, id string) string {
	for _, s := range sessions {
		if s.SessionID == id {
			return s.Projections.Values.Title
		}
	}
	return ""
}

func (a *App) fetchSubagentEntries(parentID string) []subagentEntry {
	if strings.TrimSpace(parentID) == "" {
		return nil
	}
	raw, err := a.dsh.RPC("subagent.list", map[string]any{"parentSessionId": parentID})
	if err != nil {
		return nil // 旧版 DSH 无该方法 → 走 session.list 兜底
	}
	var res struct {
		Entries []subagentEntry `json:"entries"`
	}
	if err := DecodeRPC(raw, &res); err != nil {
		return nil
	}
	return res.Entries
}

// normalizeSessionID 把 subagent entry 的裸 id 与 session 的 "session-<id>" 对齐比较。
func normalizeSessionID(id string) string {
	return strings.TrimPrefix(strings.TrimSpace(id), "session-")
}

// mergeSubagentRows 把 subagent.list 条目与会话状态合并成面板行。
func mergeSubagentRows(entries []subagentEntry, sessions []panelSession, parentID string) []map[string]any {
	byID := map[string]panelSession{}
	for _, s := range sessions {
		byID[normalizeSessionID(s.SessionID)] = s
	}
	rows := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		key := normalizeSessionID(e.ID)
		row := map[string]any{
			"id":          e.ID,
			"sessionId":   "session-" + key,
			"kind":        e.Kind,
			"mode":        e.Mode,
			"label":       e.Label,
			"activity":    e.Activity,
			"hasChildren": e.HasChildren,
			"parentId":    parentID,
			"running":     false,
			"cwd":         "",
			"title":       "",
		}
		if s, ok := byID[key]; ok {
			row["running"] = s.Running
			row["cwd"] = s.Cwd
			row["title"] = s.Projections.Values.Title
			row["agentPreset"] = s.AgentPreset
			row["updatedAt"] = s.UpdatedAt
		}
		rows = append(rows, row)
	}
	sortSubagentRows(rows)
	return rows
}

// fallbackSubagentsFromSessions subagent.list 不可用时的兜底：直接按 parentSessionId 过滤。
func fallbackSubagentsFromSessions(sessions []panelSession, parentID string) []map[string]any {
	rows := []map[string]any{}
	if strings.TrimSpace(parentID) == "" {
		return rows
	}
	for _, s := range sessions {
		if strings.TrimSpace(s.ParentSessionID) == "" {
			continue
		}
		if normalizeSessionID(s.ParentSessionID) != normalizeSessionID(parentID) {
			continue
		}
		rows = append(rows, map[string]any{
			"id":          normalizeSessionID(s.SessionID),
			"sessionId":   s.SessionID,
			"kind":        "child",
			"mode":        "session",
			"label":       s.Projections.Values.Title,
			"activity":    activityOf(s.Running),
			"hasChildren": false,
			"parentId":    parentID,
			"running":     s.Running,
			"cwd":         s.Cwd,
			"title":       s.Projections.Values.Title,
			"agentPreset": s.AgentPreset,
			"updatedAt":   s.UpdatedAt,
		})
	}
	sortSubagentRows(rows)
	return rows
}

func activityOf(running bool) string {
	if running {
		return "active"
	}
	return "inactive"
}

// sortSubagentRows 活跃优先，其次按更新时间倒序，最后按 label。
func sortSubagentRows(rows []map[string]any) {
	sort.SliceStable(rows, func(i, j int) bool {
		ai, _ := rows[i]["activity"].(string)
		aj, _ := rows[j]["activity"].(string)
		if (ai == "active") != (aj == "active") {
			return ai == "active"
		}
		ui, _ := rows[i]["updatedAt"].(int64)
		uj, _ := rows[j]["updatedAt"].(int64)
		if ui != uj {
			return ui > uj
		}
		li, _ := rows[i]["label"].(string)
		lj, _ := rows[j]["label"].(string)
		return li < lj
	})
}

func countActive(rows []map[string]any) int {
	n := 0
	for _, r := range rows {
		if a, _ := r["activity"].(string); a == "active" {
			n++
		}
	}
	return n
}

// ===== 后台进程 =====

// rawProc Windows 进程原始行（Win32_Process 投影）。
type rawProc struct {
	ProcessID       int    `json:"ProcessId"`
	ParentProcessID int    `json:"ParentProcessId"`
	Name            string `json:"Name"`
	WorkingSetSize  int64  `json:"WorkingSetSize"`
	CommandLine     string `json:"CommandLine"`
}

var relatedProcKeywords = []string{
	"dsh", "deepseek-harness", "mcp", "agent-teams", "dsh-reasonix",
}

// classifyProc 判定进程与本项目/DSH 的相关性，返回类别（空 = 不相关）。
func classifyProc(p rawProc, parentRelated map[int]bool) string {
	cmd := strings.ToLower(p.CommandLine)
	name := strings.ToLower(p.Name)
	switch {
	case strings.Contains(cmd, "agent-teams"):
		return "agent-teams"
	case strings.Contains(cmd, "mcp"):
		return "MCP"
	case strings.Contains(cmd, "deepseek-harness"):
		return "DSH"
	case strings.Contains(cmd, "dsh-reasonix"):
		return "本项目"
	case strings.Contains(cmd, "dsh") || strings.Contains(name, "dsh"):
		return "DSH"
	}
	if parentRelated[p.ParentProcessID] {
		return "子进程"
	}
	return ""
}

// filterRelatedProcesses 从原始进程表筛出相关进程（含一轮父子传播）。
func filterRelatedProcesses(all []rawProc) []map[string]any {
	related := map[int]bool{}
	// 第一轮：关键词命中
	type hit struct {
		p   rawProc
		cat string
	}
	hits := []hit{}
	for _, p := range all {
		if cat := classifyProc(p, nil); cat != "" {
			hits = append(hits, hit{p, cat})
			related[p.ProcessID] = true
		}
	}
	// 第二轮：父进程相关的子进程
	for _, p := range all {
		if related[p.ProcessID] {
			continue
		}
		if cat := classifyProc(p, related); cat != "" {
			hits = append(hits, hit{p, cat})
			related[p.ProcessID] = true
		}
	}
	rows := make([]map[string]any, 0, len(hits))
	for _, h := range hits {
		rows = append(rows, map[string]any{
			"pid":      h.p.ProcessID,
			"ppid":     h.p.ParentProcessID,
			"name":     h.p.Name,
			"memMb":    float64(h.p.WorkingSetSize) / (1024 * 1024),
			"cmd":      truncateCmd(h.p.CommandLine, 200),
			"category": h.cat,
		})
	}
	sort.SliceStable(rows, func(i, j int) bool {
		mi, _ := rows[i]["memMb"].(float64)
		mj, _ := rows[j]["memMb"].(float64)
		return mi > mj
	})
	return rows
}

func truncateCmd(s string, n int) string {
	s = strings.TrimSpace(strings.Join(strings.Fields(s), " "))
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// cachedRelatedProcesses 带 TTL 的进程枚举（Win32_Process 查询约 1s，面板会轮询）。
func cachedRelatedProcesses() []map[string]any {
	procCacheMu.Lock()
	if procCache != nil && time.Since(procCacheAt) < procCacheTTL {
		cached := procCache
		procCacheMu.Unlock()
		return cached
	}
	procCacheMu.Unlock()

	rows := enumerateRelatedProcesses()

	procCacheMu.Lock()
	procCache = rows
	procCacheAt = time.Now()
	procCacheMu.Unlock()
	return rows
}

func enumerateRelatedProcesses() []map[string]any {
	ps := `$ErrorActionPreference='SilentlyContinue'; Get-CimInstance Win32_Process | ` +
		`Select-Object ProcessId,ParentProcessId,Name,WorkingSetSize,CommandLine | ` +
		`ConvertTo-Json -Compress -Depth 2`
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", ps)
	out, err := cmd.Output()
	if err != nil || len(out) == 0 {
		resumeLog("process enumerate failed: %v", err)
		return []map[string]any{}
	}
	trimmed := strings.TrimSpace(string(out))
	var procs []rawProc
	if strings.HasPrefix(trimmed, "[") {
		if err := json.Unmarshal([]byte(trimmed), &procs); err != nil {
			resumeLog("process json array err=%v", err)
			return []map[string]any{}
		}
	} else if strings.HasPrefix(trimmed, "{") {
		var one rawProc
		if err := json.Unmarshal([]byte(trimmed), &one); err != nil {
			resumeLog("process json object err=%v", err)
			return []map[string]any{}
		}
		procs = []rawProc{one}
	} else {
		return []map[string]any{}
	}
	return filterRelatedProcesses(procs)
}
