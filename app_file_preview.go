package main

// 文件预览桥：PreviewFile 读取并解析 md/文本/docx/xlsx/pptx，
// ListWorkspaceFiles 浏览工作区文件。全部用标准库（archive/zip + encoding/xml），
// 无第三方依赖；文本自动 UTF-8/GBK 识别。
//
// 项目原则：桥是通用透传，这里只做"读取用户路径的文件并转成可展示内容"——
// 不设白名单（用户自己的机器，路径由前端传），但有大小上限防内存问题。

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
)

const (
	previewTextMaxBytes = 4 << 20  // 文本类上限 4MB
	previewZipMaxBytes  = 64 << 20 // office(zip) 上限 64MB
)

// textExts 按扩展名识别纯文本（含代码），预览直接返回文本。
var textExts = map[string]bool{
	".md": true, ".markdown": true, ".txt": true, ".log": true, ".json": true,
	".yaml": true, ".yml": true, ".csv": true, ".tsv": true, ".ini": true,
	".cfg": true, ".conf": true, ".toml": true, ".xml": true, ".html": true,
	".htm": true, ".css": true, ".js": true, ".mjs": true, ".cjs": true,
	".ts": true, ".tsx": true, ".jsx": true, ".vue": true, ".go": true,
	".py": true, ".rb": true, ".rs": true, ".java": true, ".c": true,
	".h": true, ".cpp": true, ".hpp": true, ".cs": true, ".php": true,
	".sh": true, ".bat": true, ".ps1": true, ".sql": true, ".env": true,
	".gitignore": true, ".dockerignore": true, ".editorconfig": true,
	".properties": true, ".gradle": true, ".lock": true, ".diff": true,
	".patch": true, ".rst": true, ".tex": true,
}

// PreviewFile 读取并解析文件，返回可展示内容。
// 返回 {ok, type: text|table|slides|binary|error, ...}。
func (a *App) PreviewFile(path string) map[string]any {
	path = strings.TrimSpace(path)
	if path == "" {
		return map[string]any{"ok": false, "error": "empty path"}
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return map[string]any{"ok": false, "error": "bad path"}
	}
	fi, err := os.Stat(abs)
	if err != nil || fi.IsDir() {
		return map[string]any{"ok": false, "error": "file not found"}
	}
	if fi.Size() > previewZipMaxBytes {
		return map[string]any{"ok": false, "error": fmt.Sprintf("文件过大（%.1f MB > 64 MB）", float64(fi.Size())/(1<<20))}
	}
	ext := strings.ToLower(filepath.Ext(abs))
	base := map[string]any{
		"ok": true, "path": abs, "ext": ext,
		"name": filepath.Base(abs), "size": fi.Size(),
		"modified": fi.ModTime().Format("2006-01-02 15:04"),
	}
	switch ext {
	case ".docx":
		return previewDocx(base, abs)
	case ".xlsx":
		return previewXlsx(base, abs)
	case ".pptx":
		return previewPptx(base, abs)
	default:
		if textExts[ext] {
			content, err := readTextFile(abs, previewTextMaxBytes)
			if err != nil {
				base["ok"] = false
				base["error"] = err.Error()
				return base
			}
			base["type"] = "text"
			base["content"] = content
			return base
		}
		// 未知类型：尝试按文本读，失败按二进制
		if content, err := readTextFile(abs, previewTextMaxBytes); err == nil {
			base["type"] = "text"
			base["content"] = content
			base["guessedText"] = true
			return base
		}
		base["type"] = "binary"
		base["hint"] = "二进制文件，暂不支持预览"
		return base
	}
}

// readTextFile 读文件并识别编码（UTF-8 优先，非 UTF-8 尝试 GBK）。
func readTextFile(path string, limit int64) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return "", err
	}
	if len(data) > int(limit) {
		return "", fmt.Errorf("文本超过 %d KB 预览上限", limit/(1<<10))
	}
	if utf8.Valid(data) {
		return string(data), nil
	}
	// 尝试 GBK
	if dec, err := simplifiedchinese.GBK.NewDecoder().Bytes(data); err == nil && utf8.Valid(dec) {
		return string(dec), nil
	}
	return string(data), nil
}

// openZipSafe 打开 zip（限制大小 + 条目数量防御）。
func openZipSafe(path string, maxBytes int64) (*zip.ReadCloser, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if fi.Size() > maxBytes {
		return nil, fmt.Errorf("文件过大（%.1f MB）", float64(fi.Size())/(1<<20))
	}
	return zip.OpenReader(path)
}

// zipReadFile 读取 zip 内指定路径的文件内容（不存在返回空）。
func zipReadFile(zr *zip.ReadCloser, name string) []byte {
	for _, f := range zr.File {
		if f.Name == name {
			rc, err := f.Open()
			if err != nil {
				return nil
			}
			defer rc.Close()
			b, _ := io.ReadAll(io.LimitReader(rc, previewTextMaxBytes))
			return b
		}
	}
	return nil
}

