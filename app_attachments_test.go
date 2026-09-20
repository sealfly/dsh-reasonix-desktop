package main

// app_attachments_test.go — 粘贴图片链路与拖放附件的真机契约测试。
//
// 背景：这几个方法曾是参数错位的占位桩（SavePastedImage 收两个字符串形参、前端只传 dataURL；
// AttachmentDataURL 零参而前端传路径），粘贴图片整条链路静默失败。这里锁住行为。

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// pngDataURL 造一个最小合法 PNG 的 data URL。
func pngDataURL(payload string) string {
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte(payload))
}

func TestSavePastedImageWritesFileAndRoundTrips(t *testing.T) {
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)

	a := &App{}
	path := a.SavePastedImage(pngDataURL("PNGDATA"))
	if path == "" {
		t.Fatal("应返回落盘路径")
	}
	if filepath.Ext(path) != ".png" {
		t.Fatalf("扩展名应由 MIME 推导为 .png，实际 %s", path)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("文件应真实存在: %v", err)
	}
	if string(raw) != "PNGDATA" {
		t.Fatalf("内容不一致: %q", string(raw))
	}
	// 落在 ~/.reasonix/attachments 下
	if !strings.Contains(path, filepath.Join(".reasonix", "attachments")) {
		t.Fatalf("应保存在附件目录，实际 %s", path)
	}
	// 往返：AttachmentDataURL(path) 应还原成 data URL（前端用它做预览）
	back := a.AttachmentDataURL(path)
	if !strings.HasPrefix(back, "data:image/png;base64,") {
		t.Fatalf("应还原为 image/png 的 data URL，实际 %q", back)
	}
	want := base64.StdEncoding.EncodeToString([]byte("PNGDATA"))
	if !strings.HasSuffix(back, want) {
		t.Fatalf("data URL 载荷不一致：%q", back)
	}
}

func TestSavePastedImageRejectsBadInput(t *testing.T) {
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	a := &App{}
	for _, bad := range []string{"", "not-a-data-url", "data:image/png,plain-not-base64"} {
		if got := a.SavePastedImage(bad); got != "" {
			t.Fatalf("非法输入 %q 应返回空串，实际 %q", bad, got)
		}
	}
}

func TestSavePastedImageSupportsJpegExtension(t *testing.T) {
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	a := &App{}
	url := "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString([]byte("JPGDATA"))
	path := a.SavePastedImage(url)
	if !strings.HasSuffix(path, ".jpg") && !strings.HasSuffix(path, ".jpeg") {
		t.Fatalf("jpeg 应推导出 .jpg/.jpeg，实际 %s", path)
	}
}

func TestAttachmentDataURLMissingFileIsEmpty(t *testing.T) {
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	a := &App{}
	if got := a.AttachmentDataURL(filepath.Join(home, "nope.png")); got != "" {
		t.Fatalf("文件不存在应返回空串，实际 %q", got)
	}
	if got := a.AttachmentDataURL(""); got != "" {
		t.Fatal("空路径应返回空串")
	}
}

func TestAttachDroppedDescribesFileAndDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	a := &App{}

	// 文件：kind=file，带预览 data URL
	file := filepath.Join(home, "note.png")
	if err := os.WriteFile(file, []byte("IMG"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := a.AttachDropped(file)
	if got["kind"] != "file" || got["isDir"] != false {
		t.Fatalf("文件应描述为 kind=file/isDir=false：%v", got)
	}
	if got["displayPath"] != "note.png" {
		t.Fatalf("displayPath 应为文件名：%v", got["displayPath"])
	}
	if s, _ := got["previewUrl"].(string); !strings.HasPrefix(s, "data:image/png;base64,") {
		t.Fatalf("图片文件应带预览 URL：%v", got["previewUrl"])
	}

	// 目录：kind=workspace（前端据此加入工作区引用）
	gotDir := a.AttachDropped(home)
	if gotDir["kind"] != "workspace" || gotDir["isDir"] != true {
		t.Fatalf("目录应描述为 kind=workspace/isDir=true：%v", gotDir)
	}

	// 不存在：不 panic，返回可用结构
	missing := a.AttachDropped(filepath.Join(home, "ghost.txt"))
	if missing["kind"] != "file" || missing["path"] == "" {
		t.Fatalf("不存在的路径也应返回结构：%v", missing)
	}
}

func TestAttachmentDataURLUsesMimeFromExtension(t *testing.T) {
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	a := &App{}
	p := filepath.Join(home, "x.json")
	if err := os.WriteFile(p, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := a.AttachmentDataURL(p)
	if !strings.HasPrefix(got, "data:application/json") {
		t.Fatalf("应按扩展名给出 MIME，实际 %q", got)
	}
}
