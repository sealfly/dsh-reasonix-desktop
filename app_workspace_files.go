package main

// 原生 WorkspacePanel（Reasonix 右侧"文件"栏）桥的真实现。
// 对照官方 Reasonix desktop/app.go 契约：
//   - 文件树：ListDirForTab(tabID, rel) []DirEntry{name,isDir}，rel 是相对会话工作区的
//     路径（空串=根）；前端按 name 拼接相对路径递归展开。
//   - 点文件预览：ReadFileForTab(tabID, rel) FilePreview{path,body,size,truncated,binary,...}
//     —— md/文本/代码返回 body（前端 markdown 渲染）；office/媒体/二进制返回 binary，
//     用户用右键「用默认程序打开」（OpenWorkspacePathForTab）调起 WPS 等系统关联程序。
//   - 打开：OpenWorkspacePathForTab → ShellExecute（docx→WPS 等）；Reveal* → explorer。
// 工作区根 = DSH 会话（tabId）的 cwd（session.list）；相对路径限定在根内防穿越。

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
)

// DirEntry 目录条目（官方契约；Path 留空，前端拼相对路径）。
type DirEntry struct {
	Name  string `json:"name"`
	Path  string `json:"path,omitempty"`
	IsDir bool   `json:"isDir"`
}

// FilePreview 文件预览负载（官方契约）。
type FilePreview struct {
	Path      string `json:"path"`
	Body      string `json:"body"`
	Size      int64  `json:"size"`
	Truncated bool   `json:"truncated"`
	Binary    bool   `json:"binary"`
	Kind      string `json:"kind,omitempty"`
	Mime      string `json:"mime,omitempty"`
	URL       string `json:"url,omitempty"`
	Err       string `json:"err,omitempty"`
}

// workspaceRootForTabID 按 tabId 查会话工作区根（走 Tabs 缓存，避免每次 session.list）。
// 返回空表示未知会话。
func (a *App) workspaceRootForTabID(tabID string) string {
	tabID = strings.TrimSpace(tabID)
	if tabID == "" {
		return ""
	}
	for _, t := range a.Tabs() {
		if m, ok := t.(map[string]any); ok {
			if id, _ := m["tabId"].(string); id == tabID {
				if root, _ := m["workspaceRoot"].(string); root != "" {
					return root
				}
				break
			}
		}
	}
	return ""
}

// resolveWorkspacePath 把 (tabID, rel) 解析成绝对路径。
// rel 空 → 工作区根；相对 → 根下拼接（Clean 后防 ../ 逃逸）；绝对 → 原样（树传相对为主，
// 绝对路径用于外部引用场景）。root 为空（未知会话）时仅接受绝对路径。
func (a *App) resolveWorkspacePath(tabID, rel string) (string, bool) {
	root := a.workspaceRootForTabID(tabID)
	return resolveRelToRoot(root, rel)
}

// resolveRelToRoot rel→绝对路径解析（纯函数，防 ../ 逃逸）。
// rel 空 → root；相对 → root 内拼接；绝对 → 原样。root 空时仅接受绝对路径。
func resolveRelToRoot(root, rel string) (string, bool) {
	rel = strings.TrimSpace(rel)
	if filepath.IsAbs(rel) {
		return filepath.Clean(rel), true
	}
	if root == "" {
		return "", false
	}
	if rel == "" {
		return filepath.Clean(root), true
	}
	p := filepath.Clean(filepath.Join(root, filepath.FromSlash(rel)))
	cleanRoot := filepath.Clean(root)
	if p == cleanRoot || strings.HasPrefix(p, cleanRoot+string(os.PathSeparator)) {
		return p, true
	}
	return "", false
}

// skipListedDir 文件树跳过的强噪声目录（node_modules/.git 等；dist/build 不跳——
// 用户可能要看构建产物，且本仓库自身就是 go/wails 项目）。
var skipListedDir = map[string]bool{
	"node_modules": true, ".git": true, ".svn": true, ".hg": true,
	".DS_Store": true, ".dsh": true, "__pycache__": true, ".venv": true,
	"venv": true, ".idea": true, ".vscode": true, ".next": true, ".nuxt": true,
	".turbo": true, ".pnpm-store": true,
}

// ListDirForTab 列出会话工作区相对目录（dirs 在前、各自按名排序）。
func (a *App) ListDirForTab(tabID, rel string) []DirEntry {
	dir, ok := a.resolveWorkspacePath(tabID, rel)
	if !ok {
		return []DirEntry{}
	}
	es, err := os.ReadDir(dir)
	if err != nil {
		return []DirEntry{}
	}
	var dirs, files []DirEntry
	for _, e := range es {
		if e.IsDir() && skipListedDir[e.Name()] {
			continue
		}
		entry := DirEntry{Name: e.Name(), IsDir: e.IsDir()}
		if e.IsDir() {
			dirs = append(dirs, entry)
		} else {
			files = append(files, entry)
		}
	}
	lower := func(i, j int) bool { return strings.ToLower(dirs[i].Name) < strings.ToLower(dirs[j].Name) }
	sort.Slice(dirs, lower)
	lowerF := func(i, j int) bool { return strings.ToLower(files[i].Name) < strings.ToLower(files[j].Name) }
	sort.Slice(files, lowerF)
	return append(dirs, files...)
}

// ListDir 无会话版列目录（绝对路径直读，供旧桥/外部路径用）。
func (a *App) ListDir(path string) []DirEntry {
	return a.ListDirForTab("", path)
}

