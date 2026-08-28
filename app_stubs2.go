package main

// 鎵归噺鐢熸垚鐨勭┖瀹炵幇锛坓en-stubs.js 浠?preload.js + 鍓嶇璋冪敤娓呭崟瀵规瘮鐢熸垚锛夈€?
// 瑕嗙洊鍓嶇璋冪敤浣?Go 绔己澶辩殑鏂规硶锛岃繑鍥炲畨鍏ㄧ┖鎬?闄嶇骇锛岄槻 not-a-function 宕╂簝銆?

func (a *App) AcceptDeliveryToTab(_a1 any) error { return nil }
func (a *App) AcceptMemorySuggestion() error { return nil }
func (a *App) AcceptMemorySuggestionForTab(_a1 any, _a2 any) error { return nil }
func (a *App) AcceptSkillSuggestion() error { return nil }
func (a *App) AcceptSkillSuggestionForTab(_a1 any, _a2 any) error { return nil }
func (a *App) ActivateThemePack(_a1 any) error { return nil }
func (a *App) AddOfficialProviderAccess(_a1 any, _a2 any) error { return nil }
func (a *App) AddPermissionRule(_a1 any, _a2 any) error { return nil }
func (a *App) AddProviderPresetAccess(_a1 any, _a2 any) error { return nil }
func (a *App) AddRemoteForward() error { return nil }
func (a *App) AddRemoteHost() error { return nil }
func (a *App) AddSkillPath(_a1 any) error { return nil }
func (a *App) AnswerQuestionForTab(_a1 any, _a2 any, _a3 any) error { return nil }
func (a *App) ApproveTab(_a1 any, _a2 any, _a3 any, _a4 any, _a5 any) error { return nil }
func (a *App) AttachmentDataURL() string { return "" }
func (a *App) AuthenticateMCPServer(_a1 any) error { return nil }
func (a *App) AvailableSubagentTools() []any {
	// 贴合 DSH：子智能体是运行时派生的子会话，从 subagent.list 读真实条目。
	entries := []any{}
	if a.dsh != nil {
		if raw, err := a.dsh.RPC("subagent.list", map[string]any{"parentSessionId": a.activeSessionID("")}); err == nil {
			var list struct {
				Entries []struct {
					Kind     string `json:"kind"`
					ID       string `json:"id"`
					Mode     string `json:"mode"`
					Activity string `json:"activity"`
					Label    string `json:"label"`
				} `json:"entries"`
			}
			if err := DecodeRPC(raw, &list); err == nil {
				for _, e := range list.Entries {
					entries = append(entries, map[string]any{
						"id": e.ID, "kind": e.Kind, "mode": e.Mode,
						"activity": e.Activity, "label": e.Label,
					})
				}
			}
		}
	}
	return entries
}
func (a *App) BalanceForTab() error { return nil }
func (a *App) CancelJobForTab(_a1 any, _a2 any) error { return nil }
func (a *App) CancelTab(_a1 any) error { return nil }
func (a *App) CancelTabWithInboxItems(_a1 any, _a2 any) error { return nil }
func (a *App) CancelTabWithInboxItemsResult(_a1 any, _a2 any) error { return nil }
func (a *App) CancelTrySubagentProfile() error { return nil }
func (a *App) Capabilities() map[string]any {
	// 能力面板总览（MCP 服务器/技能/插件）——skills 接 DSH skill.list，
	// servers/plugins 置空（DSH 的 MCP 是配置式插件，无运行时 RPC）。
	sv := a.skillsView("")
	return map[string]any{
		"servers":                 []any{},
		"skills":                  sv["skills"],
		"skillRoots":              []any{},
		"plugins":                 []any{},
		"allowImplicitInvocation": true,
	}
}
func (a *App) ChooseRecoveryBranch() error { return nil }
func (a *App) CleanRecoveryLineage(_a1 any) error { return nil }
func (a *App) CleanRemoteLegacyWorkbenchData() error { return nil }
func (a *App) ClearBotSecret(_a1 any) error { return nil }
func (a *App) ClearMCPServerAuthentication(_a1 any) error { return nil }
func (a *App) ClearSession() error { return nil }
func (a *App) ClearSessionForTab() error { return nil }
func (a *App) CommitRewindForTab() error { return nil }
func (a *App) CommitWorkspaceFileRevertForTab() error { return nil }
func (a *App) CompactForTab() error { return nil }
func (a *App) ConfirmRemoteHostKey() error { return nil }
func (a *App) ConfirmRemoteSecret() error { return nil }
func (a *App) ConnectRemoteHost() error { return nil }
func (a *App) ContextPanel() error { return nil }
func (a *App) CopyThemePack(_a1 any, _a2 any, _a3 any) error { return nil }
func (a *App) CreateBlankProject(_parentDir string, _projectName string) error { return nil }
func (a *App) CreateIsolatedWorktree(_workspaceRoot string) error { return nil }
func (a *App) CreateSubagentProfile(_a1 any) error { return nil }
func (a *App) CreateTopic() error { return nil }
func (a *App) DeleteInboxItem() error { return nil }
func (a *App) DeleteRecoveryCopy(_a1 any) error { return nil }
func (a *App) DeleteSubagentProfile(_a1 any, _a2 any) error { return nil }
func (a *App) DeleteThemePack(_a1 any) error { return nil }
func (a *App) DiagnoseBotConnection(_a1 any) error { return nil }
func (a *App) DisconnectRemoteHost() error { return nil }
func (a *App) DismissTodoBatchForTab() error { return nil }
func (a *App) EnqueueInboxSteer(_a1 any, _a2 any, _a3 any, _a4 any) error { return nil }
// EnsureBlankSurface 新建空白会话（单面布局；前端"新建对话"入口），返回 tabMeta（新会话）。
func (a *App) EnsureBlankSurface(scope string, workspaceRoot string) map[string]any {
	return a.blankSessionMeta(scope, workspaceRoot)
}

