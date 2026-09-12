// app_plugin_seed.go — 默认附加插件 seed（面向电脑小白：装完我们的应用即得
// 记忆插件 + dsh-agent-teams，零操作）。
//
// 数据源优先级：
//   1. 安装目录 plugins-offline/（安装包随附的离线 npm 树 + manifest.json）
//   2. 在线回退：离线源缺失且 dsh CLI 可用 → dsh plugin --profile web add <spec>（需网络）
//
// 注入目标（幂等）：~/.dsh/profiles/web
//   - package.json   dsh.profile.bundles += 插件名；dependencies += name: installedVersion
//   - node_modules   复制离线树中 profile 缺失的条目（跳过已存在，保护 pnpm 布局）
//   - cordis.patch.yml  记忆插件默认 disabled（省 token；设置-记忆一键启用）；
//                       agent-teams 默认启用（无行 = 启用）
//
// 幂等：bundles 已含 → 跳过；状态留痕 ~/.reasonix/plugin-seed.json。
// 路径 A（懒人包）：内置 DSH，离线源随包 → 自动注入成功率高；
// 路径 B（经典包）：接外部/在线 DSH，同源注入；DSH CLI 可用时在线兜底。

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	seedOfflineDirName = "plugins-offline"
	seedProfileName    = "web"
	seedStateFileName  = "plugin-seed.json"
)

// seedPluginManifest 离线包 manifest（prepare-plugin-offline.ps1 产出）。
type seedPluginManifest struct {
	Updated string `json:"updated"`
	Source  string `json:"source"`
	Plugins []struct {
		Name             string `json:"name"`
		Spec             string `json:"spec"`
		InstalledVersion string `json:"installedVersion"`
		DefaultEnabled   bool   `json:"defaultEnabled"`
		Bundle           string `json:"bundle"`
	} `json:"plugins"`
}

// seedState 注入留痕。
type seedState struct {
	Done    bool     `json:"done"`
	Seeded  []string `json:"seeded"`
	Skipped []string `json:"skipped"`
	At      int64    `json:"at"`
}

// seedProfileDirOverride 测试注入（默认空 = 真实 ~/.dsh/profiles/<name>）。
var seedProfileDirOverride string

// seedProfilePath ~/.dsh/profiles/<name>/package.json。
func seedProfilePath() string {
	if seedProfileDirOverride != "" {
		return filepath.Join(seedProfileDirOverride, "package.json")
	}
	return filepath.Join(profileRootDir(), "profiles", seedProfileName, "package.json")
}

// seedProfileNodeModules 注入目标 node_modules（测试可注入同目录）。
func seedProfileNodeModules() string {
	if seedProfileDirOverride != "" {
		return filepath.Join(seedProfileDirOverride, "node_modules")
	}
	return filepath.Join(profileRootDir(), "profiles", seedProfileName, "node_modules")
}

// profileRootDir ~/.dsh。
func profileRootDir() string {
	return filepath.Join(os.Getenv("USERPROFILE"), ".dsh")
}

// seedDefaultPlugins 启动入口（后台）：等 DSH profile 就绪后按离线源/在线回退注入。
// 幂等——bundles 已含直接跳过；不阻塞启动。
func (a *App) seedDefaultPlugins() {
	resumeLog("seedDefaultPlugins: start")
	defer func() {
		if r := recover(); r != nil {
			resumeLog("seedDefaultPlugins panic: %v", r) // 失败留痕兜底，不崩启动
		}
	}()

	// 1) 等 DSH web profile 出现（懒人包场景 DSH 首启才建 profile；外部 DSH 已在跑则立即可用）
	if !waitProfileReady(60 * time.Second) {
		resumeLog("seedDefaultPlugins: profile not ready (skip; retry next launch)")
		return
	}

	// 2) 定位离线源
	dir, manifest, err := locateSeedOffline()
	if err == nil && manifest != nil {
		seedFromOffline(dir, manifest)
		return
	}
	resumeLog("seedDefaultPlugins: no offline source (%v); online fallback", err)
	seedOnlineFallback()
}

// waitProfileReady 轮询 profile package.json 出现（上限 timeout）。
func waitProfileReady(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(seedProfilePath()); err == nil {
			return true
		}
		time.Sleep(2 * time.Second)
	}
	return false
}

// readSeedManifest 读 plugins-offline/manifest.json（容忍 UTF-8 BOM——PowerShell
// Set-Content -Encoding UTF8 会写 BOM，Go json.Unmarshal 直接失败）。
func readSeedManifest(dir string) (*seedPluginManifest, error) {
	data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return nil, err
	}
	data = []byte(strings.TrimPrefix(string(data), "\uFEFF"))
	var manifest seedPluginManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, err
	}
	if len(manifest.Plugins) == 0 {
		return nil, fmt.Errorf("empty plugin manifest")
	}
	return &manifest, nil
}

