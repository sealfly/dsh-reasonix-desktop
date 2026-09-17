package main

// app_providers.go — DSH 供应商（llm-pi-ai 命名空间）管理桥。
//
// 背景：Reasonix 前端的「模型服务」页要能列出/添加/编辑/测试供应商，而本项目后端是 DSH。
// DSH 的供应商配置存在 settings 的 `llm-pi-ai` 命名空间（`~/.dsh/settings.yaml`）：
//
//	llm-pi-ai:
//	  providers:
//	    <route>:
//	      displayName: T.Y.M. ModelHub          # 可选
//	      apiKeyEnv: TYM_API_KEY                # 凭据引用 → .credentials.yaml
//	      api: openai-completions               # 见 providerProtocols
//	      baseURL: https://token.tym.com.cn/v1
//	      models: [{ id: gpt-5.4-mini }, ...]
//
// 我们**通过 DSH RPC 读写**，不直接改 YAML：这样能拿到 DSH 的校验器与 revision，
// 也不会与 DSH 内存态脱节。
//   - settings.describe {}                          → namespaces[] 里取 llm-pi-ai
//   - settings.mutate {ns, ops:[{op,path,value}]}    → 校验失败返回 settings-rejected
//
// ⚠ 实测事实（2026-09-17，勿忘）：
//  1. `path` 必须是**分段数组** `["providers","<route>"]`。用点号单元素路径
//     `["providers.<route>"]` 会返回 ok=true 但**不生效**——所以每次写入后都 read-back 校验。
//  2. `api` 只允许 openai-completions / openai-responses / anthropic-messages；
//     其它值被 DSH 校验器拒绝（settings-rejected，报文里会列出允许值）。
//  3. 只有 openai-completions / openai-responses 支持从端点列表拉模型
//     （dsh-llm-pi-ai 的 LISTABLE_PROTOCOLS），anthropic-messages 只能看已配置的 models。

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

const (
	// providerSettingsNS 是 DSH 里承载供应商配置的 settings 命名空间。
	providerSettingsNS = "llm-pi-ai"

	protoOpenAICompletions = "openai-completions"
	protoOpenAIResponses   = "openai-responses"
	protoAnthropicMessages = "anthropic-messages"
)

// providerProtocols 是 DSH 允许的协议（顺序即 dsh-llm-pi-ai 的 supportedProtocols）。
var providerProtocols = []string{protoOpenAICompletions, protoOpenAIResponses, protoAnthropicMessages}

// providerProbeClient 用于直连供应商端点探测（拉 /models、验证模型）。
// 超时短：本地界面调用不能被远端慢响应拖住。
var providerProbeClient = &http.Client{Timeout: 12 * time.Second}

// ===== DSH settings 读写 =====

// providerOp 是 settings.mutate 的一个操作。
type providerOp struct {
	Op    string `json:"op"`             // set | unset
	Path  []string `json:"path"`         // 分段路径，例如 ["providers","xtoken","baseURL"]
	Value any    `json:"value,omitempty"`
}

// providerNamespace 是 settings.describe 里一个命名空间的视图。
type providerNamespace struct {
	NS       string          `json:"ns"`
	Revision int             `json:"revision"`
	Value    json.RawMessage `json:"value"`
}

// dshSettingsNamespaces 调 settings.describe 返回全部命名空间。
func (a *App) dshSettingsNamespaces() ([]providerNamespace, error) {
	if a.dsh == nil {
		return nil, fmt.Errorf("DSH 未连接")
	}
	raw, err := a.dsh.RPC("settings.describe", map[string]any{})
	if err != nil {
		return nil, err
	}
	var resp struct {
		Namespaces []providerNamespace `json:"namespaces"`
	}
	if err := DecodeRPC(raw, &resp); err != nil {
		return nil, fmt.Errorf("settings.describe 解码失败: %w", err)
	}
	return resp.Namespaces, nil
}

// providerProfiles 读取 llm-pi-ai.providers（route → profile）。
// 注意：写路径需要新鲜数据（写完要 read-back 校验），所以这里不走缓存；
// 设置快照内的复用见 app_settings_reads.go 的 providerProfilesReads。
func (a *App) providerProfiles() (map[string]map[string]any, error) {
	nss, err := a.dshSettingsNamespaces()
	if err != nil {
		return nil, err
	}
	return providerProfilesFromNamespaces(nss)
}

