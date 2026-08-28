package main

import "os"

// App 的设置/主题/诊断桥方法。
// 这些方法的返回结构必须与 Reasonix v1.29.0 前端 bridge.ts 的接口契约一致，
// 否则会复现 Electron 版的问题（GetThemeExperience 返回空 → 主题锁浅色；
// CapabilityDiagnostics 返回空 → 诊断页崩溃 → 设置面板关不掉；bot 返回空 → sync 崩溃）。

// ===== 布局样式 =====

// SetDesktopLayoutStyle 设置布局样式（workbench/classic/creation）。
// 前端设置面板调用后立即 ReloadSettings→Settings() 重读并热切换布局。
// 注意：Wails 的原生拖拽（--wails-draggable）不经过渲染合成，热切换无叠影。
func (a *App) SetDesktopLayoutStyle(style string) error {
	return a.st.SetLayoutStyle(style)
}

// ===== 外观/主题 =====

// SetDesktopAppearance 设置外观（明/暗/自动 + 风格）。
func (a *App) SetDesktopAppearance(theme, style string) error {
	if err := a.st.SetTheme(theme); err != nil {
		return err
	}
	if style != "" {
		a.st.SetThemeStyle(style)
	}
	return nil
}

// MigrateDesktopPreferences 主题迁移：Electron 版里 legacy reasonix-theme 值已不可信，
// 这里直接丢弃（主题走 Settings 的 Theme）。前端会清除 legacy localStorage。
func (a *App) MigrateDesktopPreferences(_legacyLanguage, _theme, _style string) error {
	return nil
}

// GetThemeExperience 主题体验（v1.29.0 统一主题机制，前端 loadThemeExperience 优先调它）。
// 返回真实结构；返回空会导致 normalizeExperience 兜底 themeMode='auto' → 界面锁浅色。
func (a *App) GetThemeExperience() map[string]any {
	theme := a.st.Theme()
	style := a.st.ThemeStyle()
	if style == "" {
		style = "graphite"
	}
	return map[string]any{
		"themeMode":      theme,
		"baseStyle":      style,
		"effectiveStyle": style,
		"activeThemeId":  nil,
		"activePack":     nil,
	}
}

// GetActiveThemePack 主题包（我们未实现主题包，返回空结构，前端降级）。
func (a *App) GetActiveThemePack() map[string]any {
	return map[string]any{"pack": nil, "activeThemeId": nil}
}

// ===== 设置视图 =====

// Settings 返回设置面板的数据（前端 SettingsPanel 重读）。
// providers 从 DSH session.models 的 groups 生成——否则设置-模型-接入-供应商
// 页显示空（前端 SettingsPanel 读 view.providers 渲染已有供应商）。
func (a *App) Settings() map[string]any {
	return map[string]any{
		"providers":                 a.providerViews(),
		"officialProviders":         a.officialProviderViews(),
		"defaultModel":              "deepseek-v4-flash",
		"plannerModel":              "deepseek-v4-flash",
		"subagentModel":             "deepseek-v4-flash",
		"subagentEffort":            "auto",
		"maxSubagentDepth":          3,
		"maxSubagentConcurrency":    2,
		"maxParallelWriters":        1,
		"autoPlan":                  "none",
		"defaultToolApprovalMode":   a.st.DefaultToolApprovalMode(),
		"compactRatio":              1,
		"desktopLayoutStyle":        a.st.LayoutStyle(),
		"desktopCurrency":           a.st.Currency(),
		"reasoningDisplayMode":      a.st.ReasoningMode(),
		"reasoningDisplayModeExplicit": a.st.ReasoningMode() != "",
		"bot":                       mockBotSettings(),
	}
}

// providerViews 从 DSH session.models 的 groups 生成 ProviderView 列表。
// DSH 的 provider（deepseek-official/xtoken）即前端"供应商接入"页的已有供应商。
func (a *App) providerViews() []any {
	m := a.modelsView("")
	if m == nil {
		return []any{}
	}
	out := []any{}
	for _, g := range m.Groups {
		out = append(out, a.providerViewFromGroup(g))
	}
	return out
}

// officialProviderViews 官方供应商（builtIn）——前端"官方接入"引导区。
func (a *App) officialProviderViews() []any {
	m := a.modelsView("")
	if m == nil {
		return []any{}
	}
	out := []any{}
	for _, g := range m.Groups {
		if g.ID == "deepseek-official" {
			out = append(out, a.providerViewFromGroup(g))
		}
	}
	return out
}

