package main

import "strings"

// 启动路径必需 + 常见空实现（补齐前端调用但 Go 端缺失的方法，防 not-a-function 崩溃）。
// 前端实际调用 332 个方法，Go 端只实现了核心 60 个；其余在此按"安全空实现"补齐，
// 返回空态/降级，前端能正确处理。已实现真实逻辑的方法不在此重复定义。

// Commands 斜杠命令列表（Composer 调用）。
func (a *App) Commands() []any { return []any{} }

// BuildProbe DSH-ReasonixUI 构建探针(无害,用于区分构建指纹)。
func (a *App) BuildProbe() string { return "dsh-reasonix-build-2026-08-24A" }

// SlashArgs 斜杠命令参数补全（Composer 输入 /xxx 空格后调用）。
// 返回 {items: [{label, insert, hint, descend}], from}；DSH 当前无静态子命令表，返回空补全，前端正确降级。
func (a *App) SlashArgs(input string) map[string]any {
	from := 0
	if i := strings.LastIndex(input, " "); i >= 0 {
		from = i + 1
	}
	return map[string]any{"items": []any{}, "from": from}
}

// ReplayPendingPrompts 重放待处理提示（无 PTY/待处理概念，空操作）。
func (a *App) ReplayPendingPrompts() {}

// NeedsOnboarding 是否需要引导（DSH 无引导，返回 false）。
func (a *App) NeedsOnboarding() bool { return false }

// BotRuntimeStatus bot 运行时状态（无 bot，返回空）。
func (a *App) BotRuntimeStatus() map[string]any {
	return map[string]any{"status": "stopped", "running": false, "connections": 0}
}

// ListThemePacks 主题包列表（未实现，空）。
func (a *App) ListThemePacks() []any { return []any{} }

// ThemePacks 同 ListThemePacks。
func (a *App) ThemePacks() []any { return []any{} }

// MCPServers MCP 服务器列表（DSH 无 MCP 配置，空）。
func (a *App) MCPServers() []any { return []any{} }

// RemoteHosts 远程主机列表（未实现，空）。
func (a *App) RemoteHosts() []any { return []any{} }

// RemoteConnectionStatuses 远程连接状态。
func (a *App) RemoteConnectionStatuses() []any { return []any{} }

// RemoteForwards 远程转发列表。
func (a *App) RemoteForwards() []any { return []any{} }

// ExtensionActions 扩展动作。
func (a *App) ExtensionActions() []any { return []any{} }

// BackgroundRuntimes 后台运行时。
func (a *App) BackgroundRuntimes() []any { return []any{} }

// HooksSettings 钩子设置。
func (a *App) HooksSettings() map[string]any { return map[string]any{"hooks": []any{}} }

// ExternalOpeners 外部打开器。
func (a *App) ExternalOpeners() map[string]any { return map[string]any{"openers": []any{}, "preferred": ""} }

// InboxSnapshot 收件箱快照。
func (a *App) InboxSnapshot() []any { return []any{} }

// Memory 记忆（DSH 无记忆能力，空态）。
func (a *App) Memory() map[string]any { return map[string]any{"memories": []any{}, "available": false} }

// MemoryForTab 指定会话记忆。
func (a *App) MemoryForTab(_tabID string) map[string]any { return a.Memory() }

// MemoryRevisions 记忆修订。
func (a *App) MemoryRevisions() []any { return []any{} }

// MemoryRevisionsForTab 指定会话记忆修订。
func (a *App) MemoryRevisionsForTab(_a1 any, _a2 any) []any { return []any{} }

// MemorySuggestions 记忆建议。
func (a *App) MemorySuggestions() map[string]any {
	return map[string]any{"memories": []any{}, "skills": []any{}, "generatedAt": "", "available": false, "source": "dsh"}
}

// MemorySuggestionsForTab 指定会话记忆建议。
func (a *App) MemorySuggestionsForTab(_tabID string) map[string]any { return a.MemorySuggestions() }

// CheckpointsForTab 检查点列表（DSH 无检查点，空）。
func (a *App) CheckpointsForTab(_tabID string) []any { return []any{} }

// JobsForTab 任务列表（空）。
func (a *App) JobsForTab(_tabID string) []any { return []any{} }

// ActiveWorkForTab 活动工作状态。
func (a *App) ActiveWorkForTab(_tabID string) map[string]any {
	return map[string]any{"running": false, "pendingPrompt": false, "cancellable": false, "jobs": []any{}}
}

// Meta 会话元数据（当前会话）。
func (a *App) Meta() map[string]any { return a.MetaForTab("") }

