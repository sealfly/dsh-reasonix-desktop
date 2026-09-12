package main

// 右侧"改动"栏的 git 桥实现（官方 Reasonix desktop/app.go 契约，从 WorkspacePanel 打包产物提取）：
//   WorkspaceChanges(tabId) ->
//     {files:[{path,oldPath?,gitStatus?,latestPrompt?,canSessionRevert?,sources:[]}],
//      gitAvailable, gitErr?, gitBranch?}
//   WorkspaceChangeDetail(tabId, path) ->
//     {diff, source:"git", added, removed, binary?, truncated?, err?}
//   WorkspaceGitHistory(tabId, path) -> [{hash,message,author,date}]
//   WorkspaceGitCommitDetail(tabId, commit, path) -> {diff, hash}
//
// gitStatus 用 porcelain v1 的 XY 归一码：?? / M / A / D / R / C / U…
// 前端 workspaceGitStatusLabel 按 ??→R→C→D→A→M 顺序 includes 匹配后本地化。
// 工作区根 = 会话（tabId）的 cwd，与 app_workspace_files.go 同一来源；
// 仓库相对路径转换回工作区根相对路径，越界项丢弃。

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	workspaceGitTimeout      = 10 * time.Second
	workspaceChangesLimit    = 800
	workspaceGitHistoryLimit = 60
	workspaceDiffMaxBytes    = 512 * 1024
	workspaceGitBlobReadMax  = 2 * 1024 * 1024
)

// workspaceChangeEntry 改动条目（前端 WorkspaceChanges.files 元素）。
type workspaceChangeEntry struct {
	Path             string   `json:"path"`
	OldPath          string   `json:"oldPath,omitempty"`
	GitStatus        string   `json:"gitStatus,omitempty"`
	LatestPrompt     string   `json:"latestPrompt,omitempty"`
	CanSessionRevert bool     `json:"canSessionRevert,omitempty"`
	Sources          []string `json:"sources"`
}

var (
	gitExeOnce sync.Once
	gitExePath string
)

// gitExecutable 定位 git（PATH 优先，其次常见安装位置）。空 = 不可用。
func gitExecutable() string {
	gitExeOnce.Do(func() {
		if p, err := exec.LookPath("git"); err == nil {
			gitExePath = p
			return
		}
		cands := []string{
			filepath.Join(os.Getenv("ProgramFiles"), "Git", "cmd", "git.exe"),
			filepath.Join(os.Getenv("ProgramFiles(x86)"), "Git", "cmd", "git.exe"),
			filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "Git", "cmd", "git.exe"),
			filepath.Join(os.Getenv("ProgramW6432"), "Git", "cmd", "git.exe"),
		}
		for _, c := range cands {
			if c == "" || c == "Git" {
				continue
			}
			if st, err := os.Stat(c); err == nil && !st.IsDir() {
				gitExePath = c
				return
			}
		}
	})
	return gitExePath
}

// runWorkspaceGit 在 root 下执行 git，返回 stdout；err 为人类可读的 stderr。
func (a *App) runWorkspaceGit(root string, args ...string) (string, error) {
	if strings.TrimSpace(root) == "" {
		return "", errors.New("workspace root unknown")
	}
	exe := gitExecutable()
	if exe == "" {
		return "", errors.New("git not found")
	}
	ctx, cancel := context.WithTimeout(context.Background(), workspaceGitTimeout)
	defer cancel()
	full := make([]string, 0, len(args)+3)
	full = append(full, "--no-pager", "-c", "core.quotepath=false")
	full = append(full, args...)
	cmd := exec.CommandContext(ctx, exe, full...)
	cmd.Dir = root
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0")
	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return out.String(), errors.New("git timeout")
	}
	if err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		return out.String(), errors.New(msg)
	}
	return out.String(), nil
}

// gitRepoPrefix root 相对仓库根的前缀（仓库根为空串）；非仓库返回 ok=false。
func (a *App) gitRepoPrefix(root string) (string, bool) {
	out, err := a.runWorkspaceGit(root, "rev-parse", "--show-prefix")
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(strings.SplitN(out, "\n", 2)[0]), true
}

