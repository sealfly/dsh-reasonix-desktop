package main

// app_subagents.go — 子智能体（Subagent Profile）配置的本地持久化桥（DSH 语义）。
//
// 探测确认 DSH 无 agent/subagent 注册 RPC（agent.* 全 404）；DSH 的 subagent 是
// 运行时派生的子会话（session 带 parentSessionId + origin="subagent"），无静态
// 配置概念。按项目原则 1（展示与持久化适配）：Reasonix 前端"子智能体"设置页的
// profile（名称/描述/系统提示/模型/努力/允许工具）持久化到
// ~/.reasonix/subagent-profiles.json，经 SkillsSettings 以 runAs="subagent"
// 条目合并展示（前端 SubagentsPanel 过滤该值）。试运行（TrySubagentProfile）
// 无 DSH 对应物，保留安全空实现。

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// subagentProfileEntry 持久化的子智能体 profile（对齐官方 SubagentProfileInput 字段）。
type subagentProfileEntry struct {
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	SystemPrompt string   `json:"systemPrompt"`
	Color        string   `json:"color,omitempty"`
	Model        string   `json:"model,omitempty"`
	Effort       string   `json:"effort,omitempty"`
	AllowedTools []string `json:"allowedTools,omitempty"`
	ReadOnly     bool     `json:"readOnly,omitempty"`
	Scope        string   `json:"scope,omitempty"`
}

// subagentManager 本地子智能体 profile 管理器（单例，仿 pluginManager/mcpManager 模式）。
type subagentManager struct {
	mu   sync.Mutex
	path string
}

var (
	appSubagentMgr  *subagentManager
	appSubagentOnce sync.Once
)

func getSubagentManager() *subagentManager {
	appSubagentOnce.Do(func() {
		home, _ := os.UserHomeDir()
		if home == "" {
			home = "."
		}
		dir := filepath.Join(home, ".reasonix")
		_ = os.MkdirAll(dir, 0o755)
		appSubagentMgr = &subagentManager{path: filepath.Join(dir, "subagent-profiles.json")}
	})
	return appSubagentMgr
}

func (m *subagentManager) load() []subagentProfileEntry {
	m.mu.Lock()
	defer m.mu.Unlock()
	data, err := os.ReadFile(m.path)
	if err != nil {
		return []subagentProfileEntry{}
	}
	var list []subagentProfileEntry
	if json.Unmarshal(data, &list) != nil || list == nil {
		return []subagentProfileEntry{}
	}
	return list
}

func (m *subagentManager) save(list []subagentProfileEntry) {
	m.mu.Lock()
	defer m.mu.Unlock()
	data, _ := json.MarshalIndent(list, "", "  ")
	_ = os.WriteFile(m.path, data, 0o644)
}

// subagentInputFromMap 从前端 JSON 提取 SubagentProfileInput 字段。
func subagentInputFromMap(in map[string]any) subagentProfileEntry {
	var e subagentProfileEntry
	e.Name = strings.TrimSpace(strOf(in["name"]))
	e.Description = strings.TrimSpace(strOf(in["description"]))
	e.SystemPrompt = strings.TrimSpace(strOf(in["systemPrompt"]))
	e.Color = strings.TrimSpace(strOf(in["color"]))
	e.Model = strings.TrimSpace(strOf(in["model"]))
	e.Effort = strings.TrimSpace(strOf(in["effort"]))
	if a, ok := in["allowedTools"].([]any); ok {
		for _, v := range a {
			e.AllowedTools = append(e.AllowedTools, strOf(v))
		}
	}
	if b, ok := in["readOnly"].(bool); ok {
		e.ReadOnly = b
	}
	e.Scope = strings.TrimSpace(strOf(in["scope"]))
	return e
}

// subagentScope 规范化 scope（默认 global，只接受 project/global）。
func subagentScope(raw string) (string, error) {
	switch strings.TrimSpace(raw) {
	case "", "global":
		return "global", nil
	case "project":
		return "project", nil
	default:
		return "", fmt.Errorf("invalid scope %q (want project or global)", raw)
	}
}