// locateSeedOffline 查找安装目录旁的 plugins-offline（exe 同级；懒人包 DSH 运行时同层）。
// 返回 (离线目录, 解析后的 manifest, error)。
func locateSeedOffline() (string, *seedPluginManifest, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", nil, err
	}
	candidates := []string{
		filepath.Join(filepath.Dir(exe), seedOfflineDirName),                // 安装根
		filepath.Join(filepath.Dir(exe), "dsh-runtime", seedOfflineDirName), // 懒人包运行时旁
		filepath.Join(filepath.Dir(exe), "resources", seedOfflineDirName),   // 资源目录
	}
	for _, dir := range candidates {
		manifest, err := readSeedManifest(dir)
		if err != nil {
			continue
		}
		return dir, manifest, nil
	}
	return "", nil, fmt.Errorf("plugins-offline not found next to executable")
}

// seedFromOffline 用离线树注入（复制缺失条目 + 合并 bundles/dependencies + patch 启停）。
func seedFromOffline(dir string, manifest *seedPluginManifest) {
	offlineNm := filepath.Join(dir, "node_modules")
	profileNm := seedProfileNodeModules()
	skipped := []string{}
	seeded := []string{}

	for _, p := range manifest.Plugins {
		if profileHasBundle(p.Name) {
			skipped = append(skipped, p.Name)
			continue
		}
		if err := copyOfflineTree(offlineNm, profileNm); err != nil {
			resumeLog("seed %s: copy tree failed: %v", p.Name, err)
			continue
		}
		if err := mergeSeedBundles(p); err != nil {
			resumeLog("seed %s: merge bundles failed: %v", p.Name, err)
			continue
		}
		seeded = append(seeded, p.Name)
	}

	// 记忆插件默认 disabled（省 token；设置-记忆页一键启用，重启 DSH 生效）
	for _, p := range manifest.Plugins {
		row := memoryPluginRowID(p.Name) // 复用记忆插件的行 id 映射
		if row == "" {
			continue // 非记忆插件（agent-teams 等）默认启用 → 不写 disabled
		}
		if !p.DefaultEnabled {
			if err := patchMemoryRowDisabled(row, true); err != nil {
				resumeLog("seed patch %s disabled: %v", p.Name, err)
			}
		}
	}

	recordSeedState(seeded, skipped)
	resumeLog("seedDefaultPlugins done: seeded=%v skipped=%v", seeded, skipped)
}

// profileHasBundle 检查 profile package.json 的 dsh.profile.bundles 是否已含该插件。
func profileHasBundle(name string) bool {
	pj := readProfilePackageJSON()
	if pj == nil {
		return false
	}
	for _, b := range pj.Bundles {
		if strings.EqualFold(b, name) {
			return true
		}
	}
	for _, b := range pj.DSH.Profile.Bundles {
		if strings.EqualFold(b, name) {
			return true
		}
	}
	return false
}

// profilePackageJSON 读 profile package.json（bundles + dependencies）。
type profilePackageJSON struct {
	Bundles      []string          `json:"bundles"`
	Dependencies map[string]string `json:"dependencies"`
	DSH          struct {
		Profile struct {
			Bundles []string `json:"bundles"`
		} `json:"profile"`
	} `json:"dsh"`
}

func readProfilePackageJSON() *profilePackageJSON {
	data, err := os.ReadFile(seedProfilePath())
	if err != nil {
		return nil
	}
	var pj profilePackageJSON
	if json.Unmarshal(data, &pj) != nil {
		return nil
	}
	return &pj
}

// mergeSeedBundles 把插件追加进 profile package.json（bundles 去重 + dependencies 记版本）。
// 写回时保留原文件其余字段——用 map 承载后整体 Marshal。
func mergeSeedBundles(p seedPluginManifestPlugin) error {
	path := seedProfilePath()
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		return err
	}
	// dsh.profile.bundles 追加
	dshObj, _ := doc["dsh"].(map[string]any)
	if dshObj == nil {
		dshObj = map[string]any{}
		doc["dsh"] = dshObj
	}
	profObj, _ := dshObj["profile"].(map[string]any)
	if profObj == nil {
		profObj = map[string]any{}
		dshObj["profile"] = profObj
	}
	bundles := toStringSlice(profObj["bundles"])
	bundles = appendUniqueFold(bundles, p.Name)
	profObj["bundles"] = bundles
	// dependencies 追加（精确版本）
	deps, _ := doc["dependencies"].(map[string]any)
	if deps == nil {
		deps = map[string]any{}
		doc["dependencies"] = deps
	}
	if _, ok := deps[p.Name]; !ok && p.InstalledVersion != "" {
		deps[p.Name] = p.InstalledVersion
	}
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(out, '\n'), 0644)
}

