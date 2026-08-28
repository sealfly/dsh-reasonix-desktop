package main

// app_skills_prefs.go — 技能开关偏好的本地持久化桥（DSH 语义）。
//
// 探测确认 DSH 无 skill 管理 RPC（skill.enable/disable/update 等全 404），
// 只有只读的 skill.list。按项目原则 1（展示与持久化适配）：
// "停用技能"（SetSkillEnabled）与"允许 Agent 自动调用技能"
// （SetSkillImplicitInvocation）是 Reasonix 前端概念，偏好持久化到
// ~/.reasonix/skill-preferences.json，skillsView 返回时叠加到 DSH 数据上。
// DSH 侧不消费（技能调用策略由 DSH 自身决定），配置留待 DSH 支持时对接。

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// skillPreferences 持久化的技能偏好。
type skillPreferences struct {
	// ImplicitInvocation 允许 Agent 自动调用技能（前端"允许自动调用"开关）。
	ImplicitInvocation bool `json:"implicitInvocation"`
	// DisabledSkills 用户停用的技能名集合（前端技能行 enabled 开关）。
	DisabledSkills []string `json:"disabledSkills"`
}

// skillPrefsManager 技能偏好管理器（单例）。
type skillPrefsManager struct {
	mu   sync.Mutex
	path string
}

var (
	appSkillPrefsMgr  *skillPrefsManager
	appSkillPrefsOnce sync.Once
)

func getSkillPrefsManager() *skillPrefsManager {
	appSkillPrefsOnce.Do(func() {
		home, _ := os.UserHomeDir()
		if home == "" {
			home = "."
		}
		dir := filepath.Join(home, ".reasonix")
		_ = os.MkdirAll(dir, 0o755)
		appSkillPrefsMgr = &skillPrefsManager{path: filepath.Join(dir, "skill-preferences.json")}
	})
	return appSkillPrefsMgr
}

func (m *skillPrefsManager) load() skillPreferences {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := skillPreferences{ImplicitInvocation: true} // 默认允许自动调用
	data, err := os.ReadFile(m.path)
	if err != nil {
		return p
	}
	if json.Unmarshal(data, &p) != nil {
		return skillPreferences{ImplicitInvocation: true}
	}
	return p
}

func (m *skillPrefsManager) save(p skillPreferences) {
	m.mu.Lock()
	defer m.mu.Unlock()
	data, _ := json.MarshalIndent(p, "", "  ")
	_ = os.WriteFile(m.path, data, 0o644)
}

// isSkillDisabled 技能名是否在停用集合。
func (m *skillPrefsManager) isSkillDisabled(name string) bool {
	p := m.load()
	for _, n := range p.DisabledSkills {
		if n == name {
			return true
		}
	}
	return false
}

// SetSkillEnabled 停用/启用技能（DSH 语义：本地偏好持久化）。
// 注意：DSH 侧技能调用策略由 DSH 决定，此开关目前仅影响前端展示
// （诚实降级：持久化成功即生效于列表显示）。
func (a *App) SetSkillEnabled(name string, enabled bool) error {
	mgr := getSkillPrefsManager()
	p := mgr.load()
	if enabled {
		out := p.DisabledSkills[:0]
		for _, n := range p.DisabledSkills {
			if n != name {
				out = append(out, n)
			}
		}
		p.DisabledSkills = out
	} else {
		for _, n := range p.DisabledSkills {
			if n == name {
				mgr.save(p)
				return nil
			}
		}
		p.DisabledSkills = append(p.DisabledSkills, name)
	}
	mgr.save(p)
	return nil
}

// SetSkillImplicitInvocation 允许/禁止 Agent 自动调用技能（本地偏好持久化）。
func (a *App) SetSkillImplicitInvocation(enabled bool) error {
	mgr := getSkillPrefsManager()
	p := mgr.load()
	p.ImplicitInvocation = enabled
	mgr.save(p)
	return nil
}
