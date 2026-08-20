package main

// 防崩溃结构验证：这些方法曾因返回空导致前端崩溃（Electron 版踩过的坑）。
// 测试断言它们返回完整结构（关键字段非空），防止回退成空占位。

import "testing"

func newTestApp() *App {
	a := &App{}
	a.st = NewSettings()
	return a
}

// GetThemeExperience 必须返回 themeMode（否则 normalizeExperience 兜底 auto → 锁浅色）。
func TestGetThemeExperienceNotEmpty(t *testing.T) {
	a := newTestApp()
	v := a.GetThemeExperience()
	if v["themeMode"] == nil || v["themeMode"] == "" {
		t.Fatalf("GetThemeExperience themeMode 为空: %v", v)
	}
	if v["baseStyle"] == nil || v["baseStyle"] == "" {
		t.Fatalf("GetThemeExperience baseStyle 为空: %v", v)
	}
}

// CapabilityDiagnostics 必须返回 summary（否则诊断页读 summary.errors 崩溃）。
func TestCapabilityDiagnosticsHasSummary(t *testing.T) {
	a := newTestApp()
	v := a.CapabilityDiagnostics(true)
	if v["summary"] == nil {
		t.Fatalf("CapabilityDiagnostics summary 为空: %v", v)
	}
	summary, ok := v["summary"].(map[string]any)
	if !ok {
		t.Fatalf("summary 类型错误")
	}
	if _, ok := summary["errors"]; !ok {
		t.Fatalf("summary.errors 缺失")
	}
}

// Settings 必须返回 bot（否则 sidebarImQQAdded 读 bot.qq.enabled 崩溃）。
func TestSettingsHasBot(t *testing.T) {
	a := newTestApp()
	v := a.Settings()
	bot, ok := v["bot"].(map[string]any)
	if !ok {
		t.Fatalf("Settings.bot 缺失或类型错误: %v", v["bot"])
	}
	if bot["qq"] == nil {
		t.Fatalf("bot.qq 缺失")
	}
	if bot["allowlist"] == nil {
		t.Fatalf("bot.allowlist 缺失")
	}
}

// StorageSettings 必须返回 4 个字段（否则设置面板存储页读 undefined）。
func TestStorageSettingsFields(t *testing.T) {
	a := newTestApp()
	v := a.StorageSettings()
	for _, k := range []string{"defaultWorkspace", "statePath", "cachePath", "extensionsPath"} {
		if v[k] == nil || v[k] == "" {
			t.Fatalf("StorageSettings.%s 为空: %v", k, v)
		}
	}
}

// SkillsSettings 必须返回 skills/skillRoots（否则技能页读 undefined）。
func TestSkillsSettingsFields(t *testing.T) {
	a := newTestApp()
	v := a.SkillsSettings()
	if v["skills"] == nil {
		t.Fatalf("SkillsSettings.skills 缺失")
	}
	if v["skillRoots"] == nil {
		t.Fatalf("SkillsSettings.skillRoots 缺失")
	}
}

// RuntimeDoctor 必须返回 text/allowResume。
func TestRuntimeDoctorFields(t *testing.T) {
	a := newTestApp()
	v := a.RuntimeDoctor()
	if v["text"] == nil || v["text"] == "" {
		t.Fatalf("RuntimeDoctor.text 为空")
	}
}

// DesktopStartupSettings 必须返回完整启动设置（主题/布局/bot）。
func TestDesktopStartupSettings(t *testing.T) {
	a := newTestApp()
	v := a.DesktopStartupSettings()
	for _, k := range []string{"desktopTheme", "desktopLayoutStyle", "bot", "statusBarItems"} {
		if v[k] == nil {
			t.Fatalf("DesktopStartupSettings.%s 缺失", k)
		}
	}
}