// MetaForTab 会话元数据（DSH session.list 投影）。
func (a *App) MetaForTab(tabID string) map[string]any {
	s := a.findSession(tabID)
	if s == nil {
		return map[string]any{"cwd": homeDir(), "readOnly": false}
	}
	v := projectionValues(s)
	return map[string]any{"cwd": s.Cwd, "readOnly": false, "projections": map[string]any{"values": v}}
}

// ListSessionsForTab 指定会话的会话列表（DSH 无子会话概念，返回空）。
func (a *App) ListSessionsForTab(_tabID string) []any { return []any{} }

// ListTrashedSessions 回收站会话列表。
func (a *App) ListTrashedSessions() []any { return []any{} }

// WorkspaceChanges Git 变更（DSH 无 git 集成，空）。
func (a *App) WorkspaceChanges(_tabID string) []any { return []any{} }

// WorkspaceChangeDetail Git 变更详情。
func (a *App) WorkspaceChangeDetail(_tabID, _path string) map[string]any { return nil }

// WorkspaceConflictForTab 工作区冲突。
func (a *App) WorkspaceConflictForTab(_tabID string) map[string]any { return nil }

// WorkspaceGitHistory Git 历史。
func (a *App) WorkspaceGitHistory(_tabID, _path string) []any { return []any{} }

// SetCloseBehavior 关闭行为（quit/background）。
func (a *App) SetCloseBehavior(behavior string) error {
	a.st.SetCloseBehavior(behavior)
	return nil
}

// SetDesktopLanguage 界面语言。
func (a *App) SetDesktopLanguage(lang string) error {
	a.st.SetLanguage(lang)
	return nil
}

// SetDesktopCurrency 费用币种。
func (a *App) SetDesktopCurrency(currency string) error {
	return a.st.SetCurrency(currency)
}

// SetReasoningDisplayMode 思考显示模式。
func (a *App) SetReasoningDisplayMode(mode string) error {
	a.st.SetReasoningMode(mode)
	return nil
}

// SetDesktopZoomFactor 窗口缩放。
func (a *App) SetDesktopZoomFactor(factor float64) error {
	a.st.SetZoom(factor)
	return nil
}

// GetDesktopZoomFactor 读窗口缩放。
func (a *App) GetDesktopZoomFactor() float64 { return a.st.Zoom() }

// SetDesktopCheckUpdates 检查更新开关。
func (a *App) SetDesktopCheckUpdates(_enabled bool) error { return nil }

// SetDesktopConversationWidth 会话宽度。
func (a *App) SetDesktopConversationWidth(_width string) error { return nil }

// SetDesktopTerminalTheme 终端主题。
func (a *App) SetDesktopTerminalTheme(_theme string) error { return nil }

// SetDesktopMetrics 桌面指标开关。
func (a *App) SetDesktopMetrics(_enabled bool) error { return nil }

// SetDesktopTelemetry 遥测开关。
func (a *App) SetDesktopTelemetry(_enabled bool) error { return nil }

// SetTrayLocale 托盘语言。
func (a *App) SetTrayLocale(_locale string) error { return nil }

// SetDisplayMode 显示模式。
func (a *App) SetDisplayMode(_mode string) error { return nil }

// SetStatusBarStyle 状态栏样式（icon/text，持久化）。
func (a *App) SetStatusBarStyle(style string) error {
	a.st.SetStatusBarStyle(style)
	return nil
}

// SetStatusBarItems 状态栏显示项（持久化）。
func (a *App) SetStatusBarItems(items []string) error {
	a.st.SetStatusBarItems(items)
	return nil
}

// SetDefaultModel 默认模型。
func (a *App) SetDefaultModel(_ref string) error { return nil }

// SetPlannerModel 规划器模型。
func (a *App) SetPlannerModel(_ref string) error { return nil }

// SetSubagentModel 子代理模型。
func (a *App) SetSubagentModel(_ref string) error { return nil }

// SetSubagentEffort 子代理强度。
func (a *App) SetSubagentEffort(_effort string) error { return nil }

// SetMaxSubagentDepth 子代理最大深度。
func (a *App) SetMaxSubagentDepth(_depth int) error { return nil }

// SetMaxSubagentConcurrency 子代理最大并发。
func (a *App) SetMaxSubagentConcurrency(_n int) error { return nil }

// SetMaxParallelWriters 最大并行写入。
func (a *App) SetMaxParallelWriters(_n int) error { return nil }

// SetCompactRatio 压缩比例。
func (a *App) SetCompactRatio(_ratio float64) error { return nil }

// SetDefaultToolApprovalMode 默认工具审批模式（ask/auto/yolo，持久化；新会话自动应用）。
func (a *App) SetDefaultToolApprovalMode(mode string) error {
	a.st.SetDefaultToolApprovalMode(mode)
	return nil
}

