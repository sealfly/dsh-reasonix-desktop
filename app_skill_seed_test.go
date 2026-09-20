package main

// app_skill_seed_test.go — 内置 skill seed 测试：
// ① 播种语义（首次安装 / 已最新 / 用户未改可更新 / 用户改过绝不覆盖 / 同名外来 skill 不碰）
// ② 缺标记时的补写（手工拷过的也能跟随升级）
// ③ 随包一致性（仓库 skills/<name>/SKILL.md 存在且 frontmatter 的 name 与目录名一致）

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeSkillFileForTest(t *testing.T, root, name, body string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func withSkillOverrides(t *testing.T, src, dst string) {
	t.Helper()
	oldSrc, oldDst := seedSkillsSrcOverride, seedSkillsDstOverride
	seedSkillsSrcOverride, seedSkillsDstOverride = src, dst
	t.Cleanup(func() { seedSkillsSrcOverride, seedSkillsDstOverride = oldSrc, oldDst })
}

func TestSeedSkillsLifecycle(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	v1 := "---\nname: demo\ndescription: v1\n---\nbody v1\n"
	v2 := "---\nname: demo\ndescription: v2\n---\nbody v2\n"
	writeSkillFileForTest(t, src, "demo", v1)
	withSkillOverrides(t, src, dst)
	a := &App{}

	// ① 首次 → installed
	inst, upd, kept, failed := a.seedSkillsFrom(src)
	if len(inst) != 1 || inst[0] != "demo" || len(upd)+len(kept)+len(failed) != 0 {
		t.Fatalf("首次应为 installed=[demo]，实际 inst=%v upd=%v kept=%v failed=%v", inst, upd, kept, failed)
	}
	got, _ := os.ReadFile(filepath.Join(dst, "demo", "SKILL.md"))
	if string(got) != v1 {
		t.Fatalf("播种内容不符: %q", string(got))
	}
	if _, err := os.Stat(filepath.Join(dst, "demo", skillSeedMarkerName)); err != nil {
		t.Fatalf("应写入 %s 标记: %v", skillSeedMarkerName, err)
	}

	// ② 重复运行 → 无动作
	inst, upd, kept, failed = a.seedSkillsFrom(src)
	if len(inst)+len(upd)+len(kept)+len(failed) != 0 {
		t.Fatalf("重复播种应无动作，实际 inst=%v upd=%v kept=%v failed=%v", inst, upd, kept, failed)
	}

	// ③ 源更新 + 用户没改过 → updated
	writeSkillFileForTest(t, src, "demo", v2)
	inst, upd, kept, failed = a.seedSkillsFrom(src)
	if len(upd) != 1 || upd[0] != "demo" {
		t.Fatalf("应为 updated=[demo]，实际 inst=%v upd=%v kept=%v failed=%v", inst, upd, kept, failed)
	}
	got, _ = os.ReadFile(filepath.Join(dst, "demo", "SKILL.md"))
	if string(got) != v2 {
		t.Fatalf("更新后内容不符: %q", string(got))
	}

	// ④ 用户改过 → kept-user，绝不覆盖
	userEdit := "---\nname: demo\n---\nMY OWN EDITS\n"
	if err := os.WriteFile(filepath.Join(dst, "demo", "SKILL.md"), []byte(userEdit), 0o644); err != nil {
		t.Fatal(err)
	}
	writeSkillFileForTest(t, src, "demo", v1)
	inst, upd, kept, failed = a.seedSkillsFrom(src)
	if len(kept) != 1 || kept[0] != "demo" {
		t.Fatalf("用户改过应 kept-user=[demo]，实际 inst=%v upd=%v kept=%v failed=%v", inst, upd, kept, failed)
	}
	got, _ = os.ReadFile(filepath.Join(dst, "demo", "SKILL.md"))
	if string(got) != userEdit {
		t.Fatalf("用户内容被覆盖: %q", string(got))
	}
}

// 内容一致但缺标记（用户手工拷过 / 早期版本无标记）→ 补写标记，使后续能跟随升级。
func TestSeedSkillsBackfillsMarker(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	body := "---\nname: demo\ndescription: x\n---\nbody\n"
	writeSkillFileForTest(t, src, "demo", body)
	writeSkillFileForTest(t, dst, "demo", body) // 同内容，但没有标记
	withSkillOverrides(t, src, dst)
	a := &App{}
	inst, upd, kept, failed := a.seedSkillsFrom(src)
	if len(inst)+len(upd)+len(kept)+len(failed) != 0 {
		t.Fatalf("同内容应无动作，实际 inst=%v upd=%v kept=%v failed=%v", inst, upd, kept, failed)
	}
	marker, err := os.ReadFile(filepath.Join(dst, "demo", skillSeedMarkerName))
	if err != nil || markerFirstLine(marker) != sha256Hex([]byte(body)) {
		t.Fatalf("应补写内容哈希标记，实际 err=%v marker=%q", err, string(marker))
	}
	writeSkillFileForTest(t, src, "demo", body+"new line\n")
	_, upd, _, _ = a.seedSkillsFrom(src)
	if len(upd) != 1 {
		t.Fatalf("补标记后源更新应 updated，实际 %v", upd)
	}
}

func TestSeedSkillsKeepsForeignSameName(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	writeSkillFileForTest(t, src, "demo", "---\nname: demo\n---\nfrom-package\n")
	writeSkillFileForTest(t, dst, "demo", "---\nname: demo\n---\nuser-owned\n") // 无我们的标记
	withSkillOverrides(t, src, dst)
	a := &App{}
	_, _, kept, _ := a.seedSkillsFrom(src)
	if len(kept) != 1 {
		t.Fatalf("无标记的同名 skill 应保留，实际 kept=%v", kept)
	}
	got, _ := os.ReadFile(filepath.Join(dst, "demo", "SKILL.md"))
	if !strings.Contains(string(got), "user-owned") {
		t.Fatalf("用户自己的 skill 被覆盖: %q", string(got))
	}
}

func TestSeedSkillsMissingSourceIsNoop(t *testing.T) {
	dst := t.TempDir()
	withSkillOverrides(t, filepath.Join(t.TempDir(), "nope"), dst)
	a := &App{}
	_, _, _, failed := a.seedSkillsFrom(filepath.Join(t.TempDir(), "nope"))
	if len(failed) != 1 {
		t.Fatalf("源目录不存在应记 1 条失败（不 panic），实际 %v", failed)
	}
}

// 随包一致性：仓库 skills/<name>/SKILL.md 必须存在，frontmatter 的 name 必须与目录名一致。
func TestRepoSkillsArePackagable(t *testing.T) {
	entries, err := os.ReadDir("skills")
	if err != nil {
		t.Fatalf("skills/ 缺失: %v", err)
	}
	n := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		n++
		data, err := os.ReadFile(filepath.Join("skills", e.Name(), "SKILL.md"))
		if err != nil {
			t.Fatalf("skills/%s/SKILL.md 缺失: %v", e.Name(), err)
		}
		// 仓库文件在 Windows 上是 CRLF，判定前先规范化换行
		s := strings.ReplaceAll(string(data), "\r\n", "\n")
		if !strings.HasPrefix(s, "---\n") {
			t.Fatalf("skills/%s/SKILL.md 缺 YAML frontmatter", e.Name())
		}
		if !strings.Contains(s, "name: "+e.Name()) {
			t.Fatalf("skills/%s 的 frontmatter name 与目录名不一致", e.Name())
		}
		if !strings.Contains(s, "description:") {
			t.Fatalf("skills/%s 缺 description", e.Name())
		}
	}
	if n == 0 {
		t.Fatal("skills/ 下没有任何 skill —— 内置 skill 分发链路为空")
	}
	t.Logf("仓库内置 skill 共 %d 个", n)
}
