package main

// App 的模型桥方法（DSH session.models / session.selectModel 透传）。
// DSH 的模型数据经 session.models 读取（分组 + 当前选择），这里转换成前端期望的
// ModelRef 列表（{ref, provider, model, current}）。
//
// 实测 DSH 契约（2026-08）：
//   - session.models   payload={sessionId}，返回 {current:{provider,model,reasoningEffort}, groups:[{id,name,models:[{id,name,reasoning:{efforts:[{id,name}],defaultEffort}}]}]}
//   - session.selectModel payload={sessionId,provider,model,reasoningEffort?}——必须同时带 provider+model，单传 reasoningEffort 会 400

// dshEffort 是单个档位。
type dshEffort struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// dshReasoning 是模型的推理能力（档位列表）。
type dshReasoning struct {
	Efforts       []dshEffort `json:"efforts"`
	DefaultEffort string      `json:"defaultEffort"`
}

// dshModel 是分组内的单个模型。
type dshModel struct {
	ID        string        `json:"id"`
	Name      string        `json:"name"`
	Reasoning *dshReasoning `json:"reasoning"`
}

// dshModelGroup 是 session.models 返回的分组（provider）。
type dshModelGroup struct {
	ID     string     `json:"id"`
	Name   string     `json:"name"`
	Models []dshModel `json:"models"`
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

// activeSessionID 取当前会话 ID（tabID 为空时用 session.list 第一个活跃会话）。
// 本项目中 tabID 即 DSH sessionId（见 app_session.go 的 tabMeta），故非空时直接返回。
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
	return modelsRefsFrom(a.modelsView(tabID))
}

