package main

import (
	"reflect"
	"testing"
)

// TestDesktopPreferencesContractKeys 是「深色模式按别的按钮就跳」的回归守卫。
//
// 背景（2026-09-17 修）：前端有**两套不同键名**的主题契约：
//   - themeExperience 契约：themeMode / baseStyle（lib/themeExperience.ts 读）
//   - 设置快照契约：desktopTheme / desktopThemeStyle / conversationWidth /
//     desktopLanguage / sessionExperience（SettingsPanel.tsx L202-210、
//     app-runtime/desktopPreferencesAdapter.ts L19-26 读）
//
// Settings() 曾只返回第一套键名，于是前端 normalizeThemePreference(undefined) 得到
// DEFAULT_THEME("auto")+默认风格，且 SettingsPanel 的外观 effect 每次设置重读都重放一次
// → 用户选好的深色主题/风格/会话宽度被重置（表现为"按别的按钮就跳"）。
//
// 本测试锁住的不变量：
//  1. 两个设置载荷都给出契约键名；
//  2. 两处取值必须一致（不一致 = 启动正确、之后跳变）；
//  3. 用户设置能往返（设进去 → 读出来）；
//  4. 旧键名仍保留（兼容历史读取点）。
func TestDesktopPreferencesContractKeys(t *testing.T) {
	// 隔离：settingsPath() 基于 os.UserHomeDir()，不改用户真实设置。
	t.Setenv("USERPROFILE", t.TempDir())
	a := newTestApp()

	if err := a.SetDesktopAppearance("dark", "nocturne"); err != nil {
		t.Fatalf("SetDesktopAppearance: %v", err)
	}
	if err := a.SetSessionExperience("deep"); err != nil {
		t.Fatalf("SetSessionExperience: %v", err)
	}
	if err := a.SetDesktopLayoutStyle("classic"); err != nil {
		t.Fatalf("SetDesktopLayoutStyle: %v", err)
	}

	settings := a.Settings()
	startup := a.DesktopStartupSettings()

	contractKeys := []string{
		"desktopTheme", "desktopThemeStyle", "desktopLanguage", "desktopLayoutStyle",
		"desktopTerminalTheme", "conversationWidth", "sessionExperience",
		"reasoningDisplayMode", "statusBarStyle", "statusBarItems",
		"checkUpdates", "updateChannel", "telemetry", "metrics", "displayMode",
	}
	for _, k := range contractKeys {
		sv, sok := settings[k]
		uv, uok := startup[k]
		if !sok {
			t.Errorf("Settings() 缺契约键 %q —— 前端会读到 undefined 并回落到默认值", k)
		}
		if !uok {
			t.Errorf("DesktopStartupSettings() 缺契约键 %q", k)
		}
		// 用 DeepEqual：statusBarItems 等键是 []string，不可用 != 比较。
		if sok && uok && !reflect.DeepEqual(sv, uv) {
			t.Errorf("键 %q 两处载荷不一致: Settings=%v DesktopStartupSettings=%v —— 会导致设置重读时外观被重置（主题跳变）", k, sv, uv)
		}
	}

	// 用户设置必须真的带出来 —— 这些正是 normalizeThemePreference 读的值。
	if got := settings["desktopTheme"]; got != "dark" {
		t.Errorf("desktopTheme = %v, want dark（否则按按钮会跳回 auto，而 DEFAULT_THEME=auto）", got)
	}
	if got := settings["desktopThemeStyle"]; got != "nocturne" {
		t.Errorf("desktopThemeStyle = %v, want nocturne", got)
	}
	if got := settings["desktopLayoutStyle"]; got != "classic" {
		t.Errorf("desktopLayoutStyle = %v, want classic", got)
	}
	if got := settings["sessionExperience"]; got != "deep" {
		t.Errorf("sessionExperience = %v, want deep（deep 模式靠它 hydrate，丢失会重置推理显示）", got)
	}

	// 旧键名保留：兼容 v1.29–1.31 的读取点，不能因为修契约键就删掉。
	if got := settings["themeMode"]; got != "dark" {
		t.Errorf("themeMode(旧键) = %v, want dark", got)
	}
	if got := settings["baseStyle"]; got != "nocturne" {
		t.Errorf("baseStyle(旧键) = %v, want nocturne", got)
	}
	if got := settings["desktopConversationWidth"]; got == nil {
		t.Errorf("desktopConversationWidth(旧键) 缺失")
	}

	// GetThemeExperience 走的是另一套键名（themeExperience 契约），不能被这次改动影响。
	exp := a.GetThemeExperience()
	if exp["themeMode"] != "dark" || exp["baseStyle"] != "nocturne" {
		t.Errorf("GetThemeExperience 键名/取值回归: %v", exp)
	}
}

// TestSessionExperiencePersists 覆盖 v1.38.2 新增设置项的持久化与取值约束。
func TestSessionExperiencePersists(t *testing.T) {
	t.Setenv("USERPROFILE", t.TempDir())
	a := newTestApp()

	if got := a.Settings()["sessionExperience"]; got != "standard" {
		t.Errorf("空值时 sessionExperience = %v, want standard（与前端 hydrate 默认一致）", got)
	}
	if err := a.SetSessionExperience("deep"); err != nil {
		t.Fatalf("SetSessionExperience: %v", err)
	}
	if got := a.Settings()["sessionExperience"]; got != "deep" {
		t.Errorf("设置后 = %v, want deep", got)
	}
	// 非法值不应污染设置（前端只发 standard/deep）。
	if err := a.SetSessionExperience("bogus"); err != nil {
		t.Fatalf("SetSessionExperience(bogus): %v", err)
	}
	if got := a.Settings()["sessionExperience"]; got != "deep" {
		t.Errorf("非法值应被忽略，实际 = %v", got)
	}
}
