package main

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// buildZip 构造 zip 文件（name→content）
func buildZip(t *testing.T, path string, files map[string]string) {
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestPreviewFileText(t *testing.T) {
	dir := t.TempDir()
	md := filepath.Join(dir, "README.md")
	if err := os.WriteFile(md, []byte("# 标题\n\n正文内容\n- 列表项\n"), 0644); err != nil {
		t.Fatal(err)
	}
	r := (&App{}).PreviewFile(md)
	if r["ok"] != true || r["type"] != "text" {
		t.Fatalf("md 预览失败: %v", r)
	}
	if !strings.Contains(r["content"].(string), "标题") {
		t.Fatalf("md 内容缺失: %v", r["content"])
	}
	// 不存在文件
	r2 := (&App{}).PreviewFile(filepath.Join(dir, "nope.docx"))
	if r2["ok"] == true {
		t.Fatal("不存在文件应失败")
	}
}

func TestPreviewDocx(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "doc.docx")
	docXML := `<?xml version="1.0"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
<w:body>
<w:p><w:r><w:t>第一段标题</w:t></w:r></w:p>
<w:p><w:r><w:t>第二段正文内容</w:t></w:r><w:r><w:t>追加</w:t></w:r></w:p>
</w:body>
</w:document>`
	buildZip(t, p, map[string]string{"word/document.xml": docXML})
	r := (&App{}).PreviewFile(p)
	if r["ok"] != true || r["type"] != "text" {
		t.Fatalf("docx 预览失败: %v", r)
	}
	content := r["content"].(string)
	if !strings.Contains(content, "第一段标题") || !strings.Contains(content, "第二段正文内容追加") {
		t.Fatalf("docx 段落解析错误: %q", content)
	}
}

func TestPreviewXlsx(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "book.xlsx")
	shared := `<?xml version="1.0"?><sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" count="2">
<si><t>姓名</t></si><si><t>张三</t></si></sst>`
	sheet := `<?xml version="1.0"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
<sheetData>
<row r="1"><c r="A1" t="s"><v>0</v></c><c r="B1"><v>100</v></c></row>
<row r="2"><c r="A2" t="s"><v>1</v></c><c r="B2"><v>200</v></c></row>
</sheetData></worksheet>`
	buildZip(t, p, map[string]string{
		"xl/sharedStrings.xml": shared,
		"xl/worksheets/sheet1.xml": sheet,
	})
	r := (&App{}).PreviewFile(p)
	if r["ok"] != true || r["type"] != "table" {
		t.Fatalf("xlsx 预览失败: %v", r)
	}
	sheets := r["sheets"].([]any)
	rows := sheets[0].(map[string]any)["rows"].([][]any)
	if len(rows) != 2 {
		t.Fatalf("xlsx 行数错误: %d", len(rows))
	}
	row0 := rows[0]
	if row0[0] != "姓名" || row0[1] != "100" {
		t.Fatalf("xlsx 单元格解析错误: %v", row0)
	}
	row1 := rows[1]
	if row1[0] != "张三" {
		t.Fatalf("xlsx 共享字符串解析错误: %v", row1)
	}
}

func TestPreviewPptx(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "deck.pptx")
	slide1 := `<?xml version="1.0"?><p:sld xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main">
<p:cSld><p:spTree><p:sp><p:txBody><a:p xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"><a:r><a:t>第一页标题</a:t></a:r></a:p></p:txBody></p:sp></p:spTree></p:cSld></p:sld>`
	buildZip(t, p, map[string]string{"ppt/slides/slide1.xml": slide1})
	r := (&App{}).PreviewFile(p)
	if r["ok"] != true || r["type"] != "slides" {
		t.Fatalf("pptx 预览失败: %v", r)
	}
	slides := r["slides"].([]any)
	if len(slides) != 1 {
		t.Fatalf("pptx 页数错误: %d", len(slides))
	}
	page := slides[0].(map[string]any)
	if !strings.Contains(page["text"].(string), "第一页标题") {
		t.Fatalf("pptx 文本解析错误: %v", page["text"])
	}
}

func TestListWorkspaceFiles(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hi"), 0644)
	_ = os.MkdirAll(filepath.Join(dir, "sub"), 0755)
	_ = os.WriteFile(filepath.Join(dir, "sub", "b.md"), []byte("# b"), 0644)
	_ = os.MkdirAll(filepath.Join(dir, "node_modules"), 0755) // 应被跳过
	r := (&App{}).ListWorkspaceFiles(dir, 1)
	if r["ok"] != true {
		t.Fatalf("ListWorkspaceFiles 失败: %v", r)
	}
	entries := r["entries"].([]any)
	names := map[string]bool{}
	for _, e := range entries {
		m := e.(map[string]any)
		names[m["name"].(string)] = true
	}
	if !names["a.txt"] || !names["sub"] || !names["b.md"] {
		t.Fatalf("文件浏览缺项: %v", names)
	}
	if names["node_modules"] {
		t.Fatal("node_modules 应被跳过")
	}
}
