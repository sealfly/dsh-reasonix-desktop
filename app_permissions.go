package main

// app_permissions.go — 设置「自动化与开发者」区块：权限模式/规则 + 沙箱配置。
// 前端 SettingsPanel 的 PermissionsSection/SandboxSection 调用这些方法。
// 数据本地持久化（settings.json），权限模式尽力贴合 DSH 的 ask/auto/yolo 三档。

import (
	"encoding/json"
	"sort"
	"strings"
)

// ===== 权限模式（ask/allow/deny）=====

// SetPermissionMode 设置权限模式（ask=每次询问, allow=自动允许, deny=拒绝）。
// 贴合 DSH: ask→read-only, allow→workspace-write, deny→拒绝。
func (a *App) SetPermissionMode(mode string) error {
	switch mode {
	case "ask", "allow", "deny":
		a.st.SetPermissionMode(mode)
	default:
		mode = "ask"
		a.st.SetPermissionMode(mode)
	}
	return nil
}

// AddPermissionRule 添加权限规则（kind=allow/ask/deny, rule=工具/路径模式）。
func (a *App) AddPermissionRule(kind, rule string) error {
	if rule == "" {
		return nil
	}
	return a.st.AddPermissionRule(kind, rule)
}

// RemovePermissionRule 移除权限规则。
func (a *App) RemovePermissionRule(kind, rule string) error {
	return a.st.RemovePermissionRule(kind, rule)
}

// ===== 沙箱配置 =====

// SetSandbox 设置沙箱（bash/network/workspaceRoot/allowWrite/shell）。
func (a *App) SetSandbox(bash, network string, workspaceRoot string, allowWrite any, shell string) error {
	if bash == "" {
		bash = "enforce"
	}
	net := network == "true" || network == "1" || network == "on"
	// allowWrite 可能是 []string 或 JSON 字符串
	var writes []string
	switch v := allowWrite.(type) {
	case []any:
		for _, x := range v {
			if s, ok := x.(string); ok && s != "" {
				writes = append(writes, s)
			}
		}
	case []string:
		writes = v
	case string:
		if v != "" {
			var arr []string
			if json.Unmarshal([]byte(v), &arr) == nil {
				writes = arr
			} else {
				writes = []string{v}
			}
		}
	}
	if shell == "" {
		shell = "auto"
	}
	return a.st.SetSandbox(bash, net, workspaceRoot, writes, shell)
}

// ReloadSettings 重载会话配置（前端设置保存后调用）。
func (a *App) ReloadSettings() {
	// 配置已实时持久化；此处触发前端重读（空实现即可，前端随后调 Settings()）。
}

// Permissions 返回权限配置结构（前端 PermissionsSection 读取）。
func (a *App) Permissions() map[string]any {
	return a.st.PermissionsView()
}

// Sandbox 返回沙箱配置结构（前端 SandboxSection 读取）。
func (a *App) Sandbox() map[string]any {
	return a.st.SandboxView()
}

// ===== settings.go 扩展的辅助（避免在 settings 里重复）=====

// normalizePermissionRules 排序去重。
func normalizePermissionRules(rules []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, r := range rules {
		r = strings.TrimSpace(r)
		if r != "" && !seen[r] {
			seen[r] = true
			out = append(out, r)
		}
	}
	sort.Strings(out)
	return out
}