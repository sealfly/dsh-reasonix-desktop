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
			StatusBarStyle: "text",
			StatusBarItems: []string{"workspace", "model", "context", "usage", "cache", "cost"},
			DefaultToolApprovalMode: "auto",
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
