package main

// app_providers_presets.go — 「模型服务」页用的供应商预设目录 + 模型目录/测试桥方法。
//
// 预设是**本项目提供的、确定可用的**第三方网关模板：每个预设明确写出 DSH 协议、
// 端点与凭据引用，用户点一下即可接入（等同于在 settings.yaml 里手写一段 llm-pi-ai 配置）。
//
// 设计取舍：
//   - 只收录「协议 + 端点 + key 环境变量」三元组明确的官方/主流网关，不猜端点；
//   - models 留空：模型列表由 FetchProviderModelCatalog 从端点实时拉（或用户在编辑页填），
//     避免把会过期的模型清单硬编码进来；
//   - 预设的 route 名固定，便于 UI 判断"已接入"（added）。

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
)

// providerPreset 是一个可一键接入的供应商模板。
type providerPreset struct {
	ID          string
	Label       string
	Description string
	KeyEnv      string
	Route       string // DSH 里的 provider route（settings.llm-pi-ai.providers 的键）
	API         string // DSH 协议
	BaseURL     string
	DisplayTier string // primary | advanced | compatibility
	Recommended bool
	DisplayOrder int
}

// profile 返回该预设对应的 DSH profile（不含 apiKeyEnv，由调用方决定）。
func (p providerPreset) profile() map[string]any {
	out := map[string]any{"api": p.API}
	if p.BaseURL != "" {
		out["baseURL"] = p.BaseURL
	}
	if p.Label != "" {
		out["displayName"] = p.Label
	}
	return out
}

