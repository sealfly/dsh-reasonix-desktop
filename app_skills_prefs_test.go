package main

// app_skills_prefs_test.go — 技能开关偏好持久化桥测试
// （停用/启用技能、允许自动调用开关、skillsView 叠加生效）。

import (
	"path/filepath"
	"testing"
)

// useTempSkillPrefsDir 把技能偏好管理器指向临时目录，测试后还原。
func useTempSkillPrefsDir(t *testing.T) string {
	t.Helper()
	_ = getSkillPrefsManager()
	old := appSkillPrefsMgr
	dir := t.TempDir()
	appSkillPrefsMgr = &skillPrefsManager{path: filepath.Join(dir, "skill-preferences.json")}
	t.Cleanup(func() { appSkillPrefsMgr = old })
	return dir
}

func TestSetSkillEnabledDisableEnable(t *testing.T) {
	useTempSkillPrefsDir(t)
	a := &App{}
	mgr := getSkillPrefsManager()
	if mgr.isSkillDisabled("fmt") {
		t.Fatal("初始不应禁用")
	}
	if err := a.SetSkillEnabled("fmt", false); err != nil {
		t.Fatalf("禁用失败: %v", err)
	}
	if !mgr.isSkillDisabled("fmt") {
		t.Fatal("禁用后应生效")
	}
	if mgr.isSkillDisabled("other") {
		t.Fatal("不应影响其他技能")
	}
	if err := a.SetSkillEnabled("fmt", true); err != nil {
		t.Fatalf("启用失败: %v", err)
	}
	if mgr.isSkillDisabled("fmt") {
		t.Fatal("重新启用后不应再禁用")
	}
}

func TestSetSkillImplicitInvocation(t *testing.T) {
	useTempSkillPrefsDir(t)
	a := &App{}
	if !mgrSkillPrefsImplicit() {
		t.Fatal("默认应允许自动调用")
	}
	if err := a.SetSkillImplicitInvocation(false); err != nil {
		t.Fatalf("关闭失败: %v", err)
	}
	if mgrSkillPrefsImplicit() {
		t.Fatal("关闭后应生效")
	}
	if err := a.SetSkillImplicitInvocation(true); err != nil {
		t.Fatalf("重开失败: %v", err)
	}
	if !mgrSkillPrefsImplicit() {
		t.Fatal("重开后应允许")
	}
}

func mgrSkillPrefsImplicit() bool {
	return getSkillPrefsManager().load().ImplicitInvocation
}

func TestSkillsViewRespectsPrefs(t *testing.T) {
	useTempSkillPrefsDir(t)
	a := &App{}
	// dsh == nil：无 DSH skills；直接验证 allowImplicitInvocation 叠加
	if err := a.SetSkillImplicitInvocation(false); err != nil {
		t.Fatalf("关闭失败: %v", err)
	}
	view := a.SkillsSettings()
	if view["allowImplicitInvocation"] != false {
		t.Fatalf("SkillsSettings 应返回 false: %v", view["allowImplicitInvocation"])
	}
	if err := a.SetSkillImplicitInvocation(true); err != nil {
		t.Fatalf("重开失败: %v", err)
	}
	if view2 := a.SkillsSettings(); view2["allowImplicitInvocation"] != true {
		t.Fatalf("重开后应为 true: %v", view2["allowImplicitInvocation"])
	}
}

func TestCapabilitiesRespectsImplicitInvocation(t *testing.T) {
	useTempSkillPrefsDir(t)
	a := &App{}
	if err := a.SetSkillImplicitInvocation(false); err != nil {
		t.Fatalf("关闭失败: %v", err)
	}
	caps := a.Capabilities()
	if caps["allowImplicitInvocation"] != false {
		t.Fatalf("Capabilities 应返回 false: %v", caps["allowImplicitInvocation"])
	}
}

func TestSkillPrefsPersistence(t *testing.T) {
	useTempSkillPrefsDir(t)
	a := &App{}
	if err := a.SetSkillEnabled("fmt", false); err != nil {
		t.Fatalf("禁用失败: %v", err)
	}
	if err := a.SetSkillImplicitInvocation(false); err != nil {
		t.Fatalf("关闭失败: %v", err)
	}
	// 模拟重启：偏好仍生效（磁盘持久化）
	if !getSkillPrefsManager().isSkillDisabled("fmt") {
		t.Fatal("重启后禁用应保留")
	}
	if getSkillPrefsManager().load().ImplicitInvocation {
		t.Fatal("重启后自动调用开关应保留")
	}
}
