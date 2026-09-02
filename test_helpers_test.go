package main

// test_helpers_test.go — 集成测试辅助：创建临时会话 + 自动清理。
// 集成测试每次 session.create 都会在 DSH 磁盘留一个会话目录，长期堆积成
// 大量"未命名会话"。DSH 无删除 API，这里在测试结束时删除会话目录文件，
// 防止再次堆积（DSH 重启后已删目录不再出现在列表）。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// workspaceDirName DSH 工作区目录名 = "--" + cwd 编码 + "--"
// （反斜杠→-、冒号删除；DSH 落盘用 cwd 编码做工作区目录）。
func workspaceDirName(cwd string) string {
	return "--" + strings.ReplaceAll(strings.ReplaceAll(cwd, "\\", "-"), ":", "") + "--"
}

// cleanupTestSession 删除测试会话的磁盘目录（~/.dsh/sessions/<workspace>/<sid>）。
// DSH 运行中删除不影响进程（内存态仍在），但重启后该会话不再出现。
// 根治残留：除按 sid 删外，再按 cwd 编码直接删整个工作区目录——
// DSH 会话目录是延迟落盘（cleanup 时可能尚未创建），按 cwd 删不依赖落盘时机。
func cleanupTestSession(t *testing.T, sessionID, cwd string) {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	base := filepath.Join(home, ".dsh", "sessions")
	// 1) 按 cwd 编码删工作区（根治延迟落盘残留）
	if cwd != "" {
		_ = os.RemoveAll(filepath.Join(base, workspaceDirName(cwd)))
	}
	if sessionID == "" {
		return
	}
	// 2) 按 sid 删（cwd 未知时的兜底）
	workspaces, err := os.ReadDir(base)
	if err != nil {
		return
	}
	for _, ws := range workspaces {
		if !ws.IsDir() {
			continue
		}
		wsPath := filepath.Join(base, ws.Name())
		dir := filepath.Join(wsPath, sessionID)
		if fi, err := os.Stat(dir); err == nil && fi.IsDir() {
			_ = os.RemoveAll(dir)
		}
		// 工作区目录空壳（该会话已删）一并清理，防 Temp/TestXxx/001 工作区累积
		if entries, err := os.ReadDir(wsPath); err == nil && len(entries) == 0 {
			_ = os.RemoveAll(wsPath)
		}
	}
}

// removeTempSessionDir 删除测试临时工作目录（cwd），带重试：
// DSH 可能短暂持有文件句柄导致一次删除失败，重试 3 次防残留。
func removeTempSessionDir(t *testing.T, dir string) {
	t.Helper()
	if dir == "" {
		return
	}
	for i := 0; i < 3; i++ {
		if err := os.RemoveAll(dir); err == nil {
			return
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Logf("警告: 测试临时目录未能删除: %s（DSH 句柄占用，可手动清理）", dir)
}

// createTempSession 创建临时会话并在测试结束时清理目录。
// 返回 sessionID；DSH 不可用时返回 ""（调用方决定是否跳过）。
func createTempSession(t *testing.T) string {
	t.Helper()
	a := newTestApp()
	a.dsh = NewDshClient(3080)
	if _, err := a.dsh.RPC("session.list", map[string]any{}); err != nil {
		t.Skipf("DSH 不可用，跳过: %v", err)
	}
	// 用本机测试临时目录（避免硬编码他人机器路径导致 EPERM）
	cwd := t.TempDir()
	raw, err := a.dsh.RPC("session.create", map[string]any{
		"cwd": cwd,
	})
	if err != nil {
		t.Fatalf("session.create 失败: %v", err)
	}
	var created struct {
		SessionID string `json:"sessionId"`
	}
	if err := DecodeRPC(raw, &created); err != nil || created.SessionID == "" {
		t.Fatalf("解析 session.create 失败: %v raw=%s", err, string(raw))
	}
	t.Cleanup(func() {
		// 1) 归档会话：DSH 运行时持有会话文件句柄，直接删磁盘会被占用；
		//    workspace.archiveSession 让 DSH 释放管理后再删才可靠。
		if a.dsh != nil {
			_, _ = a.dsh.RPC("workspace.archiveSession", map[string]any{"sessionId": created.SessionID})
		}
		time.Sleep(1 * time.Second)
		cleanupTestSession(t, created.SessionID, cwd)
		// DSH 会话目录延迟落盘/句柄释放需要时间，等写完再清一次
		time.Sleep(2 * time.Second)
		cleanupTestSession(t, created.SessionID, cwd)
		removeTempSessionDir(t, cwd)
	})
	return created.SessionID
}
