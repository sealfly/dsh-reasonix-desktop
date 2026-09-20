// app_skill_seed.go — 内置 skill seed：把随包 skills/ 注入 ~/.dsh/skills。
//
// 与插件 seed（app_plugin_seed.go）同一套路：
//   数据源：安装目录 skills/（NSIS `File /r "skills"`；本机开发用 Junction 指到仓库）
//   目标：  ~/.dsh/skills/<name>/SKILL.md（DSH 的用户级 skill 发现路径）
//
// 为什么需要它：仓库里的 skills/ 不会自己出现在用户机器上——安装包不带、应用不装，
// 换机器/重装后内置 skill 直接消失（2026-09-19 排查确认的分发缺口）。
//
// 幂等 + 保护用户修改（关键设计）：
//   播种时在 skill 目录写 .dsh-seeded 标记，内容 = 本次播入内容的 sha256。
//   - 目标不存在                        → 写入（installed）
//   - 目标与源逐字节一致                → 跳过（up-to-date）
//   - 目标被改，但改的正是我们播的版本  → 用户没动过 → 更新为新版（updated）
//   - 目标被用户改过（哈希对不上标记）  → 保留用户版本（kept-user，只留日志）
//   - 目标存在但没有我们的标记          → 视为用户自己的 skill，一律不碰
// 卸载不删除（~/.dsh 是用户数据）。

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	builtinSkillDirName = "skills"
	skillSeedMarkerName = ".dsh-seeded"
)

// 测试注入（默认空 = 真实安装目录 / ~/.dsh/skills）。
var (
	seedSkillsSrcOverride string
	seedSkillsDstOverride string
)

// builtinSkillRoots 候选源目录（与插件离线源同套路：exe 同级 / dsh-runtime / resources）。
func builtinSkillRoots() []string {
	exe, err := os.Executable()
	if err != nil {
		return nil
	}
	dir := filepath.Dir(exe)
	return []string{
		filepath.Join(dir, builtinSkillDirName),
		filepath.Join(dir, "dsh-runtime", builtinSkillDirName),
		filepath.Join(dir, "resources", builtinSkillDirName),
	}
}

// seedSkillsDir 目标根（~/.dsh/skills）。
func seedSkillsDir() string {
	if seedSkillsDstOverride != "" {
		return seedSkillsDstOverride
	}
	return filepath.Join(homeDir(), ".dsh", "skills")
}

// locateBuiltinSkillRoot 找到随包 skills/；找不到返回空（源码运行属正常情况）。
func locateBuiltinSkillRoot() string {
	if seedSkillsSrcOverride != "" {
		return seedSkillsSrcOverride
	}
	for _, c := range builtinSkillRoots() {
		if st, err := os.Stat(c); err == nil && st.IsDir() {
			return c
		}
	}
	return ""
}

// seedBuiltinSkills 启动入口（后台执行；任何失败只留日志，不崩启动）。
func (a *App) seedBuiltinSkills() {
	defer func() {
		if r := recover(); r != nil {
			resumeLog("seedBuiltinSkills panic: %v", r)
		}
	}()
	root := locateBuiltinSkillRoot()
	if root == "" {
		resumeLog("seedBuiltinSkills: 未找到随包 skills/（源码运行或旧安装包），跳过")
		return
	}
	installed, updated, kept, failed := a.seedSkillsFrom(root)
	resumeLog("seedBuiltinSkills done: src=%s installed=%v updated=%v kept-user=%v failed=%v",
		root, installed, updated, kept, failed)
}

// seedSkillsFrom 遍历源目录内每个含 SKILL.md 的一级子目录并播种。
func (a *App) seedSkillsFrom(srcRoot string) (installed, updated, kept, failed []string) {
	entries, err := os.ReadDir(srcRoot)
	if err != nil {
		return nil, nil, nil, []string{"read-src:" + err.Error()}
	}
	dstRoot := seedSkillsDir()
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		data, err := os.ReadFile(filepath.Join(srcRoot, name, "SKILL.md"))
		if err != nil {
			continue // 不是 skill 目录
		}
		dstDir := filepath.Join(dstRoot, name)
		if err := os.MkdirAll(dstDir, 0o755); err != nil {
			failed = append(failed, name+":"+err.Error())
			continue
		}
		action, err := writeSkillFile(filepath.Join(dstDir, "SKILL.md"), data)
		if err != nil {
			failed = append(failed, name+":"+err.Error())
			continue
		}
		switch action {
		case skillActionInstalled:
			installed = append(installed, name)
		case skillActionUpdated:
			updated = append(updated, name)
		case skillActionKeptUser:
			kept = append(kept, name)
		}
	}
	sort.Strings(installed)
	sort.Strings(updated)
	sort.Strings(kept)
	sort.Strings(failed)
	return
}

type skillAction string

const (
	skillActionNone      skillAction = "none"
	skillActionInstalled skillAction = "installed"
	skillActionUpdated   skillAction = "updated"
	skillActionKeptUser  skillAction = "kept-user"
)

// writeSkillFile 单个 skill 的幂等播种（纯文件操作，便于单测）。
func writeSkillFile(dstFile string, srcData []byte) (skillAction, error) {
	srcSum := sha256Hex(srcData)
	cur, err := os.ReadFile(dstFile)
	if err != nil {
		if !os.IsNotExist(err) {
			return skillActionNone, err
		}
		if err := writeSkillWithMarker(dstFile, srcData, srcSum); err != nil {
			return skillActionNone, err
		}
		return skillActionInstalled, nil
	}
	curSum := sha256Hex(cur)
	if curSum == srcSum {
		// 内容已一致。若标记缺失（用户手工拷过、或更早版本没写标记），补上——
		// 否则以后内置 skill 升级时会被当成"用户自己的"永久跳过。
		if marker, _ := os.ReadFile(skillMarkerPath(dstFile)); markerFirstLine(marker) != srcSum {
			_ = os.WriteFile(skillMarkerPath(dstFile), []byte(srcSum+"\n"), 0o644)
		}
		return skillActionNone, nil
	}
	// 内容不同：只有「当前内容正是我们上次播进去的那份」才允许覆盖，
	// 否则说明用户改过（或这个 skill 本来就是用户自己的）—— 保留用户版本。
	marker, _ := os.ReadFile(skillMarkerPath(dstFile))
	if markerFirstLine(marker) == curSum {
		if err := writeSkillWithMarker(dstFile, srcData, srcSum); err != nil {
			return skillActionNone, err
		}
		return skillActionUpdated, nil
	}
	return skillActionKeptUser, nil
}

// markerFirstLine 取标记文件首行（容忍历史格式里额外的行）。
func markerFirstLine(b []byte) string {
	return strings.TrimSpace(strings.SplitN(strings.TrimSpace(string(b)), "\n", 2)[0])
}

func skillMarkerPath(dstFile string) string {
	return filepath.Join(filepath.Dir(dstFile), skillSeedMarkerName)
}

// writeSkillWithMarker 写目标文件，并把「本次播入内容的哈希」写进标记。
// 标记只放哈希一行（不要掺时间戳——比较时必须能一眼取到哈希本身）。
func writeSkillWithMarker(dstFile string, data []byte, sum string) error {
	if err := os.WriteFile(dstFile, data, 0o644); err != nil {
		return err
	}
	return os.WriteFile(skillMarkerPath(dstFile), []byte(sum+"\n"), 0o644)
}

func sha256Hex(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}
