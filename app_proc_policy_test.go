package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// TestNoBareExecCommand 回归保护（可在任意环境运行）：
// 产品代码启动外部命令**必须**走 hiddenCmd / hiddenCmdContext（带 CREATE_NO_WINDOW + HideWindow），
// 否则 GUI 宿主会为子进程弹出控制台黑窗（2026-09-14 事故：子代理面板每 8s 枚举进程 → 黑窗狂闪）。
//
// 白名单（合理例外）：
//   - proc_windows.go / proc_other.go           —— helper 自身的实现
//   - app_workspace_open_windows.go / _other.go —— 打开外部程序（用户主动行为，需可见）
//   - terminal.go                               —— 终端面板功能本身（用户主动开终端）
//   - *_test.go                                 —— 测试代码
var bareExecAllowed = map[string]bool{
	"proc_windows.go":            true,
	"proc_other.go":              true,
	"app_workspace_open_windows.go": true,
	"app_workspace_open_other.go":   true,
	"terminal.go":                true,
}

var bareExecRe = regexp.MustCompile(`\bexec\.Command(Context)?\(`)

func TestNoBareExecCommand(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	var violations []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") {
			continue
		}
		if strings.HasSuffix(name, "_test.go") || bareExecAllowed[name] {
			continue
		}
		data, err := os.ReadFile(filepath.Clean(name))
		if err != nil {
			continue
		}
		for i, line := range strings.Split(string(data), "\n") {
			if bareExecRe.MatchString(line) {
				violations = append(violations, name+":"+strconv.Itoa(i+1)+": "+strings.TrimSpace(line))
			}
		}
	}
	if len(violations) > 0 {
		t.Fatalf("发现裸 exec.Command（必须改用 hiddenCmd/hiddenCmdContext，否则会弹出控制台黑窗）:\n  %s",
			strings.Join(violations, "\n  "))
	}
}

