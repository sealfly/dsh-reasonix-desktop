package main

// app_subagents_test.go — 子智能体 profile 本地持久化桥测试（add/list/update/delete/字段设置 + 前端合并视图）。

import (
	"path/filepath"
	"testing"
)

// useTempSubagentDir 把子智能体管理器指向临时目录，测试后还原。
func useTempSubagentDir(t *testing.T) string {
	t.Helper()
	_ = getSubagentManager() // 确保单例 once 已执行，之后覆盖指针即可生效
	old := appSubagentMgr
	dir := t.TempDir()
	appSubagentMgr = &subagentManager{path: filepath.Join(dir, "subagent-profiles.json")}
	t.Cleanup(func() { appSubagentMgr = old })
	return dir
}

func TestCreateSubagentProfile(t *testing.T) {
	useTempSubagentDir(t)
	a := &App{}
	path, err := a.CreateSubagentProfile(map[string]any{
		"name": "test-agent", "description": "测试 agent", "systemPrompt": "你是测试 agent",
		"model": "deepseek-v4-flash", "effort": "high",
	})
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	if path == "" {
		t.Fatal("应返回非空标识")
	}
	skills := subagentProfilesAsSkills()
	if len(skills) != 1 {
		t.Fatalf("期望 1 个 profile，got %d", len(skills))
	}
	m := skills[0].(map[string]any)
	if m["name"] != "test-agent" || m["runAs"] != "subagent" {
		t.Fatalf("字段不符: %v", m)
	}
	if m["configuredModel"] != "deepseek-v4-flash" || m["configuredEffort"] != "high" {
		t.Fatalf("模型/努力未带上: %v", m)
	}
}

func TestCreateSubagentProfileValidation(t *testing.T) {
	useTempSubagentDir(t)
	a := &App{}
	if _, err := a.CreateSubagentProfile(map[string]any{"description": "x", "systemPrompt": "y"}); err == nil {
		t.Fatal("缺 name 应报错")
	}
	if _, err := a.CreateSubagentProfile(map[string]any{"name": "x", "systemPrompt": "y"}); err == nil {
		t.Fatal("缺 description 应报错")
	}
	if _, err := a.CreateSubagentProfile(map[string]any{"name": "x", "description": "y"}); err == nil {
		t.Fatal("缺 systemPrompt 应报错")
	}
	if _, err := a.CreateSubagentProfile(map[string]any{"name": "x", "description": "y", "systemPrompt": "z", "scope": "bogus"}); err == nil {
		t.Fatal("非法 scope 应报错")
	}
	if len(subagentProfilesAsSkills()) != 0 {
		t.Fatal("失败的创建不应留下条目")
	}
}

func TestCreateSubagentProfileDuplicate(t *testing.T) {
	useTempSubagentDir(t)
	a := &App{}
	in := map[string]any{"name": "dup", "description": "d", "systemPrompt": "p"}
	if _, err := a.CreateSubagentProfile(in); err != nil {
		t.Fatalf("首次创建失败: %v", err)
	}
	if _, err := a.CreateSubagentProfile(in); err == nil {
		t.Fatal("重复创建应报错")
	}
}

func TestUpdateSubagentProfile(t *testing.T) {
	useTempSubagentDir(t)
	a := &App{}
	in := map[string]any{"name": "u", "description": "d", "systemPrompt": "p"}
	if _, err := a.CreateSubagentProfile(in); err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	if err := a.UpdateSubagentProfile("u", "global", map[string]any{
		"description": "新描述", "systemPrompt": "新提示", "color": "#f00",
	}); err != nil {
		t.Fatalf("更新失败: %v", err)
	}
	skills := subagentProfilesAsSkills()
	m := skills[0].(map[string]any)
	if m["description"] != "新描述" || m["color"] != "#f00" {
		t.Fatalf("更新未生效: %v", m)
	}
	if err := a.UpdateSubagentProfile("missing", "global", in); err == nil {
		t.Fatal("不存在的 profile 更新应报错")
	}
}

