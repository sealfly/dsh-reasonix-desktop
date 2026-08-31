package main

// app_agent_presets_test.go — DSH 四模式（Agent 预设）测试。
// 单测覆盖归一化；集成测试走真实 DSH（临时会话，不打扰当前会话）。

import "testing"

func TestNormalizeAgentPreset(t *testing.T) {
	cases := map[string]string{
		"standard": "standard",
		"code":     "code",
		"minimal":  "minimal",
		"cordis":   "cordis",
		"标准模式": "standard",
		"极简模式": "minimal",
		"创造模式": "cordis",
		"PTC模式":  "code",
		"":         "",
		"bogus":    "",
		"STANDARD": "standard",
	}
	for in, want := range cases {
		if got := normalizeAgentPreset(in); got != want {
			t.Fatalf("normalizeAgentPreset(%q) = %q, want %q", in, got, want)
		}
	}
}

// 默认预设持久化。
func TestDefaultAgentPresetPersistence(t *testing.T) {
	a := newTestApp()
	r := a.SetDefaultAgentPreset("minimal")
	if r["ok"] != true || r["preset"] != "minimal" {
		t.Fatalf("SetDefaultAgentPreset 失败: %v", r)
	}
	if a.st.DefaultAgentPreset() != "minimal" {
		t.Fatalf("默认预设未持久化: %q", a.st.DefaultAgentPreset())
	}
	// 非法回退 standard
	r2 := a.SetDefaultAgentPreset("bogus")
	if r2["ok"] != false {
		t.Fatalf("非法预设应失败: %v", r2)
	}
	if a.st.DefaultAgentPreset() != "minimal" {
		t.Fatalf("非法输入不应改默认: %q", a.st.DefaultAgentPreset())
	}
}

// 集成：拉取 DSH 四模式清单。
func TestAgentPresetsIntegration(t *testing.T) {
	a := newTestApp()
	a.dsh = NewDshClient(3080)
	if _, err := a.dsh.RPC("session.list", map[string]any{}); err != nil {
		t.Skipf("DSH 不可用，跳过: %v", err)
	}
	v := a.AgentPresets()
	if e, _ := v["error"].(string); e != "" {
		t.Fatalf("AgentPresets error: %s", e)
	}
	presets, ok := v["presets"].([]any)
	if !ok || len(presets) == 0 {
		t.Fatalf("presets 为空: %v", v)
	}
	// 必须含四个内置 id
	ids := map[string]bool{}
	for _, p := range presets {
		m, _ := p.(map[string]any)
		if id, _ := m["id"].(string); id != "" {
			ids[id] = true
		}
	}
	for _, want := range []string{"standard", "code", "minimal", "cordis"} {
		if !ids[want] {
			t.Fatalf("缺少内置预设 %q: %v", want, ids)
		}
	}
}

// 集成：为临时会话切换预设（agentPreset.select 真实生效）。
func TestSetAgentPresetForTabIntegration(t *testing.T) {
	a := newTestApp()
	a.dsh = NewDshClient(3080)
	sid := createTempSession(t)
	r := a.SetAgentPresetForTab(sid, "minimal")
	if r["ok"] != true || r["preset"] != "minimal" {
		t.Fatalf("SetAgentPresetForTab 失败: %v", r)
	}
	// 验证: session.list 该会话 agentPreset 已变
	listRaw, err := a.dsh.RPC("session.list", map[string]any{})
	if err != nil {
		t.Fatalf("session.list 失败: %v", err)
	}
	var list struct {
		Items []struct {
			SessionID   string `json:"sessionId"`
			AgentPreset string `json:"agentPreset"`
		} `json:"items"`
	}
	if err := DecodeRPC(listRaw, &list); err != nil {
		t.Fatalf("session.list 解析失败: %v", err)
	}
	found := false
	for _, it := range list.Items {
		if it.SessionID == sid {
			found = true
			if it.AgentPreset != "minimal" {
				t.Fatalf("会话预设应为 minimal, 实际 %q", it.AgentPreset)
			}
		}
	}
	if !found {
		t.Fatalf("未找到测试会话 %s", sid)
	}
}

// SetModeForTab 转发 DSH 预设名。
func TestSetModeForTabPreset(t *testing.T) {
	a := newTestApp()
	a.dsh = NewDshClient(3080)
	sid := createTempSession(t)
	if err := a.SetModeForTab(sid, "code"); err != nil {
		t.Fatalf("SetModeForTab(code) 失败: %v", err)
	}
	// Reasonix 模式 no-op 不崩溃
	if err := a.SetModeForTab(sid, "plan"); err != nil {
		t.Fatalf("SetModeForTab(plan) 不应报错: %v", err)
	}
}
