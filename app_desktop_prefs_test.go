package main

import "testing"

func TestDesktopPrefsPersistence(t *testing.T) {
	s := NewSettings()
	s.SetTerminalTheme("light")
	if s.TerminalTheme() != "light" { t.Fatal("TerminalTheme") }
	s.SetConversationWidth("wide")
	if s.ConversationWidth() != "wide" { t.Fatal("ConversationWidth") }
	s.SetCheckUpdates(false)
	if s.CheckUpdates() { t.Fatal("CheckUpdates") }
	s.SetDesktopMetrics(false)
	if s.DesktopMetrics() { t.Fatal("DesktopMetrics") }
	s.SetDefaultModel("deepseek-v4-flash")
	if s.DefaultModel() == "" { t.Fatal("DefaultModel") }
	s.SetMaxSubagentDepth(5)
	if s.MaxSubagentDepth() != 5 { t.Fatal("MaxSubagentDepth") }
	s.SetCompactRatio(2)
	if s.CompactRatio() != 2 { t.Fatal("CompactRatio") }
}

func TestDesktopPrefsBridge(t *testing.T) {
	a := newTestApp()
	_ = a.SetDesktopTerminalTheme("dark")
	_ = a.SetDesktopConversationWidth("narrow")
	_ = a.SetMaxSubagentConcurrency(4)
	z := a.GetDesktopZoomFactor()
	if z < 0.5 || z > 2.0 { t.Fatalf("zoom = %v", z) }
	v := a.Settings()
	if v["desktopTerminalTheme"] == nil || v["maxSubagentConcurrency"] == nil {
		t.Fatalf("Settings 缺字段: %v", v)
	}
}
