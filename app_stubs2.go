package main

// 批量生成的空实现（gen-stubs.js 从 preload.js + 前端调用清单对比生成）。
// 覆盖前端调用但 Go 端缺失的方法，返回安全空态/降级，防 not-a-function 崩溃。

func (a *App) AcceptDeliveryToTab() error { return nil }
func (a *App) AcceptMemorySuggestion() error { return nil }
func (a *App) AcceptMemorySuggestionForTab() error { return nil }
func (a *App) AcceptSkillSuggestion() error { return nil }
func (a *App) AcceptSkillSuggestionForTab() error { return nil }
func (a *App) ActivateThemePack() error { return nil }
func (a *App) AddOfficialProviderAccess() error { return nil }
func (a *App) AddPermissionRule() error { return nil }
func (a *App) AddProviderPresetAccess() error { return nil }
func (a *App) AddRemoteForward() error { return nil }
func (a *App) AddRemoteHost() error { return nil }
func (a *App) AddSkillPath() error { return nil }
func (a *App) AnswerQuestionForTab() error { return nil }
func (a *App) ApproveTab() error { return nil }
func (a *App) AttachmentDataURL() string { return "" }
func (a *App) AuthenticateMCPServer() error { return nil }
func (a *App) AvailableSubagentTools() []any { return []any{} }
func (a *App) BalanceForTab() error { return nil }
func (a *App) CancelJobForTab() error { return nil }
func (a *App) CancelTab() error { return nil }
func (a *App) CancelTabWithInboxItems() error { return nil }
func (a *App) CancelTabWithInboxItemsResult() error { return nil }
func (a *App) CancelTrySubagentProfile() error { return nil }
func (a *App) Capabilities() map[string]any { return nil }
func (a *App) ChooseRecoveryBranch() error { return nil }
func (a *App) CleanRecoveryLineage() error { return nil }
func (a *App) CleanRemoteLegacyWorkbenchData() error { return nil }
func (a *App) ClearBotSecret() error { return nil }
func (a *App) ClearMCPServerAuthentication() error { return nil }
func (a *App) ClearSession() error { return nil }
func (a *App) ClearSessionForTab() error { return nil }
func (a *App) CommitRewindForTab() error { return nil }
func (a *App) CommitWorkspaceFileRevertForTab() error { return nil }
func (a *App) CompactForTab() error { return nil }
func (a *App) ConfirmRemoteHostKey() error { return nil }
func (a *App) ConfirmRemoteSecret() error { return nil }
func (a *App) ConnectRemoteHost() error { return nil }
func (a *App) ContextPanel() error { return nil }
func (a *App) CopyThemePack() error { return nil }
func (a *App) CreateBlankProject(_parentDir string, _projectName string) error { return nil }
func (a *App) CreateIsolatedWorktree(_workspaceRoot string) error { return nil }
func (a *App) CreateSubagentProfile() error { return nil }
func (a *App) CreateTopic() error { return nil }
func (a *App) DeleteInboxItem() error { return nil }
func (a *App) DeleteRecoveryCopy() error { return nil }
func (a *App) DeleteSubagentProfile() error { return nil }
func (a *App) DeleteThemePack() error { return nil }
func (a *App) DiagnoseBotConnection() error { return nil }
func (a *App) DisconnectRemoteHost() error { return nil }
func (a *App) DismissTodoBatchForTab() error { return nil }
func (a *App) EnqueueInboxSteer() error { return nil }
func (a *App) EnsureBlankSurface() error { return nil }
func (a *App) EnsureBlankTab() error { return nil }
func (a *App) ExportThemePack() string { return "" }
func (a *App) Forget(_name string) error { return nil }
func (a *App) ForgetForTab(_tabID string, _name string) error { return nil }
func (a *App) ForkForTab() error { return nil }
func (a *App) GetRecoveryLineage() map[string]any { return nil }
func (a *App) GetSessionCatalogStatus() map[string]any { return nil }
func (a *App) GetTopicSummary(_key string) map[string]any { return nil }
func (a *App) HeartbeatGenerateID() error { return nil }
func (a *App) HeartbeatReloadConfig() error { return nil }
func (a *App) HeartbeatSaveConfig() error { return nil }
func (a *App) HeartbeatTriggerNow() error { return nil }
func (a *App) HistoryContentForTab() error { return nil }
func (a *App) HistorySliceForTab(_tabID string, _req map[string]any) map[string]any {
	// 返回 HistorySlice 结构（前端读 .entries/.hasOlder/.error 等，不能返回 null）
	return map[string]any{
		"entries": []any{}, "nextCursor": "", "hasOlder": false,
		"totalTurns": 0, "startTurn": 0, "endTurn": 0, "stale": false,
		"revision": 0, "revisionKnown": false, "digest": "", "source": "dsh", "error": "",
	}
}