// providerPresetCatalog 是本项目提供的预设目录。
//
// 端点均来自各家官方文档的 OpenAI/Anthropic 兼容入口；协议只能是 DSH 支持的三者。
var providerPresetCatalog = []providerPreset{
	{
		ID: "deepseek-api", Label: "DeepSeek API", Route: "deepseek-api",
		Description: "DeepSeek 官方 API Key 直连（按量计费）。DSH 内置的 deepseek-official 走订阅额度，这条是独立的路由。",
		KeyEnv: "DEEPSEEK_API_KEY", API: protoOpenAICompletions,
		BaseURL: "https://api.deepseek.com/v1", DisplayTier: "primary", Recommended: true, DisplayOrder: 10,
	},
	{
		ID: "openai", Label: "OpenAI", Route: "openai",
		Description: "OpenAI 官方 API（GPT 系列，Chat Completions 协议）。",
		KeyEnv: "OPENAI_API_KEY", API: protoOpenAICompletions,
		BaseURL: "https://api.openai.com/v1", DisplayTier: "primary", DisplayOrder: 20,
	},
	{
		ID: "openai-responses", Label: "OpenAI Responses", Route: "openai-responses",
		Description: "OpenAI Responses API（有状态响应协议，适合新一代推理模型）。",
		KeyEnv: "OPENAI_API_KEY", API: protoOpenAIResponses,
		BaseURL: "https://api.openai.com/v1", DisplayTier: "advanced", DisplayOrder: 25,
	},
	{
		ID: "anthropic", Label: "Anthropic", Route: "anthropic",
		Description: "Anthropic 官方 Messages 协议（Claude 系列）。",
		KeyEnv: "ANTHROPIC_API_KEY", API: protoAnthropicMessages,
		BaseURL: "https://api.anthropic.com", DisplayTier: "primary", DisplayOrder: 30,
	},
	{
		ID: "kimi-cn", Label: "Kimi（中国站）", Route: "kimi-cn",
		Description: "Moonshot Kimi 中国站 OpenAI 兼容入口。",
		KeyEnv: "MOONSHOT_API_KEY", API: protoOpenAICompletions,
		BaseURL: "https://api.moonshot.cn/v1", DisplayTier: "primary", DisplayOrder: 40,
	},
	{
		ID: "kimi-global", Label: "Kimi（国际站）", Route: "kimi-global",
		Description: "Moonshot Kimi 国际站 OpenAI 兼容入口。",
		KeyEnv: "MOONSHOT_API_KEY", API: protoOpenAICompletions,
		BaseURL: "https://api.moonshot.ai/v1", DisplayTier: "primary", DisplayOrder: 45,
	},
	{
		ID: "glm-cn", Label: "智谱 GLM", Route: "glm-cn",
		Description: "智谱 AI 中国站 OpenAI 兼容入口（GLM 系列）。",
		KeyEnv: "GLM_API_KEY", API: protoOpenAICompletions,
		BaseURL: "https://open.bigmodel.cn/api/paas/v4", DisplayTier: "primary", DisplayOrder: 50,
	},
	{
		ID: "zai-global", Label: "Z.AI（国际站）", Route: "zai-global",
		Description: "Z.AI 国际站 OpenAI 兼容入口（GLM 系列）。",
		KeyEnv: "ZAI_API_KEY", API: protoOpenAICompletions,
		BaseURL: "https://api.z.ai/api/paas/v4", DisplayTier: "primary", DisplayOrder: 55,
	},
	{
		ID: "qwen-cn", Label: "通义千问（中国站）", Route: "qwen-cn",
		Description: "阿里云百炼 DashScope 中国站 OpenAI 兼容入口。",
		KeyEnv: "DASHSCOPE_API_KEY", API: protoOpenAICompletions,
		BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1", DisplayTier: "primary", DisplayOrder: 60,
	},
	{
		ID: "qwen-global", Label: "通义千问（国际站）", Route: "qwen-global",
		Description: "阿里云百炼 DashScope 国际站 OpenAI 兼容入口。",
		KeyEnv: "DASHSCOPE_API_KEY", API: protoOpenAICompletions,
		BaseURL: "https://dashscope-intl.aliyuncs.com/compatible-mode/v1", DisplayTier: "primary", DisplayOrder: 65,
	},
	{
		ID: "minimax-cn", Label: "MiniMax", Route: "minimax-cn",
		Description: "MiniMax 开放平台 OpenAI 兼容入口。",
		KeyEnv: "MINIMAX_API_KEY", API: protoOpenAICompletions,
		BaseURL: "https://api.minimaxi.com/v1", DisplayTier: "advanced", DisplayOrder: 70,
	},
	{
		ID: "stepfun", Label: "阶跃星辰 StepFun", Route: "stepfun",
		Description: "StepFun 开放平台 OpenAI 兼容入口。",
		KeyEnv: "STEPFUN_API_KEY", API: protoOpenAICompletions,
		BaseURL: "https://api.stepfun.com/v1", DisplayTier: "advanced", DisplayOrder: 75,
	},
	{
		ID: "siliconflow", Label: "硅基流动 SiliconFlow", Route: "siliconflow",
		Description: "SiliconFlow 多模型聚合网关（OpenAI 兼容）。",
		KeyEnv: "SILICONFLOW_API_KEY", API: protoOpenAICompletions,
		BaseURL: "https://api.siliconflow.cn/v1", DisplayTier: "compatibility", DisplayOrder: 80,
	},
	{
		ID: "openrouter", Label: "OpenRouter", Route: "openrouter",
		Description: "OpenRouter 多模型聚合网关（OpenAI 兼容）。",
		KeyEnv: "OPENROUTER_API_KEY", API: protoOpenAICompletions,
		BaseURL: "https://openrouter.ai/api/v1", DisplayTier: "compatibility", DisplayOrder: 85,
	},
	{
		ID: "novita", Label: "NovitaAI", Route: "novita",
		Description: "NovitaAI 多模型网关（OpenAI 兼容）。",
		KeyEnv: "NOVITA_API_KEY", API: protoOpenAICompletions,
		BaseURL: "https://api.novita.ai/v3/openai", DisplayTier: "compatibility", DisplayOrder: 90,
	},
	{
		ID: "ollama-local", Label: "Ollama（本机）", Route: "ollama-local",
		Description: "本机 Ollama 服务（默认 11434 端口，OpenAI 兼容），无需 API key。",
		KeyEnv: "", API: protoOpenAICompletions,
		BaseURL: "http://127.0.0.1:11434/v1", DisplayTier: "compatibility", DisplayOrder: 95,
	},
}

// lookupProviderPreset 按 id 查预设。
func lookupProviderPreset(id string) (providerPreset, bool) {
	key := strings.ToLower(strings.TrimSpace(id))
	if key == "" {
		return providerPreset{}, false
	}
	for _, p := range providerPresetCatalog {
		if strings.ToLower(p.ID) == key {
			return p, true
		}
	}
	return providerPreset{}, false
}

