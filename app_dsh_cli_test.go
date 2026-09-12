package main

// app_dsh_cli_test.go — dsh 调用方式统一探测测试（源码布局形态）。

import (
	"os"
	"path/filepath"
	"testing"
)

// findNodeExe 至少在本机开发环境应能找到 node。
func TestFindNodeExe(t *testing.T) {
	p := findNodeExe()
	if p == "" {
		t.Skip("本机无 node（跳过）")
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("findNodeExe 返回不存在的路径: %s", p)
	}
}

// dshInvocation：本机是源码布局时应返回 node + bin.js 形态。
func TestDshInvocationForms(t *testing.T) {
	exe, prefix, ok := dshInvocation()
	if !ok {
		t.Skip("本机无可用 dsh（跳过）")
	}
	if exe == "" {
		t.Fatal("exe 不应为空")
	}
	if _, err := os.Stat(exe); err != nil {
		t.Fatalf("exe 不存在: %s", exe)
	}
	// node 形态：prefix 必须是存在的 bin.js
	if len(prefix) > 0 {
		bin := prefix[0]
		if filepath.Base(bin) != "bin.js" {
			t.Fatalf("node 形态 prefix 首项应为 bin.js: %s", bin)
		}
		if _, err := os.Stat(bin); err != nil {
			t.Fatalf("bin.js 不存在: %s", bin)
		}
		t.Logf("node 形态: %s + %s", exe, bin)
	} else {
		t.Logf("CLI 形态: %s", exe)
	}
}

// dshAvailable 与 dshInvocation 一致。
func TestDshAvailableConsistent(t *testing.T) {
	_, _, ok := dshInvocation()
	if dshAvailable() != ok {
		t.Fatal("dshAvailable 与 dshInvocation 不一致")
	}
}

// dshBinJsCandidates 含源码布局路径。
func TestDshBinJsCandidates(t *testing.T) {
	cands := dshBinJsCandidates()
	if len(cands) == 0 {
		t.Fatal("候选不应为空")
	}
	home, _ := os.UserHomeDir()
	want := filepath.Join(home, "deepseek-harness", "apps", "cli", "lib", "bin.js")
	found := false
	for _, c := range cands {
		if c == want {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("候选应含源码布局路径 %s: %v", want, cands)
	}
}
