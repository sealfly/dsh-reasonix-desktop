package main

// Settings — 桌面偏好持久化（JSON 文件）。
// 对应 Electron 版 preload 里用 localStorage 存的那批键（dsh:theme、dsh:layout-style 等）。
// Wails 的 Go 端没有 localStorage，改用 JSON 文件；前端 WebView 的 localStorage 仍由
// 前端自己管理（theme.ts 读 reasonix-theme 等），Go 端只管「桌面级」偏好。

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// desktopSettings 是持久化的桌面偏好（对应 preload 的 startupSetting 键）。
type desktopSettings struct {
	LayoutStyle      string `json:"layoutStyle"`      // workbench/classic/creation
	Theme            string `json:"theme"`            // dark/light/auto
	ThemeStyle       string `json:"themeStyle"`       // graphite/nocturne/...
	Currency         string `json:"currency"`         // CNY/USD/""（空=跟随语言）
	ReasoningMode    string `json:"reasoningMode"`    // hidden/summary/auto/expanded
	Zoom             float64 `json:"zoom"`            // 窗口缩放 0.5–2.0
	CloseBehavior    string   `json:"closeBehavior"`    // quit/background
	Language         string   `json:"language"`         // zh/en
	StatusBarStyle   string   `json:"statusBarStyle"`   // icon/text
	StatusBarItems   []string `json:"statusBarItems"`   // workspace/model/context/usage/cache/cost...
	DefaultToolApprovalMode string `json:"defaultToolApprovalMode"` // ask/auto/yolo
	QualityFloor     string `json:"qualityFloor"`     // standard/delivery
	DefaultAgentPreset string `json:"defaultAgentPreset"` // standard/code/minimal/cordis (DSH 四模式)
	PermissionMode   string   `json:"permissionMode"`   // ask/allow/deny
	PermissionRules  map[string][]string `json:"permissionRules"`
	SandboxBash      string   `json:"sandboxBash"`
	SandboxNetwork   bool     `json:"sandboxNetwork"`
	SandboxWorkspace string   `json:"sandboxWorkspace"`
	SandboxWrites    []string `json:"sandboxWrites"`
	SandboxShell     string   `json:"sandboxShell"`
	TerminalTheme    string   `json:"terminalTheme"`
	ConversationWidth string  `json:"conversationWidth"`
	CheckUpdates     bool     `json:"checkUpdates"`
	DesktopMetrics   bool     `json:"desktopMetrics"`
	DesktopTelemetry bool     `json:"desktopTelemetry"`
	DefaultModel     string   `json:"defaultModel"`
	PlannerModel     string   `json:"plannerModel"`
	SubagentModel    string   `json:"subagentModel"`
	SubagentEffort   string   `json:"subagentEffort"`
	MaxSubagentDepth int      `json:"maxSubagentDepth"`
	MaxSubagentConcurrency int `json:"maxSubagentConcurrency"`
	MaxParallelWriters int    `json:"maxParallelWriters"`
	CompactRatio     int      `json:"compactRatio"`
}

// Settings 是设置持久化的句柄（内存缓存 + JSON 文件）。
type Settings struct {
	path string
	data desktopSettings
}

// NewSettings 加载（或初始化）设置文件。
func NewSettings() *Settings {
	s := &Settings{
		path: settingsPath(),
		data: desktopSettings{
			LayoutStyle:   "workbench",
			Theme:         "dark",
			ThemeStyle:    "graphite",
			Currency:      "",
			ReasoningMode: "auto",
			Zoom:          1.0,
			CloseBehavior: "quit",
			Language:      "zh",
			StatusBarStyle:   "text",
			StatusBarItems:   []string{"workspace", "model", "context", "usage", "cache", "cost"},
			DefaultToolApprovalMode: "auto",
			QualityFloor:     "standard",
			DefaultAgentPreset: "standard",
			PermissionMode:   "ask",
			PermissionRules:  map[string][]string{},
			SandboxBash:      "enforce",
			SandboxNetwork:   false,
			SandboxWorkspace: "",
			SandboxWrites:    []string{},
			SandboxShell:     "auto",
			TerminalTheme:    "dark",
			ConversationWidth: "standard",
			CheckUpdates:     true,
			DesktopMetrics:   true,
			DesktopTelemetry: false,
			DefaultModel:     "deepseek-v4-flash",
			PlannerModel:     "deepseek-v4-flash",
			SubagentModel:    "deepseek-v4-flash",
			SubagentEffort:   "auto",
			MaxSubagentDepth: 3,
			MaxSubagentConcurrency: 2,
			MaxParallelWriters: 1,
			CompactRatio:     1,
		},
	}
	if raw, err := os.ReadFile(s.path); err == nil {
		_ = json.Unmarshal(raw, &s.data)
	}
	return s
}