// providerKinds 是自定义供应商表单可选的服务类型（值取自前沿 mock 契约）。
var providerKinds = []string{"openai", "anthropic", "responses"}

// providerPresetViews 生成设置页用的预设视图（ProviderPresetView[]）。
//
// added/keySet 是**实时**状态：route 是否已存在于 DSH settings，凭据是否已配置。
func (a *App) providerPresetViews() []any {
	profiles, err := a.providerProfiles()
	if err != nil {
		// DSH 读不到时返回空表而不是伪造"未接入"，避免用户点了添加却写到别处。
		return []any{}
	}

	envs := []string{}
	for _, p := range providerPresetCatalog {
		if p.KeyEnv != "" {
			envs = append(envs, p.KeyEnv)
		}
	}
	keyStatus := a.providerCredentialStatus(envs)

	ordered := make([]providerPreset, len(providerPresetCatalog))
	copy(ordered, providerPresetCatalog)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].DisplayOrder < ordered[j].DisplayOrder })

	out := []any{}
	for _, p := range ordered {
		route := providerRouteName(p.Route)
		_, added := profiles[route]
		// 也认"别人已经配过同一端点"的情况：route 名不同但 baseURL 相同 → 视为已接入同款。
		if !added && p.BaseURL != "" {
			for _, prof := range profiles {
				if strings.EqualFold(strings.TrimRight(strAt(prof, "baseURL"), "/"), strings.TrimRight(p.BaseURL, "/")) {
					added = true
					break
				}
			}
		}
		view := map[string]any{
			"id":            p.ID,
			"label":         p.Label,
			"description":   p.Description,
			"keyEnv":        p.KeyEnv,
			"providerNames": []any{route},
			"models":        providerModelsFromDSHProfile(profiles[route]),
			"added":         added,
			"keySet":        p.KeyEnv == "" || keyStatus[p.KeyEnv],
			"routeKind":     p.API,
			"displayTier":   p.DisplayTier,
			"displayOrder":  p.DisplayOrder,
			"optional":      p.KeyEnv == "",
			"recommended":   p.Recommended,
			"requiresKey":   p.KeyEnv != "",
			"catalog": map[string]any{
				"brandId":    p.ID,
				"brandLabel": p.Label,
				"region":     providerPresetRegion(p.ID),
				"product":    "api",
				"format":     p.API,
				"baseUrl":    p.BaseURL,
				"protocols": map[string]any{
					p.API: map[string]any{
						"baseUrl":   p.BaseURL,
						"source":    "dsh-reasonix-preset",
						"checkedOn": "",
					},
				},
			},
		}
		if added {
			view["status"] = "installed"
		} else {
			view["status"] = "available"
		}
		out = append(out, view)
	}
	return out
}

// providerPresetRegion 给预设标一个区域（仅用于分组展示）。
func providerPresetRegion(id string) string {
	if strings.HasSuffix(id, "-cn") {
		return "cn"
	}
	if strings.HasSuffix(id, "-global") {
		return "global"
	}
	return ""
}

// providerModelsFromDSHProfile 取 profile 里已配置的模型 id。
func providerModelsFromDSHProfile(profile map[string]any) []any {
	if profile == nil {
		return []any{}
	}
	models := providerModelsFromDSH(providerStrings(profile["models"]))
	out := make([]any, 0, len(models))
	for _, m := range models {
		out = append(out, m)
	}
	return out
}

// ===== 模型目录 =====

