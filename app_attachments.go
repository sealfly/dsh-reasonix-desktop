package main

// app_attachments.go — 粘贴/拖放附件的真实实现（替换零参/参数错位的占位桩）。
//
// 真机踩到的问题：Wails 绑定按**参数个数**校验，这些方法形参与前端调用不一致时直接抛
// `error parsing arguments: received N arguments to method 'main.App.X', expected M`，
// 界面只显示一行小字，功能静默失效。这里按 1.38.2 前端的真实调用补齐并实现：
//
//	SavePastedImage(dataURL)          → 落盘到附件目录，返回**文件路径**
//	AttachmentDataURL(path)           → 读文件转成 data URL（前端用它做图片预览）
//	AttachDropped(path)               → 返回 {kind,path,isDir,displayPath,previewUrl}
//	SaveClipboardImage()              → 无参，返回空串（剪贴板位图由前端 dataURL 路径处理）
//
// 附件目录：~/.reasonix/attachments/（与其它用户数据同处 ~/.reasonix，便于清理与备份）。

import (
	"encoding/base64"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// attachmentsDir 返回附件目录（不存在则创建）。
func attachmentsDir() string {
	dir := filepath.Join(reasonixDataDir(), "attachments")
	_ = os.MkdirAll(dir, 0o755)
	return dir
}

// decodeDataURL 解析 `data:<mime>;base64,<payload>`，返回原始字节与建议扩展名。
func decodeDataURL(dataURL string) ([]byte, string, error) {
	s := strings.TrimSpace(dataURL)
	if !strings.HasPrefix(s, "data:") {
		return nil, "", fmt.Errorf("不是 data URL")
	}
	comma := strings.IndexByte(s, ',')
	if comma < 0 {
		return nil, "", fmt.Errorf("data URL 缺少逗号分隔")
	}
	header, payload := s[:comma], s[comma+1:]
	if !strings.Contains(header, "base64") {
		return nil, "", fmt.Errorf("仅支持 base64 编码的 data URL")
	}
	mimeType := strings.TrimPrefix(strings.Split(header, ";")[0], "data:")
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(payload))
	if err != nil {
		return nil, "", fmt.Errorf("base64 解码失败: %w", err)
	}
	ext := preferredImageExt(mimeType)
	if ext == "" {
		if exts, _ := mime.ExtensionsByType(mimeType); len(exts) > 0 {
			ext = exts[0]
		}
	}
	if ext == "" {
		ext = ".bin"
	}
	return raw, ext, nil
}

// preferredImageExt 给出**约定俗成**的图片扩展名。
//
// 为什么不直接用 mime.ExtensionsByType：Windows 注册表里 image/jpeg 的首选扩展名可能是 `.jfif`，
// 落盘成 paste-xxx.jfif 既反直觉、也会让部分预览组件认不出（实测踩到）。
func preferredImageExt(mimeType string) string {
	switch strings.ToLower(strings.TrimSpace(mimeType)) {
	case "image/png":
		return ".png"
	case "image/jpeg", "image/jpg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "image/bmp":
		return ".bmp"
	case "image/svg+xml":
		return ".svg"
	}
	return ""
}

// SavePastedImage 保存粘贴的图片（data URL）到附件目录，返回文件路径。
//
// 前端链路：SavePastedImage(dataURL) → path → AttachmentDataURL(path) → 预览。
// 旧实现是零参错位桩 `SavePastedImage(_name, _data string) string`：既抛参数错、也不落盘，
// 粘贴图片因此静默失败。
func (a *App) SavePastedImage(dataURL string) string {
	raw, ext, err := decodeDataURL(dataURL)
	if err != nil {
		resumeLog("attachments: SavePastedImage 失败：%v", err)
		return ""
	}
	name := fmt.Sprintf("paste-%d%s", time.Now().UnixNano(), ext)
	full := filepath.Join(attachmentsDir(), name)
	if err := os.WriteFile(full, raw, 0o644); err != nil {
		resumeLog("attachments: 写入 %s 失败：%v", full, err)
		return ""
	}
	resumeLog("attachments: 已保存粘贴图片 %s（%d 字节）", full, len(raw))
	return full
}

// AttachmentDataURL 把本地文件转成 data URL（供前端做图片预览）。
func (a *App) AttachmentDataURL(path string) string {
	p := strings.TrimSpace(path)
	if p == "" {
		return ""
	}
	data, err := os.ReadFile(p)
	if err != nil {
		resumeLog("attachments: 读取 %s 失败：%v", p, err)
		return ""
	}
	mimeType := mime.TypeByExtension(strings.ToLower(filepath.Ext(p)))
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	if i := strings.IndexByte(mimeType, ';'); i > 0 {
		mimeType = mimeType[:i]
	}
	return "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(data)
}

// AttachDropped 处理拖放进来的路径，返回前端所需的描述对象：
// {kind:"workspace"|"file", path, isDir, displayPath, previewUrl}。
func (a *App) AttachDropped(path string) map[string]any {
	p := strings.TrimSpace(path)
	if p == "" {
		return map[string]any{"kind": "file", "path": "", "isDir": false, "displayPath": "", "previewUrl": ""}
	}
	info, err := os.Stat(p)
	if err != nil {
		resumeLog("attachments: AttachDropped 无法访问 %s：%v", p, err)
		return map[string]any{"kind": "file", "path": p, "isDir": false, "displayPath": filepath.Base(p), "previewUrl": ""}
	}
	kind := "file"
	preview := ""
	display := filepath.Base(p)
	if info.IsDir() {
		kind = "workspace" // 前端据 kind==="workspace" 加入工作区引用
		display = p
	} else {
		preview = a.AttachmentDataURL(p)
	}
	return map[string]any{
		"kind":        kind,
		"path":        p,
		"isDir":       info.IsDir(),
		"displayPath": display,
		"previewUrl":  preview,
	}
}
