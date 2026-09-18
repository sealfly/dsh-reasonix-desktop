package main

import "testing"

// app_mcp_test.go — MCP 的纯函数测试。
//
// ⚠ 这里**只**保留不依赖 DSH profile 的纯函数测试。
// 原先的一组桥行为测试（TestAddMCPServerPersists / TestMCPServersFileRoundTrip 等）断言的是
// **旧的"写 ~/.reasonix/mcp-servers.json"契约**——该契约已废弃（DSH 永远不读那个文件），
// 且它们没有隔离 DSH_HOME，会往**真实的** DSH profile 配置里写测试条目
// （2026-09-18 实测踩过：真实 profile 被写进 gh/filesystem/srv/b 四条，已用写入器的备份恢复）。
// 新契约的测试见 app_mcp_remote_test.go（全部用 t.Setenv("DSH_HOME", ...) 隔离），
// 另由 app_testmain_test.go 的全局守卫兜底：测试进程默认不碰真实 ~/.dsh。

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

// TestMCPYAMLQuote 锁住 YAML 标量引号规则（写 profile 配置的关键正确性）。
func TestMCPYAMLQuote(t *testing.T) {
	cases := map[string]string{
		// 能不加引号就不加（保持配置可读）
		"tok-123":                    "tok-123",
		"npx":                        "npx",
		"streamable-http":            "streamable-http",
		"https://h.example:3000/mcp": "https://h.example:3000/mcp",
		"a/b_c.d":                    "a/b_c.d",
		// 必须加引号的情形
		"":                 "''",
		"@scope/pkg":       "'@scope/pkg'",   // @ 是 YAML 保留指示符
		"-y":               "'-y'",           // - 开头会被当成列表项
		"has: space":       "'has: space'",   // 冒号+空格
		"tail #comment":    "'tail #comment'",// 空格+井号
		" padded ":         "' padded '",     // 首尾空格
		"it's":             "'it''s'",        // 单引号翻倍
		"[1,2]":            "'[1,2]'",        // 流式集合
	}
	for in, want := range cases {
		if got := mcpYAMLQuoteForTest(in); got != want {
			t.Errorf("yamlQuote(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestMCPParseInlineList 行内数组解析（args 的 README 常见写法）。
func TestMCPParseInlineList(t *testing.T) {
	got := parseInlineList("['-y', '@modelcontextprotocol/server-github']")
	if len(got) != 2 || got[0] != "-y" || got[1] != "@modelcontextprotocol/server-github" {
		t.Errorf("parseInlineList = %v", got)
	}
	if v := parseInlineList("[]"); len(v) != 0 {
		t.Errorf("空数组应返回 nil/空: %v", v)
	}
	if v := parseInlineList("not-a-list"); v != nil {
		t.Errorf("非数组应返回 nil: %v", v)
	}
}
