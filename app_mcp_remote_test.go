package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ===== MCP：profile 配置读写（本项目最危险的一处写操作：改的是 DSH 的 profile YAML）=====

// setupMCPProfile 造一个临时 DSH profile，含注释、无关插件、一个 MCP 条目。
func setupMCPProfile(t *testing.T) (home string, patch string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("DSH_HOME", home)
	t.Setenv("DSH_PROFILE", "web")
	dir := filepath.Join(home, "profiles", "web")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	patch = filepath.Join(dir, "cordis.patch.yml")
	content := strings.Join([]string{
		"# 用户手写的注释必须保留",
		"- id: dsh-better-sidebar",
		"  disabled: true",
		"",
		"# MCP 段",
		"- id: mcp-github",
		"  name: '@deepseek-ai/dsh-mcp-client'",
		"  config:",
		"    serverName: github",
		"    transport: stdio",
		"    command: npx",
		"    args:",
		"      - -y",
		"      - '@modelcontextprotocol/server-github'",
		"    env:",
		"      GITHUB_TOKEN: tok-123",
		"",
	}, "\n")
	if err := os.WriteFile(patch, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return home, patch
}

func TestMCPReadProfileServers(t *testing.T) {
	_, patch := setupMCPProfile(t)
	servers, err := readProfileMCPServers(patch)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(servers) != 1 {
		t.Fatalf("应解析出 1 个 MCP 实例，实际 %d", len(servers))
	}
	s := servers[0]
	if s.Name != "github" || s.Transport != "stdio" || s.Command != "npx" {
		t.Errorf("字段解析错误: %+v", s)
	}
	if len(s.Args) != 2 || s.Args[0] != "-y" {
		t.Errorf("args 解析错误: %v", s.Args)
	}
	if s.Env["GITHUB_TOKEN"] != "tok-123" {
		t.Errorf("env 解析错误: %v", s.Env)
	}
	if s.Disabled {
		t.Error("该实例未标记 disabled，应为启用")
	}
	// 无关插件（dsh-better-sidebar）不应被当成 MCP 实例
	if servers[0].ID != "mcp-github" {
		t.Errorf("id = %q", servers[0].ID)
	}
}

func TestMCPWritePreservesCommentsAndOtherPlugins(t *testing.T) {
	_, patch := setupMCPProfile(t)
	servers, _ := readProfileMCPServers(patch)
	servers = append(servers, mcpDSHServer{
		ID: "mcp-web", Name: "web", Transport: "streamable-http",
		URL: "http://localhost:3000/mcp", Headers: map[string]string{"Authorization": "Bearer x"},
	})
	if err := writeProfileMCPServers(patch, servers); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	data, _ := os.ReadFile(patch)
	text := string(data)
	for _, must := range []string{
		"# 用户手写的注释必须保留",
		"dsh-better-sidebar",
		"GITHUB_TOKEN: tok-123",
		"serverName: web",
		"url: http://localhost:3000/mcp",
	} {
		if !strings.Contains(text, must) {
			t.Errorf("写入后丢失内容: %q\n---\n%s", must, text)
		}
	}
	// 重解析校验：两个实例都在，且字段正确
	back, err := readProfileMCPServers(patch)
	if err != nil || len(back) != 2 {
		t.Fatalf("重解析得到 %d 个（err=%v）", len(back), err)
	}
	byName := map[string]mcpDSHServer{}
	for _, s := range back {
		byName[s.Name] = s
	}
	if byName["web"].URL != "http://localhost:3000/mcp" {
		t.Errorf("web.url = %q", byName["web"].URL)
	}
	if byName["github"].Command != "npx" || len(byName["github"].Args) != 2 {
		t.Errorf("github 字段在重写后损坏: %+v", byName["github"])
	}
}

func TestMCPDisableEnableRoundTrip(t *testing.T) {
	_, patch := setupMCPProfile(t)
	a := &App{}
	if err := a.SetMCPServerEnabled("github", false); err != nil {
		t.Fatalf("禁用失败: %v", err)
	}
	servers, _ := readProfileMCPServers(patch)
	if len(servers) != 1 || !servers[0].Disabled {
		t.Fatalf("禁用未生效: %+v", servers)
	}
	// 禁用后其它字段不能丢
	if servers[0].Command != "npx" {
		t.Errorf("禁用后 command 丢失: %+v", servers[0])
	}
	if err := a.SetMCPServerEnabled("github", true); err != nil {
		t.Fatalf("启用失败: %v", err)
	}
	servers, _ = readProfileMCPServers(patch)
	if len(servers) != 1 || servers[0].Disabled {
		t.Fatalf("启用未生效: %+v", servers)
	}
	// 状态视图必须与 enabled 一致（诚实映射）
	views := a.MCPServers()
	if len(views) != 1 {
		t.Fatalf("MCPServers 返回 %d 项", len(views))
	}
	view := views[0].(map[string]any)
	if view["enabled"] != true || view["status"] != "deferred" {
		t.Errorf("启用后的视图不对: enabled=%v status=%v", view["enabled"], view["status"])
	}
}

func TestMCPAddUpdateRemove(t *testing.T) {
	_, patch := setupMCPProfile(t)
	a := &App{}

	// 新增（前端 MCPServerInput 形状）
	n := a.AddMCPServer(map[string]any{
		"name": "slack", "transport": "stdio", "command": "npx",
		"args": []any{"-y", "mcp-slack"}, "env": map[string]any{"SLACK_TOKEN": "s"},
	})
	if n != 0 { // 添加时工具数未知，诚实返回 0
		t.Errorf("AddMCPServer 应返回 0（DSH 按需连接），实际 %d", n)
	}
	servers, _ := readProfileMCPServers(patch)
	if len(servers) != 2 {
		t.Fatalf("新增后应有 2 个实例，实际 %d", len(servers))
	}
	// 更新
	if err := a.UpdateMCPServer("slack", map[string]any{
		"name": "slack", "transport": "stdio", "command": "uvx", "args": []any{"slack-mcp"},
	}); err != nil {
		t.Fatalf("更新失败: %v", err)
	}
	servers, _ = readProfileMCPServers(patch)
	byName := map[string]mcpDSHServer{}
	for _, s := range servers {
		byName[s.Name] = s
	}
	if byName["slack"].Command != "uvx" {
		t.Errorf("更新未生效: %+v", byName["slack"])
	}
	// 改名应被拒绝（官方语义：name 是稳定身份）
	if err := a.UpdateMCPServer("slack", map[string]any{"name": "slack2"}); err == nil {
		t.Error("改名应被拒绝")
	}
	// 删除
	if err := a.RemoveMCPServer("slack"); err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	servers, _ = readProfileMCPServers(patch)
	if len(servers) != 1 {
		t.Fatalf("删除后应剩 1 个，实际 %d", len(servers))
	}
	// 删除不存在的应报错
	if err := a.RemoveMCPServer("nope"); err == nil {
		t.Error("删除不存在的服务器应报错")
	}
}

func TestMCPLegacyMigration(t *testing.T) {
	setupMCPProfile(t)
	// 旧实现写的本地 JSON
	legacy := mcpServerEntry{Name: "old-one", Transport: "stdio", Command: "npx", Args: []string{"a"}}
	getMCPManager().save([]mcpServerEntry{legacy})

	a := &App{}
	views := a.MCPServers()
	if len(views) != 2 {
		t.Fatalf("应同时列出 profile 与旧配置（2 项），实际 %d", len(views))
	}
	found := false
	for _, raw := range views {
		v := raw.(map[string]any)
		if v["name"] == "old-one" {
			found = true
			if v["source"] != "legacy-local" || v["enabled"] != false {
				t.Errorf("旧配置应标为 legacy-local 且未启用: %+v", v)
			}
		}
	}
	if !found {
		t.Error("旧配置未出现在列表里（用户会以为配置丢了）")
	}

	// InstallMCPServer 迁移并把旧条目清掉
	res := a.InstallMCPServer(map[string]any{"name": "old-one", "transport": "stdio", "command": "npx"})
	if res["state"] != "ready" {
		t.Errorf("InstallMCPServer state = %v（msg=%v）", res["state"], res["message"])
	}
	after := a.MCPServers()
	if len(after) != 2 {
		t.Fatalf("迁移后应仍是 2 项（profile 里的 github + old-one），实际 %d", len(after))
	}
	for _, raw := range after {
		v := raw.(map[string]any)
		if v["name"] == "old-one" && v["source"] == "legacy-local" {
			t.Error("迁移后旧条目仍在（会出现重复项）")
		}
	}
}

func TestMCPCapabilityMatrixAndUnsupportedAuth(t *testing.T) {
	setupMCPProfile(t)
	a := &App{}
	m := a.MCPCapabilityMatrix()
	if m["configured"].(int) != 1 || m["enabled"].(int) != 1 {
		t.Errorf("矩阵统计错误: %+v", m)
	}
	if m["toolPrefixPattern"] != "mcp__<serverName>__<tool>" {
		t.Errorf("工具前缀形状错误: %v", m["toolPrefixPattern"])
	}
	// DSH 的 mcp-client 不支持交互授权 → 必须如实报错而不是假装成功
	if err := a.AuthenticateMCPServer("github"); err == nil {
		t.Error("AuthenticateMCPServer 应明确不支持")
	}
}

// ===== 远程 SSH =====

// setupRemoteHome 隔离 HOME 并写入一份 ssh config。
func setupRemoteHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := strings.Join([]string{
		"# 模板条目（应被跳过）",
		"Host *",
		"  ServerAliveInterval 30",
		"",
		"Host prod",
		"  HostName 10.0.0.5",
		"  User deploy",
		"  Port 2222",
		"  IdentityFile ~/.ssh/id_prod",
		"  ProxyJump bastion",
		"",
		"Host dev-box",
		"  HostName dev.example.com",
		"  User chenz",
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(sshDir, "config"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	return home
}

func TestScanSSHConfigSkipsWildcards(t *testing.T) {
	setupRemoteHome(t)
	hosts := scanSSHConfigHosts()
	if len(hosts) != 2 {
		t.Fatalf("应解析出 2 台主机（跳过 Host *），实际 %d: %+v", len(hosts), hosts)
	}
	byLabel := map[string]map[string]any{}
	for _, h := range hosts {
		byLabel[h["label"].(string)] = h
	}
	prod := byLabel["prod"]
	if prod == nil {
		t.Fatal("缺少 prod")
	}
	if prod["host"] != "10.0.0.5" || prod["user"] != "deploy" || prod["port"] != 2222 {
		t.Errorf("prod 字段错误: %+v", prod)
	}
	if prod["identityFile"] != "~/.ssh/id_prod" || prod["proxyJump"] != "bastion" {
		t.Errorf("prod 密钥/跳板机错误: %+v", prod)
	}
	dev := byLabel["dev-box"]
	if dev["host"] != "dev.example.com" || dev["port"] != 22 {
		t.Errorf("dev-box 字段错误: %+v", dev)
	}
}

func TestRemoteHostCRUDAndScanFiltering(t *testing.T) {
	setupRemoteHome(t)
	a := &App{}

	// 初始为空
	if got := a.RemoteHosts(); len(got) != 0 {
		t.Fatalf("初始应无主机，实际 %d", len(got))
	}

	// 扫描 → 应给出 2 条可导入
	scanned := a.ScanSSHConfig()
	if len(scanned) != 2 {
		t.Fatalf("扫描应给出 2 条，实际 %d", len(scanned))
	}

	// 添加（模拟前端导入）
	view := a.AddRemoteHost(scanned[0].(map[string]any))
	id, _ := view["id"].(string)
	if id == "" {
		t.Fatal("新增主机应返回 id")
	}
	if got := a.RemoteHosts(); len(got) != 1 {
		t.Fatalf("新增后应有 1 台，实际 %d", len(got))
	}

	// 再扫描：已导入的那台应被过滤掉
	scanned2 := a.ScanSSHConfig()
	if len(scanned2) != 1 {
		t.Errorf("已导入的主机不应重复出现，扫描剩余应为 1，实际 %d", len(scanned2))
	}

	// 更新
	upd := a.UpdateRemoteHost(id, map[string]any{"label": "prod-renamed", "host": "10.0.0.5",
		"user": "deploy", "port": 2222, "useSSHConfig": true})
	if upd["label"] != "prod-renamed" {
		t.Errorf("更新未生效: %+v", upd)
	}

	// 删除
	if err := a.RemoveRemoteHost(id); err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	if got := a.RemoteHosts(); len(got) != 0 {
		t.Errorf("删除后应为空，实际 %d", len(got))
	}
	if err := a.RemoveRemoteHost("nope"); err == nil {
		t.Error("删除不存在的主机应报错")
	}
}

func TestRemoteHostPersistence(t *testing.T) {
	home := setupRemoteHome(t)
	a := &App{}
	view := a.AddRemoteHost(map[string]any{"label": "kept", "host": "h.example", "user": "u", "port": 2200})
	_ = view
	// 直接读文件，确认落盘
	data, err := os.ReadFile(filepath.Join(home, ".reasonix", "remote-hosts.json"))
	if err != nil {
		t.Fatalf("主机库未落盘: %v", err)
	}
	if !strings.Contains(string(data), "kept") || !strings.Contains(string(data), "h.example") {
		t.Errorf("落盘内容不含期望字段: %s", data)
	}
}

func TestRemoteConnectionFailureIsHonest(t *testing.T) {
	setupRemoteHome(t)
	a := &App{}
	// 指向必然不可达的地址：必须报错，不能假装成功
	view := a.AddRemoteHost(map[string]any{
		"label": "unreachable", "host": "127.0.0.1", "user": "nobody", "port": 9,
	})
	id := view["id"].(string)
	err := a.ConnectRemoteHost(id)
	if err == nil {
		t.Error("连接不可达主机应返回错误（不能假装成功）")
	}
	statuses := a.RemoteConnectionStatuses()
	if len(statuses) != 1 {
		t.Fatalf("状态条数 = %d", len(statuses))
	}
	st := statuses[0].(map[string]any)
	if st["state"] == "connected" {
		t.Errorf("不可达主机不应报 connected: %+v", st)
	}
	// 断开后状态归零
	if err := a.DisconnectRemoteHost(id); err != nil {
		t.Fatalf("断开失败: %v", err)
	}
	st2 := a.RemoteConnectionStatuses()[0].(map[string]any)
	if st2["state"] != "stopped" {
		t.Errorf("断开后应 stopped，实际 %v", st2["state"])
	}
}

func TestRemoteLegacyWorkbenchScanAndClean(t *testing.T) {
	home := setupRemoteHome(t)
	a := &App{}
	base := filepath.Join(home, ".reasonix")
	mirrorDir := filepath.Join(base, "remote-mirrors")
	if err := os.MkdirAll(filepath.Join(mirrorDir, "r1"), 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(mirrorDir, "r1", "f.bin"), []byte("1234567890"), 0o644)
	trust := filepath.Join(base, "remote-trust.json")
	_ = os.WriteFile(trust, []byte("{}"), 0o644)

	scan := a.ScanRemoteLegacyWorkbenchData()
	if scan["mirrorCount"].(int) < 1 || scan["mirrorBytes"].(int64) <= 0 || scan["trustFile"] != true {
		t.Errorf("扫描结果错误: %+v", scan)
	}
	if err := a.CleanRemoteLegacyWorkbenchData("mirrors"); err != nil {
		t.Fatalf("清理 mirrors 失败: %v", err)
	}
	if _, err := os.Stat(mirrorDir); !os.IsNotExist(err) {
		t.Error("mirrors 目录应被删除")
	}
	if err := a.CleanRemoteLegacyWorkbenchData("trust"); err != nil {
		t.Fatalf("清理 trust 失败: %v", err)
	}
	if _, err := os.Stat(trust); !os.IsNotExist(err) {
		t.Error("信任文件应被删除")
	}
	if err := a.CleanRemoteLegacyWorkbenchData("bogus"); err == nil {
		t.Error("未知清理目标应报错")
	}
}

// TestRemoteUnsupportedFeaturesAreHonest 固定"未实现"语义：不能返回假数据。
func TestRemoteUnsupportedFeaturesAreHonest(t *testing.T) {
	setupRemoteHome(t)
	a := &App{}
	if got := a.RemoteForwards("any"); len(got) != 0 {
		t.Errorf("端口转发未实现时应返回空表，实际 %d", len(got))
	}
	if _, err := a.AddRemoteForward("any", nil); err == nil {
		t.Error("添加转发应明确报未实现，而不是假装成功")
	}
	if st := a.RemoteServerStatus("h", "/w"); st["state"] != "stopped" {
		t.Errorf("远端服务未托管时应报 stopped: %+v", st)
	}
	if got := a.RemoteServerLogs("h", "/w", 100); got != "" {
		t.Errorf("未托管时日志应为空，实际 %q", got)
	}
}