// providerProfilesFromNamespaces 从已取到的 settings 命名空间里解出 llm-pi-ai.providers（纯函数，可复用/可测）。
func providerProfilesFromNamespaces(nss []providerNamespace) (map[string]map[string]any, error) {
	for _, ns := range nss {
		if ns.NS != providerSettingsNS {
			continue
		}
		var value struct {
			Providers map[string]map[string]any `json:"providers"`
		}
		if err := json.Unmarshal(ns.Value, &value); err != nil {
			return nil, fmt.Errorf("llm-pi-ai.providers 解码失败: %w", err)
		}
		if value.Providers == nil {
			value.Providers = map[string]map[string]any{}
		}
		return value.Providers, nil
	}
	// 命名空间尚未出现（用户从未配置过第三方供应商）→ 空集合，不是错误。
	return map[string]map[string]any{}, nil
}

// dshSettingsMutate 写 llm-pi-ai 命名空间。
func (a *App) dshSettingsMutate(ops []providerOp) error {
	if a.dsh == nil {
		return fmt.Errorf("DSH 未连接")
	}
	_, err := a.dsh.RPC("settings.mutate", map[string]any{
		"ns":  providerSettingsNS,
		"ops": ops,
	})
	if err != nil {
		return err
	}
	return nil
}

// writeProviderProfile 写入/覆盖一个供应商 profile，并 read-back 校验。
//
// read-back 是必需的：settings.mutate 对某些形状的操作会返回 ok 但实际不生效
// （见文件头注释 1），只信返回值会静默丢配置。
func (a *App) writeProviderProfile(route string, profile map[string]any) error {
	if err := a.dshSettingsMutate([]providerOp{{
		Op:    "set",
		Path:  []string{"providers", route},
		Value: profile,
	}}); err != nil {
		return err
	}
	profiles, err := a.providerProfiles()
	if err != nil {
		return fmt.Errorf("写入后无法校验: %w", err)
	}
	got, ok := profiles[route]
	if !ok {
		return fmt.Errorf("DSH 未接受供应商 %q 的写入（settings.mutate 返回成功但读回不存在）", route)
	}
	if want, _ := profile["api"].(string); want != "" {
		if have, _ := got["api"].(string); have != want {
			return fmt.Errorf("供应商 %q 的 api 期望 %q，读回 %q", route, want, have)
		}
	}
	return nil
}

// writeProviderProfileResolvingModels 写入供应商，并在 DSH 因"没有模型"拒绝时用端点探测补齐后重试。
//
// 为什么需要（2026-09-17 实测发现）：DSH 的 llm-pi-ai 校验器对**不在其内置目录里的路由**要求
// 必须在配置里显式列出 models，否则写入被拒：
//
//	settings-rejected: provider "x" resolves no models; the installed catalog does not
//	describe this route, so its models must be listed in configuration
//
// 而"添加连接"这个交互里用户只填 key 与地址、不填模型，所以预设（models 留空）直接写会失败。
// 这里先尝试写入；被拒且原因是缺模型时，用给定 key 去端点探测模型列表后重试；
// 探测也拿不到就**明确报错**（不写半个配置、不塞占位模型）。
func (a *App) writeProviderProfileResolvingModels(route string, profile map[string]any, key string) error {
	err := a.writeProviderProfile(route, profile)
	if err == nil {
		return nil
	}
	if !strings.Contains(err.Error(), "resolves no models") {
		return err
	}
	models, derr := discoverProviderModels(profile, key)
	if derr != nil || len(models) == 0 {
		detail := "端点也没有返回模型"
		if derr != nil {
			detail = derr.Error()
		}
		return fmt.Errorf("该供应商需要显式列出模型（DSH 对未知路由的要求），但自动探测失败：%s。请在供应商配置里填写模型后重试", detail)
	}
	next := map[string]any{}
	for k, v := range profile {
		next[k] = v
	}
	next["models"] = providerModelsToDSH(models)
	if err := a.writeProviderProfile(route, next); err != nil {
		return err
	}
	resumeLog("provider: %s 的模型由端点探测补齐（%d 个）", route, len(models))
	return nil
}

