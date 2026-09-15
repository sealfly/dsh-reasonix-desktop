//go:build !windows

package main

import (
	"context"
	"os/exec"
)

// 非 Windows 平台无需隐藏控制台窗口（保留同名 API 以便跨平台编译）。
func applyHiddenWindow(cmd *exec.Cmd) *exec.Cmd { return cmd }

func hiddenCmd(name string, args ...string) *exec.Cmd {
	return exec.Command(name, args...)
}

func hiddenCmdContext(ctx context.Context, name string, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, name, args...)
}
