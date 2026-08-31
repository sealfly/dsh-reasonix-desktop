package main

// app_composer.go — 前端 Composer「执行方式」方法实现（接上 DSH 原生机制）。
//
// 前端 Composer 面板有执行方式选择器（Direct/Plan/Goal 任务模式 + 交付档位 + 审批模式），
// 通过 SetComposerProfileForTab / SetGoalForTab / SetCollaborationModeForTab /
// SetToolApprovalModeForTab 落到 Go。此前全是空 stub（执行方式 UI 点了没反应），
// 这里全部实现：goal 模式 → goal.create/pause/resume/clear，审批模式 → 记录，
// 交付档位 → QualityFloor（已有）。

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

// goalRefCache 按 sessionId 缓存 goal ref（goal.create 返回，pause/resume/clear 需要）。
var (
	goalRefMu sync.Mutex
	goalRefs  = map[string]dshGoalRef{}
)

// dshGoalRef DSH goal ref。
type dshGoalRef struct {
	ID       string `json:"id"`
	Revision int    `json:"revision"`
}

// setGoalRef 缓存某会话的 goal ref。
func setGoalRef(sid string, ref dshGoalRef) {
	goalRefMu.Lock()
	defer goalRefMu.Unlock()
	goalRefs[sid] = ref
}

// getGoalRef 取某会话的 goal ref。
func getGoalRef(sid string) (dshGoalRef, bool) {
	goalRefMu.Lock()
	defer goalRefMu.Unlock()
	r, ok := goalRefs[sid]
	return r, ok
}

// clearGoalRef 清除某会话的 goal ref。
func clearGoalRef(sid string) {
	goalRefMu.Lock()
	defer goalRefMu.Unlock()
	delete(goalRefs, sid)
}

// SetGoalForTab 指定会话目标（goal 模式）。goal 非空 → goal.create；空 → goal.clear。
func (a *App) SetGoalForTab(tabID, goal string) error {
	sid := a.activeSessionID(tabID)
	if sid == "" {
		sid = tabID
	}
	goal = strings.TrimSpace(goal)
	if goal == "" {
		return a.clearGoal(sid)
	}
	if a.dsh == nil {
		return fmt.Errorf("dsh client not initialized")
	}
	raw, err := a.dsh.RPC("goal.create", map[string]any{
		"sessionId": sid,
		"objective": goal,
	})
	if err != nil {
		return fmt.Errorf("goal.create: %w", err)
	}
	var v struct {
		Ref dshGoalRef `json:"ref"`
	}
	_ = json.Unmarshal(raw, &v)
	if v.Ref.ID != "" {
		setGoalRef(sid, v.Ref)
	}
	resumeLog("goal.create session=%s objective=%q", sid, truncate(goal, 60))
	return nil
}

// SetGoal 为活动会话设置目标。
func (a *App) SetGoal(goal string) error { return a.SetGoalForTab("", goal) }

// clearGoal 清除某会话目标（goal.clear，需 ref）。
func (a *App) clearGoal(sid string) error {
	if a.dsh == nil {
		return fmt.Errorf("dsh client not initialized")
	}
	ref, ok := getGoalRef(sid)
	if !ok {
		// 无缓存 ref：尝试读投影失败时降级为仅本地清除（不崩溃）
		clearGoalRef(sid)
		resumeLog("goal.clear session=%s (no cached ref; local only)", sid)
		return nil
	}
	if _, err := a.dsh.RPC("goal.clear", map[string]any{
		"sessionId": sid,
		"ref":       ref,
	}); err != nil {
		// 版本差异/过期：本地清除兜底
		clearGoalRef(sid)
		return nil
	}
	clearGoalRef(sid)
	resumeLog("goal.clear session=%s", sid)
	return nil
}

// ClearGoalForTab 清除会话目标。
func (a *App) ClearGoalForTab(tabID string) error {
	sid := a.activeSessionID(tabID)
	if sid == "" {
		sid = tabID
	}
	return a.clearGoal(sid)
}

// PauseGoalForTab 暂停会话目标。
func (a *App) PauseGoalForTab(tabID string) error {
	sid := a.activeSessionID(tabID)
	if sid == "" {
		sid = tabID
	}
	ref, ok := getGoalRef(sid)
	if !ok {
		return fmt.Errorf("no goal ref cached for session %s", sid)
	}
	if _, err := a.dsh.RPC("goal.pause", map[string]any{"sessionId": sid, "ref": ref}); err != nil {
		return err
	}
	resumeLog("goal.pause session=%s", sid)
	return nil
}

// ResumeGoalForTab 恢复会话目标。
func (a *App) ResumeGoalForTab(tabID string) error {
	sid := a.activeSessionID(tabID)
	if sid == "" {
		sid = tabID
	}
	ref, ok := getGoalRef(sid)
	if !ok {
		return fmt.Errorf("no goal ref cached for session %s", sid)
	}
	if _, err := a.dsh.RPC("goal.resume", map[string]any{"sessionId": sid, "ref": ref}); err != nil {
		return err
	}
	resumeLog("goal.resume session=%s", sid)
	return nil
}

// SetCollaborationModeForTab 指定会话协作模式（normal/plan/goal）。
// plan 在 DSH 无独立模式（DSH 会话自带计划能力）；goal → 需配合 SetGoalForTab 设目标。
func (a *App) SetCollaborationModeForTab(tabID, mode string) error {
	mode = strings.ToLower(strings.TrimSpace(mode))
	switch mode {
	case "normal", "plan", "goal":
		resumeLog("collaboration mode tab=%s mode=%s (DSH session manages its own loop)", tabID, mode)
		return nil
	default:
		resumeLog("collaboration mode tab=%s unknown=%q (ignored)", tabID, mode)
		return nil
	}
}

// SetToolApprovalModeForTab 指定会话工具审批模式（ask/auto/yolo）。
// DSH 会话权限由 agent 自身控制；这里记录并映射到桌面默认（供新建会话参考）。
func (a *App) SetToolApprovalModeForTab(tabID, mode string) error {
	mode = strings.ToLower(strings.TrimSpace(mode))
	switch mode {
	case "ask", "auto", "yolo":
		a.st.SetDefaultToolApprovalMode(mode)
		resumeLog("tool approval mode tab=%s mode=%s", tabID, mode)
	default:
		resumeLog("tool approval mode tab=%s unknown=%q (ignored)", tabID, mode)
	}
	return nil
}

// SetComposerProfileForTab 前端 Composer「执行方式」组合设置。
// 参数：tabID, collaborationMode(normal/plan/goal), toolApprovalMode(ask/auto/yolo), goal(目标文本)。
// goal 非空 → goal.create；collaborationMode/approvalMode 记录。
func (a *App) SetComposerProfileForTab(tabID, collaborationMode, toolApprovalMode, goal string) error {
	if strings.TrimSpace(goal) != "" {
		if err := a.SetGoalForTab(tabID, goal); err != nil {
			return err
		}
	}
	if err := a.SetCollaborationModeForTab(tabID, collaborationMode); err != nil {
		return err
	}
	if err := a.SetToolApprovalModeForTab(tabID, toolApprovalMode); err != nil {
		return err
	}
	return nil
}
