package main

import (
	"strings"
	"testing"
)

func mkSession(id, parent string, running bool, updated int64, title string) panelSession {
	s := panelSession{SessionID: id, ParentSessionID: parent, Running: running, UpdatedAt: updated, Cwd: "C:/proj"}
	s.Projections.Values.Title = title
	return s
}

func TestPickActiveSession(t *testing.T) {
	// 父会话 updatedAt 更大者胜出；子会话（有 parent）永不入选
	sessions := []panelSession{
		mkSession("session-a", "", false, 100, "A"),
		mkSession("session-b", "", false, 900, "B"),
		mkSession("session-c", "session-b", true, 9999, "子会话"), // 更"新"但是子会话
	}
	if got := pickActiveSession(sessions); got != "session-b" {
		t.Fatalf("want session-b got %q", got)
	}
	if got := pickActiveSession(nil); got != "" {
		t.Fatalf("empty want \"\" got %q", got)
	}
	onlyChildren := []panelSession{mkSession("session-x", "session-p", true, 5, "")}
	if got := pickActiveSession(onlyChildren); got != "" {
		t.Fatalf("only children want \"\" got %q", got)
	}
}

func TestMergeSubagentRows(t *testing.T) {
	sessions := []panelSession{
		mkSession("session-parent", "", true, 50, "父"),
		mkSession("session-aaa", "session-parent", true, 20, "跑着的成员"),
		mkSession("session-bbb", "session-parent", false, 30, "歇着的成员"),
	}
	entries := []subagentEntry{
		{Kind: "child", ID: "bbb", Mode: "one-shot", Label: "task-bbb", Activity: "inactive"},
		{Kind: "child", ID: "aaa-extra", Mode: "one-shot", Label: "task-missing", Activity: "inactive"},
		{Kind: "child", ID: "aaa", Mode: "workflow", Label: "task-aaa", Activity: "active", HasChildren: true},
	}
	rows := mergeSubagentRows(entries, sessions, "session-parent")
	if len(rows) != 3 {
		t.Fatalf("want 3 rows got %d", len(rows))
	}
	// active 优先排第一
	if rows[0]["id"] != "aaa" {
		t.Fatalf("want active first (aaa) got %v", rows[0]["id"])
	}
	// 会话信息补齐：裸 id 与 session- 前缀对齐
	if rows[0]["running"] != true || rows[0]["title"] != "跑着的成员" {
		t.Fatalf("aaa should be enriched: running=%v title=%v", rows[0]["running"], rows[0]["title"])
	}
	if rows[0]["sessionId"] != "session-aaa" {
		t.Fatalf("sessionId should be prefixed: %v", rows[0]["sessionId"])
	}
	if rows[0]["hasChildren"] != true || rows[0]["mode"] != "workflow" {
		t.Fatalf("entry fields lost: %v", rows[0])
	}
	// 无匹配会话的行保留 entry 字段，状态回退为 inactive/空
	for _, r := range rows {
		if r["id"] == "aaa-extra" {
			if r["running"] != false || r["cwd"] != "" {
				t.Fatalf("unmatched row should default: %v", r)
			}
		}
	}
}

func TestFallbackSubagentsFromSessions(t *testing.T) {
	sessions := []panelSession{
		mkSession("session-parent", "", true, 50, "父"),
		mkSession("session-kid1", "session-parent", true, 10, "成员1"),
		mkSession("session-kid2", "session-parent", false, 20, "成员2"),
		mkSession("session-other", "session-otherparent", false, 99, "别人的孩子"),
	}
	rows := fallbackSubagentsFromSessions(sessions, "session-parent")
	if len(rows) != 2 {
		t.Fatalf("want 2 children got %d", len(rows))
	}
	if rows[0]["id"] != "kid1" { // active 优先
		t.Fatalf("want kid1 first got %v", rows[0]["id"])
	}
	// 传裸 id 也应能匹配（前缀对齐）
	rows2 := fallbackSubagentsFromSessions(sessions, "parent")
	if len(rows2) != 2 {
		t.Fatalf("bare parent id should match: got %d", len(rows2))
	}
}