// providerViewFromGroup 把 DSH 模型分组转成 ProviderView（字段对齐前端 normalizeProviderView）。
func (a *App) providerViewFromGroup(g dshModelGroup) map[string]any {
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
	return map[string]any{
		"name":              g.ID,
		"builtIn":           builtIn,
		"added":             true,
		"kind":              kind,
		"baseUrl":           "",
		"chatUrl":           "",
		"requestUrl":        "",
		"models":            models,
		"visionModels":      []any{},
		"modelsUrl":         "",
		"apiKeyEnv":         "",
		"keySet":            true, // DSH 已配置可用
		"requiresKey":       false,
		"configured":        true,
		"keySource":         "dsh",
		"supportedEfforts":  efforts,
		"defaultEffort":     defEffort,
		"webSearch":         false,
		"reasoningProtocol": "streamed",
	}
}

// DesktopStartupSettings 返回启动设置（前端启动 sync 时读，主题/布局/bot 等）。
func (a *App) DesktopStartupSettings() map[string]any {
	return map[string]any{
		"bot":                        mockBotSettings(),
		"desktopLanguage":            a.st.Language(),
		"desktopLayoutStyle":         a.st.LayoutStyle(),
		"desktopTheme":               a.st.Theme(),
		"desktopThemeStyle":          a.st.ThemeStyle(),
		"desktopTerminalTheme":       "dark",
		"displayMode":                "full",
		"reasoningDisplayMode":       a.st.ReasoningMode(),
		"reasoningDisplayModeExplicit": a.st.ReasoningMode() != "",
		"desktopCurrency":            a.st.Currency(),
		"statusBarStyle":             a.st.StatusBarStyle(),
		"statusBarItems":             a.st.StatusBarItems(),
		"checkUpdates":               false,
		"updateChannel":              "stable",
		"conversationWidth":          "standard",
		"configWarnings":             []any{},
		"configWarningsRevision":     0,
		"configPath":                 "",
	}
}

// ===== 诊断（防前端崩溃）=====

// CapabilityDiagnostics 能力诊断报告（设置面板"诊断"页）。
// 返回完整结构；返回空会导致 DiagnosticsSettingsPage 读 report.summary.errors 崩溃。
func (a *App) CapabilityDiagnostics(includeSessionRuntime bool) map[string]any {
	summary := map[string]any{
		"errors": 0, "warnings": 0, "infos": 0, "instructions": 0,
		"skills": 0, "commands": 0, "hooks": 0, "plugins": 0, "mcp_servers": 0,
	}
	if includeSessionRuntime {
		summary["infos"] = 1
	}
	issues := []any{}
	if includeSessionRuntime {
		issues = append(issues, map[string]any{
			"severity": "info", "code": "bridge.runtime", "subsystem": "runtime",
			"name": "bridge", "message": "Wails bridge + DSH backend (port 3080)",
			"remediation": "", "settings_tab": "general",
		})
	}
	return map[string]any{
		"schema_version": 1,
		"root":          "C:\\",
		"live":          true,
		"summary":       summary,
		"instructions":  map[string]any{"docs": []any{}},
		"skills":        map[string]any{"roots": []any{}, "entries": []any{}, "winners": 0, "shadowed": 0},
		"commands":      map[string]any{"roots": []any{}, "entries": []any{}, "winners": 0, "shadowed": 0},
		"hooks":         map[string]any{"trusted_project": true, "project_defines_hooks": false, "sources": []any{}, "entries": []any{}},
		"plugins":       map[string]any{"packages": []any{}},
		"mcp":           map[string]any{"servers": []any{}},
		"issues":        issues,
	}
}

// RuntimeDoctor 运行时健康报告。
func (a *App) RuntimeDoctor() map[string]any {
	return map[string]any{
		"text":                   "runtime: wails bridge ok\nbackend: dsh http://127.0.0.1:3080\nrecoverability: clean=true irreversible=false\n",
		"publishedGeneration":    0,
		"allowResume":            true,
		"cleanRollback":          true,
		"hasIrreversible":        false,
		"noOpRebuilds":           0,
		"fullRebuilds":           0,
		"subgraphRebuilds":       0,
		"staleDrops":             0,
		"admissionRejected":      0,
		"runtimeOwnerFallbacks":  0,
	}
}

// StorageSettings 存储设置（设置面板"存储"页签）。
func (a *App) StorageSettings() map[string]any {
	home := homeDir()
	return map[string]any{
		"defaultWorkspace": home,
		"statePath":        home + "\\.reasonix",
		"cachePath":        home + "\\.reasonix\\cache",
		"extensionsPath":   home + "\\.reasonix\\plugins",
	}
}

