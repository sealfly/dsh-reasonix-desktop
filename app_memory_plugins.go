package main

// app_memory_plugins.go — 记忆插件管理桥（方案 A：集成 DSH 记忆插件生态）。
//
// 背景：DSH 原生无 memory.* RPC（探测全 404），记忆能力来自插件生态
// （dsh-1024store 有 memory 分类 186+ 插件）。本项目通过「设置-记忆」页
// 让用户自行安装 / 启用 / 卸载记忆插件（默认关闭），插件注册的 RPC 按
// 原则 1（通用透传）自然可用，桥只做展示与持久化适配。
//
// 插件安装位置：~/.dsh/profiles/web/package.json 的 dsh.profile.bundles +
// dependencies（dsh plugin --profile web add <spec> 写入）。
// 启用状态持久化：~/.reasonix/memory-plugins.json（默认禁用）。

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// memoryPluginsStateFile 启用状态持久化文件。
const memoryPluginsStateFile = "memory-plugins.json"

// memoryRecommend 推荐记忆插件（硬编码元数据，市场拉取失败时的兜底目录）。
var memoryRecommend = []map[string]any{
	{
		"id":      "@openviking/dsh-memory-plugin",
		"name":    "dsh-memory-plugin",
		"desc":    "整合长期记忆、知识检索与技能，为智能体提供自演进的上下文数据库（34.7k stars）。",
		"stars":   34782,
		"install": "dsh plugin --profile web add @openviking/dsh-memory-plugin",
		"owner":   "volcengine/OpenViking",
	},
	{
		"id":      "@vectorize-io/hindsight-coding-agents",
		"name":    "hindsight-coding-agents",
		"desc":    "可学习的 Agent 记忆：自动召回与沉淀的长期项目记忆、知识页、深度反思与按仓库隔离的记忆库（22k stars）。",
		"stars":   22032,
		"install": "dsh plugin --profile web add @vectorize-io/hindsight-coding-agents",
		"owner":   "vectorize-io/hindsight",
	},
	{
		"id":      "@memtensor/memos-local-plugin",
		"name":    "memos-local-plugin",
		"desc":    "持久化自进化记忆系统，混合检索与跨任务技能复用，显著节省 token（11k stars）。",
		"stars":   11127,
		"install": "dsh plugin --profile web add @memtensor/memos-local-plugin",
		"owner":   "MemTensor/MemOS",
	},
}

// memoryPluginState 启用状态持久化结构。
type memoryPluginState struct {
	Enabled   map[string]bool `json:"enabled"`
	UpdatedAt int64           `json:"updatedAt"`
}

var (
	memPluginMu    sync.Mutex
	memPluginState = &memoryPluginState{Enabled: map[string]bool{}}
	memStateLoaded = false
)

// memoryStatePath 启用状态文件路径（~/.reasonix/memory-plugins.json）。
func memoryStatePath() string {
	return filepath.Join(reasonixDataDir(), memoryPluginsStateFile)
}

// loadMemoryPluginState 读启用状态（惰性加载一次）。
func loadMemoryPluginState() {
	memPluginMu.Lock()
	defer memPluginMu.Unlock()
	if memStateLoaded {
		return
	}
	memStateLoaded = true
	data, err := os.ReadFile(memoryStatePath())
	if err != nil {
		return
	}
	var st memoryPluginState
	if json.Unmarshal(data, &st) == nil && st.Enabled != nil {
		memPluginState.Enabled = st.Enabled
		memPluginState.UpdatedAt = st.UpdatedAt
	}
}

// saveMemoryPluginState 持久化启用状态。
func saveMemoryPluginState() {
	memPluginMu.Lock()
	defer memPluginMu.Unlock()
	memPluginState.UpdatedAt = time.Now().UnixMilli()
	data, _ := json.MarshalIndent(memPluginState, "", "  ")
	_ = os.MkdirAll(reasonixDataDir(), 0755)
	_ = os.WriteFile(memoryStatePath(), data, 0644)
}

// memoryPluginEnabled 查询某插件是否启用。
func memoryPluginEnabled(id string) bool {
	loadMemoryPluginState()
	memPluginMu.Lock()
	defer memPluginMu.Unlock()
	return memPluginState.Enabled[id]
}

// dshProfileWebPath 本机 DSH web profile 的 package.json（插件注册处）。
func dshProfileWebPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".dsh", "profiles", "web", "package.json")
}