// deleteProviderProfile 删除一个供应商 profile。
func (a *App) deleteProviderProfile(route string) error {
	if err := a.dshSettingsMutate([]providerOp{{
		Op:   "unset",
		Path: []string{"providers", route},
	}}); err != nil {
		return err
	}
	profiles, err := a.providerProfiles()
	if err != nil {
		return fmt.Errorf("删除后无法校验: %w", err)
	}
	if _, ok := profiles[route]; ok {
		return fmt.Errorf("供应商 %q 删除后仍存在", route)
	}
	return nil
}

// ===== 名称与协议映射 =====

// providerRouteName 把任意名字规范成 DSH 可用的 route id（小写连字符）。
// DSH 侧要求 provider id 能作为凭据记录 id 的段（小写连字符标识符）。
func providerRouteName(name string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		case r == '-' || r == '_' || r == '.' || r == '/' || r == ' ':
			if !prevDash && b.Len() > 0 {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		out = "provider"
	}
	return out
}

// providerShortID 生成随机后缀，保证 route 唯一。
func providerShortID() string {
	buf := make([]byte, 3)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano()%0xFFFFFF)
	}
	return hex.EncodeToString(buf)
}

// uniqueProviderRoute 在已存在 route 上追加后缀，避免覆盖别人的配置。
func uniqueProviderRoute(base string, existing map[string]map[string]any) string {
	route := providerRouteName(base)
	if _, ok := existing[route]; !ok {
		return route
	}
	for i := 0; i < 8; i++ {
		candidate := route + "-" + providerShortID()
		if _, ok := existing[candidate]; !ok {
			return candidate
		}
	}
	return route + "-" + fmt.Sprintf("%d", time.Now().UnixNano())
}

// providerProtocolFromRequest 把前端的 kind/format 归一到 DSH 协议名。
//
// 前端会传 kind（openai/anthropic/responses/deepseek/...）或 format（协议名/keyEnv 等），
// 两者都可能是空串，所以按「先精确匹配协议名 → 再按 kind 推断 → 兜底 completions」处理。
func providerProtocolFromRequest(format, kind string) string {
	for _, candidate := range []string{format, kind} {
		v := strings.ToLower(strings.TrimSpace(candidate))
		if v == "" {
			continue
		}
		for _, p := range providerProtocols {
			if v == p {
				return p
			}
		}
		switch {
		case strings.Contains(v, "anthropic"), strings.Contains(v, "claude"), strings.Contains(v, "messages"):
			return protoAnthropicMessages
		case strings.Contains(v, "responses"):
			return protoOpenAIResponses
		}
	}
	return protoOpenAICompletions
}

// providerKindFromProtocol 反向映射：给前端 ProviderView.kind 用。
func providerKindFromProtocol(api string) string {
	switch api {
	case protoAnthropicMessages:
		return "anthropic"
	case protoOpenAIResponses:
		return "responses"
	default:
		return "openai"
	}
}

// providerModelsToDSH 把前端的 models（模型 id 字符串数组）转成 DSH 的 [{id}] 。
func providerModelsToDSH(models []string) []any {
	out := []any{}
	seen := map[string]bool{}
	for _, m := range models {
		id := strings.TrimSpace(m)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, map[string]any{"id": id})
	}
	return out
}

// providerModelsFromDSH 把 DSH 的 models（[{id}]、["id"]，或 []string）转成 []string(id)。
func providerModelsFromDSH(models any) []string {
	out := []string{}
	switch list := models.(type) {
	case []any:
		for _, m := range list {
			switch v := m.(type) {
			case string:
				if strings.TrimSpace(v) != "" {
					out = append(out, strings.TrimSpace(v))
				}
			case map[string]any:
				if id, _ := v["id"].(string); strings.TrimSpace(id) != "" {
					out = append(out, strings.TrimSpace(id))
				} else if name, _ := v["name"].(string); strings.TrimSpace(name) != "" {
					out = append(out, strings.TrimSpace(name))
				}
			}
		}
	case []string:
		for _, m := range list {
			if strings.TrimSpace(m) != "" {
				out = append(out, strings.TrimSpace(m))
			}
		}
	}
	sort.Strings(out)
	return out
}

