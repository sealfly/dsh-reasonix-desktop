//go:build !windows

package main

// 非 Windows 平台兜底：xdg-open / open 打开，文件管理器显示尽力而为。

import (
	"os/exec"
)

func execCommand(name string, args ...string) *exec.Cmd {
	return exec.Command(name, args...)
}

// openSystemDefault 非 Windows：xdg-open（Linux）/ open（macOS）。
func openSystemDefault(path string) error {
	var cmd *exec.Cmd
	if _, err := exec.LookPath("xdg-open"); err == nil {
		cmd = exec.Command("xdg-open", path)
	} else {
		cmd = exec.Command("open", path)
	}
	return cmd.Start()
}

// revealInExplorer 非 Windows：文件管理器定位（Linux 尽力打开所在目录）。
func revealInExplorer(path string) error {
	return openSystemDefault(path)
}