func TestSetSubagentProfileModelEffort(t *testing.T) {
	useTempSubagentDir(t)
	a := &App{}
	in := map[string]any{"name": "m", "description": "d", "systemPrompt": "p"}
	if _, err := a.CreateSubagentProfile(in); err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	if err := a.SetSubagentProfileModel("m", "gpt-4o"); err != nil {
		t.Fatalf("设模型失败: %v", err)
	}
	if err := a.SetSubagentProfileEffort("m", "medium"); err != nil {
		t.Fatalf("设努力失败: %v", err)
	}
	skills := subagentProfilesAsSkills()
	m := skills[0].(map[string]any)
	if m["configuredModel"] != "gpt-4o" || m["configuredEffort"] != "medium" {
		t.Fatalf("字段未生效: %v", m)
	}
	// 清空
	if err := a.SetSubagentProfileModel("m", ""); err != nil {
		t.Fatalf("清模型失败: %v", err)
	}
	skills = subagentProfilesAsSkills()
	if _, has := skills[0].(map[string]any)["configuredModel"]; has {
		t.Fatal("清空后不应再有 configuredModel")
	}
	if err := a.SetSubagentProfileModel("missing", "x"); err == nil {
		t.Fatal("不存在 profile 应报错")
	}
}

func TestDeleteSubagentProfile(t *testing.T) {
	useTempSubagentDir(t)
	a := &App{}
	in := map[string]any{"name": "del", "description": "d", "systemPrompt": "p"}
	if _, err := a.CreateSubagentProfile(in); err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	if err := a.DeleteSubagentProfile("del", "global"); err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	if len(subagentProfilesAsSkills()) != 0 {
		t.Fatal("删除后应为空")
	}
	if err := a.DeleteSubagentProfile("del", "global"); err == nil {
		t.Fatal("重复删除应报错")
	}
}

func TestSubagentProfilesPersistence(t *testing.T) {
	useTempSubagentDir(t)
	a := &App{}
	in := map[string]any{"name": "persist", "description": "d", "systemPrompt": "p"}
	if _, err := a.CreateSubagentProfile(in); err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	// 模拟重启：新 App 实例仍能读到（持久化在磁盘，不依赖实例状态）
	skills := subagentProfilesAsSkills()
	if len(skills) != 1 || skills[0].(map[string]any)["name"] != "persist" {
		t.Fatalf("重启后应读到 profile: %v", skills)
	}
}

// TestSkillsViewMergesSubagents 验证前端子智能体面板数据链路：
// SkillsSettings → skillsView 合并本地 subagent profiles（runAs="subagent"）。
func TestSkillsViewMergesSubagents(t *testing.T) {
	useTempSubagentDir(t)
	a := &App{} // dsh == nil：DSH skills 为空，只剩本地 subagent 条目
	if _, err := a.CreateSubagentProfile(map[string]any{
		"name": "panel-agent", "description": "d", "systemPrompt": "p",
	}); err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	view := a.SkillsSettings()
	skills, ok := view["skills"].([]any)
	if !ok {
		t.Fatalf("skills 类型不对: %T", view["skills"])
	}
	if len(skills) != 1 {
		t.Fatalf("期望合并 1 个条目，got %d", len(skills))
	}
	m := skills[0].(map[string]any)
	if m["runAs"] != "subagent" || m["name"] != "panel-agent" {
		t.Fatalf("合并条目字段不符（前端过滤 runAs==='subagent'）: %v", m)
	}
}

// TestSubagentEnabledToggle 验证子智能体（test-agent）也能被技能停用开关停用：
// SetSkillEnabled(name, false) → 能力面板/技能页的 enabled 开关对 subagent 条目生效。
func TestSubagentEnabledToggle(t *testing.T) {
	useTempSubagentDir(t)
	useTempSkillPrefsDir(t)
	a := &App{}
	if _, err := a.CreateSubagentProfile(map[string]any{
		"name": "test-agent", "description": "d", "systemPrompt": "p",
	}); err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	skills := subagentProfilesAsSkills()
	if skills[0].(map[string]any)["enabled"] != true {
		t.Fatal("初始应启用")
	}
	if err := a.SetSkillEnabled("test-agent", false); err != nil {
		t.Fatalf("停用失败: %v", err)
	}
	skills = subagentProfilesAsSkills()
	if skills[0].(map[string]any)["enabled"] != false {
		t.Fatal("停用后 enabled 应为 false")
	}
	if err := a.SetSkillEnabled("test-agent", true); err != nil {
		t.Fatalf("启用失败: %v", err)
	}
	skills = subagentProfilesAsSkills()
	if skills[0].(map[string]any)["enabled"] != true {
		t.Fatal("重新启用后 enabled 应为 true")
	}
}