// CreateSubagentProfile 创建子智能体 profile（DSH 语义：本地持久化）。
// 返回标识字符串（非空即成功，前端据此关闭表单）。
func (a *App) CreateSubagentProfile(in map[string]any) (string, error) {
	entry := subagentInputFromMap(in)
	if entry.Name == "" {
		return "", fmt.Errorf("name is required")
	}
	if entry.Description == "" {
		return "", fmt.Errorf("description is required")
	}
	if entry.SystemPrompt == "" {
		return "", fmt.Errorf("system prompt is required")
	}
	scope, err := subagentScope(entry.Scope)
	if err != nil {
		return "", err
	}
	entry.Scope = scope

	mgr := getSubagentManager()
	list := mgr.load()
	for _, e := range list {
		if e.Name == entry.Name {
			return "", fmt.Errorf("subagent profile %q already exists", entry.Name)
		}
	}
	list = append(list, entry)
	mgr.save(list)
	return "local://subagents/" + entry.Name, nil
}

// UpdateSubagentProfile 覆盖更新子智能体 profile（name/scope 为稳定身份，不可改）。
func (a *App) UpdateSubagentProfile(name, scope string, in map[string]any) error {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return fmt.Errorf("name is required")
	}
	entry := subagentInputFromMap(in)
	// 身份字段以参数为准（前端表单只读）。
	entry.Name = trimmed
	if entry.Scope == "" {
		entry.Scope = strings.TrimSpace(scope)
	}
	if entry.Scope != "" {
		if s, err := subagentScope(entry.Scope); err != nil {
			return err
		} else {
			entry.Scope = s
		}
	}
	if entry.Description == "" {
		return fmt.Errorf("description is required")
	}
	if entry.SystemPrompt == "" {
		return fmt.Errorf("system prompt is required")
	}

	mgr := getSubagentManager()
	list := mgr.load()
	for i := range list {
		if list[i].Name == trimmed {
			list[i] = entry
			mgr.save(list)
			return nil
		}
	}
	return fmt.Errorf("no subagent profile named %q", trimmed)
}

// DeleteSubagentProfile 删除子智能体 profile。
func (a *App) DeleteSubagentProfile(name, scope string) error {
	trimmed := strings.TrimSpace(name)
	mgr := getSubagentManager()
	list := mgr.load()
	out := list[:0]
	found := false
	for _, e := range list {
		if e.Name == trimmed {
			found = true
			continue
		}
		out = append(out, e)
	}
	if !found {
		return fmt.Errorf("no subagent profile named %q", trimmed)
	}
	mgr.save(out)
	return nil
}

// SetSubagentProfileModel 设置 profile 的模型引用（空串清除）。
func (a *App) SetSubagentProfileModel(name string, ref string) error {
	return setSubagentProfileField(name, func(e *subagentProfileEntry) {
		e.Model = strings.TrimSpace(ref)
	})
}

// SetSubagentProfileEffort 设置 profile 的努力级别（空串清除）。
func (a *App) SetSubagentProfileEffort(name string, level string) error {
	return setSubagentProfileField(name, func(e *subagentProfileEntry) {
		e.Effort = strings.TrimSpace(level)
	})
}

// setSubagentProfileField 按 name 找到 profile 并应用字段变更。
func setSubagentProfileField(name string, apply func(*subagentProfileEntry)) error {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return fmt.Errorf("name is required")
	}
	mgr := getSubagentManager()
	list := mgr.load()
	for i := range list {
		if list[i].Name == trimmed {
			apply(&list[i])
			mgr.save(list)
			return nil
		}
	}
	return fmt.Errorf("no subagent profile named %q", trimmed)
}

// subagentProfilesAsSkills 把本地 subagent profiles 转成前端 SkillView 条目
// （runAs="subagent"，子智能体面板过滤该值）。
func subagentProfilesAsSkills() []any {
	mgr := getSubagentManager()
	list := mgr.load()
	out := make([]any, 0, len(list))
	for _, e := range list {
		item := map[string]any{
			"name":           e.Name,
			"description":    e.Description,
			"scope":          e.Scope,
			"runAs":          "subagent",
			"enabled":        !getSkillPrefsManager().isSkillDisabled(e.Name),
			"invocation":     "manual",
			"modelInvocable": false,
			"whenToUse":      "",
			"color":          e.Color,
			"allowedTools":   e.AllowedTools,
			"readOnly":       e.ReadOnly,
		}
		if e.Model != "" {
			item["configuredModel"] = e.Model
			item["model"] = e.Model
		}
		if e.Effort != "" {
			item["configuredEffort"] = e.Effort
			item["effort"] = e.Effort
		}
		out = append(out, item)
	}
	return out
}