// isZipOrKnownBinaryExts 已知二进制扩展（office/媒体/压缩/可执行等——不按文本预览，
// 需要正文请用 WPS/系统默认程序打开）。
var binaryExts = map[string]bool{
	".docx": true, ".xlsx": true, ".pptx": true, ".doc": true, ".xls": true,
	".ppt": true, ".pdf": true, ".png": true, ".jpg": true, ".jpeg": true,
	".gif": true, ".webp": true, ".bmp": true, ".ico": true, ".svg": true,
	".zip": true, ".rar": true, ".7z": true, ".tar": true, ".gz": true,
	".bz2": true, ".xz": true, ".exe": true, ".dll": true, ".so": true,
	".dylib": true, ".bin": true, ".iso": true, ".wav": true, ".mp3": true,
	".mp4": true, ".avi": true, ".mkv": true, ".mov": true, ".wmv": true,
	".flac": true, ".woff": true, ".woff2": true, ".ttf": true, ".otf": true,
	".eot": true, ".pdb": true, ".wasm": true, ".map": true, ".class": true,
	".jar": true, ".a": true, ".lib": true, ".obj": true, ".pyc": true,
}

// ReadFileForTab 读取文件供右侧文件栏预览：文本/代码 → body；二进制 → binary=true
// （前端显示二进制空态/加入引用，用户右键「用默认程序打开」调 WPS）。
func (a *App) ReadFileForTab(tabID, rel string) FilePreview {
	out := FilePreview{Path: rel}
	path, ok := a.resolveWorkspacePath(tabID, rel)
	if !ok {
		out.Err = "invalid path"
		return out
	}
	fi, err := os.Stat(path)
	if err != nil {
		out.Err = err.Error()
		return out
	}
	if fi.IsDir() {
		out.Err = "path is a directory"
		return out
	}
	out.Size = fi.Size()
	if fi.Size() > previewZipMaxBytes {
		out.Binary = true // 超大（>64MB）不当文本
		return out
	}
	ext := strings.ToLower(filepath.Ext(path))
	if binaryExts[ext] {
		out.Binary = true
		return out
	}
	if textExts[ext] {
		body, trunc := readTextPreview(path, previewTextMaxBytes)
		out.Body = body
		out.Truncated = trunc
		return out
	}
	// 未知扩展：前 8KB 探测（NUL → 二进制；UTF-8/GBK 可解码 → 文本）
	head, err := readHead(path, 8192)
	if err != nil {
		out.Binary = true
		return out
	}
	if strings.IndexByte(head, 0) >= 0 {
		out.Binary = true
		return out
	}
	body, trunc := readTextPreview(path, previewTextMaxBytes)
	out.Body = body
	out.Truncated = trunc
	return out
}

// ReadFile 无会话版（旧桥签名：返回 {path,content,size,truncated}）。
func (a *App) ReadFile(path string) map[string]any {
	pv := a.ReadFileForTab("", path)
	m := map[string]any{
		"path":      pv.Path,
		"content":   pv.Body,
		"size":      pv.Size,
		"truncated": pv.Truncated,
		"binary":    pv.Binary,
	}
	if pv.Err != "" {
		m["err"] = pv.Err
	}
	return m
}

// readHead 读文件前 n 字节。
func readHead(path string, n int64) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, n))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// readTextPreview 读文本：超上限截断（truncated=true，UTF-8 安全边界），UTF-8/GBK 识别。
func readTextPreview(path string, limit int64) (string, bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return "", false
	}
	trunc := false
	if len(data) > int(limit) {
		trunc = true
		data = data[:limit]
		for len(data) > 0 && !utf8.Valid(data) {
			data = data[:len(data)-1]
		}
	}
	if utf8.Valid(data) {
		return string(data), trunc
	}
	if dec, err := simplifiedchinese.GBK.NewDecoder().Bytes(data); err == nil && utf8.Valid(dec) {
		return string(dec), trunc
	}
	return string(data), trunc
}

// OpenWorkspacePathForTab 用系统默认程序打开工作区文件/文件夹（docx→WPS 等）。
func (a *App) OpenWorkspacePathForTab(tabID, rel string) error {
	path, ok := a.resolveWorkspacePath(tabID, rel)
	if !ok {
		return fmt.Errorf("invalid path: %q", rel)
	}
	return openSystemDefault(path)
}

// OpenWorkspacePath 无会话版。
func (a *App) OpenWorkspacePath(path string) error {
	return a.OpenWorkspacePathForTab("", path)
}

// OpenLocalPath 打开本地绝对路径（默认程序）。
func (a *App) OpenLocalPath(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("empty path")
	}
	return openSystemDefault(path)
}

// OpenWorkspaceInExternalOpener 外部打开器（未配置时等同默认程序打开）。
func (a *App) OpenWorkspaceInExternalOpener(path string) error {
	return a.OpenWorkspacePathForTab("", path)
}

// OpenLocalPathInExternalOpener 同（旧签名，两参）。
func (a *App) OpenLocalPathInExternalOpener(_a1 any, _a2 any) error { return nil }

// RevealWorkspacePathForTab 在文件管理器中显示（explorer /select）。
func (a *App) RevealWorkspacePathForTab(tabID, rel string) error {
	path, ok := a.resolveWorkspacePath(tabID, rel)
	if !ok {
		return fmt.Errorf("invalid path: %q", rel)
	}
	return revealInExplorer(path)
}

// RevealWorkspacePath 无会话版。
func (a *App) RevealWorkspacePath(path string) error {
	return a.RevealWorkspacePathForTab("", path)
}

// RevealPath 在文件管理器中显示绝对路径。
func (a *App) RevealPath(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("empty path")
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	return revealInExplorer(path)
}

// RevealWorkspaceWriterForTab 显示写入者文件（无写入者概念，打开工作区根）。
func (a *App) RevealWorkspaceWriterForTab(tabID string) error {
	root := a.workspaceRootForTabID(tabID)
	if root == "" {
		return fmt.Errorf("unknown tab")
	}
	return revealInExplorer(root)
}