// WorkspaceChanges 工作区改动 + 分支（前端右侧"改动"栏）。
func (a *App) WorkspaceChanges(tabID string) map[string]any {
	root := a.workspaceRootForTabID(tabID)
	if root == "" {
		return map[string]any{"files": []any{}, "gitAvailable": false, "gitBranch": "", "gitErr": "workspace root unknown for tab"}
	}
	return a.workspaceChangesAt(root)
}

// workspaceChangesAt WorkspaceChanges 的核心（root 已解析，便于单测）。
func (a *App) workspaceChangesAt(root string) map[string]any {
	res := map[string]any{"files": []any{}, "gitAvailable": false, "gitBranch": ""}
	if gitExecutable() == "" {
		res["gitErr"] = "git not found"
		return res
	}
	out, err := a.runWorkspaceGit(root, "status", "--porcelain=v1", "-z", "-b", "--untracked-files=all")
	if err != nil {
		// 非 git 仓库：给出明确原因（前端据此显示"无法获取 git 信息"提示）。
		// 不能静默返回空——那样用户只会看到改动栏空白，误以为功能坏了。
		if strings.Contains(err.Error(), "not a git repository") {
			res["gitErr"] = "当前工作区不是 git 仓库: " + root
			return res
		}
		res["gitErr"] = err.Error()
		return res
	}
	// 用 --show-prefix（纯字符串前缀）把仓库相对路径转成工作区根相对路径。
	// 不用 --show-toplevel + filepath.Rel：Windows 上 git 可能返回短名（8.3）
	// 或不同大小写，Rel 失败会导致改动列表整片被丢弃。
	prefix, pok := a.gitRepoPrefix(root)
	if !pok {
		prefix = ""
	}
	branch, entries := parseWorkspacePorcelain(out)
	if branch == "" || branch == "HEAD" {
		if b, e := a.runWorkspaceGit(root, "rev-parse", "--abbrev-ref", "HEAD"); e == nil {
			b = strings.TrimSpace(b)
			if b != "" && b != "HEAD" {
				branch = b
			}
		}
	}
	files := make([]any, 0, len(entries))
	for _, e := range entries {
		e.Path = repoPathToRoot(prefix, e.Path)
		if e.Path == "" {
			continue
		}
		if e.OldPath != "" {
			e.OldPath = repoPathToRoot(prefix, e.OldPath)
		}
		files = append(files, e)
	}
	if len(files) > workspaceChangesLimit {
		files = files[:workspaceChangesLimit]
	}
	res["files"] = files
	res["gitAvailable"] = true
	res["gitBranch"] = branch
	return res
}

// WorkspaceChangeDetail 单个文件的当前改动（未暂存优先，其次已暂存，最后合成未跟踪新文件 diff）。
func (a *App) WorkspaceChangeDetail(tabID, path string) map[string]any {
	root := a.workspaceRootForTabID(tabID)
	if root == "" {
		return map[string]any{"err": "workspace root unknown for tab"}
	}
	abs, ok := a.resolveWorkspacePath(tabID, path)
	if !ok {
		return map[string]any{"err": "path outside workspace"}
	}
	rel := relFromRoot(root, abs)
	if rel == "" {
		return map[string]any{"err": "path outside workspace"}
	}
	return a.workspaceChangeDetailAt(root, abs, rel)
}

// workspaceChangeDetailAt WorkspaceChangeDetail 的核心。
func (a *App) workspaceChangeDetailAt(root, abs, rel string) map[string]any {
	diff, err := a.runWorkspaceGit(root, "diff", "--no-color", "--unified=3", "--", rel)
	if err != nil {
		if strings.Contains(err.Error(), "not a git repository") {
			return map[string]any{"diff": "", "source": "git", "added": 0, "removed": 0}
		}
		return map[string]any{"err": err.Error()}
	}
	if strings.TrimSpace(diff) == "" {
		if d2, e2 := a.runWorkspaceGit(root, "diff", "--no-color", "--cached", "--unified=3", "--", rel); e2 == nil {
			diff = d2
		}
	}
	if strings.TrimSpace(diff) == "" {
		diff = synthUntrackedDiff(abs, rel)
	}
	binary := isBinaryDiff(diff)
	added, removed := countDiffStats(diff)
	truncated := false
	if len(diff) > workspaceDiffMaxBytes {
		diff = diff[:workspaceDiffMaxBytes]
		truncated = true
	}
	out := map[string]any{
		"diff":    diff,
		"source":  "git",
		"added":   added,
		"removed": removed,
	}
	if binary {
		out["binary"] = true
	}
	if truncated {
		out["truncated"] = true
	}
	return out
}

