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

// desktopPreferenceKeys 返回前端「设置快照」契约所用的桌面偏好字段。
//
// ⚠ 为什么必须有这个函数（2026-09-17 修主题跳变）：
// 前端有**两套不同键名**的主题契约，混用会静默重置用户设置：
//  1. themeExperience 契约（GetThemeExperience 的返回）：themeMode / baseStyle
//     —— lib/themeExperience.ts 读它。
//  2. 设置快照契约（Settings / DesktopStartupSettings 的返回）：desktopTheme /
//     desktopThemeStyle / conversationWidth / desktopLanguage / sessionExperience
//     —— SettingsPanel.tsx L202-210 与 app-runtime/desktopPreferencesAdapter.ts L19-26 读它。
//
// 历史 bug：Settings() 只返回了第 1 套键名（themeMode/baseStyle），于是前端
// normalizeThemePreference(undefined) → DEFAULT_THEME("auto") + 默认风格 "graphite"，
// 并且 SettingsPanel 的外观 effect 依赖 s?.desktopTheme、每次设置重读都重放一次外观
// ——表现为「深色模式下按别的按钮，主题/风格/会话宽度被重置回默认（跳变）」。
// Settings() 与 DesktopStartupSettings() 必须给出**同一套值**，否则启动正确、后续跳变。
//
// 键名以 v1.38.2 的 lib/types.ts（SettingsView / DesktopStartupSettingsView）为准；
// 末尾保留的旧键名为兼容历史读取点，不要删除。
func (a *App) desktopPreferenceKeys() map[string]any {
	style := a.st.ThemeStyle()
	if style == "" {
		style = "graphite"
	}
	return map[string]any{
		// —— v1.38.2 设置快照契约键名（正确来源）——
		"desktopTheme":                 a.st.Theme(),
		"desktopThemeStyle":            style,
		"desktopLanguage":              a.st.Language(),
		"desktopLayoutStyle":           a.st.LayoutStyle(),
		"desktopTerminalTheme":         a.st.TerminalTheme(),
		"conversationWidth":            a.st.ConversationWidth(),
		"sessionExperience":            a.st.SessionExperience(),
		"displayMode":                  "full",
		"reasoningDisplayMode":         a.st.ReasoningMode(),
		"reasoningDisplayModeExplicit": a.st.ReasoningMode() != "",
		"statusBarStyle":               a.st.StatusBarStyle(),
		"statusBarItems":               a.st.StatusBarItems(),
		"checkUpdates":                 a.st.CheckUpdates(),
		"updateChannel":                "stable",
		"telemetry":                    a.st.DesktopTelemetry(),
		"metrics":                      a.st.DesktopMetrics(),
		"closeBehavior":                a.st.CloseBehavior(),
		"configPath":                   "",
		// —— 旧键名（兼容 v1.29–1.31 读取点，勿删）——
		"themeMode":                a.st.Theme(),
		"baseStyle":                style,
		"desktopConversationWidth": a.st.ConversationWidth(),
		"desktopTelemetry":         a.st.DesktopTelemetry(),
		"desktopMetrics":           a.st.DesktopMetrics(),
	}
}