func settingsPath() string {
	home, _ := os.UserHomeDir()
	if home == "" {
		home = "."
	}
	return filepath.Join(home, ".dsh-reasonix-wails", "settings.json")
}

func (s *Settings) save() {
	_ = os.MkdirAll(filepath.Dir(s.path), 0o755)
	raw, _ := json.MarshalIndent(s.data, "", "  ")
	_ = os.WriteFile(s.path, raw, 0o644)
}

// --- 读写方法（App 桥方法调用这些） ---

func (s *Settings) LayoutStyle() string { return s.data.LayoutStyle }
func (s *Settings) SetLayoutStyle(v string) error {
	switch v {
	case "workbench", "creation":
		s.data.LayoutStyle = v
	default:
		s.data.LayoutStyle = "classic"
	}
	s.save()
	return nil
}

func (s *Settings) Theme() string { return s.data.Theme }
func (s *Settings) SetTheme(v string) error {
	switch v {
	case "light", "dark", "auto":
		s.data.Theme = v
	default:
		s.data.Theme = "dark"
	}
	s.save()
	return nil
}

func (s *Settings) ThemeStyle() string { return s.data.ThemeStyle }
func (s *Settings) SetThemeStyle(v string) {
	s.data.ThemeStyle = v
	s.save()
}

func (s *Settings) Currency() string { return s.data.Currency }
func (s *Settings) SetCurrency(v string) error {
	if v != "CNY" && v != "USD" {
		v = ""
	}
	s.data.Currency = v
	s.save()
	return nil
}

func (s *Settings) ReasoningMode() string { return s.data.ReasoningMode }
func (s *Settings) SetReasoningMode(v string) {
	s.data.ReasoningMode = v
	s.save()
}

func (s *Settings) Zoom() float64 { return s.data.Zoom }
func (s *Settings) SetZoom(v float64) {
	if v < 0.5 {
		v = 0.5
	}
	if v > 2.0 {
		v = 2.0
	}
	s.data.Zoom = v
	s.save()
}

func (s *Settings) CloseBehavior() string { return s.data.CloseBehavior }
func (s *Settings) SetCloseBehavior(v string) {
	if v != "background" {
		v = "quit"
	}
	s.data.CloseBehavior = v
	s.save()
}

func (s *Settings) Language() string { return s.data.Language }
func (s *Settings) SetLanguage(v string) {
	if v != "en" && v != "zh-TW" {
		v = "zh"
	}
	s.data.Language = v
	s.save()
}

func (s *Settings) StatusBarStyle() string { return s.data.StatusBarStyle }
func (s *Settings) SetStatusBarStyle(v string) {
	if v != "icon" {
		v = "text"
	}
	s.data.StatusBarStyle = v
	s.save()
}

func (s *Settings) StatusBarItems() []string { return s.data.StatusBarItems }
func (s *Settings) SetStatusBarItems(v []string) {
	items := []string{}
	for _, x := range v {
		if x != "" {
			items = append(items, x)
		}
	}
	if len(items) == 0 {
		items = []string{"workspace", "model", "context", "usage", "cache", "cost"}
	}
	s.data.StatusBarItems = items
	s.save()
}

func (s *Settings) DefaultToolApprovalMode() string { return s.data.DefaultToolApprovalMode }
func (s *Settings) SetDefaultToolApprovalMode(v string) {
	switch v {
	case "ask", "yolo":
		s.data.DefaultToolApprovalMode = v
	default:
		s.data.DefaultToolApprovalMode = "auto"
	}
	s.save()
}

func (s *Settings) QualityFloor() string { return s.data.QualityFloor }
func (s *Settings) SetQualityFloor(v string) {
	if v != "delivery" {
		v = "standard"
	}
	s.data.QualityFloor = v
	s.save()
}

// DefaultAgentPreset 默认 Agent 预设（DSH 四模式: standard/code/minimal/cordis）。
func (s *Settings) DefaultAgentPreset() string { return s.data.DefaultAgentPreset }
func (s *Settings) SetDefaultAgentPreset(v string) {
	switch v {
	case "code", "minimal", "cordis":
		s.data.DefaultAgentPreset = v
	default:
		s.data.DefaultAgentPreset = "standard"
	}
	s.save()
}

func (s *Settings) PermissionMode() string { return s.data.PermissionMode }
func (s *Settings) SetPermissionMode(v string) {
	if v != "allow" && v != "deny" { v = "ask" }
	s.data.PermissionMode = v
	s.save()
}

