package main

// app_mcp_test.go — MCP 本地持久化桥测试（add/list/update/remove 往返 + 语义约束）。

import (
	"os"
	"path/filepath"
	"testing"
)

// useTempMCPDir 把 MCP 管理器指向临时目录，测试后还原。
func useTempMCPDir(t *testing.T) string {
	t.Helper()
	_ = getMCPManager() // 确保单例 once 已执行，之后覆盖指针即可生效
	old := appMCPMgr
	dir := t.TempDir()
	appMCPMgr = &mcpManager{path: filepath.Join(dir, "mcp-servers.json")}
	t.Cleanup(func() { appMCPMgr = old })
	return dir
}

func TestAddMCPServerPersists(t *testing.T) {
	useTempMCPDir(t)
	a := &App{}
	in := map[string]any{
		"name": "filesystem", "transport": "stdio", "command": "npx",
		"args": []any{"-y", "@modelcontextprotocol/server-filesystem", "/tmp"},
	}
	if n := a.AddMCPServer(in); n != 0 {
		t.Fatalf("AddMCPServer 应返回 0（DSH 不加载 MCP），got %d", n)
	}
	list := a.MCPServers()
	if len(list) != 1 {
		t.Fatalf("期望 1 个服务器，got %d", len(list))
	}
	m := list[0].(map[string]any)
	if m["name"] != "filesystem" || m["transport"] != "stdio" {
		t.Fatalf("字段不符: %v", m)
	}
	if m["toolCount"] != 0 || m["tools"] != 0 {
		t.Fatalf("DSH 未加载 MCP，工具数应为 0: %v", m)
	}
	if m["status"] != "configured" || m["configured"] != true {
		t.Fatalf("应为 configured 状态: %v", m)
	}
}

func TestAddMCPServerOverwriteSameName(t *testing.T) {
	useTempMCPDir(t)
	a := &App{}
	a.AddMCPServer(map[string]any{"name": "srv", "command": "one"})
	a.AddMCPServer(map[string]any{"name": "srv", "command": "two", "url": "http://x"})
	list := a.MCPServers()
	if len(list) != 1 {
		t.Fatalf("同名添加应覆盖，got %d 个", len(list))
	}
	m := list[0].(map[string]any)
	if m["command"] != "two" {
		t.Fatalf("应覆盖为 two: %v", m["command"])
	}
}

func TestAddMCPServerEmptyNameIgnored(t *testing.T) {
	useTempMCPDir(t)
	a := &App{}
	a.AddMCPServer(map[string]any{"command": "npx"})
	if len(a.MCPServers()) != 0 {
		t.Fatal("无 name 不应保存")
	}
}

func TestUpdateMCPServer(t *testing.T) {
	useTempMCPDir(t)
	a := &App{}
	a.AddMCPServer(map[string]any{"name": "srv", "command": "one"})
	if err := a.UpdateMCPServer("srv", map[string]any{"transport": "sse", "url": "http://e"}); err != nil {
		t.Fatalf("update 失败: %v", err)
	}
	list := a.MCPServers()
	m := list[0].(map[string]any)
	if m["transport"] != "sse" || m["url"] != "http://e" {
		t.Fatalf("update 未生效: %v", m)
	}
	if err := a.UpdateMCPServer("srv", map[string]any{"name": "renamed"}); err == nil {
		t.Fatal("改名应报错（官方语义：remove + add）")
	}
	if err := a.UpdateMCPServer("missing", map[string]any{}); err == nil {
		t.Fatal("不存在的服务器更新应报错")
	}
}

func TestRemoveMCPServer(t *testing.T) {
	useTempMCPDir(t)
	a := &App{}
	a.AddMCPServer(map[string]any{"name": "a"})
	a.AddMCPServer(map[string]any{"name": "b"})
	if err := a.RemoveMCPServer("a"); err != nil {
		t.Fatalf("remove 失败: %v", err)
	}
	list := a.MCPServers()
	if len(list) != 1 || list[0].(map[string]any)["name"] != "b" {
		t.Fatalf("应只剩 b: %v", list)
	}
	if err := a.RemoveMCPServer("a"); err == nil {
		t.Fatal("重复删除应报错")
	}
}

func TestNormalizeMCPTransport(t *testing.T) {
	cases := map[string]string{
		"stdio": "stdio", "STDIO": "stdio", "": "stdio",
		"sse": "sse", "SSE": "sse",
		"http": "streamable-http", "streamable-http": "streamable-http", "streamablehttp": "streamable-http",
	}
	for in, want := range cases {
		if got := normalizeMCPTransport(in); got != want {
			t.Errorf("normalizeMCPTransport(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMCPServersFileRoundTrip(t *testing.T) {
	dir := useTempMCPDir(t)
	a := &App{}
	a.AddMCPServer(map[string]any{
		"name": "gh", "transport": "streamable-http", "url": "https://api.githubcopilot.com/mcp",
		"headers": map[string]any{"Authorization": "Bearer x"},
	})
	// 模拟重启：新建 App（同一持久化文件）
	a2 := &App{}
	list := a2.MCPServers()
	if len(list) != 1 {
		t.Fatalf("重启后应读到 1 个服务器，got %d（%s）", len(list), dir)
	}
	m := list[0].(map[string]any)
	if m["name"] != "gh" || m["headerKeys"] == nil {
		t.Fatalf("往返丢失字段: %v", m)
	}
	_ = os.Getenv("MCP_TEST")
}