// modelsRefsFrom 纯函数：把 session.models 视图转成 ModelRef 列表（可测）。
func modelsRefsFrom(m *dshModelsView) []any {
	if m == nil {
		return []any{}
	}
	out := []any{}
	for _, g := range m.Groups {
		for _, mod := range g.Models {
			cur := m.Current != nil && m.Current.Provider == g.ID && m.Current.Model == mod.ID
			it := map[string]any{
				"ref":      g.ID + "/" + mod.ID,
				"provider": g.ID,
				"model":    mod.ID,
				"current":  cur,
			}
			if mod.Name != "" {
				it["name"] = mod.Name
			}
			if mod.Reasoning != nil && len(mod.Reasoning.Efforts) > 0 {
				efforts := []any{}
				for _, e := range mod.Reasoning.Efforts {
					efforts = append(efforts, e.ID)
				}
				it["efforts"] = efforts
				it["defaultEffort"] = mod.Reasoning.DefaultEffort
			}
			out = append(out, it)
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

// currentReasoning 取当前选中模型的档位能力（无则 ok=false）。
func currentReasoning(m *dshModelsView) (*dshReasoning, bool) {
	if m == nil || m.Current == nil {
		return nil, false
	}
	for i := range m.Groups {
		g := &m.Groups[i]
		if g.ID != m.Current.Provider {
			continue
		}
		for j := range g.Models {
			if g.Models[j].ID == m.Current.Model {
				if g.Models[j].Reasoning != nil && len(g.Models[j].Reasoning.Efforts) > 0 {
					return g.Models[j].Reasoning, true
				}
				return nil, false
			}
		}
	}
	return nil, false
}

// EffortForTab 返回推理强度选项（DSH 的 reasoningEffort 档位）。
// 无档位模型（如 xtoken 组）返回 supported:true + levels:["auto"]——前端
// EffortSwitcher 在 supported:false 或 levels 为空时不渲染，用户要求档位选择器
// 始终显示，无档位时显示 "auto"（默认）。
func (a *App) EffortForTab(tabID string) map[string]any {
	m := a.modelsView(tabID)
	if m == nil || m.Current == nil {
		return map[string]any{"supported": true, "current": "auto", "default": "auto", "levels": []any{"auto"}}
	}
	reasoning, ok := currentReasoning(m)
	if !ok {
		return map[string]any{"supported": true, "current": "auto", "default": "auto", "levels": []any{"auto"}}
	}
	def := reasoning.DefaultEffort
	if def == "" {
		def = "high"
	}
	cur := m.Current.ReasoningEffort
	if cur == "" {
		cur = def
	}
	levels := []any{}
	for _, e := range reasoning.Efforts {
		levels = append(levels, e.ID)
	}
	return map[string]any{
		"supported": true, "current": cur, "default": def, "levels": levels,
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

// setModel 调 DSH session.selectModel（provider/model 拆分自 ref）。
// 切模型时带上当前档位（若目标模型支持）——否则 DSH 会把档位重置为默认值，
// 造成前端显示"档位跳来跳去"。
func (a *App) setModel(tabID, ref string) error {
	if a.dsh == nil || ref == "" {
		return nil
	}
	sid := a.activeSessionID(tabID)
	if sid == "" {
		return nil
	}
	provider, model := splitRef(ref)
	if provider == "" || model == "" {
		return nil
	}
	effort := ""
	if m := a.modelsView(tabID); m != nil && m.Current != nil {
		effort = effortForTarget(m, provider, model, m.Current.ReasoningEffort)
	}
	payload := map[string]any{"sessionId": sid, "provider": provider, "model": model}
	if effort != "" {
		payload["reasoningEffort"] = effort
	}
	_, err := a.dsh.RPC("session.selectModel", payload)
	return err
}

// effortForTarget 判断目标模型是否支持当前档位：
//   - 目标模型无档位能力 → 返回 ""（不带，DSH 用默认）
//   - 目标支持当前档位 → 返回当前档位（切模型保持档位不跳）
//   - 目标不支持当前档位 → 返回 ""（用目标默认档位）
// 带不支持的档位切模型会让 DSH 报错拒绝切换，所以必须校验。
func effortForTarget(m *dshModelsView, provider, model, curEffort string) string {
	if curEffort == "" || m == nil {
		return ""
	}
	for i := range m.Groups {
		g := &m.Groups[i]
		if g.ID != provider {
			continue
		}
		for j := range g.Models {
			mod := &g.Models[j]
			if mod.ID != model {
				continue
			}
			if mod.Reasoning == nil {
				return ""
			}
			for _, e := range mod.Reasoning.Efforts {
				if e.ID == curEffort {
					return curEffort
				}
			}
			return ""
		}
	}
	return ""
}

// splitRef 把 "provider/model" 拆成两部分（无斜杠时 model=provider）。
func splitRef(ref string) (provider, model string) {
	provider, model = ref, ref
	for i := 0; i < len(ref); i++ {
		if ref[i] == '/' {
			provider = ref[:i]
			if i+1 < len(ref) {
				model = ref[i+1:]
			}
			break
		}
	}
	return provider, model
}

// SetEffort / SetEffortForTab 设置推理强度（档位）。
// DSH selectModel 要求连同当前 provider/model 一起传，单传 reasoningEffort 会 400。
func (a *App) SetEffort(effort string) error { return a.setEffort("", effort) }
func (a *App) SetEffortForTab(tabID, effort string) error { return a.setEffort(tabID, effort) }
func (a *App) setEffort(tabID, effort string) error {
	// "auto" 表示无档位/跟随模型默认——不向 DSH 传非法档位（DSH 只认 off/low/high/max）。
	if a.dsh == nil || effort == "" || effort == "auto" {
		return nil
	}
	sid := a.activeSessionID(tabID)
	if sid == "" {
		return nil
	}
	provider, model := "", ""
	if m := a.modelsView(tabID); m != nil && m.Current != nil {
		provider, model = m.Current.Provider, m.Current.Model
	}
	if provider == "" || model == "" {
		return nil
	}
	_, err := a.dsh.RPC("session.selectModel", map[string]any{
		"sessionId": sid, "provider": provider, "model": model, "reasoningEffort": effort,
	})
	return err
}