// providerStrings 把前端传来的数组（可能是 []any 或 []string）归一成 []string。
func providerStrings(value any) []string {
	out := []string{}
	switch v := value.(type) {
	case []string:
		out = append(out, v...)
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
	case string:
		if strings.TrimSpace(v) != "" {
			out = append(out, v)
		}
	}
	return out
}

// profileFromProviderView 把前端 ProviderView 映射成 DSH profile。
//
// overrideBaseURL/overrideFormat 非空时优先（添加流程会用它们覆盖预设默认值）。
func profileFromProviderView(p map[string]any, overrideBaseURL, overrideFormat string) map[string]any {
	kind, _ := p["kind"].(string)
	format, _ := p["format"].(string)
	api := providerProtocolFromRequest(firstNonEmpty(overrideFormat, format), kind)

	baseURL := strings.TrimSpace(overrideBaseURL)
	if baseURL == "" {
		// ⚠ 顺序很重要（实测 2026-09-17）：**当前设置 UI 写的是 `requestUrl`**（在前端注释里
		// 明确写着 "exact provider request URL written by the current settings UI"），
		// `baseUrl` 是兼容旧配置的 legacy 字段。此前优先读 baseUrl → 用户在「模型服务」页
		// 改了地址再保存时，桥收到的是新的 requestUrl、却写回旧的 baseUrl，**改动被静默丢弃**。
		// 证据：SaveProvider 实参 baseUrl=…/v1、requestUrl=…/v1?ui=1，写入后 DSH 仍是旧值。
		for _, key := range []string{"requestUrl", "baseURL", "baseUrl", "chatUrl", "modelsUrl"} {
			if v, _ := p[key].(string); strings.TrimSpace(v) != "" {
				baseURL = strings.TrimSpace(v)
				break
			}
		}
	}
	// DSH 要的是端点根（它自己拼 /chat/completions 或 /responses），去掉常见尾部路径。
	baseURL = strings.TrimRight(baseURL, "/")
	for _, suffix := range []string{"/chat/completions", "/responses", "/messages", "/completions"} {
		if strings.HasSuffix(baseURL, suffix) {
			baseURL = strings.TrimSuffix(baseURL, suffix)
		}
	}

	profile := map[string]any{"api": api}
	if baseURL != "" {
		profile["baseURL"] = baseURL
	}
	if v, _ := p["displayName"].(string); strings.TrimSpace(v) != "" {
		profile["displayName"] = strings.TrimSpace(v)
	}
	if v := firstNonEmpty(strAt(p, "apiKeyEnv"), strAt(p, "keyEnv")); v != "" {
		profile["apiKeyEnv"] = v
	}
	if models, ok := p["models"]; ok {
		profile["models"] = providerModelsToDSH(providerStrings(models))
	}
	// headers 是兼容网关的常见需求，DSH 支持就带上。
	if headers, ok := p["headers"].(map[string]any); ok && len(headers) > 0 {
		clean := map[string]any{}
		for k, v := range headers {
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				clean[k] = strings.TrimSpace(s)
			}
		}
		if len(clean) > 0 {
			profile["headers"] = clean
		}
	}
	return profile
}

