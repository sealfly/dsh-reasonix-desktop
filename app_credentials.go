package main

// app_credentials.go — DeepSeek API Key 管理（默认配置 DeepSeek 官方 API，客户直接填 key 就能用）。
//
// 需求：客户装好应用后，在 Reasonix 前端的设置/引导页直接填 DeepSeek API Key，
// 无需碰命令行或配置文件。key 存到 DSH 官方凭据存储（$DSH_HOME/.credentials.yaml），
// DSH 的 Models 页 / 本应用的 key 入口写入同一处，立刻生效。
//
// 实现：透传 DSH 的 credentials.set / credentials.unset / credentials.describe RPC。
// 遵循项目原则：不设白名单、不限制 DSH 能力，只做"方便人使用"的适配。

import (
	"fmt"
	"strings"
)

// deepseekAPIKeyRef 是 DeepSeek 官方 API key 的凭据引用（DSH 官方命名）。
const deepseekAPIKeyRef = "DEEPSEEK_API_KEY"

// SetProviderKey 保存某 provider 的 API key（env=凭据引用，如 DEEPSEEK_API_KEY）。
// Reasonix 前端 Settings 面板调用：SetProviderKey(apiKeyEnv, key)。
func (a *App) SetProviderKey(env, key string) error {
	return a.credentialsSet(env, key)
}

// SaveProviderKey 同 SetProviderKey（前端保存 provider 配置时也调它）。
func (a *App) SaveProviderKey(env, key string) error {
	return a.credentialsSet(env, key)
}

// ClearProviderKey 删除某 provider 的 API key（前端"移除 key"入口）。
func (a *App) ClearProviderKey(env string) error {
	return a.credentialsUnset(env)
}

// ConnectKey 引导页"连接 DeepSeek"：写入官方 API key，返回空串表示成功。
// 前端 Onboarding 调用：ConnectKey(key)。
func (a *App) ConnectKey(key string) string {
	if strings.TrimSpace(key) == "" {
		return "key is required"
	}
	if err := a.credentialsSet(deepseekAPIKeyRef, strings.TrimSpace(key)); err != nil {
		return err.Error()
	}
	return ""
}

// SaveProviderWithKey 保存 provider 配置并写入 key（前端组合动作）。
func (a *App) SaveProviderWithKey(config map[string]any, key string) error {
	env, _ := config["apiKeyEnv"].(string)
	if env == "" {
		env = deepseekAPIKeyRef
	}
	return a.credentialsSet(env, key)
}

// credentialsSet 调 DSH credentials.set。
func (a *App) credentialsSet(ref, value string) error {
	if a.dsh == nil {
		return fmt.Errorf("DSH 未连接")
	}
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("API key 不能为空")
	}
	_, err := a.dsh.RPC("credentials.set", map[string]any{
		"ref":   ref,
		"value": strings.TrimSpace(value),
	})
	if err != nil {
		return fmt.Errorf("保存 %s 失败: %w", ref, err)
	}
	return nil
}

// credentialsUnset 调 DSH credentials.unset。
func (a *App) credentialsUnset(ref string) error {
	if a.dsh == nil {
		return fmt.Errorf("DSH 未连接")
	}
	_, err := a.dsh.RPC("credentials.unset", map[string]any{
		"ref": ref,
	})
	if err != nil {
		return fmt.Errorf("移除 %s 失败: %w", ref, err)
	}
	return nil
}

// ProviderKeyStatus 返回某 provider 的 key 是否已配置（前端显示 keySet 状态）。
// 返回：{env, configured, source, writable}。
func (a *App) ProviderKeyStatus(env string) map[string]any {
	out := map[string]any{"env": env, "configured": false, "source": "", "writable": true}
	if a.dsh == nil {
		return out
	}
	raw, err := a.dsh.RPC("credentials.describe", map[string]any{"refs": []string{env}})
	if err != nil {
		return out
	}
	var resp struct {
		Credentials map[string]struct {
			Configured bool   `json:"configured"`
			Source     string `json:"source"`
			Writable   bool   `json:"writable"`
		} `json:"credentials"`
	}
	if err := DecodeRPC(raw, &resp); err != nil {
		return out
	}
	if c, ok := resp.Credentials[env]; ok {
		out["configured"] = c.Configured
		out["source"] = c.Source
		out["writable"] = c.Writable
	}
	return out
}

// FetchProviderModels 返回某 provider 可用的模型列表（前端保存 key 后拉取验证）。
// config 是前端 provider 配置：{name, kind, apiKeyEnv, baseUrl, ...}。
// 这里从 DSH 的 session.models 读取（按当前配置的 provider 过滤）。
func (a *App) FetchProviderModels(config map[string]any) []any {
	return a.modelsRefs("")
}

// FetchAllProviderModels 返回全部 provider 的模型列表（结构同 modelsRefs）。
func (a *App) FetchAllProviderModels() []any {
	return a.modelsRefs("")
}