// EnsureBlankTab 新建空白标签（前端"新建对话"入口），返回 tabMeta（新会话）。
func (a *App) EnsureBlankTab(scope string, workspaceRoot string) map[string]any {
	return a.blankSessionMeta(scope, workspaceRoot)
}

// blankSessionMeta 创建新会话并返回 tabMeta（应用默认审批；失败返回空 map 兜底，P3）。
func (a *App) blankSessionMeta(scope, workspaceRoot string) map[string]any {
	root := workspaceRoot
	if scope != "project" {
		root = ""
	}
	if root == "" {
		root = homeDir()
	}
	s, err := a.CreateSession(root, "")
	if err != nil {
		return map[string]any{}
	}
	a.applyDefaultApproval(s)
	return s
}
func (a *App) ExportThemePack() string { return "" }
func (a *App) Forget(_name string) error { return nil }
func (a *App) ForgetForTab(_tabID string, _name string) error { return nil }
func (a *App) ForkForTab(_a1 any, _a2 any) error { return nil }
func (a *App) GetRecoveryLineage(_a1 any) map[string]any { return nil }
func (a *App) GetSessionCatalogStatus() map[string]any { return nil }
func (a *App) GetTopicSummary(_key string) map[string]any { return nil }
func (a *App) HeartbeatGenerateID() error { return nil }
func (a *App) HeartbeatReloadConfig() error { return nil }
func (a *App) HeartbeatSaveConfig() error { return nil }
func (a *App) HeartbeatTriggerNow(_a1 any) error { return nil }
func (a *App) HistoryContentForTab(_a1 any, _a2 any, _a3 any) error { return nil }
func (a *App) HistorySliceForTab(_tabID string, req map[string]any) map[string]any {
	return a.historySliceForTabImpl(_tabID, req)
}

// ListProjectGroups 椤圭洰鍒嗙粍锛堟棫鐗堝墠绔悕锛涙柊鐗堢敤 GetProjectGroups锛夈€?
func (a *App) ListProjectGroups(_scope, _workspaceRoot string) []map[string]any {
	return []map[string]any{}
}

// GetProjectGroups 椤圭洰鍒嗙粍蹇収锛堣繑鍥?{groups, revision, applied}锛夈€?
func (a *App) GetProjectGroups(_scope, _workspaceRoot string) map[string]any {
	return map[string]any{"groups": []any{}, "revision": 0, "applied": true}
}

// ExternalOpenersForTab 澶栭儴鎵撳紑鍣紙杩斿洖 {openers, preferred, workspaceOpenable}锛夈€?
func (a *App) ExternalOpenersForTab(_tabID string) map[string]any {
	return map[string]any{"openers": []any{}, "preferred": "", "workspaceOpenable": true}
}

// OpenWorkspaceInExternalOpenerForTab 鐢ㄥ閮ㄦ墦寮€鍣ㄦ墦寮€宸ヤ綔鍖恒€?
func (a *App) OpenWorkspaceInExternalOpenerForTab(_tabID, _id string) error { return nil }

