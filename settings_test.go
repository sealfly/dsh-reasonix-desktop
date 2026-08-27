package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// newTestSettings 构造指向临时文件的 Settings（避免污染用户真实 settings.json）。
func newTestSettings(t *testing.T) *Settings {
	t.Helper()
	return &Settings{path: filepath.Join(t.TempDir(), "settings.json")}
}

func TestSettingsStatusBarPersistence(t *testing.T) {
	s := newTestSettings(t)
	s.SetStatusBarStyle("icon")
	s.SetStatusBarItems([]string{"workspace", "model", "cost"})
	s.SetDefaultToolApprovalMode("ask")
	if got := s.StatusBarStyle(); got != "icon" {
		t.Fatalf("StatusBarStyle = %q, want icon", got)
	}
	if got := s.StatusBarItems(); len(got) != 3 || got[0] != "workspace" || got[2] != "cost" {
		t.Fatalf("StatusBarItems = %v, want [workspace model cost]", got)
	}
	// 持久化：从磁盘重新读回
	raw, err := os.ReadFile(s.path)
	if err != nil {
		t.Fatalf("read settings file: %v", err)
	}
	var data desktopSettings
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatalf("unmarshal settings: %v", err)
	}
	if data.StatusBarStyle != "icon" || data.DefaultToolApprovalMode != "ask" {
		t.Fatalf("persisted = style %q approval %q", data.StatusBarStyle, data.DefaultToolApprovalMode)
	}
}

func TestSettingsValidation(t *testing.T) {
	s := newTestSettings(t)
	// 非法样式 → text
	s.SetStatusBarStyle("bogus")
	if got := s.StatusBarStyle(); got != "text" {
		t.Fatalf("bogus style -> %q, want text", got)
	}
	// 空 items → 默认列表
	s.SetStatusBarItems(nil)
	if got := len(s.StatusBarItems()); got != 6 {
		t.Fatalf("nil items -> %d entries, want 6 defaults", got)
	}
	// 非法审批模式 → auto
	s.SetDefaultToolApprovalMode("bogus")
	if got := s.DefaultToolApprovalMode(); got != "auto" {
		t.Fatalf("bogus mode -> %q, want auto", got)
	}
	// 合法模式保留
	s.SetDefaultToolApprovalMode("yolo")
	if got := s.DefaultToolApprovalMode(); got != "yolo" {
		t.Fatalf("yolo mode -> %q, want yolo", got)
	}
	s.SetDefaultToolApprovalMode("ask")
	if got := s.DefaultToolApprovalMode(); got != "ask" {
		t.Fatalf("ask mode -> %q, want ask", got)
	}
}

func TestModeToPermission(t *testing.T) {
	cases := map[string]string{
		"ask":   "read-only",
		"auto":  "workspace-write",
		"yolo":  "danger-full-access",
		"":      "",
		"bogus": "",
	}
	for in, want := range cases {
		if got := modeToPermission(in); got != want {
			t.Fatalf("modeToPermission(%q) = %q, want %q", in, got, want)
		}
	}
}