// mergeKeys 把 src 的键值并入 dst 并返回 dst（src 覆盖同名键）。
func mergeKeys(dst, src map[string]any) map[string]any {
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

// SetSessionExperience 设置会话体验（v1.38.2 新增桥方法，standard/deep）。
func (a *App) SetSessionExperience(mode string) error {
	a.st.SetSessionExperience(mode)
	return nil
}

// Settings 返回设置面板的数据（前端 SettingsPanel 重读）。
// providers 从 DSH session.models 的 groups 生成——否则设置-模型-接入-供应商
// 页显示空（前端 SettingsPanel 读 view.providers 渲染已有供应商）。
// 外观类字段统一由 desktopPreferenceKeys 提供（见该函数注释：键名混用会导致主题跳变）。
func (a *App) Settings() map[string]any {
	// 一趟快照内复用 DSH 读取：session 列表 / session.models / settings.describe /
	// credentials.describe 各只取一次。原先 providerViews、officialProviderViews、
	// providerPresetViews、webSearchState 会各自重复调用这些慢 RPC（session.list 对巨型会话
	// 要 0.5~1.3s；凭据查询还是按 provider 逐个来），实测让 Settings() 达到 ~2.9s，
	// 而前端每次保存后都会 reload 它（期间 busy=true → 保存按钮临时禁用）。
	// 详见 app_settings_reads.go。
	reads := newSettingsReads()
	out := map[string]any{
		"providers":                 a.providerViewsReads(reads),
		"officialProviders":         a.officialProviderViewsReads(reads),
		// providerPresets/providerKinds：「模型服务」页的可添加目录与自定义类型选项。
		// 缺 providerPresets 时前端 asArray() 兜底成空数组——页面能开但没有可添加项，
		// 所以这两个键是"设置里的供应商可添加"的前提（见 app_providers_presets.go）。
		"providerPresets":           a.providerPresetViewsReads(reads),
		"providerKinds":             providerKinds,
		"defaultModel":              a.st.DefaultModel(),
		"plannerModel":              a.st.PlannerModel(),
		"visionModel":               a.st.VisionModel(),
		"subagentModel":             a.st.SubagentModel(),
		"subagentEffort":            a.st.SubagentEffort(),
		"maxSubagentDepth":          a.st.MaxSubagentDepth(),
		"maxSubagentConcurrency":    a.st.MaxSubagentConcurrency(),
		"maxParallelWriters":        a.st.MaxParallelWriters(),
		"autoPlan":                  "none",
		"defaultToolApprovalMode":   a.st.DefaultToolApprovalMode(),
		"compactRatio":              a.st.CompactRatio(),
		"desktopCurrency":           a.st.Currency(),
		"permissions":               a.st.PermissionsView(),
		"sandbox":                   a.st.SandboxView(),
		"network":                   a.networkView(),
		"agent":                     a.agentView(),
		"bot":                       mockBotSettings(),
		"shadowedByPath":            "",
	}
	// 联网搜索模型从 DSH 的 web-search-deepseek 命名空间读真值（复用同一趟读取）。
	out = mergeKeys(out, a.webSearchStateReads(reads))
	// autoApproveTools / bypass 由审批模式推导（两个独立开关，界面各反映真实状态）。
	autoApprove, bypass := a.toolApprovalFlags()
	out["autoApproveTools"] = autoApprove
	out["bypass"] = bypass
	return mergeKeys(out, a.desktopPreferenceKeys())
}

// providerViews 从 DSH session.models 的 groups 生成 ProviderView 列表。
// DSH 的 provider（deepseek-official/xtoken）即前端"供应商接入"页的已有供应商。
// 非缓存入口；设置快照内用 providerViewsReads 复用读取（见 app_settings_reads.go）。
func (a *App) providerViews() []any {
	return a.providerViewsReads(nil)
}

// officialProviderViews 官方供应商（builtIn）——前端"官方接入"引导区。
func (a *App) officialProviderViews() []any {
	return a.officialProviderViewsReads(nil)
}

// providerViewFromGroup 把 DSH 模型分组转成 ProviderView（字段对齐前端 normalizeProviderView）。
//
// ⚠ baseUrl / apiKeyEnv 必须从 DSH 的 llm-pi-ai profile 回填（2026-09-17 实测发现）：
// 前端「模型服务」页的「刷新模型」按钮条件是
// `disabled={busy || fetching || !p.baseUrl || !providerIsConfigured(p)}`，
// 而地址栏也直接显示 p.baseUrl。此前这里恒为空串 → **刷新按钮永久禁用、地址栏空白**，
// 用户根本没法"拉取模型"。DSH 侧配置本来就有 baseURL/apiKeyEnv，回填即可。
// providerViewFromGroup 把 DSH 模型分组转成 ProviderView（字段对齐前端 normalizeProviderView）。
// 非缓存入口；设置快照内用 providerViewFromGroupReads 复用读取与批量凭据查询。
func (a *App) providerViewFromGroup(g dshModelGroup) map[string]any {
	return a.providerViewFromGroupReads(nil, g)
}

// DesktopStartupSettings 返回启动设置（前端启动 sync 时读，主题/布局/bot 等）。
// 外观类字段与 Settings() 共用 desktopPreferenceKeys：两处键名必须一致，
// 否则会出现「启动时主题正确、之后每次设置重读被重置」的跳变（详见该函数注释）。
func (a *App) DesktopStartupSettings() map[string]any {
	out := map[string]any{
		"bot":                   mockBotSettings(),
		"displayMode":           "full",
		"configWarnings":        []any{},
		"configWarningsRevision": 0,
		"configPath":            "",
	}
	return mergeKeys(out, a.desktopPreferenceKeys())
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