// WorkspaceGitHistory 提交历史（path 为空 = 整个仓库；否则该文件的历史）。
func (a *App) WorkspaceGitHistory(tabID, path string) []any {
	root := a.workspaceRootForTabID(tabID)
	if root == "" {
		return []any{}
	}
	return a.workspaceGitHistoryAt(root, repoRelForTab(a, tabID, root, path))
}

// workspaceGitHistoryAt WorkspaceGitHistory 的核心。
func (a *App) workspaceGitHistoryAt(root, rel string) []any {
	items := []any{}
	args := []string{
		"log", "--no-color", "-n", strconv.Itoa(workspaceGitHistoryLimit),
		"--pretty=format:%H%x1f%an%x1f%aI%x1f%s%x1e",
	}
	if rel != "" {
		args = append(args, "--", rel)
	}
	out, err := a.runWorkspaceGit(root, args...)
	if err != nil {
		return items
	}
	for _, rec := range strings.Split(out, "\x1e") {
		rec = strings.TrimLeft(rec, "\r\n")
		if strings.TrimSpace(rec) == "" {
			continue
		}
		f := strings.SplitN(rec, "\x1f", 4)
		if len(f) < 4 {
			continue
		}
		items = append(items, map[string]any{
			"hash":    strings.TrimSpace(f[0]),
			"author":  f[1],
			"date":    f[2],
			"message": f[3],
		})
	}
	return items
}

// WorkspaceGitCommitDetail 单次提交的补丁（path 为空 = 整个提交）。
func (a *App) WorkspaceGitCommitDetail(tabID, commit, path string) map[string]any {
	commit = strings.TrimSpace(commit)
	if commit == "" || strings.HasPrefix(commit, "-") {
		return map[string]any{}
	}
	root := a.workspaceRootForTabID(tabID)
	if root == "" {
		return map[string]any{}
	}
	return a.workspaceGitCommitDetailAt(root, commit, repoRelForTab(a, tabID, root, path))
}

// workspaceGitCommitDetailAt WorkspaceGitCommitDetail 的核心。
func (a *App) workspaceGitCommitDetailAt(root, commit, rel string) map[string]any {
	args := []string{"show", "--no-color", "--patch", "--format=medium", commit}
	if rel != "" {
		args = append(args, "--", rel)
	}
	out, err := a.runWorkspaceGit(root, args...)
	if err != nil {
		return map[string]any{}
	}
	return map[string]any{"diff": out, "hash": commit}
}

// repoRelForTab (tabID, 前端相对路径) → 工作区根相对路径；空/越界返回 ""。
func repoRelForTab(a *App, tabID, root, path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	abs, ok := a.resolveWorkspacePath(tabID, path)
	if !ok {
		return ""
	}
	return relFromRoot(root, abs)
}

// parseWorkspacePorcelain 解析 git status --porcelain=v1 -z -b 输出。
func parseWorkspacePorcelain(out string) (string, []workspaceChangeEntry) {
	branch := ""
	entries := []workspaceChangeEntry{}
	parts := strings.Split(out, "\x00")
	for i := 0; i < len(parts); i++ {
		p := parts[i]
		if p == "" {
			continue
		}
		if strings.HasPrefix(p, "## ") {
			branch = parseGitBranchLine(strings.TrimPrefix(p, "## "))
			continue
		}
		if len(p) < 3 {
			continue
		}
		x, y := p[0], p[1]
		rest := strings.TrimPrefix(p[2:], " ")
		if rest == "" {
			continue
		}
		e := workspaceChangeEntry{
			Path:      rest,
			GitStatus: normalizeGitStatus(x, y),
			Sources:   []string{"git"},
		}
		if x == 'R' || x == 'C' || y == 'R' || y == 'C' {
			if i+1 < len(parts) && parts[i+1] != "" {
				e.OldPath = parts[i+1]
				i++
			}
		}
		entries = append(entries, e)
	}
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return branch, entries
}

