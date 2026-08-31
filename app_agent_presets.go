package main

// app_agent_presets.go — DSH 四模式（Agent 预设）接入。
//
// DSH 内置四个 Agent preset（agentPreset.list 实时确认）：
//   standard  标准模式 —— 功能完整编码 Agent（文件编辑/Shell/检索/Skills/计划/目标/子代理/工作流）
//   code      PTC 模式  —— 标准能力 + Code Mode SDK，用 TypeScript 程序组合多步操作
//   minimal   极简模式  —— 仅持久 bash + str_replace_editor 双工具
//   cordis    创造模式  —— 创建自定义 preset（运行时检查/插件实验/创作指导）
//
// 桥方法：
//   AgentPresets()          拉取 DSH 预设清单（名称/描述/isDefault）
//   SetAgentPresetForTab    为某会话切换预设（agentPreset.select）
//   SetDefaultAgentPreset   持久化默认预设（新建会话用）
//   SetModeForTab           升级：识别 DSH 预设名时转发 agentPreset.select，
//                           其余（Reasonix normal/plan/yolo）保持原语义（DSH 会话权限自行控制）。

import (
	"fmt"
	"strings"
)

// dshAgentPresetIDs 内置四模式 id（与 DSH agentPreset.list 一致）。
var dshAgentPresetIDs = map[string]string{
	"standard": "standard",
	"code":     "code",
	"minimal":  "minimal",
	"cordis":   "cordis",
	// 中文名兼容
	"标准模式": "standard",
	"ptc模式":  "code",
	"PTC模式":  "code",
	"极简模式": "minimal",
	"创造模式": "cordis",
}

// normalizeAgentPreset 把任意输入归一化成合法 preset id（"" = 不合法）。
func normalizeAgentPreset(v string) string {
	v = strings.TrimSpace(v)
	if id, ok := dshAgentPresetIDs[strings.ToLower(v)]; ok {
		return id
	}
	if id, ok := dshAgentPresetIDs[v]; ok {
		return id
	}
	return ""
}

// AgentPresets 拉取 DSH Agent 预设清单（四模式）。
// 返回 {presets:[{id,name,description,isDefault,trust}], authorable, hasDocument}。
func (a *App) AgentPresets() map[string]any {
	out := map[string]any{
		"presets":     []any{},
		"authorable":  false,
		"hasDocument": false,
		"error":       "",
	}
	if a.dsh == nil {
		out["error"] = "dsh client not initialized"
		return out
	}
	raw, err := a.dsh.RPC("agentPreset.list", map[string]any{})
	if err != nil {
		out["error"] = err.Error()
		return out
	}
	var v struct {
		Presets []struct {
			ID          string `json:"id"`
			Trust       string `json:"trust"`
			IsDefault   bool   `json:"isDefault"`
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"presets"`
		Authorable  bool `json:"authorable"`
		HasDocument bool `json:"hasDocument"`
	}
	if err := DecodeRPC(raw, &v); err != nil {
		out["error"] = "agentPreset.list decode: " + err.Error()
		return out
	}
	presets := []any{}
	for _, p := range v.Presets {
		presets = append(presets, map[string]any{
			"id":          p.ID,
			"name":        p.Name,
			"description": p.Description,
			"isDefault":   p.IsDefault,
			"trust":       p.Trust,
		})
	}
	out["presets"] = presets
	out["authorable"] = v.Authorable
	out["hasDocument"] = v.HasDocument
	return out
}

// SetAgentPresetForTab 为指定会话切换 Agent 预设（DSH 四模式）。
// presetID 支持 id（standard/code/minimal/cordis）或中文名。
func (a *App) SetAgentPresetForTab(tabID, presetID string) map[string]any {
	id := normalizeAgentPreset(presetID)
	if id == "" {
		return map[string]any{"ok": false, "error": fmt.Sprintf("unknown preset %q", presetID)}
	}
	sid := a.activeSessionID(tabID)
	if sid == "" {
		sid = tabID
	}
	if a.dsh == nil {
		return map[string]any{"ok": false, "error": "dsh client not initialized"}
	}
	if _, err := a.dsh.RPC("agentPreset.select", map[string]any{
		"sessionId":   sid,
		"agentPreset": id,
	}); err != nil {
		return map[string]any{"ok": false, "error": err.Error()}
	}
	resumeLog("agentPreset.select session=%s preset=%s", sid, id)
	return map[string]any{"ok": true, "preset": id}
}

// SetDefaultAgentPreset 持久化默认 Agent 预设（新建会话用）。
func (a *App) SetDefaultAgentPreset(presetID string) map[string]any {
	id := normalizeAgentPreset(presetID)
	if id == "" {
		return map[string]any{"ok": false, "error": fmt.Sprintf("unknown preset %q", presetID)}
	}
	a.st.SetDefaultAgentPreset(id)
	return map[string]any{"ok": true, "preset": id}
}

// DefaultAgentPreset 读取当前默认预设。
func (a *App) DefaultAgentPreset() map[string]any {
	return map[string]any{"preset": a.st.DefaultAgentPreset()}
}

// SetModeForTab 指定会话模式。
// 兼容两套：DSH 预设名（standard/code/minimal/cordis）→ agentPreset.select；
// Reasonix 模式名（normal/plan/yolo/plan-yolo）→ 本地记录（DSH 会话权限由其自身控制）。
func (a *App) SetModeForTab(tabID, mode string) error {
	if id := normalizeAgentPreset(mode); id != "" {
		r := a.SetAgentPresetForTab(tabID, id)
		if ok, _ := r["ok"].(bool); !ok {
			if e, _ := r["error"].(string); e != "" {
				return fmt.Errorf("set preset: %s", e)
			}
			return fmt.Errorf("set preset failed")
		}
		return nil
	}
	// Reasonix 模式：DSH 会话权限由 agent 自身控制，这里仅记录日志（不崩溃）。
	resumeLog("SetModeForTab tab=%s mode=%s (reasonix mode, no-op on DSH)", tabID, mode)
	return nil
}