// xmlLocalTexts 用 XML token 流提取文本：
// textTag 收集文本（如 "t"），blockEndTag 结束当前块（如 "p" 或 "row"）。
// 返回块列表（每块是文本拼接）。
func xmlLocalTexts(data []byte, textTag, blockEndTag string) []string {
	dec := xml.NewDecoder(bytes.NewReader(data))
	var blocks []string
	var cur []string
	flush := func() {
		if len(cur) > 0 {
			blocks = append(blocks, strings.Join(cur, ""))
			cur = nil
		}
	}
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == textTag {
				var sb strings.Builder
				for {
					inner, err := dec.Token()
					if err != nil {
						break
					}
					if ch, ok := inner.(xml.CharData); ok {
						sb.Write(ch)
					}
					if _, ok := inner.(xml.EndElement); ok {
						break
					}
					if st, ok := inner.(xml.StartElement); ok && st.Name.Local == "br" {
						sb.WriteString("\n")
					}
				}
				if s := strings.TrimSpace(sb.String()); s != "" {
					cur = append(cur, s)
				}
			}
		case xml.EndElement:
			if blockEndTag != "" && t.Name.Local == blockEndTag {
				flush()
			}
		}
	}
	flush()
	return blocks
}

// previewDocx 解析 docx：提取段落文本（含表格行文本）。
func previewDocx(base map[string]any, path string) map[string]any {
	zr, err := openZipSafe(path, previewZipMaxBytes)
	if err != nil {
		base["ok"] = false
		base["error"] = err.Error()
		return base
	}
	defer zr.Close()
	doc := zipReadFile(zr, "word/document.xml")
	if doc == nil {
		base["ok"] = false
		base["error"] = "不是有效的 docx（缺 word/document.xml）"
		return base
	}
	paras := xmlLocalTexts(doc, "t", "p")
	lines := make([]string, 0, len(paras))
	for _, p := range paras {
		lines = append(lines, p)
	}
	base["type"] = "text"
	base["format"] = "docx"
	base["content"] = strings.Join(lines, "\n")
	base["paragraphs"] = len(lines)
	return base
}

// previewXlsx 解析 xlsx：共享字符串 + 第一个工作表 → 表格。
func previewXlsx(base map[string]any, path string) map[string]any {
	zr, err := openZipSafe(path, previewZipMaxBytes)
	if err != nil {
		base["ok"] = false
		base["error"] = err.Error()
		return base
	}
	defer zr.Close()
	// 共享字符串
	var shared []string
	if ss := zipReadFile(zr, "xl/sharedStrings.xml"); ss != nil {
		shared = xmlLocalTexts(ss, "t", "si")
	}
	// 工作表（优先 sheet1）
	sheetName := "xl/worksheets/sheet1.xml"
	sheet := zipReadFile(zr, sheetName)
	if sheet == nil {
		base["ok"] = false
		base["error"] = "未找到工作表（xl/worksheets/sheet1.xml）"
		return base
	}
	// 解析单元格：行(row) → 单元格(c: r=坐标, t=类型, v=值)
	type cell struct{ ref, typ, val string }
	dec := xml.NewDecoder(bytes.NewReader(sheet))
	var rows [][]any
	var curRow []any
	var curCell *cell
	flushCell := func() {
		if curCell == nil {
			return
		}
		v := curCell.val
		if curCell.typ == "s" {
			// 共享字符串索引
			idx := 0
			fmt.Sscanf(strings.TrimSpace(v), "%d", &idx)
			if idx >= 0 && idx < len(shared) {
				v = shared[idx]
			}
		}
		curRow = append(curRow, v)
		curCell = nil
	}
	flushRow := func() {
		if len(curRow) > 0 {
			rows = append(rows, curRow)
			curRow = nil
		}
	}
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "c":
				flushCell()
				c := &cell{}
				for _, a := range t.Attr {
					switch a.Name.Local {
					case "r":
						c.ref = a.Value
					case "t":
						c.typ = a.Value
					}
				}
				curCell = c
			case "v":
				if curCell != nil {
					var sb strings.Builder
					for {
						inner, err := dec.Token()
						if err != nil {
							break
						}
						if ch, ok := inner.(xml.CharData); ok {
							sb.Write(ch)
						}
						if _, ok := inner.(xml.EndElement); ok {
							break
						}
					}
					curCell.val = sb.String()
				}
			case "is":
				if curCell != nil {
					var sb strings.Builder
					for {
						inner, err := dec.Token()
						if err != nil {
							break
						}
						if st, ok := inner.(xml.StartElement); ok && st.Name.Local == "t" {
							for {
								txt, err := dec.Token()
								if err != nil {
									break
								}
								if ch, ok := txt.(xml.CharData); ok {
									sb.Write(ch)
								}
								if _, ok := txt.(xml.EndElement); ok {
									break
								}
							}
						}
						if _, ok := inner.(xml.EndElement); ok && inner.(xml.EndElement).Name.Local == "is" {
							break
						}
					}
					curCell.val = sb.String()
					curCell.typ = "inline"
				}
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "c":
				flushCell()
			case "row":
				flushRow()
			}
		}
	}
	flushCell()
	flushRow()
	if len(rows) == 0 {
		base["ok"] = false
		base["error"] = "工作表为空或无法解析"
		return base
	}
	base["type"] = "table"
	base["format"] = "xlsx"
	base["sheets"] = []any{map[string]any{"name": "Sheet1", "rows": rows}}
	base["rowCount"] = len(rows)
	return base
}

