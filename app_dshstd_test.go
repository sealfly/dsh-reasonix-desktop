// app_dshstd_test.go — dsh-std 协议核心测试。

package main

import (
	"encoding/json"
	"testing"
)

func TestParseApiVersion(t *testing.T) {
	cases := map[string]bool{
		"core.dsh/v1":            true,
		"command.dsh/v1":         true,
		"connection.dsh/v1alpha1": true,
		"tool.dsh/v1beta1":       true,
		"invalid":                false,
		"dsh/v0":                 false,
		"dsh/v1.5":               false,
	}
	for ver, wantOK := range cases {
		_, err := parseApiVersion(ver)
		if (err == nil) != wantOK {
			t.Fatalf("parseApiVersion(%q) ok=%v want=%v", ver, err == nil, wantOK)
		}
	}
}

func TestNegotiateCompatible(t *testing.T) {
	// 两个参与者: host 需要 Connection+Tool, plugin 支持 Tool
	decls := []ProtocolDeclaration{
		{
			Participant: ParticipantIdentity{ID: "host"},
			Requires: []ProtocolRequirement{
				{ApiReference: ApiReference{APIVersion: "connection.dsh/v1alpha1", Kind: "Connection"}},
				{ApiReference: ApiReference{APIVersion: "tool.dsh/v1", Kind: "Tool"}},
			},
		},
		{
			Participant: ParticipantIdentity{ID: "plugin-a"},
			Supports: []ProtocolSupport{
				{ApiReference: ApiReference{APIVersion: "tool.dsh/v1", Kind: "Tool"}},
			},
		},
	}
	report := Negotiate(decls)
	// Connection 无人支持 → 整体不兼容（正确行为）
	if report.Compatible {
		t.Fatalf("Connection 无支持时应不兼容")
	}
	// 验证 Tool 协议协商成功且参与者含双方
	found := false
	for _, p := range report.Protocols {
		if p.Kind == "Tool" && p.APIVersion == "tool.dsh/v1" {
			found = true
			if len(p.Participants) != 2 {
				t.Fatalf("Tool 参与者应为2: %v", p.Participants)
			}
			if p.Agreement == nil {
				t.Fatalf("Tool 应有 agreement")
			}
		}
	}
	if !found {
		t.Fatalf("未找到 Tool 协商结果")
	}
	// Connection 无人支持 → error issue
	for _, p := range report.Protocols {
		if p.Kind == "Connection" {
			if len(p.Issues) == 0 || p.Issues[0].Severity != "error" {
				t.Fatalf("Connection 应有 error issue: %+v", p.Issues)
			}
		}
	}
}

func TestNegotiateOptional(t *testing.T) {
	decls := []ProtocolDeclaration{
		{
			Participant: ParticipantIdentity{ID: "host"},
			Requires: []ProtocolRequirement{
				{ApiReference: ApiReference{APIVersion: "model.dsh/v1", Kind: "ModelCatalog"}, Optional: true},
			},
		},
	}
	report := Negotiate(decls)
	// optional 无支持 → warning 不阻塞兼容
	if !report.Compatible {
		t.Fatalf("optional 不应阻塞兼容")
	}
	for _, p := range report.Protocols {
		if p.Kind == "ModelCatalog" && len(p.Issues) > 0 && p.Issues[0].Severity != "warning" {
			t.Fatalf("optional 应为 warning: %+v", p.Issues)
		}
	}
}

func TestParseDshPluginManifestValid(t *testing.T) {
	m := `{
		"$schema": "https://dsh-std.dev/schemas/dsh-plugin-0.15.schema.json",
		"manifestVersion": "0.15",
		"id": "com.example/test",
		"name": "Test Plugin",
		"version": "1.0.0",
		"facets": {"host": {"entry": "index.js", "apiVersion": "host.dsh/v1alpha1"}},
		"requires": {"contracts": [{"apiVersion": "tool.dsh/v1", "kind": "Tool"}]},
		"supports": {"contracts": [{"apiVersion": "command.dsh/v1", "kind": "CommandRuntime"}]}
	}`
	r := ParseDshPluginManifest([]byte(m))
	if r["valid"] != true {
		t.Fatalf("应有效: %+v", r["issues"])
	}
	man := r["manifest"].(map[string]any)
	if man["id"] != "com.example/test" {
		t.Fatalf("id 解析错误: %v", man["id"])
	}
	contracts := man["contracts"].([]any)
	if len(contracts) != 2 {
		t.Fatalf("应有2个契约: %v", contracts)
	}
}