// ListProjectGroups 项目分组（旧版前端名；新版用 GetProjectGroups）。
func (a *App) ListProjectGroups(_scope, _workspaceRoot string) []map[string]any {
	return []map[string]any{}
}

// GetProjectGroups 项目分组快照（返回 {groups, revision, applied}）。
func (a *App) GetProjectGroups(_scope, _workspaceRoot string) map[string]any {
	return map[string]any{"groups": []any{}, "revision": 0, "applied": true}
}

// ExternalOpenersForTab 外部打开器（返回 {openers, preferred, workspaceOpenable}）。
func (a *App) ExternalOpenersForTab(_tabID string) map[string]any {
	return map[string]any{"openers": []any{}, "preferred": "", "workspaceOpenable": true}
}

// OpenWorkspaceInExternalOpenerForTab 用外部打开器打开工作区。
func (a *App) OpenWorkspaceInExternalOpenerForTab(_tabID, _id string) error { return nil }

// ReorderTopics 会话排序。
func (a *App) ReorderTopics(_scope, _workspaceRoot string, _orderedTopicIDs []string) error { return nil }

// SaveSessionGroups 保存会话分组。
func (a *App) SaveSessionGroups(_scope, _workspaceRoot string, _groups []map[string]any) error { return nil }

// SaveSessionGroupsVersioned 保存会话分组（版本化）。
func (a *App) SaveSessionGroupsVersioned(_scope, _workspaceRoot string, _expectedRevision uint64, _groups []map[string]any) map[string]any {
	return map[string]any{"groups": []any{}, "revision": 0, "applied": true}
}

