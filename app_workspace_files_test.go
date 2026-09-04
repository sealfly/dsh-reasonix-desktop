package main

// 原生文件栏桥的纯函数单测（resolveRelToRoot / readTextPreview / binary 判定）。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveRelToRoot(t *testing.T) {
	root := `C:\work\proj`
	base := filepath.FromSlash("C:/work/proj")
	cases := []struct {
		rel  string
		ok   bool
		want string
	}{
		{"", true, base},
		{"src/main.go", true, filepath.Join(base, "src", "main.go")},
		{"src/main.go", true, filepath.Join(base, "src", "main.go")},
		{"../evil.txt", false, ""},          // 逃逸根
		{"a/../../evil.txt", false, ""},     // 绕行逃逸
		{"src/../README.md", true, filepath.Join(base, "README.md")}, // 内部归一
	}
	for _, c := range cases {
		got, ok := resolveRelToRoot(root, c.rel)
		if ok != c.ok {
			t.Errorf("rel=%q ok=%v want %v", c.rel, ok, c.ok)
			continue
		}
		if c.ok && got != c.want {
			t.Errorf("rel=%q got=%q want %q", c.rel, got, c.want)
		}
	}
	// 绝对路径原样
	abs, ok := resolveRelToRoot(root, `D:\other\file.txt`)
	if !ok || abs != filepath.FromSlash("D:/other/file.txt") {
		t.Errorf("absolute: got (%q,%v)", abs, ok)
	}
	// 空 root 仅绝对
	if _, ok := resolveRelToRoot("", "a/b"); ok {
		t.Error("empty root + relative should fail")
	}
	if _, ok := resolveRelToRoot("", filepath.FromSlash("C:/x/y")); !ok {
		t.Error("empty root + absolute should pass")
	}
}

func TestReadTextPreviewUTF8(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.md")
	os.WriteFile(p, []byte("# 标题\n正文 content\n"), 0o644)
	body, trunc := readTextPreview(p, 1<<20)
	if trunc || !strings.Contains(body, "# 标题") {
		t.Errorf("utf8: trunc=%v body=%q", trunc, body)
	}
}

func TestReadTextPreviewTruncated(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "big.txt")
	content := strings.Repeat("x", 1000)
	os.WriteFile(p, []byte(content), 0o644)
	body, trunc := readTextPreview(p, 100)
	if !trunc {
		t.Error("expected truncated")
	}
	if len(body) > 100 || !strings.Contains(body, "x") {
		t.Errorf("truncated body len=%d", len(body))
	}
}

func TestBinaryExtsClassify(t *testing.T) {
	for ext := range binaryExts {
		if textExts[ext] {
			t.Errorf("conflict: %s in both text and binary", ext)
		}
	}
	// spot checks
	if !binaryExts[".docx"] || !binaryExts[".xlsx"] || !binaryExts[".pdf"] || !binaryExts[".png"] {
		t.Error("office/media should be binary")
	}
	if !textExts[".md"] || !textExts[".go"] || !textExts[".txt"] {
		t.Error("code/text should be text")
	}
}

func TestListDirSortingAndSkip(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b"), 0o644)
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644)
	os.MkdirAll(filepath.Join(dir, "node_modules"), 0o755) // 跳过
	os.MkdirAll(filepath.Join(dir, "src"), 0o755)
	os.MkdirAll(filepath.Join(dir, ".git"), 0o755) // 跳过
	a := &App{}
	entries := a.ListDirForTab("", dir)
	// tabID 未知 + rel 绝对 → resolveRelToRoot 原样接受
	if len(entries) != 3 {
		t.Fatalf("want 3 entries got %d: %+v", len(entries), entries)
	}
	wantOrder := []string{"src", "a.txt", "b.txt"} // dirs 前、各自排序
	for i, w := range wantOrder {
		if entries[i].Name != w {
			t.Errorf("entry[%d]=%q want %q", i, entries[i].Name, w)
		}
	}
	if !entries[0].IsDir || entries[1].IsDir {
		t.Error("dirs must come first with isDir=true")
	}
}
