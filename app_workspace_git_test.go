package main

// app_workspace_git_test.go — 右侧"改动"栏 git 桥测试。
// 用真实 git 仓库（t.TempDir，不留残留，符合 PRINCIPLES P7）验证 status/diff/log 解析。

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func requireGit(t *testing.T) {
	t.Helper()
	if gitExecutable() == "" {
		t.Skip("git 不可用")
	}
}

func gitRun(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := exec.Command(gitExecutable(), args...)
	cmd.Dir = root
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=tester", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=tester", "GIT_COMMITTER_EMAIL=t@example.com")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v 失败: %v\n%s", args, err, out)
	}
	return string(out)
}

// initRepo 建一个含 initial 提交的临时仓库，返回 (root, branch)。
func initRepo(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	gitRun(t, root, "init", "-q")
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("line1\nline2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, root, "add", ".")
	gitRun(t, root, "commit", "-q", "-m", "initial")
	branch := strings.TrimSpace(gitRun(t, root, "rev-parse", "--abbrev-ref", "HEAD"))
	return root, branch
}

// 改动列表：分支名 + 已修改/未跟踪文件的 gitStatus。
func TestWorkspaceChangesAtRealRepo(t *testing.T) {
	requireGit(t)
	root, branch := initRepo(t)
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("line1\nline2 changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "b.txt"), []byte("new1\nnew2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res := (&App{}).workspaceChangesAt(root)
	if ok, _ := res["gitAvailable"].(bool); !ok {
		t.Fatalf("gitAvailable 应为 true: %v", res)
	}
	if got, _ := res["gitBranch"].(string); got != branch {
		t.Fatalf("分支不符 want=%q got=%q", branch, got)
	}
	files, _ := res["files"].([]any)
	got := map[string]string{}
	for _, it := range files {
		e, ok := it.(workspaceChangeEntry)
		if !ok {
			t.Fatalf("files 元素类型异常: %T", it)
		}
		got[e.Path] = e.GitStatus
	}
	if got["a.txt"] != "M" {
		t.Fatalf("a.txt 状态应为 M，实际 %q（全部 %v）", got["a.txt"], got)
	}
	if got["b.txt"] != "??" {
		t.Fatalf("b.txt 状态应为 ??，实际 %q（全部 %v）", got["b.txt"], got)
	}
}

// 非 git 目录：gitAvailable=false，且给出明确原因（前端据此显示"无法获取 git 信息"，
// 不能静默空白——用户会以为改动栏坏了）。
func TestWorkspaceChangesAtNonGitDir(t *testing.T) {
	requireGit(t)
	root := t.TempDir()
	res := (&App{}).workspaceChangesAt(root)
	if ok, _ := res["gitAvailable"].(bool); ok {
		t.Fatal("非 git 目录 gitAvailable 应为 false")
	}
	e, _ := res["gitErr"].(string)
	if !strings.Contains(e, "不是 git 仓库") {
		t.Fatalf("非 git 目录应给出明确原因，实际 %q", e)
	}
	if !strings.Contains(e, filepath.Base(root)) {
		t.Fatalf("原因里应带工作区路径，实际 %q", e)
	}
}

// 已跟踪文件改动详情：diff 含 +/-，统计正确。
func TestWorkspaceChangeDetailModified(t *testing.T) {
	requireGit(t)
	root, _ := initRepo(t)
	target := filepath.Join(root, "a.txt")
	if err := os.WriteFile(target, []byte("line1\nline2 changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res := (&App{}).workspaceChangeDetailAt(root, target, "a.txt")
	diff, _ := res["diff"].(string)
	if !strings.Contains(diff, "-line2") || !strings.Contains(diff, "+line2 changed") {
		t.Fatalf("diff 不符:\n%s", diff)
	}
	if src, _ := res["source"].(string); src != "git" {
		t.Fatalf("source 应为 git，实际 %q", src)
	}
	if added, _ := res["added"].(int); added != 1 {
		t.Fatalf("added 应为 1，实际 %v", res["added"])
	}
	if removed, _ := res["removed"].(int); removed != 1 {
		t.Fatalf("removed 应为 1，实际 %v", res["removed"])
	}
}

// 未跟踪文件：合成 new file diff，可被前端 diff 渲染器识别。
func TestWorkspaceChangeDetailUntracked(t *testing.T) {
	requireGit(t)
	root, _ := initRepo(t)
	target := filepath.Join(root, "b.txt")
	if err := os.WriteFile(target, []byte("new1\nnew2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res := (&App{}).workspaceChangeDetailAt(root, target, "b.txt")
	diff, _ := res["diff"].(string)
	if !strings.HasPrefix(diff, "diff --git a/b.txt b/b.txt") {
		t.Fatalf("未跟踪 diff 头不符:\n%s", diff)
	}
	if !strings.Contains(diff, "@@ -0,0 +1,2 @@") {
		t.Fatalf("未跟踪 diff hunk 头不符:\n%s", diff)
	}
	if added, _ := res["added"].(int); added != 2 {
		t.Fatalf("added 应为 2，实际 %v", res["added"])
	}
}

// 提交历史字段与前端契约一致（hash/message/author/date）。
func TestWorkspaceGitHistoryAt(t *testing.T) {
	requireGit(t)
	root, _ := initRepo(t)
	items := (&App{}).workspaceGitHistoryAt(root, "")
	if len(items) != 1 {
		t.Fatalf("历史条数应为 1，实际 %d", len(items))
	}
	m, _ := items[0].(map[string]any)
	if msg, _ := m["message"].(string); msg != "initial" {
		t.Fatalf("message 不符: %v", m["message"])
	}
	if author, _ := m["author"].(string); author != "tester" {
		t.Fatalf("author 不符: %v", m["author"])
	}
	hash, _ := m["hash"].(string)
	if len(hash) != 40 {
		t.Fatalf("hash 应为 40 位，实际 %q", hash)
	}
	if date, _ := m["date"].(string); !strings.Contains(date, "T") {
		t.Fatalf("date 应为 ISO8601，实际 %q", date)
	}
	// 单文件历史同样可用
	if one := (&App{}).workspaceGitHistoryAt(root, "a.txt"); len(one) != 1 {
		t.Fatalf("a.txt 历史应为 1 条，实际 %d", len(one))
	}
}

// 提交详情返回可渲染的补丁。
func TestWorkspaceGitCommitDetailAt(t *testing.T) {
	requireGit(t)
	root, _ := initRepo(t)
	items := (&App{}).workspaceGitHistoryAt(root, "")
	m, _ := items[0].(map[string]any)
	hash, _ := m["hash"].(string)
	res := (&App{}).workspaceGitCommitDetailAt(root, hash, "")
	diff, _ := res["diff"].(string)
	if !strings.Contains(diff, "initial") || !strings.Contains(diff, "a.txt") {
		t.Fatalf("提交详情不符:\n%s", diff)
	}
}

// porcelain -z 解析：普通/未跟踪/重命名（含中文名）。
func TestParseWorkspacePorcelain(t *testing.T) {
	raw := "## main...origin/main [ahead 1]\x00" +
		" M a.txt\x00" +
		"?? b.txt\x00" +
		"R  new/中文.txt\x00old/中文.txt\x00" +
		"MM c.txt\x00" +
		"UU conflict.txt\x00"
	branch, entries := parseWorkspacePorcelain(raw)
	if branch != "main" {
		t.Fatalf("branch 应为 main，实际 %q", branch)
	}
	got := map[string]string{}
	for _, e := range entries {
		got[e.Path] = e.GitStatus
	}
	want := map[string]string{
		"a.txt":       "M",
		"b.txt":       "??",
		"new/中文.txt": "R",
		"c.txt":       "M",
		"conflict.txt": "UU",
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("%s 状态应为 %q，实际 %q（全部 %v）", k, v, got[k], got)
		}
	}
	for _, e := range entries {
		if e.Path == "new/中文.txt" && e.OldPath != "old/中文.txt" {
			t.Fatalf("重命名旧路径不符: %q", e.OldPath)
		}
		if len(e.Sources) != 1 || e.Sources[0] != "git" {
			t.Fatalf("sources 应为 [git]，实际 %v", e.Sources)
		}
	}
}

func TestParseGitBranchLine(t *testing.T) {
	cases := map[string]string{
		"main...origin/main [ahead 1, behind 2]": "main",
		"main":                            "main",
		"feature/x...origin/feature/x":    "feature/x",
		"HEAD (no branch)":                "",
		"No commits yet on main":          "main",
		"":                                "",
	}
	for in, want := range cases {
		if got := parseGitBranchLine(in); got != want {
			t.Fatalf("parseGitBranchLine(%q) = %q，want %q", in, got, want)
		}
	}
}

func TestNormalizeGitStatus(t *testing.T) {
	cases := []struct{ x, y byte; want string }{
		{'?', '?', "??"},
		{' ', 'M', "M"},
		{'M', ' ', "M"},
		{'A', ' ', "A"},
		{'D', 'M', "D"},
		{'U', 'U', "UU"},
		{'A', 'A', "AA"},
		{' ', ' ', ""},
	}
	for _, c := range cases {
		if got := normalizeGitStatus(c.x, c.y); got != c.want {
			t.Fatalf("normalizeGitStatus(%c,%c) = %q，want %q", c.x, c.y, got, c.want)
		}
	}
}

func TestCountDiffStats(t *testing.T) {
	diff := "diff --git a/x b/x\n--- a/x\n+++ b/x\n@@ -1,2 +1,3 @@\n-old\n+new\n+extra\n same\n"
	added, removed := countDiffStats(diff)
	if added != 2 || removed != 1 {
		t.Fatalf("统计应为 +2/-1，实际 +%d/-%d", added, removed)
	}
}

func TestRelFromRootAndBinaryDiff(t *testing.T) {
	root := filepath.Clean("/tmp/ws")
	if got := relFromRoot(root, filepath.Join(root, "sub", "x.go")); got != "sub/x.go" {
		t.Fatalf("relFromRoot 不符: %q", got)
	}
	if got := relFromRoot(root, "/tmp/other/x.go"); got != "" {
		t.Fatalf("越界应返回空，实际 %q", got)
	}
	if !isBinaryDiff("Binary files a/x and b/x differ") {
		t.Fatal("应识别二进制 diff")
	}
	if isBinaryDiff("--- a/x\n+++ b/x\n") {
		t.Fatal("文本 diff 不应判为二进制")
	}
}