func (s *Settings) AddPermissionRule(kind, rule string) error {
	if s.data.PermissionRules == nil { s.data.PermissionRules = map[string][]string{} }
	s.data.PermissionRules[kind] = normalizePermissionRules(append(s.data.PermissionRules[kind], rule))
	s.save()
	return nil
}

func (s *Settings) RemovePermissionRule(kind, rule string) error {
	cur := s.data.PermissionRules[kind]
	out := []string{}
	for _, x := range cur { if x != rule { out = append(out, x) } }
	s.data.PermissionRules[kind] = out
	s.save()
	return nil
}

func (s *Settings) SetSandbox(bash string, network bool, workspaceRoot string, writes []string, shell string) error {
	if bash == "" { bash = "enforce" }
	if shell == "" { shell = "auto" }
	s.data.SandboxBash = bash
	s.data.SandboxNetwork = network
	s.data.SandboxWorkspace = workspaceRoot
	s.data.SandboxWrites = writes
	s.data.SandboxShell = shell
	s.save()
	return nil
}

func (s *Settings) PermissionsView() map[string]any {
	return map[string]any{
		"mode": s.data.PermissionMode,
		"allow": s.data.PermissionRules["allow"],
		"ask": s.data.PermissionRules["ask"],
		"deny": s.data.PermissionRules["deny"],
	}
}

func (s *Settings) SandboxView() map[string]any {
	return map[string]any{
		"bash": s.data.SandboxBash,
		"network": s.data.SandboxNetwork,
		"workspaceRoot": s.data.SandboxWorkspace,
		"allowWrite": s.data.SandboxWrites,
		"effectiveWorkspaceRoot": s.data.SandboxWorkspace,
		"effectiveWriteRoots": s.data.SandboxWrites,
		"shell": s.data.SandboxShell,
		"effectiveShell": s.data.SandboxShell,
	}
}

func (s *Settings) TerminalTheme() string { return s.data.TerminalTheme }
func (s *Settings) SetTerminalTheme(v string) { s.data.TerminalTheme = v; s.save() }

func (s *Settings) ConversationWidth() string { return s.data.ConversationWidth }
func (s *Settings) SetConversationWidth(v string) { s.data.ConversationWidth = v; s.save() }

func (s *Settings) CheckUpdates() bool { return s.data.CheckUpdates }
func (s *Settings) SetCheckUpdates(v bool) { s.data.CheckUpdates = v; s.save() }

func (s *Settings) DesktopMetrics() bool { return s.data.DesktopMetrics }
func (s *Settings) SetDesktopMetrics(v bool) { s.data.DesktopMetrics = v; s.save() }

func (s *Settings) DesktopTelemetry() bool { return s.data.DesktopTelemetry }
func (s *Settings) SetDesktopTelemetry(v bool) { s.data.DesktopTelemetry = v; s.save() }

func (s *Settings) DefaultModel() string { return s.data.DefaultModel }
func (s *Settings) SetDefaultModel(v string) { if v != "" { s.data.DefaultModel = v; s.save() } }

func (s *Settings) PlannerModel() string { return s.data.PlannerModel }
func (s *Settings) SetPlannerModel(v string) { if v != "" { s.data.PlannerModel = v; s.save() } }

func (s *Settings) SubagentModel() string { return s.data.SubagentModel }
func (s *Settings) SetSubagentModel(v string) { if v != "" { s.data.SubagentModel = v; s.save() } }

func (s *Settings) SubagentEffort() string { return s.data.SubagentEffort }
func (s *Settings) SetSubagentEffort(v string) { if v != "" { s.data.SubagentEffort = v; s.save() } }

func (s *Settings) MaxSubagentDepth() int { return s.data.MaxSubagentDepth }
func (s *Settings) SetMaxSubagentDepth(v int) { if v >= 1 { s.data.MaxSubagentDepth = v; s.save() } }

func (s *Settings) MaxSubagentConcurrency() int { return s.data.MaxSubagentConcurrency }
func (s *Settings) SetMaxSubagentConcurrency(v int) { if v >= 1 { s.data.MaxSubagentConcurrency = v; s.save() } }

func (s *Settings) MaxParallelWriters() int { return s.data.MaxParallelWriters }
func (s *Settings) SetMaxParallelWriters(v int) { if v >= 1 { s.data.MaxParallelWriters = v; s.save() } }

func (s *Settings) CompactRatio() int { return s.data.CompactRatio }
func (s *Settings) SetCompactRatio(v int) { if v >= 1 { s.data.CompactRatio = v; s.save() } }