func TestClassifyProc(t *testing.T) {
	cases := []struct {
		name string
		cmd  string
		proc string
		want string
	}{
		{"agent-teams 成员", `node C:\x\node_modules\@nanmicoder\dsh-agent-teams\lib\index.js`, "node.exe", "agent-teams"},
		{"MCP 服务", `python -m mcp_server --stdio`, "python.exe", "MCP"},
		{"DSH 本体", `node C:\x\deepseek-harness-desktop\dependencies\dsh\bin\dsh.js`, "node.exe", "DSH"},
		{"本项目进程", `C:\Users\x\Desktop\dsh-reasonix-wails\DSH-ReasonixUI-new.exe`, "DSH-ReasonixUI-new.exe", "本项目"},
		{"dsh 关键词", `node C:\tools\dsh-web\server.js`, "node.exe", "DSH"},
		{"无关", `C:\Windows\explorer.exe`, "explorer.exe", ""},
	}
	for _, c := range cases {
		got := classifyProc(rawProc{Name: c.proc, CommandLine: c.cmd}, nil)
		if got != c.want {
			t.Fatalf("%s: want %q got %q", c.name, c.want, got)
		}
	}
	// 父进程相关 → 子进程
	parentRelated := map[int]bool{4242: true}
	if got := classifyProc(rawProc{Name: "cmd.exe", CommandLine: `cmd /c echo hi`, ParentProcessID: 4242}, parentRelated); got != "子进程" {
		t.Fatalf("child of related should classify 子进程 got %q", got)
	}
}

func TestFilterRelatedProcesses(t *testing.T) {
	all := []rawProc{
		{ProcessID: 10, ParentProcessID: 1, Name: "node.exe", CommandLine: `node dsh\bin\dsh.js`, WorkingSetSize: 100 * 1024 * 1024},
		{ProcessID: 11, ParentProcessID: 10, Name: "cmd.exe", CommandLine: `cmd /c build.bat`, WorkingSetSize: 10 * 1024 * 1024},
		{ProcessID: 20, ParentProcessID: 1, Name: "explorer.exe", CommandLine: `C:\Windows\explorer.exe`, WorkingSetSize: 500 * 1024 * 1024},
	}
	rows := filterRelatedProcesses(all)
	if len(rows) != 2 {
		t.Fatalf("want 2 related (dsh + its child) got %d: %v", len(rows), rows)
	}
	// 按内存降序：node(100MB) 在 cmd(10MB) 前
	if rows[0]["pid"] != 10 || rows[1]["pid"] != 11 {
		t.Fatalf("order by mem wrong: %v", rows)
	}
	if rows[1]["category"] != "子进程" {
		t.Fatalf("pid 11 should be 子进程 got %v", rows[1]["category"])
	}
	if rows[0]["category"] != "DSH" {
		t.Fatalf("pid 10 should be DSH got %v", rows[0]["category"])
	}
}

func TestTruncateCmd(t *testing.T) {
	if got := truncateCmd("a   b\n\tc", 100); got != "a b c" {
		t.Fatalf("whitespace collapse failed: %q", got)
	}
	long := strings.Repeat("x", 300)
	got := truncateCmd(long, 200)
	if len([]rune(got)) != 201 || !strings.HasSuffix(got, "…") {
		t.Fatalf("truncate failed: len=%d", len([]rune(got)))
	}
}

func TestSubagentPanelNoDSH(t *testing.T) {
	app := &App{}
	res := app.SubagentPanel("")
	if res["ok"] != false {
		t.Fatalf("no DSH should report ok=false, got %v", res["ok"])
	}
	if _, hasSubs := res["subagents"]; !hasSubs {
		t.Fatalf("shape should still contain subagents key: %v", res)
	}
}
