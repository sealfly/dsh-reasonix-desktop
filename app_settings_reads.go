package main

// app_settings_reads.go — 设置快照构建期间的 DSH 读取复用（性能）。
//
// 为什么要这个：`app.Settings()` 实测约 2.9s，而前端**每次保存/应用操作后都会 reload 它**
// （reload 期间 busy=true → 「保存更改」等按钮临时禁用，用户感觉"点了没反应"）。
// 慢的原因是同一份数据在一趟里被反复取：
//   - providerViews / officialProviderViews / webSearchState 各自调一次
//     session.list（DSH 对巨型会话要 0.5~1.3s）+ session.models；
//   - providerViewFromGroup 对**每个** provider 各调一次 credentials.describe；
//   - providerPresetViews / webSearchState 又各调一次 settings.describe。
//
// 解法：一次快照内只取一次（session 列表 / session.models / settings.describe /
// credentials.describe 各 1 次），结果放进 settingsReads 复用。
//
// ⚠ **写路径不共用这个缓存**：写操作必须读新鲜数据（写完还要 read-back 校验）。
// settingsReads 只在 Settings()/DesktopStartupSettings() 内部创建并传递，
// 其它调用点走 *Reads(nil) = 原始的非缓存实现。

import (
	"encoding/json"
	"sort"
	"strings"
	"time"
)

// orderedProviderPresets 按 displayOrder 返回预设目录（设置页展示顺序）。
func orderedProviderPresets() []providerPreset {
	ordered := make([]providerPreset, len(providerPresetCatalog))
	copy(ordered, providerPresetCatalog)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].DisplayOrder < ordered[j].DisplayOrder })
	return ordered
}

// equalURL 比较两个端点地址（忽略结尾斜杠差异）。
func equalURL(a, b string) bool {
	return strings.EqualFold(strings.TrimRight(strings.TrimSpace(a), "/"), strings.TrimRight(strings.TrimSpace(b), "/"))
}

// decodeInto 把 JSON 解到目标结构（便于在缓存版里复用解码逻辑）。
func decodeInto(raw json.RawMessage, target any) error {
	return json.Unmarshal(raw, target)
}

// settingsReads 是一次设置快照构建内的读取缓存。
type settingsReads struct {
	namespaces     []providerNamespace
	namespacesDone bool

	profiles     map[string]map[string]any
	profilesDone bool

	keys       map[string]bool
	keysDone   bool
	keysLoaded time.Time

	models     *dshModelsView
	modelsDone bool
}

// newSettingsReads 创建一趟快照的读取缓存。
func newSettingsReads() *settingsReads { return &settingsReads{} }

// dshSettingsNamespacesReads 取（缓存的）settings.describe。
func (a *App) dshSettingsNamespacesReads(r *settingsReads) ([]providerNamespace, error) {
	if r != nil && r.namespacesDone {
		return r.namespaces, nil
	}
	nss, err := a.dshSettingsNamespaces()
	if err == nil && r != nil {
		r.namespaces = nss
		r.namespacesDone = true
	}
	return nss, err
}

// providerProfilesReads 取（缓存的）llm-pi-ai.providers。
func (a *App) providerProfilesReads(r *settingsReads) (map[string]map[string]any, error) {
	if r != nil && r.profilesDone {
		return r.profiles, nil
	}
	nss, err := a.dshSettingsNamespacesReads(r)
	if err != nil {
		return nil, err
	}
	profiles, err := providerProfilesFromNamespaces(nss)
	if err == nil && r != nil {
		r.profiles = profiles
		r.profilesDone = true
	}
	return profiles, err
}

// prefetchCredentialsReads 一次批量拉取全部相关凭据状态（幂等）。
//
// 覆盖两类环境变量：DSH 里已配置供应商的 apiKeyEnv + 预设目录里的 keyEnv。
// 只拉一次很重要：原来每个 provider 各调一次 credentials.describe（N+1 次 RPC）。
func (a *App) prefetchCredentialsReads(r *settingsReads) {
	if r == nil || r.keysDone {
		return
	}
	r.keysDone = true
	envs := []string{}
	seen := map[string]bool{}
	add := func(env string) {
		if env != "" && !seen[env] {
			seen[env] = true
			envs = append(envs, env)
		}
	}
	if profiles, err := a.providerProfilesReads(r); err == nil {
		for _, p := range profiles {
			add(strAt(p, "apiKeyEnv"))
		}
	}
	for _, p := range providerPresetCatalog {
		add(p.KeyEnv)
	}
	r.keys = a.providerCredentialStatus(envs)
	r.keysLoaded = time.Now()
}

// keyConfiguredReads 读某个凭据引用是否已配置（用缓存；nil 时单查一次）。
func (a *App) keyConfiguredReads(r *settingsReads, env string) bool {
	if env == "" {
		return false
	}
	if r == nil {
		return a.providerCredentialStatus([]string{env})[env]
	}
	a.prefetchCredentialsReads(r)
	return r.keys[env]
}

