package main

import "testing"

func TestPermissionModePersistence(t *testing.T) {
	s := NewSettings()
	s.SetPermissionMode("allow")
	if got := s.PermissionMode(); got != "allow" {
		t.Fatalf("PermissionMode = %q, want allow", got)
	}
	s.SetPermissionMode("invalid")
	if got := s.PermissionMode(); got != "ask" {
		t.Fatalf("invalid 应回退 ask, got %q", got)
	}
}

func TestPermissionRules(t *testing.T) {
	s := NewSettings()
	_ = s.AddPermissionRule("allow", "bash:run")
	_ = s.AddPermissionRule("allow", "bash:run") // 去重
	_ = s.AddPermissionRule("deny", "rm:*")
	v := s.PermissionsView()
	allow := v["allow"].([]string)
	if len(allow) != 1 || allow[0] != "bash:run" {
		t.Fatalf("allow rules = %v, want [bash:run]", allow)
	}
	_ = s.RemovePermissionRule("deny", "rm:*")
	v2 := s.PermissionsView()
	deny := v2["deny"].([]string)
	if len(deny) != 0 {
		t.Fatalf("deny rules = %v, want empty", deny)
	}
}

func TestSandboxPersistence(t *testing.T) {
	s := NewSettings()
	_ = s.SetSandbox("allow", true, "/workspace", []string{"/out"}, "pwsh")
	v := s.SandboxView()
	if v["bash"] != "allow" || v["network"] != true || v["shell"] != "pwsh" {
		t.Fatalf("sandbox = %v", v)
	}
	if v["effectiveWorkspaceRoot"] != "/workspace" {
		t.Fatalf("effectiveWorkspaceRoot = %v", v["effectiveWorkspaceRoot"])
	}
}