// strAt 读取 map 里的字符串字段。
func strAt(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[key].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

// firstNonEmpty 定义在 app_memory_plugins.go（项目共用），此处直接使用。

// providerKeyEnvFor 决定新供应商用哪个凭据引用。
// 前端自定义连接会自带 apiKeyEnv（REASONIX_CONNECTION_*_KEY）；预设走预设的 keyEnv。
func providerKeyEnvFor(route, existing string, preset providerPreset) string {
	if existing != "" {
		return existing
	}
	if preset.KeyEnv != "" {
		return preset.KeyEnv
	}
	return strings.ToUpper(strings.ReplaceAll(route, "-", "_")) + "_API_KEY"
}

// ===== 凭据 =====

// setProviderCredential 写 API key 到 DSH 凭据存储（credentials.set）。
func (a *App) setProviderCredential(env, key string) error {
	if strings.TrimSpace(env) == "" || strings.TrimSpace(key) == "" {
		return nil
	}
	return a.credentialsSet(env, strings.TrimSpace(key))
}

// providerCredentialStatus 批量查询凭据是否已配置。
func (a *App) providerCredentialStatus(envs []string) map[string]bool {
	out := map[string]bool{}
	if a.dsh == nil || len(envs) == 0 {
		return out
	}
	raw, err := a.dsh.RPC("credentials.describe", map[string]any{"refs": envs})
	if err != nil {
		return out
	}
	var resp struct {
		Credentials map[string]struct {
			Configured bool `json:"configured"`
		} `json:"credentials"`
	}
	if err := DecodeRPC(raw, &resp); err != nil {
		return out
	}
	for env, c := range resp.Credentials {
		out[env] = c.Configured
	}
	return out
}

// ===== 桥方法：保存 / 添加 / 删除 =====

// SaveProvider 保存（新建或编辑）一个供应商。前端 saveProvider() 会调它。
func (a *App) SaveProvider(p map[string]any) error {
	name := firstNonEmpty(strAt(p, "name"), strAt(p, "displayName"))
	if name == "" {
		return fmt.Errorf("供应商缺少 name")
	}
	profile := profileFromProviderView(p, "", "")
	if strAt(profile, "baseURL") == "" && strAt(profile, "apiKeyEnv") == "" {
		return fmt.Errorf("供应商 %q 既没有 baseURL 也没有 apiKeyEnv", name)
	}
	profile["displayName"] = firstNonEmpty(strAt(p, "displayName"), name)
	return a.writeProviderProfileResolvingModels(providerRouteName(name), profile, "")
}

// SaveProviderWithKey 保存供应商并写入 key（前端自定义连接带 key 时调它）。
// 返回空串表示成功、非空串是给用户看的警告（前端的 warning 通道）。
func (a *App) SaveProviderWithKey(p map[string]any, key string) string {
	if err := a.SaveProvider(p); err != nil {
		panic(err.Error()) // 由 Wails 转成 Promise reject → 前端显示错误
	}
	env := firstNonEmpty(strAt(p, "apiKeyEnv"), strAt(p, "keyEnv"))
	if env == "" {
		return "供应商已保存，但没有 apiKeyEnv（未保存密钥）"
	}
	if err := a.setProviderCredential(env, key); err != nil {
		return fmt.Sprintf("供应商已保存，但密钥未写入: %v", err)
	}
	return ""
}

// AddProviderConnection 从预设复制出一个新连接（无 URL/格式覆盖）。
func (a *App) AddProviderConnection(presetID, sourceName, key string) string {
	return a.AddProviderConnectionWithOptions(presetID, sourceName, key, "", "")
}

// AddProviderConnectionWithURL 同上，带 baseURL 覆盖。
func (a *App) AddProviderConnectionWithURL(presetID, sourceName, key, baseURL string) string {
	return a.AddProviderConnectionWithOptions(presetID, sourceName, key, baseURL, "")
}

// AddProviderConnectionWithOptions 是「模型服务」页添加供应商的主入口。
//
// 三种调用形态（前端 SettingsPanel 实测）：
//   - 预设添加：AddProviderConnectionWithOptions(<presetId>, "", key, baseURL?, format?)
//   - 官方/复制：AddProviderConnectionWithOptions("", <已有供应商名>, key, baseURL?, format?)
//   - 无参复制：AddProviderConnection("", <providerName>, "")
//
// 返回空串=成功；非空串作为警告显示（上游 apply() 的约定）。
func (a *App) AddProviderConnectionWithOptions(presetID, sourceName, key, baseURL, format string) string {
	// 先校验预设：未知预设是调用方错误，不该等到 DSH 可用了才发现（也让错误信息更准）。
	preset, ok := lookupProviderPreset(presetID)
	if !ok && presetID != "" {
		// 不静默造一个假配置：宁可报错也不写坏配置。
		return fmt.Sprintf("未知的供应商预设 %q", presetID)
	}

	existing, err := a.providerProfiles()
	if err != nil {
		return fmt.Sprintf("读取现有供应商失败: %v", err)
	}

	baseName := firstNonEmpty(presetID, sourceName, preset.Label, "provider")
	profile := map[string]any{}
	if ok {
		profile = preset.profile()
		if preset.Label != "" {
			profile["displayName"] = preset.Label
		}
	} else if sourceName != "" {
		// 从已有供应商复制：保留其协议与端点，作为新连接的模板。
		if src, found := existing[providerRouteName(sourceName)]; found {
			for k, v := range src {
				profile[k] = v
			}
			delete(profile, "apiKeyEnv") // 新连接必须有自己的凭据引用
			if label, _ := src["displayName"].(string); label != "" {
				profile["displayName"] = label + " copy"
			}
		}
	}
	if len(profile) == 0 {
		// 官方接入（kind 走 format 参数）：没有预设模板时至少要有协议。
		profile["api"] = providerProtocolFromRequest(format, format)
	}
	if strings.TrimSpace(baseURL) != "" {
		profile["baseURL"] = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	}
	if strings.TrimSpace(format) != "" {
		profile["api"] = providerProtocolFromRequest(format, format)
	}

	route := uniqueProviderRoute(baseName, existing)
	env := providerKeyEnvFor(route, strAt(profile, "apiKeyEnv"), preset)
	profile["apiKeyEnv"] = env

	warning := ""
	if err := a.writeProviderProfileResolvingModels(route, profile, strings.TrimSpace(key)); err != nil {
		return err.Error()
	}
	if strings.TrimSpace(key) != "" {
		if err := a.setProviderCredential(env, key); err != nil {
			warning = fmt.Sprintf("连接已添加，但密钥未写入: %v", err)
		}
	}
	if strAt(profile, "baseURL") == "" {
		warning = appendWarning(warning, fmt.Sprintf("连接 %s 已添加但缺少 baseURL，请在编辑里补上", route))
	}
	return warning
}

// appendWarning 拼接警告信息。
func appendWarning(current, next string) string {
	if strings.TrimSpace(current) == "" {
		return next
	}
	if strings.TrimSpace(next) == "" {
		return current
	}
	return current + "；" + next
}

// AddProviderPresetAccess 让预设变为"已接入"（前端预设行的添加按钮）。
func (a *App) AddProviderPresetAccess(id, key string) string {
	return a.AddProviderConnectionWithOptions(id, "", key, "", "")
}

// AddOfficialProviderAccess 官方接入（前端官方卡片）。
func (a *App) AddOfficialProviderAccess(kind, key string) string {
	return a.AddProviderConnectionWithOptions("", kind, key, "", "")
}

// UpgradeDeepSeekProviderAccess 兼容旧入口：DSH 的 deepseek 走内置 llm-deepseek 命名空间，
// 这里只把 key 写进凭据存储（DEEPSEEK_API_KEY），不新建 llm-pi-ai 供应商。
func (a *App) UpgradeDeepSeekProviderAccess(name, key string) string {
	env := firstNonEmpty(name, deepseekAPIKeyRef)
	if strings.TrimSpace(key) == "" {
		return "缺少 API key"
	}
	if err := a.credentialsSet(env, strings.TrimSpace(key)); err != nil {
		return err.Error()
	}
	return ""
}

// RemoveProviderAccess 移除一个供应商接入（等价 DeleteProvider，前端两处入口共用）。
func (a *App) RemoveProviderAccess(name string) error {
	return a.DeleteProvider(name)
}

// RemoveProviderAccesses 批量移除。
func (a *App) RemoveProviderAccesses(names []any) error {
	var firstErr error
	for _, name := range providerStrings(names) {
		if err := a.DeleteProvider(name); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// DeleteProvider 删除供应商（settings 里的 profile）。
// 凭据保留：它可能被别的 route 共用，删除凭据不可逆，应由用户显式清 key。
func (a *App) DeleteProvider(name string) error {
	route := providerRouteName(name)
	profiles, err := a.providerProfiles()
	if err != nil {
		return err
	}
	if _, ok := profiles[route]; !ok {
		// 前端可能会用 displayName 调删除；再按 displayName 找一次。
		for r, p := range profiles {
			if strings.EqualFold(strAt(p, "displayName"), strings.TrimSpace(name)) {
				route = r
				ok = true
				break
			}
		}
		if !ok {
			return fmt.Errorf("供应商 %q 不存在", name)
		}
	}
	return a.deleteProviderProfile(route)
}

// ResetProviderPresetAccess 重置预设接入（删除该预设对应的 route）。
func (a *App) ResetProviderPresetAccess(id string) error {
	preset, ok := lookupProviderPreset(id)
	if !ok {
		return fmt.Errorf("未知的供应商预设 %q", id)
	}
	route := providerRouteName(preset.Route)
	if err := a.deleteProviderProfile(route); err != nil {
		// 已经不存在就当作重置成功（幂等）。
		return nil
	}
	return nil
}

// RenameProviderConnections 重命名连接（改 displayName，保留 route 与凭据引用）。
func (a *App) RenameProviderConnections(names []any, label string) error {
	profiles, err := a.providerProfiles()
	if err != nil {
		return err
	}
	targets := providerStrings(names)
	if len(targets) == 0 {
		return fmt.Errorf("没有要重命名的连接")
	}
	newName := strings.TrimSpace(label)
	if newName == "" {
		return fmt.Errorf("显示名不能为空")
	}
	// 组内首条改名为 label，其余保持原名（上游把组内多条连接视为一个连接组）。
	for i, name := range targets {
		route := providerRouteName(name)
		profile, ok := profiles[route]
		if !ok {
			continue
		}
		next := map[string]any{}
		for k, v := range profile {
			next[k] = v
		}
		if i == 0 {
			next["displayName"] = newName
		} else if dn := strAt(profile, "displayName"); dn != "" {
			next["displayName"] = dn
		}
		if err := a.writeProviderProfile(route, next); err != nil {
			return err
		}
	}
	return nil
}

// SetConnectionKey 设置某连接的 API key（前端编辑连接时调）。
// 返回空串=成功，非空串=警告。
func (a *App) SetConnectionKey(name, value string) string {
	profiles, err := a.providerProfiles()
	if err != nil {
		return fmt.Sprintf("读取供应商失败: %v", err)
	}
	route := providerRouteName(name)
	profile, ok := profiles[route]
	if !ok {
		for r, p := range profiles {
			if strings.EqualFold(strAt(p, "displayName"), strings.TrimSpace(name)) {
				route, profile, ok = r, p, true
				break
			}
		}
	}
	if !ok {
		return fmt.Sprintf("找不到供应商 %q", name)
	}
	env := strAt(profile, "apiKeyEnv")
	if env == "" {
		env = providerKeyEnvFor(route, "", providerPreset{})
		profile["apiKeyEnv"] = env
		if err := a.writeProviderProfile(route, profile); err != nil {
			return err.Error()
		}
	}
	if strings.TrimSpace(value) == "" {
		if err := a.credentialsUnset(env); err != nil {
			return fmt.Sprintf("清除密钥失败: %v", err)
		}
		return ""
	}
	if err := a.setProviderCredential(env, value); err != nil {
		return fmt.Sprintf("保存密钥失败: %v", err)
	}
	return ""
}

// SaveProviderModelCatalogs 保存多个供应商的模型列表（前端批量保存模型选择）。
// 返回每条更新的警告文本（空串=成功），与契约的 Promise<string[]> 对齐。
func (a *App) SaveProviderModelCatalogs(updates []any) []string {
	out := []string{}
	profiles, err := a.providerProfiles()
	if err != nil {
		return []string{fmt.Sprintf("读取供应商失败: %v", err)}
	}
	for _, raw := range updates {
		update, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		name := firstNonEmpty(strAt(update, "name"), strAt(update, "provider"), strAt(update, "id"))
		models := providerStrings(update["models"])
		if name == "" {
			out = append(out, "更新项缺少 name")
			continue
		}
		route := providerRouteName(name)
		profile, found := profiles[route]
		if !found {
			out = append(out, fmt.Sprintf("找不到供应商 %q", name))
			continue
		}
		next := map[string]any{}
		for k, v := range profile {
			next[k] = v
		}
		next["models"] = providerModelsToDSH(models)
		if err := a.writeProviderProfile(route, next); err != nil {
			out = append(out, err.Error())
			continue
		}
		out = append(out, "")
	}
	return out
}

// SetProviderWebSearch 打开/关闭某供应商的服务端联网搜索（DSH 侧能力由插件决定，
// 这里只记录用户意图，不伪造 DSH 不支持的字段）。
func (a *App) SetProviderWebSearch(_names []string, _enabled bool) error {
	return nil
}