// seedPluginManifestPlugin 便捷别名（避免超长类型名）。
type seedPluginManifestPlugin = struct {
	Name             string `json:"name"`
	Spec             string `json:"spec"`
	InstalledVersion string `json:"installedVersion"`
	DefaultEnabled   bool   `json:"defaultEnabled"`
	Bundle           string `json:"bundle"`
}

func toStringSlice(v any) []string {
	switch t := v.(type) {
	case []string:
		return t
	case []any:
		out := make([]string, 0, len(t))
		for _, e := range t {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

func appendUniqueFold(list []string, name string) []string {
	for _, e := range list {
		if strings.EqualFold(e, name) {
			return list
		}
	}
	return append(list, name)
}

// copyOfflineTree 把离线 node_modules 顶层条目复制进 profile node_modules：
// 跳过 @deepseek-ai 官方命名空间（profile 已有整套，保护 pnpm 布局）与已存在条目。
func copyOfflineTree(offlineNm, profileNm string) error {
	entries, err := os.ReadDir(offlineNm)
	if err != nil {
		return err
	}
	for _, e := range entries {
		name := e.Name()
		if name == ".package-lock.json" || name == ".bin" {
			continue
		}
		if strings.HasPrefix(name, "@") {
			// 处理 @scope/name
			sub, err := os.ReadDir(filepath.Join(offlineNm, name))
			if err != nil {
				continue
			}
			for _, se := range sub {
				pkg := name + "/" + se.Name()
				if strings.HasPrefix(pkg, "@deepseek-ai/") {
					continue // 官方依赖 profile 已有
				}
				dst := filepath.Join(profileNm, name, se.Name())
				if _, err := os.Stat(dst); err == nil {
					continue // 已存在（含 pnpm 链接）——不覆盖
				}
				if err := copyDirTree(filepath.Join(offlineNm, name, se.Name()), dst); err != nil {
					resumeLog("copyOfflineTree: %s failed: %v", pkg, err)
				}
			}
			continue
		}
		dst := filepath.Join(profileNm, name)
		if _, err := os.Stat(dst); err == nil {
			continue
		}
		if err := copyDirTree(filepath.Join(offlineNm, name), dst); err != nil {
			resumeLog("copyOfflineTree: %s failed: %v", name, err)
		}
	}
	return nil
}

// copyDirTree 递归复制目录（Go 标准库无 CopyDir）。
func copyDirTree(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		return os.WriteFile(target, data, 0644)
	})
}

// seedOnlineFallback 在线回退：无离线源且 bundles 缺 → dsh CLI add（需网络）。
// 仅尝试清单里已发布 npm 的插件；装完提示重启 DSH。
func seedOnlineFallback() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	dir := filepath.Join(filepath.Dir(exe), seedOfflineDirName)
	manifest, err := readSeedManifest(dir)
	if err != nil {
		return // 无清单 → 不知道装什么 → 保持现状（记忆插件走旧 preinstall 通道）
	}
	seeded := []string{}
	skipped := []string{}
	for _, p := range manifest.Plugins {
		if profileHasBundle(p.Name) {
			skipped = append(skipped, p.Name)
			continue
		}
		if !dshAvailable() {
			resumeLog("seedOnlineFallback: no usable dsh invocation (skip %s)", p.Name)
			continue
		}
		out, err := runDshPlugin("add", p.Name+"@"+p.InstalledVersion)
		if err != nil {
			resumeLog("seedOnlineFallback: %s failed: %v :: %s", p.Name, err, tail(out, 300))
			continue
		}
		seeded = append(seeded, p.Name)
	}
	recordSeedState(seeded, skipped)
	if len(seeded) > 0 {
		resumeLog("seedOnlineFallback done: seeded=%v (restart DSH to activate)", seeded)
	}
}

// recordSeedState 注入留痕（幂等快路径 + 状态查询）。
func recordSeedState(seeded, skipped []string) {
	path := filepath.Join(reasonixDataDir(), seedStateFileName)
	st := seedState{Done: true, Seeded: seeded, Skipped: skipped, At: time.Now().UnixMilli()}
	_ = os.MkdirAll(reasonixDataDir(), 0755)
	data, _ := json.MarshalIndent(st, "", "  ")
	_ = os.WriteFile(path, data, 0644)
}

// SeedStatus 返回默认附加插件注入状态（前端设置页可选展示）。
func (a *App) SeedStatus() map[string]any {
	path := filepath.Join(reasonixDataDir(), seedStateFileName)
	st := seedState{}
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &st)
	}
	return map[string]any{
		"done":    st.Done,
		"seeded":  st.Seeded,
		"skipped": st.Skipped,
		"at":      st.At,
		"offline": seedOfflineFound(),
		"profile": seedProfilePath(),
	}
}

func seedOfflineFound() bool {
	_, _, err := locateSeedOffline()
	return err == nil
}