// SkillsSettings 技能设置（设置面板"技能"页签）——贴合 DSH：
// 从 skill.list（payload {sessionId}）读真实 skills。
// DSH 无 skill 时返回空列表（前端 UI 正常显示空态）。
func (a *App) SkillsSettings() map[string]any {
	return a.skillsView("")
}

// RefreshSkills 刷新技能列表——DSH skill.list 无缓存，走一遍真实查询。
func (a *App) RefreshSkills() error {
	a.skillsView("")
	return nil
}

// skillsView 从 DSH skill.list 读取并转成前端 SkillView 结构。
func (a *App) skillsView(tabID string) map[string]any {
	skills := []any{}
	if a.dsh != nil {
		sid := a.activeSessionID(tabID)
		if sid != "" {
			if raw, err := a.dsh.RPC("skill.list", map[string]any{"sessionId": sid}); err == nil {
				var list struct {
					Skills []struct {
						Name           string `json:"name"`
						Description    string `json:"description"`
						WhenToUse      string `json:"whenToUse"`
						ModelInvocable bool   `json:"modelInvocable"`
					} `json:"skills"`
				}
				if err := DecodeRPC(raw, &list); err == nil {
					prefs := getSkillPrefsManager()
					for _, s := range list.Skills {
						skills = append(skills, map[string]any{
							"name":           s.Name,
							"description":    s.Description,
							"scope":          "dsh",
							"runAs":          "agent",
							"enabled":        !prefs.isSkillDisabled(s.Name),
							"invocation":     "manual",
							"modelInvocable": s.ModelInvocable,
							"whenToUse":      s.WhenToUse,
						})
					}
				}
			}
		}
	}
	return map[string]any{
		// 合并本地子智能体 profile（runAs="subagent"，前端子智能体面板过滤该值）。
		"skills":                  append(skills, subagentProfilesAsSkills()...),
		"skillRoots":              []any{},
		"allowImplicitInvocation": getSkillPrefsManager().load().ImplicitInvocation,
	}
}

// ===== bot 安全结构（防前端读 undefined 崩溃）=====

// mockBotSettings 返回 bot 安全结构。Electron 版里 bot={} 导致
// sidebarImQQAdded 读 qq.enabled 崩溃（desktop preferences sync failed）。
func mockBotSettings() map[string]any {
	return map[string]any{
		"enabled":             false,
		"model":               "",
		"toolApprovalMode":    "ask",
		"maxSteps":            0,
		"debounceMs":          1500,
		"queueMode":           "steer",
		"queueCap":            20,
		"queueDrop":           "summarize",
		"ignoreSelfMessages":  true,
		"selfUserIds":         map[string]any{"qq": []any{}, "feishu": []any{}, "weixin": []any{}},
		"control":             map[string]any{"enabled": false, "addr": "127.0.0.1:37913", "tokenEnv": "REASONIX_BOT_CONTROL_TOKEN"},
		"pairing":             map[string]any{"enabled": true, "requestTtlMinutes": 60, "maxPendingPerPlatform": 3},
		"routes":              []any{},
		"allowlist": map[string]any{
			"enabled": true, "allowAll": false,
			"qqUsers": []any{}, "feishuUsers": []any{}, "weixinUsers": []any{},
			"qqApprovers": []any{}, "feishuApprovers": []any{}, "weixinApprovers": []any{},
			"qqAdmins": []any{}, "feishuAdmins": []any{}, "weixinAdmins": []any{},
			"qqGroups": []any{}, "feishuGroups": []any{}, "weixinGroups": []any{},
		},
		"qq": map[string]any{
			"enabled": false, "appId": "", "appSecretEnv": "QQ_BOT_APP_SECRET", "secretSet": false,
			"sandbox": false, "model": "", "toolApprovalMode": "ask", "workspaceRoot": "",
			"access": map[string]any{"enabled": true, "allowAll": false, "pairingEnabled": true, "users": []any{}, "groups": []any{}, "approvers": []any{}, "admins": []any{}},
		},
		"feishu": map[string]any{
			"enabled": false, "domain": "feishu", "appId": "", "appSecretEnv": "FEISHU_BOT_APP_SECRET",
			"secretSet": false, "verificationToken": "", "mode": "webhook", "webhookPort": 8080, "requireMention": true,
		},
		"weixin": map[string]any{
			"enabled": false, "accountId": "default", "tokenEnv": "WEIXIN_BOT_TOKEN",
			"tokenSet": false, "apiBase": "https://ilinkai.weixin.qq.com",
		},
		"connections": []any{},
	}
}

// homeDir 返回用户主目录（兜底 "."）。
func homeDir() string {
	home, _ := os.UserHomeDir()
	if home == "" {
		return "."
	}
	return home
}