// installedMemoryBundles 读 profile package.json 的 bundles（已装插件集合）。
func installedMemoryBundles() ([]string, []string) {
	path := dshProfileWebPath()
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil
	}
	var pkg struct {
		Bundles []string `json:"bundles"`
		Deps    map[string]string `json:"dependencies"`
		DSH     struct {
			Profile struct {
				Bundles []string `json:"bundles"`
			} `json:"profile"`
		} `json:"dsh"`
	}
	if json.Unmarshal(data, &pkg) != nil {
		return nil, nil
	}
	bundles := pkg.Bundles
	if len(bundles) == 0 {
		bundles = pkg.DSH.Profile.Bundles
	}
	deps := make([]string, 0, len(pkg.Deps))
	for name := range pkg.Deps {
		deps = append(deps, name)
	}
	return bundles, deps
}

// memoryInstalledSet 返回已装插件名集合（bundles + dependencies）。
func memoryInstalledSet() map[string]bool {
	set := map[string]bool{}
	bundles, deps := installedMemoryBundles()
	for _, b := range bundles {
		set[strings.TrimSpace(b)] = true
	}
	for _, d := range deps {
		set[strings.TrimSpace(d)] = true
	}
	return set
}

// dshCliPath 定位 dsh 可执行（Harness Desktop 内置优先，再 PATH）。
func dshCliPath() string {
	// 内置：...dependencies\dsh\node_modules\.bin\dsh.cmd
	builtin := filepath.Join(os.Getenv("APPDATA"), "io.github.hairyf.deepseek-harness-desktop", "dependencies", "dsh", "node_modules", ".bin", "dsh.cmd")
	if _, err := os.Stat(builtin); err == nil {
		return builtin
	}
	// PATH 里的 dsh
	if p, err := exec.LookPath("dsh"); err == nil {
		return p
	}
	// 常见全局位置
	for _, p := range []string{
		filepath.Join(os.Getenv("APPDATA"), "npm", "dsh.cmd"),
		filepath.Join(os.Getenv("ProgramFiles"), "nodejs", "dsh.cmd"),
	} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// MemoryPlugins 返回设置-记忆页数据：推荐插件 + 已装状态 + 启用状态。
func (a *App) MemoryPlugins() map[string]any {
	loadMemoryPluginState()
	installed := memoryInstalledSet()
	recs := make([]any, 0, len(memoryRecommend))
	for _, r := range memoryRecommend {
		id := r["id"].(string)
		entry := map[string]any{
			"id":        id,
			"name":      r["name"],
			"desc":      r["desc"],
			"stars":     r["stars"],
			"install":   r["install"],
			"owner":     r["owner"],
			"installed": installed[id],
			"enabled":   memoryPluginEnabled(id),
		}
		recs = append(recs, entry)
	}
	// 本机已装的记忆类插件（含用户自装——与插件市场关联的检测）
	installedList := make([]any, 0)
	for id := range installed {
		installedList = append(installedList, map[string]any{
			"id":      id,
			"enabled": memoryPluginEnabled(id),
		})
	}
	return map[string]any{
		"recommended": recs,
		"installed":   installedList,
		"profilePath": dshProfileWebPath(),
		"dshCli":      dshCliPath(),
		"checkedAt":   time.Now().UnixMilli(),
	}
}

// MemoryPluginMarket 查询记忆插件市场（dsh-1024store memory 分类，与插件市场同源）。
// query 为空时按 category=memory；page 从 1 起。
func (a *App) MemoryPluginMarket(query string, page int) map[string]any {
	if page < 1 {
		page = 1
	}
	client := &http.Client{Timeout: 10 * time.Second}
	url := "https://api.deepseek1024.com/v1/plugins/search?category=memory&page=" + strconv.Itoa(page) + "&limit=20&sortBy=stars"
	if q := strings.TrimSpace(query); q != "" {
		url += "&q=" + strings.ReplaceAll(q, " ", "%20")
	}
	resp, err := client.Get(url)
	if err != nil {
		return map[string]any{"items": []any{}, "total": 0, "error": err.Error()}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return map[string]any{"items": []any{}, "total": 0, "error": "HTTP " + strconv.Itoa(resp.StatusCode)}
	}
	var doc struct {
		Total      int `json:"total"`
		TotalPages int `json:"totalPages"`
		Results    []struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			Owner       string `json:"owner"`
			URL         string `json:"url"`
			Stars       int    `json:"stars"`
			Install     string `json:"install"`
			Description struct {
				Zh string `json:"zh"`
				En string `json:"en"`
			} `json:"description"`
		} `json:"results"`
	}
	if json.NewDecoder(resp.Body).Decode(&doc) != nil {
		return map[string]any{"items": []any{}, "total": 0, "error": "decode failed"}
	}
	installed := memoryInstalledSet()
	items := make([]any, 0, len(doc.Results))
	for _, r := range doc.Results {
		items = append(items, map[string]any{
			"id":        r.ID,
			"name":      r.Name,
			"owner":     r.Owner,
			"url":       r.URL,
			"stars":     r.Stars,
			"install":   r.Install,
			"desc":      firstNonEmpty(r.Description.Zh, r.Description.En),
			"installed": installed[r.ID] || installed[r.Name],
		})
	}
	return map[string]any{"items": items, "total": doc.Total, "pages": doc.TotalPages}
}

