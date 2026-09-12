package main

// app_dsh_cli.go — DSH CLI 调用方式统一探测（含"源码/包布局 bin.js + node"形态）。
//
// 背景：dsh 有三种落地形态，之前 dshCliPath() 只覆盖前两种，导致源码运行 DSH 的机器上
// "dsh CLI not found" → 默认附加插件注入失败（实测缺陷）。
//
//   1. Desktop 内置：%APPDATA%\io.github.hairyf.deepseek-harness-desktop\dependencies\dsh\...\dsh.cmd
//   2. npm 全局 / PATH：dsh.cmd 可直接执行
//   3. 源码或本地包布局：<root>/apps/cli/lib/bin.js（需 node 执行）
//      —— 例如 ~/deepseek-harness（源码运行）、任意含 apps/cli/lib/bin.js 的仓库
//
// dshInvocation 返回 (exe, prefixArgs)；runDshPlugin 据此构造命令，两种形态统一。

import (
	"os"
	"os/exec"
	"path/filepath"
)

// findNodeExe 定位 node 可执行文件（PATH 优先，其次官方安装位置）。返回 "" 表示不可用。
func findNodeExe() string {
	if p, err := exec.LookPath("node"); err == nil && p != "" {
		return p
	}
	seen := map[string]bool{}
	cands := []string{
		filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "nodejs", "node.exe"),
		filepath.Join(os.Getenv("ProgramFiles"), "nodejs", "node.exe"),
		filepath.Join(os.Getenv("ProgramFiles(x86)"), "nodejs", "node.exe"),
		filepath.Join(os.Getenv("APPDATA"), "npm", "node.exe"),
	}
	for _, c := range cands {
		if c == "" || seen[c] {
			continue
		}
		seen[c] = true
		if fi, err := os.Stat(c); err == nil && !fi.IsDir() {
			return c
		}
	}
	return ""
}

// dshBinJsCandidates 返回可能的 dsh CLI 入口 bin.js 路径（源码/包布局）。
func dshBinJsCandidates() []string {
	out := []string{}
	home, _ := os.UserHomeDir()
	if home != "" {
		// 源码仓库（本机开发/源码运行）
		out = append(out, filepath.Join(home, "deepseek-harness", "apps", "cli", "lib", "bin.js"))
		// 用户级安装的包布局
		out = append(out, filepath.Join(home, ".dsh", "profiles", "web", "node_modules", "@deepseek-ai", "dsh", "lib", "bin.js"))
	}
	// Desktop 内置的包布局（除 .cmd 外还有 bin.js）
	out = append(out, filepath.Join(os.Getenv("APPDATA"), "io.github.hairyf.deepseek-harness-desktop", "dependencies", "dsh", "node_modules", "@deepseek-ai", "dsh", "lib", "bin.js"))
	return out
}

// dshInvocation 统一返回执行 dsh 的方式：
//   exe = 可执行文件（dsh.cmd / node.exe），prefix = 前置参数（node 形态时为 bin.js 路径）。
// ok=false 表示本机无可用 dsh（保持调用方原有兜底）。
func dshInvocation() (exe string, prefix []string, ok bool) {
	// 1/2. 可直接执行的 dsh 命令（Desktop 内置 → PATH → npm 全局）
	if cli := dshCliPath(); cli != "" {
		return cli, nil, true
	}
	// 3. node + bin.js 形态（源码/包布局）
	node := findNodeExe()
	if node != "" {
		seen := map[string]bool{}
		for _, bin := range dshBinJsCandidates() {
			if bin == "" || seen[bin] {
				continue
			}
			seen[bin] = true
			if fi, err := os.Stat(bin); err == nil && !fi.IsDir() {
				return node, []string{bin}, true
			}
		}
	}
	return "", nil, false
}

// dshAvailable 报告本机是否存在任何可用的 dsh 调用方式。
func dshAvailable() bool {
	_, _, ok := dshInvocation()
	return ok
}