// SetAutoPlan 自动规划。
func (a *App) SetAutoPlan(_mode string) error { return nil }

// SetToolApprovalModeForTab 指定会话工具审批模式。
func (a *App) SetToolApprovalModeForTab(_tabID, _mode string) error { return nil }

// SetModeForTab 指定会话模式。
func (a *App) SetModeForTab(_tabID, _mode string) error { return nil }

// SetCollaborationModeForTab 指定会话协作模式。
func (a *App) SetCollaborationModeForTab(_tabID, _mode string) error { return nil }

// SetGoalForTab 指定会话目标。
func (a *App) SetGoalForTab(_tabID, _goal string) error { return nil }

// ClearGoalForTab 清除会话目标。
func (a *App) ClearGoalForTab(_tabID string) error { return nil }

// PauseGoalForTab 暂停会话目标。
func (a *App) PauseGoalForTab(_tabID string) error { return nil }

// ResumeGoalForTab 恢复会话目标。
func (a *App) ResumeGoalForTab(_tabID string) error { return nil }

// SetProjectPinned 项目钉住。
func (a *App) SetProjectPinned(_root string, _pinned bool) error { return nil }

// SetTopicPinned 会话钉住。
func (a *App) SetTopicPinned(_topicID string, _pinned bool) error { return nil }

// SetProjectColor 项目颜色。
func (a *App) SetProjectColor(_root, _color string) error { return nil }

// ReorderProjects 项目排序。
func (a *App) ReorderProjects(_roots []string) error { return nil }

// ReorderTabs 标签排序。
func (a *App) ReorderTabs(_tabIDs []string) error { return nil }

// SetActiveTab 设置活动标签。
func (a *App) SetActiveTab(_tabID string) error { return nil }

// CloseTab 关闭标签。
func (a *App) CloseTab(_tabID string) error { return nil }

// CloseTabWithPolicy 关闭标签（带策略）。
func (a *App) CloseTabWithPolicy(_tabID, _policy string) error { return nil }

// NewSession 新建会话。
func (a *App) NewSession() map[string]any { return a.NewSessionForTab("") }

// NewSessionForTab 新建会话。
func (a *App) NewSessionForTab(_tabID string) map[string]any {
	s, err := a.CreateSession(homeDir(), "")
	if err != nil {
		return map[string]any{}
	}
	a.applyDefaultApproval(s)
	return s
}

// applyDefaultApproval 新会话自动应用默认审批（ask/auto/yolo）。
// auto（默认）不干预（对齐旧版语义：DSH 默认）；ask/yolo 经 DSH commands/execute
// /permission <preset> 设置；失败仅日志，不阻断会话创建（P3：兜底不崩溃）。
func (a *App) applyDefaultApproval(meta map[string]any) {
	mode := ""
	if a.st != nil {
		mode = a.st.DefaultToolApprovalMode()
	}
	if mode == "" || mode == "auto" {
		return
	}
	sid, _ := meta["tabId"].(string)
	if sid == "" {
		sid, _ = meta["id"].(string)
	}
	if sid == "" || a.dsh == nil {
		return
	}
	perm := modeToPermission(mode)
	if perm == "" {
		return
	}
	apply := func(p string) error {
		_, err := a.dsh.RPC("commands/execute", map[string]any{
			"args": map[string]any{"agentId": sid, "line": "/permission " + p},
		})
		return err
	}
	if err := apply(perm); err != nil {
		// ask 的 read-only preset 在默认配置可能不存在，回退 workspace-write（对齐旧版）。
		if mode == "ask" {
			if err2 := apply("workspace-write"); err2 != nil {
				resumeLog("applyDefaultApproval: %v; fallback failed: %v", err, err2)
				return
			}
			resumeLog("applyDefaultApproval: session %s -> ask (fallback workspace-write)", sid)
			return
		}
		resumeLog("applyDefaultApproval: %v", err)
		return
	}
	resumeLog("applyDefaultApproval: session %s -> %s (%s)", sid, mode, perm)
}

// modeToPermission 审批模式 → DSH /permission preset（对齐旧版 MODE_TO_PERMISSION）。
func modeToPermission(mode string) string {
	switch mode {
	case "ask":
		return "read-only"
	case "auto":
		return "workspace-write"
	case "yolo":
		return "danger-full-access"
	}
	return ""
}

// OpenGlobalTab 打开全局标签。
func (a *App) OpenGlobalTab(_a1 any) map[string]any { return a.NewSessionForTab("") }

// OpenProjectTab 打开项目标签。
func (a *App) OpenProjectTab(_a1 any, _a2 any) map[string]any { return a.NewSessionForTab("") }

