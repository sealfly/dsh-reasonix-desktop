package main

// app_stubs_v1314.go — Reasonix v1.31.4 前端新增桥方法的安全空实现。
// 官方 v1.31.4 前端比 v1.29.0 多调用这些方法（历史 API 改名 + 新功能）。
// 本地 DSH 模式无对应运行时语义，按"安全空实现"补齐，防 not-a-function 崩溃；
// 返回空态/降级值，前端能正确处理。需要真实逻辑的方法后续按需增强。

// History 当前标签页消息历史（v1.31.4 历史 API，替代旧 ListHistorySessions 路径）。
func (a *App) History() []any { return []any{} }

// HistoryForTab 指定标签页消息历史。
func (a *App) HistoryForTab(_tabID string) []any { return []any{} }

// Balance 账户余额信息（DSH 无此概念，空）。
func (a *App) Balance() map[string]any { return map[string]any{} }

// SetToolApprovalMode 设置工具审批模式（转发到 ForTab 版本，同样空实现）。
func (a *App) SetToolApprovalMode(mode string) error { return a.SetToolApprovalModeForTab("", mode) }

// SetAutoApproveTools 自动批准工具（yolo 模式开关）。
func (a *App) SetAutoApproveTools(on bool) error {
	mode := "ask"
	if on {
		mode = "yolo"
	}
	return a.SetToolApprovalModeForTab("", mode)
}

// SetVisionModel 设置视觉模型（DSH 模型由后端管理，空操作）。
func (a *App) SetVisionModel(_model string) error { return nil }


// Steer 转向指示（无对应运行时，空操作）。
func (a *App) Steer(_text string) error { return nil }

// Fork 从指定轮次分叉会话（返回空 TabMeta，前端降级）。
func (a *App) Fork(_turn float64) map[string]any {
	return map[string]any{"tabId": "", "topicId": "", "title": "", "scope": "global", "workspaceRoot": ""}
}

// Approve 审批请求（DSH 模式审批在 DSH 侧处理，空操作）。
func (a *App) Approve(_a1 string, _a2 bool, _a3 bool, _a4 bool) error { return nil }

// Submit 提交消息（会话提交走 DSH，空操作）。
func (a *App) Submit(_text string) error { return nil }

// SubmitDisplay 提交 display 回合。
func (a *App) SubmitDisplay(_a1 string, _a2 string) error { return nil }

// SubmitDisplayToTab 提交 display 回合到指定标签页。
func (a *App) SubmitDisplayToTab(_a1 string, _a2 string) error { return nil }

// SubmitInvocationsToTab 提交调用列表到指定标签页。
func (a *App) SubmitInvocationsToTab(_a1 string, _a2 string, _a3 string, _a4 []any) error { return nil }

// ReadFile 读取文件预览（DSH 无文件服务，空）。
func (a *App) ReadFile(path string) map[string]any {
	return map[string]any{"path": path, "content": "", "size": 0, "truncated": false}
}

// ListDir 列出目录（空）。
func (a *App) ListDir(_path string) []any { return []any{} }

// SearchFileRefs 按文件名搜索（空）。
func (a *App) SearchFileRefs(_query string) []any { return []any{} }

// OpenWorkspacePath 打开工作区路径（空操作）。
func (a *App) OpenWorkspacePath(_path string) error { return nil }

// RevealWorkspacePath 在文件管理器中显示路径（空操作）。
func (a *App) RevealWorkspacePath(_path string) error { return nil }

// OpenWorkspaceInExternalOpener 用外部打开器打开工作区（空操作）。
func (a *App) OpenWorkspaceInExternalOpener(_path string) error { return nil }

// RunShell 在 shell 中执行（DSH 无 shell 集成，空操作）。
func (a *App) RunShell(_cmd string) error { return nil }

// Checkpoints 检查点列表（DSH 无检查点，空）。
func (a *App) Checkpoints() []any { return []any{} }

// Jobs 任务列表（空）。
func (a *App) Jobs() []any { return []any{} }

// CancelJob 取消任务（返回 false 表示未取消）。
func (a *App) CancelJob(_id string) bool { return false }

// GetTaskCatalogStatus 任务目录索引状态（空）。
func (a *App) GetTaskCatalogStatus() map[string]any { return map[string]any{"state": "ready"} }

// RebuildSessionCatalog 重建会话目录（DSH 无索引，空操作）。
func (a *App) RebuildSessionCatalog() error { return nil }

// ActivateBaseStyle 激活基础样式（主题机制，空操作）。
func (a *App) ActivateBaseStyle(_style string) error { return nil }

// DisableThemePack 禁用主题包（空操作）。
func (a *App) DisableThemePack() error { return nil }

// RestoreGraphiteAppearance 恢复石墨外观（空操作）。
func (a *App) RestoreGraphiteAppearance() error { return nil }

// AnswerQuestion 回答澄清问题（空操作）。
func (a *App) AnswerQuestion(_a1 string, _a2 []any) error { return nil }

// EnqueueInboxFollowup 入队收件箱跟进（返回空回执）。
func (a *App) EnqueueInboxFollowup(_a1 string, _a2 string, _a3 string, _a4 string) map[string]any {
	return map[string]any{}
}

// EnqueueInboxFollowupWithInvocations 入队带调用的跟进（返回空回执）。
func (a *App) EnqueueInboxFollowupWithInvocations(_a1 string, _a2 string, _a3 string, _a4 []any, _a5 string) map[string]any {
	return map[string]any{}
}

// OpenChannelSessionForTab 打开渠道会话（空）。
func (a *App) OpenChannelSessionForTab(_a1 string, _a2 string) []any { return []any{} }

// SetBotDingtalkToolApprovalMode 钉钉机器人工具审批模式（空操作）。
func (a *App) SetBotDingtalkToolApprovalMode(_mode string) error { return nil }

// TestDingtalkBot 测试钉钉机器人（返回空诊断）。
func (a *App) TestDingtalkBot() map[string]any { return map[string]any{} }

// WorkspaceGitCommitDetail Git 提交详情（空）。
func (a *App) WorkspaceGitCommitDetail(_a1 string, _a2 string, _a3 string) map[string]any { return map[string]any{} }