// SetPreferredExternalOpener 设置首选外部打开器。
func (a *App) SetPreferredExternalOpener(_id string) error { return nil }
func (a *App) ImportThemePack() error { return nil }
func (a *App) InstallMCPServer() error { return nil }
func (a *App) InstallPlugin(_source string, _options map[string]any) error { return nil }
func (a *App) InvokeExtensionAction() error { return nil }
func (a *App) IsolatedWorktreeAvailability() error { return nil }
func (a *App) ListRemoteDir() []any { return []any{} }
func (a *App) ListTaskEventPage(_req map[string]any) error { return nil }
func (a *App) ListTaskEventsForTab() error { return nil }
func (a *App) ListTaskPage() error { return nil }
func (a *App) ListTasksForTab() error { return nil }
func (a *App) MCPMarketplace(_query string) []any { return []any{} }
func (a *App) MCPMarketplaceResolve(_registryName string) error { return nil }
func (a *App) OpenChannelSessionPageForTab() error { return nil }
func (a *App) OpenRemoteWorkspace() error { return nil }
func (a *App) OpenTaskSessionByKey(_req map[string]any) error { return nil }
func (a *App) OpenTaskSessionForTab() error { return nil }
func (a *App) PickBlankProjectParent() error { return nil }
func (a *App) PickPluginFolder() error { return nil }
func (a *App) PickSkillFolder() error { return nil }
func (a *App) PickThemeBackground() error { return nil }
func (a *App) PlanPluginInstall(_source string, _options map[string]any) error { return nil }
func (a *App) PollBotConnectionInstall() error { return nil }
func (a *App) PreviewRewindForTab() error { return nil }
func (a *App) PreviewSession() map[string]any { return nil }
func (a *App) PreviewWorkspaceFileRevertForTab(_tabID string, _path string) error { return nil }
func (a *App) PurgeRecoveryCopy() error { return nil }
func (a *App) PurgeTrashedSession() error { return nil }
func (a *App) ReadInboxItem() error { return nil }
func (a *App) ReadRemoteFile() error { return nil }
func (a *App) ReconnectMCPServer() error { return nil }
func (a *App) RefreshSkills() error { return nil }
func (a *App) Remember(_scope string, _note string) error { return nil }
func (a *App) RememberForTab(_tabID string, _scope string, _note string) error { return nil }
func (a *App) RemoteLastWorkspace() map[string]any { return nil }
func (a *App) RemoteServerLogs() map[string]any { return nil }
func (a *App) RemoteServerStatus() map[string]any { return nil }
func (a *App) RemoveMCPServer() error { return nil }
func (a *App) RemovePermissionRule() error { return nil }
func (a *App) RemovePlugin(_name string) error { return nil }
func (a *App) RemoveProviderAccesses() error { return nil }
func (a *App) RemoveRemoteForward() error { return nil }
func (a *App) RemoveRemoteHost() error { return nil }
func (a *App) RequeueTaskByKey(_req map[string]any) error { return nil }
func (a *App) RequeueTaskForTab() error { return nil }
func (a *App) ResetProviderPresetAccess() error { return nil }
func (a *App) ResetThemePack() error { return nil }
func (a *App) ResolvePlanDecisionTab() error { return nil }
func (a *App) ResolveRecoveryTab() error { return nil }
func (a *App) RestoreArchivedMemory() error { return nil }
func (a *App) RestoreArchivedMemoryForTab() error { return nil }
func (a *App) RestoreMemoryRevision() error { return nil }
func (a *App) RestoreMemoryRevisionForTab() error { return nil }
func (a *App) RestoreSession() error { return nil }
func (a *App) ResumeSessionPage() error { return nil }
func (a *App) ResumeSessionPageForTab() error { return nil }
func (a *App) RetryInboxItem() error { return nil }
func (a *App) RevealBackgroundRuntime() error { return nil }
func (a *App) RunShellForTab() error { return nil }
func (a *App) SaveHooksSettingsForRoot() error { return nil }
func (a *App) SaveLocalPathAs() error { return nil }
func (a *App) SaveProvider() error { return nil }
func (a *App) SaveProviderModelCatalogs() error { return nil }
func (a *App) SaveThemePack() error { return nil }
func (a *App) ScanPromptHistory() []any { return []any{} }
func (a *App) ScanRemoteLegacyWorkbenchData() error { return nil }
func (a *App) ScanSSHConfig() []any { return []any{} }
func (a *App) SetBotConnectionToolApprovalMode() error { return nil }
func (a *App) SetBotSecret() error { return nil }
func (a *App) SetBotSettings() error { return nil }
func (a *App) SetComposerProfileForTab() error { return nil }
func (a *App) SetInboxPaused() error { return nil }
func (a *App) SetMCPServerEnabled() error { return nil }
func (a *App) SetNetwork() error { return nil }
func (a *App) SetPermissionMode() error { return nil }
func (a *App) SetPluginEnabled(_name string, _enabled bool) error { return nil }
func (a *App) SetProviderWebSearch() error { return nil }
func (a *App) SetReasoningLanguage() error { return nil }
func (a *App) SetSandbox() error { return nil }
func (a *App) SetSkillEnabled() error { return nil }
func (a *App) SetSkillImplicitInvocation() error { return nil }
func (a *App) SetSkillPathEnabled() error { return nil }
func (a *App) SetSubagentProfileEffort() error { return nil }
func (a *App) SetSubagentProfileModel() error { return nil }
func (a *App) StartBotConnectionInstall() error { return nil }
func (a *App) StartTopicActivation(_req map[string]any) error { return nil }
func (a *App) SteerInboxItem() error { return nil }
func (a *App) StopRemoteServer() error { return nil }
func (a *App) StopTaskByKey(_req map[string]any) error { return nil }
func (a *App) StopTaskForTab() error { return nil }
func (a *App) SubmitDeliveryRecoveryToTabWithID(_tabID string, __display string, _input map[string]any) error { return nil }
func (a *App) SubmitDisplayToTabWithID(_tabID string, __display string, _input map[string]any) error { return nil }
func (a *App) SubmitEditedDisplayToTabWithID(_tabID string, __display string, _input map[string]any) error { return nil }
func (a *App) SubmitExtensionForm() error { return nil }
func (a *App) SubmitInitialGoalToTabWithID(_tabID string, _goal string) error { return nil }
func (a *App) SubmitInvocationsToTabWithID(_tabID string, __display string, _input map[string]any) error { return nil }
func (a *App) SubmitToTabWithID(_tabID string, _input map[string]any) error { return nil }
func (a *App) SummarizeFromForTab() error { return nil }
func (a *App) SummarizeUpToForTab() error { return nil }
func (a *App) TerminalOutputForTab() error { return nil }
func (a *App) TestBotConnection() error { return nil }
func (a *App) ToolResultForTab() error { return nil }
func (a *App) TrySubagentProfile() error { return nil }
func (a *App) UndoRewindForTab() error { return nil }
func (a *App) UpdateInboxItem() error { return nil }
func (a *App) UpdateMCPServer() error { return nil }
func (a *App) UpdatePlugin(_name string) error { return nil }
func (a *App) UpdateRemoteHost() error { return nil }
func (a *App) UpdateSubagentProfile() error { return nil }
func (a *App) UpgradeDeepSeekProviderAccess() error { return nil }
func (a *App) UsageStats() error { return nil }
func (a *App) WriteRemoteFile() error { return nil }
