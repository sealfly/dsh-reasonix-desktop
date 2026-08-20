package main

// App 的模型桥方法（DSH session.models / session.selectModel 透传）。
// DSH 的模型数据经 session.models 读取（分组 + 当前选择），这里转换成前端期望的
// ModelRef 列表（{ref, provider, model, current}）。

// dshModelGroup 是 session.models 返回的分组。
type dshModelGroup struct {
	ID     string `json:"id"`
	Models []struct {
		ID string `json:"id"`
	} `json:"models"`
}

// dshModelCurrent 是当前选中的模型。
type dshModelCurrent struct {
	Provider        string `json:"provider"`
	Model           string `json:"model"`
	ReasoningEffort string `json:"reasoningEffort"`
}

// dshModelsView 是 session.models 的返回结构。
type dshModelsView struct {
	Groups  []dshModelGroup  `json:"groups"`
	Current *dshModelCurrent `json:"current"`
}

// activeSessionID 取当前会话 ID（tabID 为空时用 session.list 第一个）。
func (a *App) activeSessionID(tabID string) string {
	if tabID != "" {
		return tabID
	}
	if a.dsh == nil {
		return ""
	}
	raw, err := a.dsh.RPC("session.list", map[string]any{})
	if err != nil {
		return ""
	}
	var list struct {
		Items []struct {
			SessionID string `json:"sessionId"`
		} `json:"items"`
	}
	if err := DecodeRPC(raw, &list); err != nil {
		return ""
	}
	if len(list.Items) > 0 {
		return list.Items[0].SessionID
	}
	return ""
}

// modelsView 读取 session.models。
func (a *App) modelsView(tabID string) *dshModelsView {
	if a.dsh == nil {
		return nil
	}
	sid := a.activeSessionID(tabID)
	if sid == "" {
		return nil
	}
	raw, err := a.dsh.RPC("session.models", map[string]any{"sessionId": sid})
	if err != nil {
		return nil
	}
	var m dshModelsView
	if err := DecodeRPC(raw, &m); err != nil {
		return nil
	}
	return &m
}

// Models 返回模型列表（前端模型选择器）。
func (a *App) Models() []any {
	return a.modelsRefs("")
}

// ModelsForTab 返回指定会话的模型列表。
func (a *App) ModelsForTab(tabID string) []any {
	return a.modelsRefs(tabID)
}

// modelsRefs 把 session.models 的分组转成 ModelRef 列表。
func (a *App) modelsRefs(tabID string) []any {
	m := a.modelsView(tabID)
	if m == nil {
		return []any{}
	}
	out := []any{}
	for _, g := range m.Groups {
		for _, mod := range g.Models {
			cur := m.Current != nil && m.Current.Provider == g.ID && m.Current.Model == mod.ID
			out = append(out, map[string]any{
				"ref":      g.ID + "/" + mod.ID,
				"provider": g.ID,
				"model":    mod.ID,
				"current":  cur,
			})
		}
	}
	return out
}

// ModelForTab 返回当前模型 ref（"provider/model"）。
func (a *App) ModelForTab(tabID string) string {
	m := a.modelsView(tabID)
	if m != nil && m.Current != nil {
		return m.Current.Provider + "/" + m.Current.Model
	}
	return "deepseek-official/deepseek-v4-flash"
}

// EffortForTab 返回推理强度选项（DSH 的 reasoningEffort）。
func (a *App) EffortForTab(tabID string) map[string]any {
	m := a.modelsView(tabID)
	if m == nil || m.Current == nil {
		return map[string]any{"supported": false, "current": "auto", "default": "auto", "levels": []any{}}
	}
	cur := m.Current.ReasoningEffort
	if cur == "" {
		cur = "high"
	}
	return map[string]any{
		"supported": true, "current": cur, "default": "high",
		"levels": []any{"off", "high", "max"},
	}
}

// Effort 同 EffortForTab（当前会话）。
func (a *App) Effort() map[string]any { return a.EffortForTab("") }

// DefaultModel 返回默认模型。
func (a *App) DefaultModel() string { return "deepseek-official/deepseek-v4-flash" }

// SetModel 设置当前会话模型。
func (a *App) SetModel(ref string) error { return a.setModel("", ref) }

// SetModelForTab 设置指定会话模型。
func (a *App) SetModelForTab(tabID, ref string) error { return a.setModel(tabID, ref) }

// setModel 调 DSH session.selectModel。
func (a *App) setModel(tabID, ref string) error {
	if a.dsh == nil || ref == "" {
		return nil
	}
	sid := a.activeSessionID(tabID)
	if sid == "" {
		return nil
	}
	provider := ref
	model := ref
	for i := 0; i < len(ref); i++ {
		if ref[i] == '/' {
			provider = ref[:i]
			if i+1 < len(ref) {
				model = ref[i+1:]
			}
			break
		}
	}
	_, err := a.dsh.RPC("session.selectModel", map[string]any{
		"sessionId": sid, "provider": provider, "model": model,
	})
	return err
}

// SetEffort / SetEffortForTab 设置推理强度（DSH 若支持）。
func (a *App) SetEffort(effort string) error { return a.setEffort("", effort) }
func (a *App) SetEffortForTab(tabID, effort string) error { return a.setEffort(tabID, effort) }
func (a *App) setEffort(tabID, effort string) error {
	if a.dsh == nil || effort == "" {
		return nil
	}
	sid := a.activeSessionID(tabID)
	if sid == "" {
		return nil
	}
	_, err := a.dsh.RPC("session.selectModel", map[string]any{
		"sessionId": sid, "reasoningEffort": effort,
	})
	return err
}