// modelsViewReads 取（缓存的）session.models（只缓存 tabID 为空的那次；按 tab 的查询不缓存）。
func (a *App) modelsViewReads(r *settingsReads, tabID string) *dshModelsView {
	if r == nil || tabID != "" {
		return a.modelsView(tabID)
	}
	if r.modelsDone {
		return r.models
	}
	m := a.modelsView("")
	r.models = m
	r.modelsDone = true
	return m
}

// modelsRefsReads 同 modelsRefs，但复用快照内的 session.models。
func (a *App) modelsRefsReads(r *settingsReads, tabID string) []any {
	if r == nil || tabID != "" {
		return a.modelsRefs(tabID)
	}
	return modelsRefsFrom(a.modelsViewReads(r, ""))
}

// providerViewsReads 是 providerViews 的缓存版。
func (a *App) providerViewsReads(r *settingsReads) []any {
	// 提前一次把凭据状态批量拉好（避免每个 provider 一次 RPC）
	a.prefetchCredentialsReads(r)
	m := a.modelsViewReads(r, "")
	if m == nil {
		return []any{}
	}
	out := []any{}
	for _, g := range m.Groups {
		out = append(out, a.providerViewFromGroupReads(r, g))
	}
	return out
}

// officialProviderViewsReads 是 officialProviderViews 的缓存版。
func (a *App) officialProviderViewsReads(r *settingsReads) []any {
	out := []any{}
	for _, view := range a.providerViewsReads(r) {
		if m, ok := view.(map[string]any); ok {
			if strAt(m, "name") == "deepseek-official" {
				out = append(out, m)
			}
		}
	}
	return out
}

// providerViewFromGroupReads 是 providerViewFromGroup 的缓存版。
func (a *App) providerViewFromGroupReads(r *settingsReads, g dshModelGroup) map[string]any {
	kind := "custom"
	builtIn := false
	if g.ID == "deepseek-official" {
		kind = "deepseek"
		builtIn = true
	}
	models := []any{}
	efforts := []any{}
	defEffort := ""
	for _, mod := range g.Models {
		models = append(models, mod.ID)
		if mod.Reasoning != nil && len(efforts) == 0 && len(mod.Reasoning.Efforts) > 0 {
			for _, e := range mod.Reasoning.Efforts {
				efforts = append(efforts, e.ID)
			}
			defEffort = mod.Reasoning.DefaultEffort
		}
	}

	baseURL := ""
	apiKeyEnv := ""
	keySet := false
	if profiles, err := a.providerProfilesReads(r); err == nil {
		if profile, ok := profiles[providerRouteName(g.ID)]; ok {
			baseURL = strAt(profile, "baseURL")
			apiKeyEnv = strAt(profile, "apiKeyEnv")
			kind = providerKindFromProtocol(strAt(profile, "api"))
			if apiKeyEnv != "" {
				keySet = a.keyConfiguredReads(r, apiKeyEnv)
			}
		}
	}
	requiresKey := apiKeyEnv != ""
	configured := keySet || !requiresKey
	if builtIn {
		requiresKey = false
		configured = true
	}

	return map[string]any{
		"name":              g.ID,
		"builtIn":           builtIn,
		"added":             true,
		"kind":              kind,
		"baseUrl":           baseURL,
		"chatUrl":           "",
		"requestUrl":        baseURL,
		"models":            models,
		"visionModels":      []any{},
		"modelsUrl":         baseURL,
		"apiKeyEnv":         apiKeyEnv,
		"keySet":            keySet || builtIn,
		"requiresKey":       requiresKey,
		"configured":        configured,
		"keySource":         "dsh",
		"supportedEfforts":  efforts,
		"defaultEffort":     defEffort,
		"webSearch":         false,
		"reasoningProtocol": "streamed",
	}
}

// providerPresetViewsReads 是 providerPresetViews 的缓存版。
func (a *App) providerPresetViewsReads(r *settingsReads) []any {
	a.prefetchCredentialsReads(r)
	profiles, err := a.providerProfilesReads(r)
	if err != nil {
		// DSH 读不到时返回空表而不是伪造"未接入"，避免用户点了添加却写到别处。
		return []any{}
	}
	ordered := orderedProviderPresets()
	out := []any{}
	for _, p := range ordered {
		route := providerRouteName(p.Route)
		_, added := profiles[route]
		if !added && p.BaseURL != "" {
			for _, prof := range profiles {
				if equalURL(strAt(prof, "baseURL"), p.BaseURL) {
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
			"keySet":        p.KeyEnv == "" || a.keyConfiguredReads(r, p.KeyEnv),
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

// webSearchStateReads 是 webSearchState 的缓存版。
func (a *App) webSearchStateReads(r *settingsReads) map[string]any {
	var value map[string]any
	if nss, err := a.dshSettingsNamespacesReads(r); err == nil {
		for _, n := range nss {
			if n.NS != dshWebSearchNS {
				continue
			}
			value = map[string]any{}
			if len(n.Value) > 0 {
				if err := decodeInto(n.Value, &value); err != nil {
					value = nil
				}
			}
			break
		}
	}
	configured := ""
	if value != nil {
		if v, ok := value["model"].(string); ok {
			configured = strings.TrimSpace(v)
		}
	}
	candidates := []string{}
	for _, ref := range a.modelsRefsReads(r, "") {
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