// providerViewFromAny 把前端传来的 ProviderView 归一成 map。
func providerViewFromAny(raw any) map[string]any {
	if m, ok := raw.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

// providerProfileForCatalog 得到"用于探测"的 profile：
// settings 里已有的用 settings 的（权威），否则用前端传来的视图（草稿/未保存）。
func (a *App) providerProfileForCatalog(p map[string]any) (map[string]any, string) {
	profiles, err := a.providerProfiles()
	route := providerRouteName(firstNonEmpty(strAt(p, "name"), strAt(p, "displayName")))
	if err == nil {
		if prof, ok := profiles[route]; ok {
			return prof, route
		}
		for r, prof := range profiles {
			if dn := strAt(prof, "displayName"); dn != "" && strings.EqualFold(dn, strAt(p, "displayName")) {
				return prof, r
			}
		}
	}
	return profileFromProviderView(p, "", ""), route
}

// discoverProviderModels 从供应商端点拉模型列表（仅 openai-completions / openai-responses）。
// 返回模型 id 列表；错误由调用方决定是提示还是忽略。
func discoverProviderModels(profile map[string]any, apiKey string) ([]string, error) {
	api := strAt(profile, "api")
	if api == protoAnthropicMessages {
		return nil, fmt.Errorf("anthropic-messages 协议不支持从端点列举模型（DSH 侧限制）")
	}
	baseURL := strings.TrimRight(strAt(profile, "baseURL"), "/")
	if baseURL == "" {
		return nil, fmt.Errorf("供应商没有 baseURL")
	}
	req, err := http.NewRequest("GET", baseURL+"/models", nil)
	if err != nil {
		return nil, err
	}
	key := firstNonEmpty(apiKey, strAt(profile, "apiKey"))
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	if headers, ok := profile["headers"].(map[string]any); ok {
		for k, v := range headers {
			if s, ok := v.(string); ok {
				req.Header.Set(k, s)
			}
		}
	}
	resp, err := providerProbeClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求 %s/models 失败: %w", baseURL, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%s/models 返回 %d: %s", baseURL, resp.StatusCode, truncateForMessage(string(body), 200))
	}
	// 兼容两种常见形状：{data:[{id}]} 与 {models:[...]} / [ "id", ... ]
	var envelope struct {
		Data   []map[string]any `json:"data"`
		Models []any            `json:"models"`
	}
	ids := []string{}
	if err := json.Unmarshal(body, &envelope); err == nil {
		for _, m := range envelope.Data {
			if id, _ := m["id"].(string); id != "" {
				ids = append(ids, id)
			}
		}
		for _, m := range envelope.Models {
			switch v := m.(type) {
			case string:
				ids = append(ids, v)
			case map[string]any:
				if id, _ := v["id"].(string); id != "" {
					ids = append(ids, id)
				} else if name, _ := v["name"].(string); name != "" {
					ids = append(ids, name)
				}
			}
		}
	}
	if len(ids) == 0 {
		var list []map[string]any
		if err := json.Unmarshal(body, &list); err == nil {
			for _, m := range list {
				if id, _ := m["id"].(string); id != "" {
					ids = append(ids, id)
				}
			}
		}
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("%s/models 响应里没有解析到模型", baseURL)
	}
	sort.Strings(ids)
	return ids, nil
}