// ReorderTopics 浼氳瘽鎺掑簭銆?
func (a *App) ReorderTopics(_scope, _workspaceRoot string, _orderedTopicIDs []string) error { return nil }

// SaveSessionGroups 淇濆瓨浼氳瘽鍒嗙粍銆?
func (a *App) SaveSessionGroups(_scope, _workspaceRoot string, _groups []map[string]any) error { return nil }

// SaveSessionGroupsVersioned 淇濆瓨浼氳瘽鍒嗙粍锛堢増鏈寲锛夈€?
func (a *App) SaveSessionGroupsVersioned(_scope, _workspaceRoot string, _expectedRevision uint64, _groups []map[string]any) map[string]any {
	return map[string]any{"groups": []any{}, "revision": 0, "applied": true}
}

// SetPreferredExternalOpener 璁剧疆棣栭€夊閮ㄦ墦寮€鍣ㄣ€?
func (a *App) SetPreferredExternalOpener(_id string) error { return nil }
func (a *App) ImportThemePack() error { return nil }
func (a *App) InstallMCPServer(_a1 any) error { return nil }
func (a *App) InvokeExtensionAction() error { return nil }
func (a *App) IsolatedWorktreeAvailability() error { return nil }
func (a *App) ListRemoteDir() []any { return []any{} }
func (a *App) ListTaskEventPage(_req map[string]any) error { return nil }
func (a *App) ListTaskEventsForTab() error { return nil }
func (a *App) ListTaskPage() error { return nil }
func (a *App) ListTasksForTab() error { return nil }
func (a *App) OpenChannelSessionPageForTab(_a1 any, _a2 any, _a3 any) error { return nil }
func (a *App) OpenRemoteWorkspace() error { return nil }
func (a *App) OpenTaskSessionByKey(_req map[string]any) error { return nil }
func (a *App) OpenTaskSessionForTab() error { return nil }
func (a *App) PickBlankProjectParent() error { return nil }
func (a *App) PickSkillFolder() error { return nil }
func (a *App) PickThemeBackground() error { return nil }
func (a *App) PollBotConnectionInstall(_a1 any) error { return nil }
func (a *App) PreviewRewindForTab() error { return nil }
func (a *App) PreviewSession() map[string]any { return nil }
func (a *App) PreviewWorkspaceFileRevertForTab(_tabID string, _path string) error { return nil }
func (a *App) PurgeRecoveryCopy(_a1 any) error { return nil }
func (a *App) PurgeTrashedSession(_a1 any) error { return nil }
func (a *App) ReadInboxItem() error { return nil }
func (a *App) ReadRemoteFile() error { return nil }
func (a *App) ReconnectMCPServer(_a1 any) error { return nil }
func (a *App) Remember(_scope string, _note string) error { return nil }
func (a *App) RememberForTab(_tabID string, _scope string, _note string) error { return nil }
func (a *App) RemoteLastWorkspace() map[string]any { return nil }
func (a *App) RemoteServerLogs() map[string]any { return nil }
func (a *App) RemoteServerStatus() map[string]any { return nil }
func (a *App) RemovePermissionRule(_a1 any, _a2 any) error { return nil }
func (a *App) RemoveProviderAccesses(_a1 any) error { return nil }
func (a *App) RemoveRemoteForward() error { return nil }
func (a *App) RemoveRemoteHost() error { return nil }
func (a *App) RequeueTaskByKey(_req map[string]any) error { return nil }
func (a *App) RequeueTaskForTab() error { return nil }
func (a *App) ResetProviderPresetAccess(_a1 any) error { return nil }
func (a *App) ResetThemePack() error { return nil }
func (a *App) ResolvePlanDecisionTab(_a1 any, _a2 any, _a3 any) error { return nil }
func (a *App) ResolveRecoveryTab(_a1 any, _a2 any, _a3 any, _a4 any) error { return nil }
func (a *App) RestoreArchivedMemory() error { return nil }
func (a *App) RestoreArchivedMemoryForTab(_a1 any, _a2 any) error { return nil }
func (a *App) RestoreMemoryRevision() error { return nil }
func (a *App) RestoreMemoryRevisionForTab(_a1 any, _a2 any, _a3 any) error { return nil }
func (a *App) RestoreSession(_a1 any) error { return nil }
func (a *App) RetryInboxItem() error { return nil }
func (a *App) RevealBackgroundRuntime() error { return nil }
func (a *App) RunShellForTab(_a1 any, _a2 any) error { return nil }
func (a *App) SaveHooksSettingsForRoot(_a1 any, _a2 any, _a3 any) error { return nil }
func (a *App) SaveLocalPathAs() error { return nil }
func (a *App) SaveProvider(_a1 any) error { return nil }
func (a *App) SaveProviderModelCatalogs(_a1 any) error { return nil }
func (a *App) SaveThemePack(_a1 any) error { return nil }
func (a *App) ScanPromptHistory() []any { return []any{} }
func (a *App) ScanRemoteLegacyWorkbenchData() error { return nil }
func (a *App) ScanSSHConfig() []any { return []any{} }
func (a *App) SetBotConnectionToolApprovalMode(_a1 any, _a2 any) error { return nil }
func (a *App) SetBotSecret(_a1 any, _a2 any) error { return nil }
func (a *App) SetBotSettings(_a1 any) error { return nil }
func (a *App) SetComposerProfileForTab(_a1 any, _a2 any, _a3 any, _a4 any) error { return nil }
func (a *App) SetInboxPaused(_a1 any, _a2 any) error { return nil }
func (a *App) SetMCPServerEnabled(_a1 any, _a2 any) error { return nil }
func (a *App) SetNetwork(_a1 any) error { return nil }
func (a *App) SetPermissionMode(_a1 any) error { return nil }
func (a *App) SetProviderWebSearch(_a1 any, _a2 any) error { return nil }
func (a *App) SetReasoningLanguage(_a1 any) error { return nil }
func (a *App) SetSandbox(_a1 any, _a2 any, _a3 any, _a4 any, _a5 any) error { return nil }
func (a *App) SetSkillEnabled(_a1 any, _a2 any) error { return nil }
func (a *App) SetSkillImplicitInvocation(_a1 any) error { return nil }
func (a *App) SetSkillPathEnabled(_a1 any, _a2 any) error { return nil }
func (a *App) SetSubagentProfileEffort(_a1 any, _a2 any) error { return nil }
func (a *App) SetSubagentProfileModel(_a1 any, _a2 any) error { return nil }
func (a *App) StartBotConnectionInstall(_a1 any, _a2 any) error { return nil }
func (a *App) StartTopicActivation(req map[string]any) map[string]any {
	return a.StartTopicActivationImpl(req)
}
func (a *App) SteerInboxItem() error { return nil }
func (a *App) StopRemoteServer() error { return nil }
func (a *App) StopTaskByKey(_req map[string]any) error { return nil }
func (a *App) StopTaskForTab() error { return nil }
func (a *App) SubmitDeliveryRecoveryToTabWithID(_a1 any, _a2 any, _a3 any, _a4 any) error { return nil }
func (a *App) SubmitDisplayToTabWithID(_a1 any, _a2 any, _a3 any, _a4 any) error { return nil }
func (a *App) SubmitEditedDisplayToTabWithID(_a1 any, _a2 any, _a3 any, _a4 any, _a5 any) error { return nil }
func (a *App) SubmitExtensionForm() error { return nil }
func (a *App) SubmitInitialGoalToTabWithID(_a1 any, _a2 any, _a3 any, _a4 any, _a5 any, _a6 any, _a7 any, _a8 any) error { return nil }
func (a *App) SubmitInvocationsToTabWithID(_a1 any, _a2 any, _a3 any, _a4 any, _a5 any) error { return nil }
func (a *App) SubmitToTabWithID(_a1 any, _a2 any, _a3 any) error { return nil }
func (a *App) SummarizeFromForTab() error { return nil }
func (a *App) SummarizeUpToForTab() error { return nil }
func (a *App) TerminalOutputForTab() error { return nil }
func (a *App) TestBotConnection(_a1 any, _a2 any) error { return nil }
func (a *App) ToolResultForTab() error { return nil }
func (a *App) TrySubagentProfile(_a1 any, _a2 any) error { return nil }
func (a *App) UndoRewindForTab() error { return nil }
func (a *App) UpdateInboxItem() error { return nil }
func (a *App) UpdateRemoteHost() error { return nil }
func (a *App) UpdateSubagentProfile(_a1 any, _a2 any, _a3 any) error { return nil }
func (a *App) UpgradeDeepSeekProviderAccess(_a1 any) error { return nil }
func (a *App) UsageStats() error { return nil }
func (a *App) WriteRemoteFile() error { return nil }
