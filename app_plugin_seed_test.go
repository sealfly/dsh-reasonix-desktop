package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// seedTestManifest 构造最小 manifest。
func seedTestManifest() *seedPluginManifest {
	return &seedPluginManifest{
		Updated: "2026-09-08",
		Source:  "npm:test",
		Plugins: []seedPluginManifestPlugin{
			{Name: "@openviking/dsh-memory-plugin", Spec: "@openviking/dsh-memory-plugin@0.3.0", InstalledVersion: "0.3.0", DefaultEnabled: false, Bundle: "@openviking/dsh-memory-plugin"},
			{Name: "@nanmicoder/dsh-agent-teams", Spec: "@nanmicoder/dsh-agent-teams@0.1.15", InstalledVersion: "0.1.15", DefaultEnabled: true, Bundle: "@nanmicoder/dsh-agent-teams"},
		},
	}
}

// writeTestProfile 在 tempDir 写一个官方形态 profile package.json。
func writeTestProfile(t *testing.T, dir string, bundles []string) {
	t.Helper()
	doc := map[string]any{
		"name": "web",
		"dsh": map[string]any{
			"profile": map[string]any{"bundles": bundles},
		},
		"dependencies": map[string]any{"@deepseek-ai/cordis": "0.1.0"},
	}
	data, _ := json.MarshalIndent(doc, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "package.json"), append(data, '\n'), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestMergeSeedBundles(t *testing.T) {
	dir := t.TempDir()
	seedProfileDirOverride = dir
	defer func() { seedProfileDirOverride = "" }()

	writeTestProfile(t, dir, []string{"@deepseek-ai/dsh-base", "@deepseek-ai/dsh-web-app"})

	p := seedTestManifest().Plugins[0]
	if err := mergeSeedBundles(p); err != nil {
		t.Fatalf("merge failed: %v", err)
	}
	// 幂等：再合并一次不得重复
	if err := mergeSeedBundles(p); err != nil {
		t.Fatalf("merge(2nd) failed: %v", err)
	}

	pj := readProfilePackageJSON()
	if pj == nil {
		t.Fatal("cannot read back profile")
	}
	found := 0
	for _, b := range pj.DSH.Profile.Bundles {
		if b == p.Name {
			found++
		}
	}
	if found != 1 {
		t.Fatalf("bundle should appear exactly once, got %d (list=%v)", found, pj.DSH.Profile.Bundles)
	}
	if pj.Dependencies[p.Name] != "0.3.0" {
		t.Fatalf("dependencies[%s] = %q, want 0.3.0", p.Name, pj.Dependencies[p.Name])
	}
	// 原有字段保留
	if pj.Dependencies["@deepseek-ai/cordis"] != "0.1.0" {
		t.Fatal("existing dependencies lost")
	}
}

func TestProfileHasBundle(t *testing.T) {
	dir := t.TempDir()
	seedProfileDirOverride = dir
	defer func() { seedProfileDirOverride = "" }()

	writeTestProfile(t, dir, []string{"@deepseek-ai/dsh-base"})
	if !profileHasBundle("@deepseek-ai/dsh-base") {
		t.Fatal("existing bundle should be found")
	}
	if profileHasBundle("@nanmicoder/dsh-agent-teams") {
		t.Fatal("absent bundle should not be found")
	}
	if profileHasBundle("anything") {
		t.Fatal("no profile dir should return false, not panic")
	}
}

func TestAppendUniqueFold(t *testing.T) {
	got := appendUniqueFold([]string{"@deepseek-ai/dsh-base", "B"}, "@deepseek-ai/dsh-base")
	if len(got) != 2 {
		t.Fatalf("case-fold duplicate appended: %v", got)
	}
	got = appendUniqueFold([]string{"@deepseek-ai/dsh-base"}, "@nanmicoder/dsh-agent-teams")
	if len(got) != 2 || got[1] != "@nanmicoder/dsh-agent-teams" {
		t.Fatalf("append failed: %v", got)
	}
}

// TestCopyOfflineTree：@deepseek-ai 跳过、已存在跳过、缺失条目复制。
func TestCopyOfflineTree(t *testing.T) {
	offline := t.TempDir()
	nm := filepath.Join(offline, "node_modules")
	// 造离线树：scoped 插件 + @deepseek-ai（应跳过）+ 平铺第三方
	mk := func(parts ...string) string {
		p := filepath.Join(append([]string{nm}, parts...)...)
		if err := os.MkdirAll(p, 0755); err != nil {
			t.Fatal(err)
		}
		return p
	}
	os.WriteFile(filepath.Join(mk("@openviking", "dsh-memory-plugin"), "package.json"), []byte(`{"name":"@openviking/dsh-memory-plugin"}`), 0644)
	os.WriteFile(filepath.Join(mk("@deepseek-ai", "cordis"), "package.json"), []byte(`{"name":"@deepseek-ai/cordis"}`), 0644)
	os.WriteFile(filepath.Join(mk("zod"), "package.json"), []byte(`{"name":"zod"}`), 0644)
	os.WriteFile(filepath.Join(mk(".bin"), "x"), []byte("x"), 0644) // .bin 跳过

	profile := t.TempDir()
	profileNm := filepath.Join(profile, "node_modules")
	if err := os.MkdirAll(filepath.Join(profileNm, "@deepseek-ai"), 0755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(profileNm, "@deepseek-ai", "cordis"), []byte("link"), 0644) // 已存在占位
	// 已存在的第三方（模拟已有条目不覆盖）
	os.MkdirAll(filepath.Join(profileNm, "zod"), 0755)

	if err := copyOfflineTree(nm, profileNm); err != nil {
		t.Fatalf("copyOfflineTree: %v", err)
	}

	// @openviking 应被复制
	if _, err := os.Stat(filepath.Join(profileNm, "@openviking", "dsh-memory-plugin", "package.json")); err != nil {
		t.Fatal("@openviking not copied")
	}
	// @deepseek-ai 内容不能被覆盖
	data, _ := os.ReadFile(filepath.Join(profileNm, "@deepseek-ai", "cordis"))
	if string(data) != "link" {
		t.Fatal("@deepseek-ai entry overwritten (must preserve host layout)")
	}
	// zod 已存在 → 不覆盖其 package.json（无则仍无）
	if _, err := os.Stat(filepath.Join(profileNm, "zod", "package.json")); err == nil {
		t.Fatal("existing entry was overwritten")
	}
	// .bin 不复制
	if _, err := os.Stat(filepath.Join(profileNm, ".bin")); err == nil {
		t.Fatal(".bin should not be copied")
	}
}

func TestCopyDirTree(t *testing.T) {
	src := filepath.Join(t.TempDir(), "src")
	dst := filepath.Join(t.TempDir(), "dst")
	if err := os.MkdirAll(filepath.Join(src, "sub"), 0755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(src, "a.txt"), []byte("hello"), 0644)
	os.WriteFile(filepath.Join(src, "sub", "b.txt"), []byte("world"), 0644)
	if err := copyDirTree(src, dst); err != nil {
		t.Fatalf("copyDirTree: %v", err)
	}
	for _, f := range []string{"a.txt", filepath.Join("sub", "b.txt")} {
		if _, err := os.Stat(filepath.Join(dst, f)); err != nil {
			t.Fatalf("missing %s: %v", f, err)
		}
	}
}

// TestSeedManifestRoundTrip：真实离线包 manifest 可被 seed 结构解析。
func TestSeedManifestRoundTrip(t *testing.T) {
	m := seedTestManifest()
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	var back seedPluginManifest
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if len(back.Plugins) != 2 || back.Plugins[0].Name != "@openviking/dsh-memory-plugin" {
		t.Fatalf("round trip broken: %+v", back.Plugins)
	}
	if strings.TrimSpace(string(b)) == "" {
		t.Fatal("empty marshal")
	}
}