func TestParseDshPluginManifestInvalid(t *testing.T) {
	m := `{"manifestVersion": "0.15"}` // 缺 id/name/version
	r := ParseDshPluginManifest([]byte(m))
	if r["valid"] != false {
		t.Fatalf("应无效")
	}
}

func TestDshStdCapabilities(t *testing.T) {
	a := newTestApp()
	c := a.DshStdCapabilities()
	if c["standard"] != "dsh-std" {
		t.Fatalf("standard 错误: %v", c["standard"])
	}
	supports := c["supports"].([]any)
	if len(supports) < 5 {
		t.Fatalf("应支持至少5个协议: %v", supports)
	}
}

func TestDshStdNegotiateBridge(t *testing.T) {
	a := newTestApp()
	peer := `[{"participant":{"id":"peer"},"supports":[{"apiVersion":"tool.dsh/v1","kind":"Tool"}]}]`
	r := a.DshStdNegotiate(peer)
	if r["compatible"] != true {
		t.Fatalf("应兼容: %+v", r["issues"])
	}
	// 验证返回结构
	if r["apiVersion"] != "core.dsh/report/v1alpha1" {
		t.Fatalf("report apiVersion 错误: %v", r["apiVersion"])
	}
	protocols := r["protocols"].([]any)
	found := false
	for _, p := range protocols {
		m := p.(map[string]any)
		if m["kind"] == "Tool" {
			found = true
		}
	}
	if !found {
		t.Fatalf("协商结果应含 Tool")
	}
}

func TestDshStdSelfManifest(t *testing.T) {
	a := newTestApp()
	m := a.DshStdSelfManifest()
	// 用我们的解析器验证自清单
	data, _ := json.Marshal(m)
	r := ParseDshPluginManifest(data)
	if r["valid"] != true {
		t.Fatalf("自清单应有效: %+v", r["issues"])
	}
}

func TestValidSemver(t *testing.T) {
	cases := map[string]bool{
		"1.0.0":     true,
		"0.15.1":    true,
		"2.3.4-rc.1": true,
		"1.0.0+build5": true,
		"1.0":       false,
		"v1.0.0":    false,
		"1.0.0.0":   false,
		"":          false,
		"abc":       false,
	}
	for v, want := range cases {
		if validSemver(v) != want {
			t.Fatalf("validSemver(%q)=%v want=%v", v, validSemver(v), want)
		}
	}
}

func TestSupportedManifestVersion(t *testing.T) {
	cases := map[string]bool{
		"0.15": true,
		"0.16": true,
		"0.14": false,
		"1.0":  false,
		"":     false,
	}
	for v, want := range cases {
		if isSupportedManifestVersion(v) != want {
			t.Fatalf("isSupportedManifestVersion(%q)=%v want=%v", v, isSupportedManifestVersion(v), want)
		}
	}
}

func TestParseDshPluginManifestVersionChecks(t *testing.T) {
	// 非法 semver 版本应报 invalid-version
	m := `{"manifestVersion":"0.15","id":"com.example/t","name":"T","version":"1.0"}`
	r := ParseDshPluginManifest([]byte(m))
	if r["valid"] != false {
		t.Fatalf("非法版本应无效")
	}
	found := false
	for _, i := range r["issues"].([]any) {
		if i.(map[string]any)["code"] == "invalid-version" {
			found = true
		}
	}
	if !found {
		t.Fatalf("应报 invalid-version: %+v", r["issues"])
	}
	// 不支持的 manifestVersion 应报 unsupported-manifestVersion
	m2 := `{"manifestVersion":"2.0","id":"com.example/t","name":"T","version":"1.0.0"}`
	r2 := ParseDshPluginManifest([]byte(m2))
	if r2["valid"] != false {
		t.Fatalf("不支持的 manifestVersion 应无效")
	}
	found = false
	for _, i := range r2["issues"].([]any) {
		if i.(map[string]any)["code"] == "unsupported-manifestVersion" {
			found = true
		}
	}
	if !found {
		t.Fatalf("应报 unsupported-manifestVersion: %+v", r2["issues"])
	}
}
