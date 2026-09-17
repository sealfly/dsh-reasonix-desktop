package main

// app_settings_extra.go — 补齐 v1.38.2 设置契约里剩下的字段与对应 setter。
//
// 为什么单独一个文件：这些字段跨了两个数据源，混进 app_settings.go 会看不清边界——
//   - **DSH 拥有的**（真值必须从 DSH 读，不能自己编）：默认模型/推理强度（agent-default-model）、
//     联网搜索模型（web-search-deepseek.model）。
//   - **本项目桌面偏好**（我们自己的 settings.json）：视觉/搜索模型引用、子代理限制、
//     压缩比、推理语言、代理设置、代理模式的 UI 状态。
//
// 契约字段名见 v1.38.2 lib/types.ts 的 SettingsView（缺字段会让前端静默回落默认值，
// 这正是「主题跳变」那一类问题的成因，见 PRINCIPLES 原则 6 类别 3）。

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

const (
	// dshWebSearchNS 是 DSH 承载联网搜索配置的命名空间。
	dshWebSearchNS = "web-search-deepseek"
	// dshDefaultModelNS 是 DSH 的默认模型命名空间。
	dshDefaultModelNS = "agent-default-model"
)

// dshNamespaceValue 读取某个 settings 命名空间的值（不存在时返回 nil, false）。
func (a *App) dshNamespaceValue(ns string) (map[string]any, bool) {
	nss, err := a.dshSettingsNamespaces()
	if err != nil {
		return nil, false
	}
	for _, n := range nss {
		if n.NS != ns {
			continue
		}
		if len(n.Value) == 0 {
			return map[string]any{}, true
		}
		var value map[string]any
		if err := json.Unmarshal(n.Value, &value); err != nil {
			return nil, false
		}
		return value, true
	}
	return nil, false
}

// dshNamespaceSet 写入某个 settings 命名空间的一个字段（分段路径）。
func (a *App) dshNamespaceSet(ns, field string, value any) error {
	if a.dsh == nil {
		return fmt.Errorf("DSH 未连接")
	}
	if _, err := a.dsh.RPC("settings.mutate", map[string]any{
		"ns": ns,
		"ops": []map[string]any{{
			"op":    "set",
			"path":  strings.Split(field, "."),
			"value": value,
		}},
	}); err != nil {
		return err
	}
	return nil
}

// ===== 联网搜索模型（DSH 拥有）=====

// webSearchState 返回联网搜索模型相关的契约字段。
//
// webSearchModel 的真实来源是 DSH 的 web-search-deepseek.model：
// 用户在设置页选了搜索模型，实际是写进 DSH 的搜索插件配置里的。
func (a *App) webSearchState() map[string]any {
	value, _ := a.dshNamespaceValue(dshWebSearchNS)
	configured := ""
	if value != nil {
		if v, ok := value["model"].(string); ok {
			configured = strings.TrimSpace(v)
		}
	}
	candidates := []string{}
	for _, ref := range a.modelsRefs("") {
		if s, ok := ref.(string); ok && s != "" {
			candidates = append(candidates, s)
		}
	}
	effective := configured
	if effective == "" {
		effective = a.st.DefaultModel()
	}
	status := "unset"
	reason := "尚未指定联网搜索模型，DSH 搜索插件将使用默认模型"
	if configured != "" {
		status = "ready"
		reason = ""
	}
	return map[string]any{
		"webSearchModel":           configured,
		"webSearchModels":          candidates,
		"webSearchModelStatus":     status,
		"webSearchModelReason":     reason,
		"effectiveWebSearchModel":  effective,
		"webSearchModelOverridden": configured != "",
	}
}

// SetWebSearchModel 设置联网搜索模型（写 DSH 的 web-search-deepseek.model）。
// 空串表示交还给 DSH 默认。
func (a *App) SetWebSearchModel(ref string) error {
	ref = strings.TrimSpace(ref)
	if err := a.dshNamespaceSet(dshWebSearchNS, "model", ref); err != nil {
		return fmt.Errorf("保存联网搜索模型失败: %w", err)
	}
	value, _ := a.dshNamespaceValue(dshWebSearchNS)
	got := ""
	if value != nil {
		if v, ok := value["model"].(string); ok {
			got = strings.TrimSpace(v)
		}
	}
	if got != ref {
		return fmt.Errorf("DSH 未接受联网搜索模型 %q（读回 %q）", ref, got)
	}
	return nil
}

// SetVisionModel 设置视觉模型引用（本项目桌面偏好：DSH 侧视觉能力由模型自身声明决定，
// 这里只记录用户的选择，供前端展示与本地文件预览等场景使用）。
func (a *App) SetVisionModel(ref string) error {
	a.st.SetVisionModel(strings.TrimSpace(ref))
	return nil
}

