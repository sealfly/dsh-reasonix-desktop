package main

// app_acp_test.go — ACP 预留接口测试：方法存在、返回 reserved 不崩溃。

import "testing"

func TestAcpAvailable(t *testing.T) {
	a := &App{}
	v := a.AcpAvailable()
	// 结构完整
	if v["stage"] != "reserved" {
		t.Fatalf("stage 应为 reserved: %v", v)
	}
	if v["enabled"] != false {
		t.Fatalf("enabled 应为 false: %v", v)
	}
	if _, ok := v["dshDetected"].(bool); !ok {
		t.Fatalf("dshDetected 应为 bool: %v", v)
	}
	if v["note"] == "" {
		t.Fatal("note 不应为空")
	}
}

// 预留方法返回 reserved + 明确 error, 不崩溃。
func TestAcpReservedMethods(t *testing.T) {
	a := &App{}
	if r := a.AcpStart(map[string]any{"cwd": "/tmp"}); r["stage"] != "reserved" || r["ok"] != false {
		t.Fatalf("AcpStart: %v", r)
	}
	if r := a.AcpPrompt("s1", "hi"); r["stage"] != "reserved" || r["sessionId"] != "s1" {
		t.Fatalf("AcpPrompt: %v", r)
	}
	if r := a.AcpCancel("s1"); r["stage"] != "reserved" {
		t.Fatalf("AcpCancel: %v", r)
	}
	if r := a.AcpStop("s1"); r["stage"] != "reserved" {
		t.Fatalf("AcpStop: %v", r)
	}
}

// dshCmdExists 至少不崩溃(本机有/无 dsh 都返回 bool)。
func TestDshCmdExists(t *testing.T) {
	_ = dshCmdExists() // 只验证不 panic
}