// truncateForMessage 截断长报文，避免把整页 HTML 塞进界面提示。
func truncateForMessage(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

// capabilityViews 把模型 id 列表转成前端 ProviderModelCapabilityView[]。
// source 说明这些能力信息从哪来（configured / provider-endpoint / dsh）。
func capabilityViews(models []string, source string, known map[string]bool) []any {
	out := []any{}
	for _, m := range models {
		state := "supported"
		view := map[string]any{
			"model":           m,
			"inputModalities": []any{"text"},
			"state":           state,
			"source":          source,
		}
		if known != nil && known[m] {
			view["inputModalities"] = []any{"text", "image"}
			view["automaticState"] = "supported"
			view["automaticSource"] = "dsh"
		}
		out = append(out, view)
	}
	return out
}

// providerVisionModelSet 从 DSH 的模型视图里猜出哪些模型带视觉能力。
// 只用 DSH 已声明的事实（模型名里带 vision/vl/omni 等），不做网络探测。
func (a *App) providerVisionModelSet() map[string]bool {
	out := map[string]bool{}
	m := a.modelsView("")
	if m == nil {
		return out
	}
	for _, g := range m.Groups {
		for _, model := range g.Models {
			id := strings.ToLower(model.ID)
			for _, hint := range []string{"vision", "-vl", "vl-", "omni", "image"} {
				if strings.Contains(id, hint) {
					out[model.ID] = true
					break
				}
			}
		}
	}
	return out
}

// FetchProviderModelCatalog 返回某供应商的模型能力列表。
// 优先用 settings 里已配置的模型；openai 系协议再尝试从端点实时补充（失败不影响返回）。
func (a *App) FetchProviderModelCatalog(p map[string]any) []any {
	return a.fetchProviderModelCatalog(p, "")
}

// FetchProviderModelCatalogDraft 用草稿 key 拉取（保存前"测试并拉取"）。
func (a *App) FetchProviderModelCatalogDraft(p map[string]any, key string) []any {
	return a.fetchProviderModelCatalog(p, key)
}

// fetchProviderModelCatalog 实现上面两者。
func (a *App) fetchProviderModelCatalog(p map[string]any, draftKey string) []any {
	profile, _ := a.providerProfileForCatalog(p)
	configured := providerModelsFromDSH(providerStrings(profile["models"]))
	key := draftKey
	if key == "" {
		if env := strAt(profile, "apiKeyEnv"); env != "" {
			// 已存凭据由 DSH 解析，这里不从磁盘读明文；仅在草稿场景用显式 key。
			key = ""
		}
	}
	discovered, err := discoverProviderModels(profile, key)
	models := configured
	source := "configured"
	if err == nil && len(discovered) > 0 {
		models = mergeModelIDs(configured, discovered)
		source = "configured+provider-endpoint"
	}
	return capabilityViews(models, source, a.providerVisionModelSet())
}

// mergeModelIDs 合并两组模型 id（去重、排序）。
func mergeModelIDs(groups ...[]string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, g := range groups {
		for _, id := range g {
			id = strings.TrimSpace(id)
			if id != "" && !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
	}
	sort.Strings(out)
	return out
}

// FetchProviderModels 返回某供应商的模型 id 列表（契约：Promise<string[]>）。
func (a *App) FetchProviderModels(config map[string]any) []any {
	views := a.fetchProviderModelCatalog(config, "")
	out := []any{}
	for _, raw := range views {
		if v, ok := raw.(map[string]any); ok {
			if id, _ := v["model"].(string); id != "" {
				out = append(out, id)
			}
		}
	}
	return out
}

// FetchAllProviderModels 返回全部供应商的模型列表（契约：Record<string, string[]>）。
func (a *App) FetchAllProviderModels(providers []any) map[string][]string {
	out := map[string][]string{}
	for _, raw := range providers {
		p := providerViewFromAny(raw)
		_, route := a.providerProfileForCatalog(p)
		if route == "" {
			route = providerRouteName(firstNonEmpty(strAt(p, "name"), strAt(p, "displayName")))
		}
		models := []string{}
		for _, v := range a.fetchProviderModelCatalog(p, "") {
			if m, ok := v.(map[string]any); ok {
				if id, _ := m["model"].(string); id != "" {
					models = append(models, id)
				}
			}
		}
		out[route] = models
	}
	return out
}

// FetchAllProviderModelCatalogs 返回全部供应商的模型能力列表（Record<string, CapabilityView[]>）。
func (a *App) FetchAllProviderModelCatalogs(providers []any) map[string][]any {
	out := map[string][]any{}
	for _, raw := range providers {
		p := providerViewFromAny(raw)
		_, route := a.providerProfileForCatalog(p)
		if route == "" {
			route = providerRouteName(firstNonEmpty(strAt(p, "name"), strAt(p, "displayName")))
		}
		out[route] = a.fetchProviderModelCatalog(p, "")
	}
	return out
}

// TestProviderModel 验证某模型在这个供应商上可用。
//
// 判据（按可靠性排序）：
//  1. 端点能列举模型 → 模型必须在列表里；
//  2. 端点不可列举（anthropic-messages）或探测失败 → 必须已在配置的 models 里。
//
// 不发起真实推理请求：那会产生费用且慢，且 DSH 未暴露"试跑一次"的 RPC。
func (a *App) TestProviderModel(p map[string]any, model, key string) error {
	if strings.TrimSpace(model) == "" {
		return fmt.Errorf("缺少要测试的模型")
	}
	profile, route := a.providerProfileForCatalog(p)
	configured := providerModelsFromDSH(providerStrings(profile["models"]))
	for _, id := range configured {
		if id == model {
			return nil
		}
	}
	discovered, err := discoverProviderModels(profile, key)
	if err != nil {
		if len(configured) > 0 {
			// 端点探测不可用，但配置里有别的模型 → 明确告诉用户模型不在配置里。
			return fmt.Errorf("供应商 %s 的配置模型里没有 %q（且端点探测失败: %v）", route, model, err)
		}
		return fmt.Errorf("无法验证模型 %q: %v", model, err)
	}
	for _, id := range discovered {
		if id == model {
			return nil
		}
	}
	return fmt.Errorf("供应商 %s 的端点没有返回模型 %q", route, model)
}