// OpenTopicSession 打开话题会话。
func (a *App) OpenTopicSession(_a1 any, _a2 any, _a3 any, _a4 any) map[string]any {
	return a.NewSessionForTab("")
}

// SwitchWorkspace 切换工作区。
func (a *App) SwitchWorkspace(_root string) error { return nil }

// RenameTopic 重命名话题。
func (a *App) RenameTopic(_topicID, _title string) error { return nil }

// TrashTopic 移话题入回收站。
func (a *App) TrashTopic(_topicID string) error { return nil }

// RenameProject 重命名项目。
func (a *App) RenameProject(_root, _name string) error { return nil }

// RemoveWorkspace 移除工作区。
func (a *App) RemoveWorkspace(_root string) error { return nil }

// RevealPath 在文件管理器显示路径。
func (a *App) RevealPath(_path string) {}

// RevealWorkspacePathForTab 显示会话工作区路径。
func (a *App) RevealWorkspacePathForTab(_a1 any, _a2 any) {}

// RevealWorkspaceWriterForTab 显示写入者。
func (a *App) RevealWorkspaceWriterForTab(_tabID string) {}

// OpenLocalPath 打开本地路径。
func (a *App) OpenLocalPath(_path string) {}

// OpenLocalPathInExternalOpener 用外部打开器打开。
func (a *App) OpenLocalPathInExternalOpener(_a1 any, _a2 any) {}

// OpenWorkspacePathForTab 打开会话工作区路径。
func (a *App) OpenWorkspacePathForTab(_a1 any, _a2 any) {}

// PickWorkspace 选择工作区（无 UI 选择器，空）。
func (a *App) PickWorkspace() string { return "" }

// PickExportFile 选择导出文件。
func (a *App) PickExportFile() map[string]any { return nil }

// SaveExportFile 保存导出文件。
func (a *App) SaveExportFile(_a1 any, _a2 any, _a3 any) error { return nil }

// SaveExportImageFiles 保存导出图片。
func (a *App) SaveExportImageFiles(_a1 any, _a2 any) error { return nil }

// ReadFileForTab 读文件。
func (a *App) ReadFileForTab(_tabID, _path string) string { return "" }

// SaveDoc / SaveDocForTab 保存文档。
func (a *App) SaveDoc(_path, _content string) error { return nil }
func (a *App) SaveDocForTab(_tabID, _path, _content string) error { return nil }

// SaveClipboardImage / SavePastedFile / SavePastedImage 保存粘贴内容。
func (a *App) SaveClipboardImage() string { return "" }
func (a *App) SavePastedFile(_name string, _data string) string { return "" }
func (a *App) SavePastedImage(_name string, _data string) string { return "" }

// AttachDropped 附加拖放文件。
func (a *App) AttachDropped(_tabID string, _paths []string) {}

// ListDirForTab 列目录。
func (a *App) ListDirForTab(_tabID, _path string) []any { return []any{} }

// SearchFileRefsForTab 搜索文件引用。
func (a *App) SearchFileRefsForTab(_tabID, _query string) []any { return []any{} }

// ResolveMarkdownImageForTab 解析 markdown 图片。
func (a *App) ResolveMarkdownImageForTab(_tabID, _src string) string { return "" }

// ResolveWorkspacePathForTab 解析工作区路径。
func (a *App) ResolveWorkspacePathForTab(_tabID string) string { return "" }

// CheckUpdate 检查更新。
func (a *App) CheckUpdate() map[string]any { return map[string]any{"available": false, "version": ""} }

// ApplyUpdateRequest 应用更新。
func (a *App) ApplyUpdateRequest(_a1 any, _a2 any, _a3 any) error { return nil }

// AbandonPendingUpdate 放弃待处理更新。
func (a *App) AbandonPendingUpdate() {}

// OpenDownloadPage 打开下载页。
func (a *App) OpenDownloadPage() {}

// ReportCrash 上报崩溃。
func (a *App) ReportCrash(_report string) {}

// RecordUIPerf 记录 UI 性能。
func (a *App) RecordUIPerf(_event string, _ms float64) {}

// ReloadSettings 重新加载设置。
func (a *App) ReloadSettings() {}

// ReloadRuntime 重新加载运行时。
func (a *App) ReloadRuntime(_a1 any) {}

// ReloadUserConfig 重新加载用户配置。
func (a *App) ReloadUserConfig() {}

// SaveWindowState 保存窗口状态。
func (a *App) SaveWindowState(_state map[string]any) error { return nil }

// ReportDesktopWebViewReady 报告 webview 就绪。
func (a *App) ReportDesktopWebViewReady() {}

// RestartApplication 重启应用。
func (a *App) RestartApplication() {}