// runDshPlugin 执行 dsh plugin 命令（带超时），返回尾部输出。
func runDshPlugin(args ...string) (string, error) {
	cli := dshCliPath()
	if cli == "" {
		return "", fmt.Errorf("dsh CLI not found (install DeepSeek Harness Desktop or add dsh to PATH)")
	}
	full := append([]string{"plugin", "--profile", "web"}, args...)
	cmd := exec.Command(cli, full...)
	cmd.Dir = filepath.Join(os.Getenv("USERPROFILE"), ".dsh", "profiles", "web")
	// dsh.cmd 是 cmd 批处理，直接 exec 在 Go 里用 cmd /c 包裹更稳
	if strings.HasSuffix(strings.ToLower(cli), ".cmd") {
		cmd = exec.Command("cmd", "/c", cli, strings.Join(full, " "))
		cmd.Dir = filepath.Join(os.Getenv("USERPROFILE"), ".dsh", "profiles", "web")
	}
	var buf strings.Builder
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	done := make(chan error, 1)
	if err := cmd.Start(); err != nil {
		return "", err
	}
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return tail(buf.String(), 2000), err
	case <-time.After(8 * time.Minute):
		_ = cmd.Process.Kill()
		return tail(buf.String(), 2000), fmt.Errorf("install timeout (8min)")
	}
}

// InstallMemoryPlugin 安装记忆插件（dsh plugin --profile web add <spec>）。
// 装完需重启 DSH（Harness Desktop）后插件 RPC 才注册。
func (a *App) InstallMemoryPlugin(spec string) map[string]any {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return map[string]any{"ok": false, "error": "empty plugin spec"}
	}
	out, err := runDshPlugin("add", spec)
	if err != nil {
		return map[string]any{"ok": false, "error": err.Error(), "output": out}
	}
	return map[string]any{"ok": true, "output": out, "restartHint": "插件已安装；重启 DSH (Harness Desktop) 后生效"}
}

// UninstallMemoryPlugin 卸载记忆插件（dsh plugin --profile web remove <spec>）。
func (a *App) UninstallMemoryPlugin(spec string) map[string]any {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return map[string]any{"ok": false, "error": "empty plugin spec"}
	}
	out, err := runDshPlugin("remove", spec)
	if err != nil {
		return map[string]any{"ok": false, "error": err.Error(), "output": out}
	}
	// 卸载时清除启用状态
	memPluginMu.Lock()
	delete(memPluginState.Enabled, spec)
	memPluginMu.Unlock()
	saveMemoryPluginState()
	return map[string]any{"ok": true, "output": out}
}

// SetMemoryPluginEnabled 启用/禁用记忆插件（默认关闭；持久化到 ~/.reasonix/memory-plugins.json）。
func (a *App) SetMemoryPluginEnabled(id string, enabled bool) map[string]any {
	loadMemoryPluginState()
	id = strings.TrimSpace(id)
	if id == "" {
		return map[string]any{"ok": false, "error": "empty plugin id"}
	}
	memPluginMu.Lock()
	if enabled {
		memPluginState.Enabled[id] = true
	} else {
		delete(memPluginState.Enabled, id)
	}
	memPluginMu.Unlock()
	saveMemoryPluginState()
	return map[string]any{"ok": true, "id": id, "enabled": enabled}
}

// tail 返回字符串末尾最多 n 字节。
func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

// firstNonEmpty 返回第一个非空串。
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
