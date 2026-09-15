//go:build windows

package main

import (
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32               = windows.NewLazySystemDLL("user32.dll")
	procEnumWindows      = user32.NewProc("EnumWindows")
	procIsWindowVisible  = user32.NewProc("IsWindowVisible")
	procGetWindowThreadProcessId = user32.NewProc("GetWindowThreadProcessId")
)

// countVisibleWindowsForPID 统计某进程拥有的可见顶层窗口数（0 = 没有弹窗）。
func countVisibleWindowsForPID(pid uint32) int {
	count := 0
	cb := windows.NewCallback(func(hwnd uintptr, lparam uintptr) uintptr {
		var wpid uint32
		procGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&wpid)))
		if wpid != pid {
			return 1 // 继续枚举
		}
		vis, _, _ := procIsWindowVisible.Call(hwnd)
		if vis != 0 {
			count++
		}
		return 1
	})
	procEnumWindows.Call(cb, 0)
	return count
}

// TestHiddenCmdCreatesNoWindow 回归保护：产品代码启动的外部命令**不得弹出控制台窗口**。
//
// 背景：本应用是 GUI 宿主，启动控制台程序（powershell/node/git…）时若不设置
// CREATE_NO_WINDOW + HideWindow，Windows 会为每个子进程弹出黑窗——曾因「子代理」面板
// 每 8s 枚举进程而不断闪现（2026-09-14 事故）。此测试真实启动一个长命 powershell，
// 用 EnumWindows 断言它没有任何可见顶层窗口。
func TestHiddenCmdCreatesNoWindow(t *testing.T) {
	cmd := hiddenCmd("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", "Start-Sleep -Seconds 8")
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot start powershell: %v", err)
	}
	defer func() { _ = cmd.Process.Kill() }()

	// 等控制台窗口（若有）出现
	time.Sleep(1500 * time.Millisecond)

	pid := uint32(cmd.Process.Pid)
	if n := countVisibleWindowsForPID(pid); n != 0 {
		t.Fatalf("hidden command produced %d visible window(s) (pid=%d): CREATE_NO_WINDOW/HideWindow not applied", n, pid)
	}

	// 对照组：不加隐藏标志时应能看到窗口——若对照也为 0，说明检测方法本身失灵，需人工核对
	plain := hiddenCmd("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", "Start-Sleep -Seconds 3")
	plain.SysProcAttr = nil // 故意去掉隐藏标志
	if err := plain.Start(); err == nil {
		time.Sleep(1200 * time.Millisecond)
		n := countVisibleWindowsForPID(uint32(plain.Process.Pid))
		t.Logf("control (no hide flags) visible windows = %d", n)
		_ = plain.Process.Kill()
		if n == 0 {
			t.Skip("control group also showed 0 windows — detector may not work in this environment (skipping strict assertion)")
		}
	}
}
