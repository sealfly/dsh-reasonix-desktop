package main

// app_workspace_write_test.go — 本地文件编辑保存(WriteFileForTab)测试。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 写回文本文件成功，内容可读回。
func TestWriteFileForTabRoundtrip(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "hello.go")
	orig := "package main\n\nfunc main() {}\n"
	if err := os.WriteFile(target, []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}
	// 用绝对路径(无会话, root 空也接受绝对路径)
	a := &App{}
	res := a.WriteFile(target, "package main\n\nfunc main() { println(1) }\n")
	if ok, _ := res["ok"].(bool); !ok {
		t.Fatalf("WriteFile 失败: %v", res)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "println(1)") {
		t.Fatalf("写回内容不符: %q", string(got))
	}
	if size, _ := res["size"].(int64); size != int64(len(got)) {
		t.Fatalf("size 不符: %v", res)
	}
}

// 相对路径防穿越：../ 逃逸被拒。
func TestWriteFileForTabEscapeBlocked(t *testing.T) {
	root := t.TempDir()
	// 构造一个 tab(让 workspaceRootForTabID 能解析)——直接调 resolveRelToRoot 测
	// 更简单：用 resolveWorkspacePath 需要 Tabs() 有该 tab；改用 resolveRelToRoot 语义验证
	// WriteFileForTab 内部走 resolveWorkspacePath；root 空 + 相对路径 → 拒绝
	a := &App{}
	res := a.WriteFileForTab("no-such-tab", "../evil.txt", "x")
	if ok, _ := res["ok"].(bool); ok {
		t.Fatal("相对路径+未知会话应拒绝")
	}
	// 有 root 时 ../ 逃逸应被 Clean 拒绝(纯函数验证)
	if _, ok := resolveRelToRoot(root, "../outside.txt"); ok {
		t.Fatal("../ 应被拒绝")
	}
	if _, ok := resolveRelToRoot(root, "sub/dir/f.go"); !ok {
		t.Fatal("正常相对路径应通过")
	}
}

// 二进制扩展名拒绝写。
func TestWriteFileForTabBinaryRejected(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "img.png")
	if err := os.WriteFile(bin, []byte{0x89, 0x50, 0x4E, 0x47}, 0o644); err != nil {
		t.Fatal(err)
	}
	a := &App{}
	res := a.WriteFile(bin, "not really png")
	if ok, _ := res["ok"].(bool); ok {
		t.Fatal("二进制文件应拒绝写入")
	}
}

// 目录拒绝写。
func TestWriteFileForTabDirRejected(t *testing.T) {
	root := t.TempDir()
	a := &App{}
	res := a.WriteFile(root, "content")
	if ok, _ := res["ok"].(bool); ok {
		t.Fatal("目录应拒绝写入")
	}
}

// 新建文件(不存在 → 创建)。
func TestWriteFileForTabCreateNew(t *testing.T) {
	root := t.TempDir()
	a := &App{}
	newFile := filepath.Join(root, "notes.md")
	res := a.WriteFile(newFile, "# hi\n")
	if ok, _ := res["ok"].(bool); !ok {
		t.Fatalf("新建文件失败: %v", res)
	}
	if _, err := os.Stat(newFile); err != nil {
		t.Fatalf("文件应已创建: %v", err)
	}
}
