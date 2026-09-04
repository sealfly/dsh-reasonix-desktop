//go:build windows

package main

// Windows 平台：系统默认程序打开（ShellExecute）与资源管理器显示（explorer /select）。

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// execCommand 创建命令（explorer 用）。
func execCommand(name string, args ...string) *exec.Cmd {
	return exec.Command(name, args...)
}

// openSystemDefault 用系统关联的默认程序打开文件/文件夹。
// 目录用 "explore"（带尾分隔符，防与同名 .lnk 混淆）；文件用 "open"。
func openSystemDefault(path string) error {
	path = filepath.Clean(path)
	fi, err := os.Stat(path)
	if err != nil {
		return err
	}
	verb := "open"
	target := path
	if fi.IsDir() {
		verb = "explore"
		if !os.IsPathSeparator(target[len(target)-1]) {
			target += string(os.PathSeparator)
		}
	}
	verbPtr, err := windows.UTF16PtrFromString(verb)
	if err != nil {
		return err
	}
	filePtr, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	return windows.ShellExecute(0, verbPtr, filePtr, nil, nil, windows.SW_SHOWNORMAL)
}

// revealInExplorer 在资源管理器中定位文件/文件夹。
func revealInExplorer(path string) error {
	path = filepath.Clean(path)
	if _, err := os.Stat(path); err != nil {
		return err
	}
	explorer := "explorer.exe"
	if root := os.Getenv("SystemRoot"); root != "" {
		explorer = filepath.Join(root, "explorer.exe")
	}
	// explorer /select,<path>：参数带逗号整体传给 explorer.exe。
	cmd := execCommand(explorer, "/select,", path)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("explorer: %w", err)
	}
	return nil
}