// parseGitBranchLine 解析 "## " 后的分支行。
func parseGitBranchLine(h string) string {
	h = strings.TrimSpace(h)
	if i := strings.Index(h, "..."); i >= 0 {
		h = h[:i]
	}
	if i := strings.Index(h, " ["); i >= 0 {
		h = h[:i]
	}
	h = strings.TrimSpace(h)
	for _, pre := range []string{"No commits yet on ", "Initial commit on "} {
		h = strings.TrimPrefix(h, pre)
	}
	h = strings.TrimSpace(h)
	if h == "" || strings.HasPrefix(h, "HEAD") {
		return ""
	}
	return h
}

// normalizeGitStatus porcelain XY → 前端可本地化的单码（未合并冲突保留双码）。
func normalizeGitStatus(x, y byte) string {
	if x == '?' || y == '?' {
		return "??"
	}
	if x == 'U' || y == 'U' || (x == 'A' && y == 'A') || (x == 'D' && y == 'D') ||
		(x == 'A' && y == 'D') || (x == 'D' && y == 'A') {
		return string([]byte{x, y})
	}
	if x != ' ' {
		return string(x)
	}
	if y != ' ' {
		return string(y)
	}
	return ""
}

// repoPathToRoot git 输出的仓库相对路径 → 工作区根相对路径（不在根内则丢弃）。
// prefix 来自 git rev-parse --show-prefix（仓库根为空串，子目录形如 "sub/dir/"）。
func repoPathToRoot(prefix, p string) string {
	p = filepath.ToSlash(strings.TrimSpace(p))
	if p == "" {
		return ""
	}
	prefix = filepath.ToSlash(strings.TrimSpace(prefix))
	if prefix == "" || prefix == "/" {
		return p
	}
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	if !strings.HasPrefix(p, prefix) {
		return ""
	}
	return strings.TrimPrefix(p, prefix)
}

// relFromRoot abs 相对 root 的斜杠路径；root 外或等于 root 时返回 ""。
func relFromRoot(root, abs string) string {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(abs))
	if err != nil || rel == "." {
		return ""
	}
	rel = filepath.ToSlash(rel)
	if strings.HasPrefix(rel, "../") {
		return ""
	}
	return rel
}

// synthUntrackedDiff 为未跟踪（或空 diff）文件合成 unified diff，便于前端 diff 渲染。
func synthUntrackedDiff(abs, rel string) string {
	f, err := os.Open(abs)
	if err != nil {
		return ""
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.IsDir() {
		return ""
	}
	buf := make([]byte, workspaceGitBlobReadMax)
	n, _ := f.Read(buf)
	data := buf[:n]
	if looksBinaryBytes(data) {
		return "diff --git a/" + rel + " b/" + rel + "\n" +
			"new file\nBinary files /dev/null and b/" + rel + " differ\n"
	}
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	lines := strings.Split(text, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	var sb strings.Builder
	sb.WriteString("diff --git a/" + rel + " b/" + rel + "\n")
	sb.WriteString("new file\n--- /dev/null\n+++ b/" + rel + "\n")
	sb.WriteString(fmt.Sprintf("@@ -0,0 +1,%d @@\n", len(lines)))
	for _, l := range lines {
		sb.WriteString("+" + l + "\n")
	}
	return sb.String()
}

// looksBinaryBytes NUL 字节启发式（与文件预览一致的判据）。
func looksBinaryBytes(b []byte) bool {
	limit := len(b)
	if limit > 8000 {
		limit = 8000
	}
	for i := 0; i < limit; i++ {
		if b[i] == 0 {
			return true
		}
	}
	return false
}

// isBinaryDiff git 对二进制文件的固定措辞。
func isBinaryDiff(diff string) bool {
	return strings.Contains(diff, "Binary files ") || strings.Contains(diff, "GIT binary patch")
}

// countDiffStats 统计 diff 的新增/删除行数（排除 ---/+++ 头）。
func countDiffStats(diff string) (added, removed int) {
	for _, l := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(l, "+++"), strings.HasPrefix(l, "---"):
			continue
		case strings.HasPrefix(l, "+"):
			added++
		case strings.HasPrefix(l, "-"):
			removed++
		}
	}
	return added, removed
}
