//go:build windows

package main

import (
	"context"
	"os/exec"
	"syscall"
)

// createNoWindow = CREATE_NO_WINDOW：子进程不分配控制台。
const createNoWindow = 0x08000000

// applyHiddenWindow 让子进程不弹出控制台窗口。
//
// ⚠️ 这是 Windows GUI 宿主的硬要求：本应用（Wails GUI）启动控制台程序（powershell /
// node / git / npm …）时，若不设置这些标志，Windows 会为子进程**弹出黑窗**。
// 曾因「子代理」面板的进程枚举每 8s 调一次 powershell 且未隐藏，导致黑窗不断闪现
// 干扰用户打字（2026-09-14 事故）。
func applyHiddenWindow(cmd *exec.Cmd) *exec.Cmd {
	if cmd == nil {
		return cmd
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNoWindow,
	}
	return cmd
}

// hiddenCmd 创建隐藏窗口的命令（产品代码里所有外部命令都应走这里）。
func hiddenCmd(name string, args ...string) *exec.Cmd {
	return applyHiddenWindow(exec.Command(name, args...))
}

// hiddenCmdContext 同 hiddenCmd，带 context（可取消）。
func hiddenCmdContext(ctx context.Context, name string, args ...string) *exec.Cmd {
	return applyHiddenWindow(exec.CommandContext(ctx, name, args...))
}
