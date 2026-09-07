package main

// app_plugin_admit.go — 插件 dsh-std 准入落地（读真实 manifest，防插件冲突）。
//
// 背景：dsh-std 的 AdmitPlugin/Negotiate 之前只服务测试（声明层"待激活"）。
// 这里把它接到真实插件安装链：装插件后定位其在 DSH profile 的 node_modules 目录，
// 读 dsh-plugin.json → ParseDshPluginManifest 校验 → AdmitPlugin 五态准入，
// 返回 {state, compatible, issues} 供前端展示（用户能看出装上的插件与本端是否冲突）。
//
// 插件真实安装目录：dsh plugin --profile web add <spec> → ~/.dsh/profiles/web/node_modules/
// （npm 包名即目录名，可带 @scope/）。找不到 manifest = 插件非 dsh-std 形态（仅 cordis），
// 返回 degraded/unknown 提示而非崩溃。

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// dshProfileModulesDir DSH web profile 的 node_modules（真实插件安装处）。
func dshProfileModulesDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".dsh", "profiles", "web", "node_modules")
}

// sanitizePluginDir 把插件 spec(github:o/r / npm 名 / @scope/name) 归一到可探测目录名。
func sanitizePluginDir(spec string) string {
	s := strings.TrimSpace(spec)
	s = strings.TrimPrefix(s, "github:")
	if i := strings.IndexByte(s, ':'); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimPrefix(s, "@")
	s = strings.ReplaceAll(s, "\\", "/")
	return strings.Trim(s, "/")
}

// locateDshPluginManifest 在 DSH profile node_modules 下找插件的 dsh-plugin.json。
// 输入可以是包名(@scope/name)、github spec 或目录名；按 node_modules 常见布局探测。
// 返回 manifest 文件路径；找不到返回 ""。
func locateDshPluginManifest(spec string) string {
	base := dshProfileModulesDir()
	if base == "" {
		return ""
	}
	s := strings.TrimSpace(spec)
	if s == "" {
		return ""
	}
	// 候选相对路径：@scope/name 与 name 两种布局
	var rels []string
	if strings.HasPrefix(s, "@") {
		rels = append(rels, s)
	} else {
		dir := sanitizePluginDir(s)
		if dir != "" {
			rels = append(rels, dir)
		}
		// github:o/r → 可能装了 @o/r 或 o-r 等，扫目录模糊匹配
	}
	for _, rel := range rels {
		for _, mf := range []string{"dsh-plugin.json", "plugin.json"} {
			p := filepath.Join(base, rel, mf)
			if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
				return p
			}
		}
	}
	// 模糊：spec 末段名匹配 node_modules 顶层目录(不带 scope 也探测一次)
	if !strings.HasPrefix(s, "@") {
		last := s
		if i := strings.LastIndex(s, "/"); i >= 0 {
			last = s[i+1:]
		}
		if last != "" && last != s {
			for _, mf := range []string{"dsh-plugin.json", "plugin.json"} {
				p := filepath.Join(base, last, mf)
				if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
					return p
				}
			}
		}
	}
	return ""
}

// readPluginManifestRaw 读 dsh-plugin.json 原始内容（失败返回空）。
func readPluginManifestRaw(path string) []byte {
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return data
}

// PluginDshStdAdmit 对已安装插件执行 dsh-std 五态准入（读真实 manifest）。
// name/source 用于定位插件（包名或 github spec）。返回：
//   {ok, state, compatible, manifestFound, manifestVersion?, apiVersion?, kind?, issues[]}
// manifestFound=false → 插件无 dsh-plugin.json（cordis 形态），state=unknown 提示。
func (a *App) PluginDshStdAdmit(name string) map[string]any {
	out := map[string]any{
		"ok": true, "state": "unknown", "compatible": false,
		"manifestFound": false, "issues": []any{},
	}
	if strings.TrimSpace(name) == "" {
		out["ok"] = false
		out["error"] = "plugin name is required"
		return out
	}
	path := locateDshPluginManifest(name)
	if path == "" {
		// 也试按 name 精确(可能是 @scope/name)
		path = locateDshPluginManifest("@" + strings.TrimPrefix(name, "@"))
	}
	if path == "" {
		out["note"] = "no dsh-plugin.json found (plugin may be cordis-only or not installed yet)"
		return out
	}
	out["manifestFound"] = true
	raw := readPluginManifestRaw(path)
	if len(raw) == 0 {
		out["error"] = "manifest unreadable"
		return out
	}
	parsed := ParseDshPluginManifest(raw)
	// ParseDshPluginManifest 返回 {valid, manifest:{...}, issues[]}——字段在 manifest 嵌套里
	valid, _ := parsed["valid"].(bool)
	out["manifestValid"] = valid
	man, _ := parsed["manifest"].(map[string]any)
	if man != nil {
		if v, ok := man["manifestVersion"].(string); ok {
			out["manifestVersion"] = v
		}
		if v, ok := man["id"].(string); ok {
			out["pluginId"] = v
		}
		if v, ok := man["version"].(string); ok {
			out["pluginVersion"] = v
		}
	}
	if issues, ok := parsed["issues"].([]any); ok && len(issues) > 0 {
		out["parseIssues"] = issues
	}
	state, compatible, issues := AdmitPlugin(parsed)
	out["state"] = string(state)
	out["compatible"] = compatible
	issuesOut := []any{}
	for _, i := range issues {
		switch v := i.(type) {
		case map[string]any:
			issuesOut = append(issuesOut, v)
		default:
			issuesOut = append(issuesOut, map[string]any{"code": "admission-issue", "severity": "warning", "message": jsonString(v)})
		}
	}
	out["issues"] = issuesOut
	return out
}


// AdmitInstalledPlugins 对本地已安装清单里所有插件做 dsh-std 准入评估（含记忆插件等）。
// 逐个尝试定位真实 dsh-plugin.json 并 AdmitPlugin，返回 [{name, state, compatible,
// manifestFound, apiVersion?, kind?, issues[]}]——前端插件管理页可展示每项合规徽标。
func (a *App) AdmitInstalledPlugins() []any {
	out := []any{}
	mgr := getPluginManager()
	for _, p := range mgr.loadInstalled() {
		admit := a.PluginDshStdAdmit(p.Name)
		item := map[string]any{
			"name":           p.Name,
			"source":         p.Source,
			"enabled":        p.Enabled,
			"state":          admit["state"],
			"compatible":     admit["compatible"],
			"manifestFound":  admit["manifestFound"],
		}
		if v, ok := admit["apiVersion"].(string); ok && v != "" {
			item["apiVersion"] = v
		}
		if v, ok := admit["kind"].(string); ok && v != "" {
			item["kind"] = v
		}
		if v, ok := admit["issues"].([]any); ok {
			item["issues"] = v
		}
		if v, ok := admit["manifestVersion"].(string); ok && v != "" {
			item["manifestVersion"] = v
		}
		out = append(out, item)
	}
	return out
}

// jsonString 把值转字符串（issues 兜底显示）。
func jsonString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	b, _ := json.Marshal(v)
	return string(b)
}