// ===== 网络代理（本项目桌面偏好 + 进程环境）=====

// networkView 返回设置-网络页的视图。
//
// DSH 的代理来自进程环境（HTTP_PROXY/HTTPS_PROXY/NO_PROXY），没有可写的 settings 命名空间，
// 所以这里：展示环境里的真实代理 + 展示用户在本应用里记录的偏好（供 UI 往返）。
func (a *App) networkView() map[string]any {
	envProxy := firstNonEmpty(os.Getenv("HTTPS_PROXY"), os.Getenv("https_proxy"),
		os.Getenv("HTTP_PROXY"), os.Getenv("http_proxy"))
	noProxy := firstNonEmpty(os.Getenv("NO_PROXY"), os.Getenv("no_proxy"))

	mode := a.st.ProxyMode()
	url := a.st.ProxyURL()
	if mode == "" {
		if envProxy != "" {
			mode = "system" // 环境已配代理：界面显示"跟随系统"
			if url == "" {
				url = envProxy
			}
		} else {
			mode = "off"
		}
	}
	proxy := map[string]any{
		"type":     mode,
		"server":   url,
		"port":     0,
		"username": "",
		"password": "",
	}
	return map[string]any{
		"proxyMode": mode,
		"proxyUrl":  url,
		"noProxy":   firstNonEmpty(a.st.NoProxy(), noProxy),
		"proxy":     proxy,
	}
}

// SetNetwork 保存网络代理偏好（界面往返；实际生效取决于进程环境与 DSH 自身）。
func (a *App) SetNetwork(n map[string]any) error {
	mode := strings.TrimSpace(strAt(n, "proxyMode"))
	if mode == "" {
		if proxy, ok := n["proxy"].(map[string]any); ok {
			mode = strings.TrimSpace(strAt(proxy, "type"))
		}
	}
	url := firstNonEmpty(strAt(n, "proxyUrl"), strAt(n, "proxyUrl"))
	if url == "" {
		if proxy, ok := n["proxy"].(map[string]any); ok {
			url = strAt(proxy, "server")
		}
	}
	a.st.SetProxy(mode, url, strings.TrimSpace(strAt(n, "noProxy")))
	return nil
}

// ===== 智能体参数 =====

// agentView 返回设置-智能体页的视图。
//
// 说明：温度/步数/系统提示词是 **Reasonix 桌面端**的参数；DSH 的对应能力由
// agent-presets 与 agent-loop 命名空间决定（本桥暂未做双向映射），
// 因此这里如实回报本应用记录的值，不由本应用伪造 DSH 的行为。
// 子代理上限与压缩比是本桥真实生效的参数（提交会话时传给 DSH）。
func (a *App) agentView() map[string]any {
	ratio := a.st.CompactRatio()
	return map[string]any{
		"temperature":              a.st.Temperature(),
		"maxSteps":                 a.st.MaxSteps(),
		"plannerMaxSteps":          a.st.PlannerMaxSteps(),
		"maxSubagentDepth":         a.st.MaxSubagentDepth(),
		"maxSubagentConcurrency":   a.st.MaxSubagentConcurrency(),
		"maxParallelWriters":       a.st.MaxParallelWriters(),
		"systemPrompt":             a.st.SystemPrompt(),
		"reasoningLanguage":        a.st.ReasoningLanguage(),
		"compactRatio":             ratio,
		"effectiveCompactRatio":    ratio,
		"compactRatioOverridden":   ratio > 0,
	}
}

// SetAgentParams 保存智能体参数（前端设置-智能体页）。
func (a *App) SetAgentParams(temperature float64, maxSteps, plannerMaxSteps int, systemPrompt string) error {
	a.st.SetAgentParams(temperature, maxSteps, plannerMaxSteps, systemPrompt)
	return nil
}

// SetReasoningLanguage 保存推理语言（auto/zh/en...）。
func (a *App) SetReasoningLanguage(lang string) error {
	a.st.SetReasoningLanguage(strings.TrimSpace(lang))
	return nil
}

// ===== 工具授权开关 =====

// SetBypass 打开/关闭"完全授权"（绕过审批）。
// 另一个开关 SetAutoApproveTools（mode=auto）在 app_stubs_v1314.go；
// 契约里 autoApproveTools 与 bypass 是两个独立字段，故两者分别映射，界面开关才能各反映真实状态。
func (a *App) SetBypass(on bool) error {
	mode := "ask"
	if on {
		mode = "yolo"
	}
	return a.SetToolApprovalModeForTab("", mode)
}

// toolApprovalFlags 由审批模式推导契约里的两个布尔字段。
func (a *App) toolApprovalFlags() (autoApprove bool, bypass bool) {
	switch a.st.DefaultToolApprovalMode() {
	case "auto":
		return true, false
	case "yolo":
		return true, true
	default:
		return false, false
	}
}
