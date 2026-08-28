// app_models_test.go — 模型/档位桥方法结构测试（用真实 DSH 响应结构）。

package main

import (
	"encoding/json"
	"testing"
)

// TestSplitRef provider/model 拆分。
func TestSplitRef(t *testing.T) {
	p, m := splitRef("deepseek-official/deepseek-v4-pro")
	if p != "deepseek-official" || m != "deepseek-v4-pro" {
		t.Fatalf("splitRef = %q/%q", p, m)
	}
	p2, m2 := splitRef("nomodel")
	if p2 != "nomodel" || m2 != "nomodel" {
		t.Fatalf("无斜杠 = %q/%q", p2, m2)
	}
	p3, m3 := splitRef("")
	if p3 != "" || m3 != "" {
		t.Fatalf("空 = %q/%q", p3, m3)
	}
}

// TestDshModelsViewDecode 用真实 DSH session.models 响应结构验证解码。
func TestDshModelsViewDecode(t *testing.T) {
	raw := `{
		"current": {"provider": "deepseek-official", "model": "deepseek-v4-pro", "reasoningEffort": "high"},
		"routable": true,
		"groups": [
			{"id": "deepseek-official", "name": "DeepSeek", "models": [
				{"id": "deepseek-v4-flash", "name": "DeepSeek-V4-Flash",
				 "reasoning": {"efforts": [{"id":"off","name":"Off"},{"id":"low","name":"Low"},{"id":"high","name":"High"},{"id":"max","name":"Max"}], "defaultEffort": "high"}},
				{"id": "deepseek-v4-pro", "name": "DeepSeek-V4-Pro",
				 "reasoning": {"efforts": [{"id":"off"},{"id":"high"},{"id":"max"}], "defaultEffort": "high"}}
			]},
			{"id": "xtoken", "name": "xtoken", "models": [
				{"id": "z-image-turbo", "name": "z-image-turbo"}
			]}
		]
	}`
	var m dshModelsView
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("解码失败: %v", err)
	}
	if m.Current == nil || m.Current.Provider != "deepseek-official" || m.Current.Model != "deepseek-v4-pro" {
		t.Fatalf("current 解析失败: %+v", m.Current)
	}
	if len(m.Groups) != 2 {
		t.Fatalf("groups = %d, want 2", len(m.Groups))
	}
	// 当前模型 deepseek-v4-pro 有 reasoning（off/high/max）
	r, ok := currentReasoning(&m)
	if !ok {
		t.Fatalf("currentReasoning 应为 true")
	}
	if len(r.Efforts) != 3 || r.Efforts[0].ID != "off" || r.DefaultEffort != "high" {
		t.Fatalf("efforts 解析失败: %+v", r)
	}
	// modelsRefs 输出（含 efforts/name）
	refs := modelsRefsFrom(&m)
	if len(refs) != 3 {
		t.Fatalf("refs = %d, want 3", len(refs))
	}
	cur := refs[1].(map[string]any)
	if cur["current"] != true || cur["ref"] != "deepseek-official/deepseek-v4-pro" {
		t.Fatalf("current ref 标记失败: %+v", cur)
	}
	if cur["name"] != "DeepSeek-V4-Pro" {
		t.Fatalf("name 未透传: %v", cur["name"])
	}
	if eff, ok := cur["efforts"].([]any); !ok || len(eff) != 3 {
		t.Fatalf("efforts 未透传: %v", cur["efforts"])
	}
}

// TestCurrentReasoningNoEffort xtoken 组模型无 reasoning → false。
func TestCurrentReasoningNoEffort(t *testing.T) {
	m := &dshModelsView{
		Current: &dshModelCurrent{Provider: "xtoken", Model: "z-image-turbo"},
		Groups: []dshModelGroup{
			{ID: "xtoken", Models: []dshModel{{ID: "z-image-turbo", Name: "z-image-turbo"}}},
		},
	}
	if _, ok := currentReasoning(m); ok {
		t.Fatalf("无 reasoning 模型应返回 false")
	}
}