// previewPptx 解析 pptx：逐页提取文本。
func previewPptx(base map[string]any, path string) map[string]any {
	zr, err := openZipSafe(path, previewZipMaxBytes)
	if err != nil {
		base["ok"] = false
		base["error"] = err.Error()
		return base
	}
	defer zr.Close()
	type slideFile struct{ n int; name string }
	var slides []slideFile
	for _, f := range zr.File {
		name := f.Name
		var n int
		if m, _ := fmt.Sscanf(name, "ppt/slides/slide%d.xml", &n); m == 1 {
			slides = append(slides, slideFile{n, name})
		}
	}
	sort.Slice(slides, func(i, j int) bool { return slides[i].n < slides[j].n })
	if len(slides) == 0 {
		base["ok"] = false
		base["error"] = "未找到幻灯片（ppt/slides/）"
		return base
	}
	out := make([]any, 0, len(slides))
	for _, s := range slides {
		data := zipReadFile(zr, s.name)
		texts := xmlLocalTexts(data, "t", "p")
		page := map[string]any{"n": s.n, "text": strings.Join(texts, "\n")}
		out = append(out, page)
	}
	base["type"] = "slides"
	base["format"] = "pptx"
	base["slides"] = out
	base["slideCount"] = len(out)
	return base
}

// skipDirNames 文件浏览时跳过的目录（大/噪声）。
var skipDirNames = map[string]bool{
	"node_modules": true, ".git": true, ".dsh": true, "dist": true,
	"build": true, ".cache": true, "__pycache__": true, ".venv": true,
	"venv": true, ".idea": true, ".vscode": true, ".next": true,
	".nuxt": true, "target": true, ".gradle": true, ".svn": true,
	".hg": true, "coverage": true, ".turbo": true, ".pnpm-store": true,
}

// ListWorkspaceFiles 浏览工作区文件。
// dir 目录路径（空 → 用户主目录），depth 递归深度（0 只列当前层，最大 3）。
// 返回 {ok, dir, entries:[{name, path, isDir, size, modified, ext}]}。
func (a *App) ListWorkspaceFiles(dir string, depth int) map[string]any {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		if home, err := os.UserHomeDir(); err == nil {
			dir = home
		} else {
			return map[string]any{"ok": false, "error": "empty dir"}
		}
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return map[string]any{"ok": false, "error": "bad path"}
	}
	fi, err := os.Stat(abs)
	if err != nil || !fi.IsDir() {
		return map[string]any{"ok": false, "error": "目录不存在"}
	}
	if depth < 0 {
		depth = 0
	}
	if depth > 3 {
		depth = 3
	}
	entries := make([]any, 0, 64)
	var walk func(d string, cur int)
	walk = func(d string, cur int) {
		items, err := os.ReadDir(d)
		if err != nil {
			return
		}
		for _, it := range items {
			name := it.Name()
			if it.IsDir() && skipDirNames[name] {
				continue
			}
			ip := filepath.Join(d, name)
			full, _ := filepath.Abs(ip)
			e := map[string]any{
				"name": name, "path": full, "isDir": it.IsDir(),
			}
			if it.IsDir() {
				e["ext"] = ""
				e["size"] = int64(0)
				e["modified"] = ""
				if cur < depth {
					walk(ip, cur+1)
				}
			} else {
				if sfi, err := it.Info(); err == nil {
					e["size"] = sfi.Size()
					e["modified"] = sfi.ModTime().Format("2006-01-02 15:04")
				}
				e["ext"] = strings.ToLower(filepath.Ext(name))
			}
			entries = append(entries, e)
		}
	}
	walk(abs, 0)
	sort.Slice(entries, func(i, j int) bool {
		a1 := entries[i].(map[string]any)
		a2 := entries[j].(map[string]any)
		if a1["isDir"] != a2["isDir"] {
			return a1["isDir"].(bool)
		}
		return a1["name"].(string) < a2["name"].(string)
	})
	return map[string]any{"ok": true, "dir": abs, "entries": entries}
}

var _ = time.Now // 保留 time 引用（部分路径未用）
