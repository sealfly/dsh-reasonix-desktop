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
	"regexp"
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
	// 直接 exec dsh.cmd：Go 的 os/exec 在 Windows 上对 .bat/.cmd 自动经 cmd.exe
	// 正确转义包装——不要手动再包 cmd /c（手动包裹会因 cmd 引号规则吞掉参数，
	// 导致 dsh CLI 报 "--profile <name> is required"）。
	cmd := exec.Command(cli, full...)
	cmd.Dir = filepath.Join(os.Getenv("USERPROFILE"), ".dsh", "profiles", "web")
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

// SetMemoryPluginEnabled 启用/禁用记忆插件。
// 双层生效：
//  1) UI 状态持久化（~/.reasonix/memory-plugins.json，默认关闭）
//  2) 真正激活控制：profile 根 cordis.patch.yml 写/删 `- id: <行id>\n disabled: true`
//     （cordis 官方禁用语法——插件加载层即被 disabled，重启 DSH 后生效）
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
	// 真正控制：改根 patch（行 id 映射；未知 id 只持久化 UI 状态）
	row := memoryPluginRowID(id)
	if row != "" {
		if err := patchMemoryRowDisabled(row, !enabled); err != nil {
			return map[string]any{"ok": true, "id": id, "enabled": enabled, "warning": "UI 状态已保存，但插件层控制失败: " + err.Error()}
		}
	}
	return map[string]any{"ok": true, "id": id, "enabled": enabled, "restartHint": "重启 DSH (Harness Desktop) 后生效"}
}

// memoryPluginRowID 插件包名 → 根 patch 行 id。
func memoryPluginRowID(id string) string {
	row := map[string]string{
		"@openviking/dsh-memory-plugin":        "openviking-memory",
		"@vectorize-io/hindsight-coding-agents": "hindsight",
		"@memtensor/memos-local-plugin":        "memos-local-memory",
	}[id]
	if row != "" {
		return row
	}
	// 兜底：包名末段做行 id（memos 的 adapter 行以 memos-local-memory 命名）
	if strings.HasSuffix(id, "/dsh-memory-plugin") {
		return "openviking-memory"
	}
	return ""
}

// memoryBudgetsDefault 各记忆插件注入预算默认值（省 token 推荐值；与插件源码默认对齐）。
func memoryBudgetsDefault() map[string]any {
	return map[string]any{
		"openviking-memory": map[string]any{
			"recallTokenBudget":   2000,
			"profileTokenBudget":  10000,
			"recallMaxContentChars": 500,
		},
		"memos-local-memory": map[string]any{
			"recallEnabled":     true,
			"contextMaxChars":   6000,
		},
	}
}

// MemoryBudgets 返回当前记忆注入预算（根 patch config override，未设置则默认）。
func (a *App) MemoryBudgets() map[string]any {
	rows, err := memoryPatchRows()
	if err != nil {
		return memoryBudgetsDefault()
	}
	out := memoryBudgetsDefault()
	for _, row := range rows {
		if cfg, ok := row["config"].(map[string]any); ok && len(cfg) > 0 {
			base := out[row["id"].(string)]
			if bm, ok := base.(map[string]any); ok {
				for k, v := range cfg {
					bm[k] = v
				}
			}
		}
	}
	return out
}

// SetMemoryBudget 设置记忆注入预算（写根 patch config override，启用后生效）。
// budget: {"recallTokenBudget": 800, "profileTokenBudget": 5000, ...}
func (a *App) SetMemoryBudget(rowID string, budget map[string]any) map[string]any {
	if rowID == "" || len(budget) == 0 {
		return map[string]any{"ok": false, "error": "invalid args"}
	}
	// 只允许已知预算键（防任意 YAML 注入）
	allowed := map[string]bool{
		"recallTokenBudget": true, "profileTokenBudget": true,
		"recallMaxContentChars": true, "recallEnabled": true,
		"contextMaxChars": true, "captureEnabled": true,
	}
	clean := map[string]any{}
	for k, v := range budget {
		if allowed[k] {
			clean[k] = v
		}
	}
	if len(clean) == 0 {
		return map[string]any{"ok": false, "error": "no allowed keys"}
	}
	if err := patchMemoryRowConfig(rowID, clean); err != nil {
		return map[string]any{"ok": false, "error": err.Error()}
	}
	return map[string]any{"ok": true, "row": rowID, "restartHint": "重启 DSH 后生效"}
}

// memoryPatchRow 解析根 patch 的一条记录（- id: X / disabled / config）。
func memoryPatchRows() ([]map[string]any, error) {
	path := memoryRootPatchPath()
	if path == "" {
		return nil, fmt.Errorf("no home dir")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(data), "\n")
	rows := []map[string]any{}
	var cur map[string]any
	for _, ln := range lines {
		trim := strings.TrimSpace(ln)
		if trim == "" || strings.HasPrefix(trim, "#") || trim == "[]" {
			continue
		}
		indent := len(ln) - len(strings.TrimLeft(ln, " "))
		// 顶层条目 - id:
		if m := regexp.MustCompile(`^- id:\s*(.+)$`).FindStringSubmatch(trim); m != nil && indent == 0 {
			if cur != nil {
				rows = append(rows, cur)
			}
			cur = map[string]any{"id": strings.TrimSpace(m[1]), "config": map[string]any{}}
			continue
		}
		if cur == nil {
			continue
		}
		// config: 段
		if m := regexp.MustCompile(`^config:\s*$`).FindStringSubmatch(trim); m != nil && indent == 2 {
			continue
		}
		// config 内的键值
		if m := regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*):\s*(.*)$`).FindStringSubmatch(trim); m != nil && indent == 4 {
			cfg := cur["config"].(map[string]any)
			cfg[m[1]] = yamlScalar(m[2])
			continue
		}
		if m := regexp.MustCompile(`^disabled:\s*(true|false)$`).FindStringSubmatch(trim); m != nil && indent == 2 {
			cur["disabled"] = m[1] == "true"
		}
	}
	if cur != nil {
		rows = append(rows, cur)
	}
	return rows, nil
}

// yamlScalar 把 YAML 标量文本转成 Go 值（bool/int/string）。
func yamlScalar(s string) any {
	s = strings.TrimSpace(s)
	if s == "true" || s == "false" {
		return s == "true"
	}
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	return strings.Trim(s, `"'`)
}

// patchMemoryRowConfig 为根 patch 某行写入/合并 config 段（不存在则创建行）。
func patchMemoryRowConfig(row string, cfg map[string]any) error {
	path := memoryRootPatchPath()
	if path == "" {
		return fmt.Errorf("no home dir")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	content := string(data)
	// 1) 移除该行现有 config 段（config: 开头到下一个顶层条目）
	content = removeRowConfig(content, row)
	// 2) 构造 config 段
	var sb strings.Builder
	sb.WriteString("  config:\n")
	for _, k := range []string{"recallTokenBudget", "profileTokenBudget", "recallMaxContentChars", "recallEnabled", "contextMaxChars", "captureEnabled"} {
		if v, ok := cfg[k]; ok {
			sb.WriteString(fmt.Sprintf("    %s: %s\n", k, yamlValue(v)))
		}
	}
	// 3) 插入：行后（disabled 行后）
	rowRe := regexp.MustCompile(`(?m)^(\s*-\s*id:\s*` + regexp.QuoteMeta(row) + `\s*\n)(?:\s+disabled:\s*(?:true|false)\s*\n)?`)
	loc := rowRe.FindStringIndex(content)
	if loc == nil {
		// 行不存在：先禁用插入（保持默认关闭语义）
		content = rowReInsert(content, row)
		loc = rowRe.FindStringIndex(content)
		if loc == nil {
			return fmt.Errorf("cannot insert row %s", row)
		}
	}
	end := loc[1]
	content = content[:end] + sb.String() + content[end:]
	return os.WriteFile(path, []byte(content), 0644)
}

// rowReInsert 插入 "- id: X\n  disabled: true\n"（数组位置）。
func rowReInsert(content, row string) string {
	entry := fmt.Sprintf("- id: %s\n  disabled: true\n", row)
	if strings.Contains(content, "[]") {
		return strings.Replace(content, "[]", entry, 1)
	}
	return strings.TrimRight(content, "\n") + "\n" + entry
}

// removeRowConfig 移除某行后的 config 段（保留 id 行与 disabled 行）。
func removeRowConfig(content, row string) string {
	re := regexp.MustCompile(`(?m)^(\s*-\s*id:\s*` + regexp.QuoteMeta(row) + `\s*\n)((?:\s+disabled:\s*(?:true|false)\s*\n)?)((?:\s+config:\s*\n(?:\s+[A-Za-z_][A-Za-z0-9_]*:\s*.*\n)*)?)`)
	return re.ReplaceAllString(content, "$1$2")
}

// yamlValue 把 Go 值转 YAML 标量。
func yamlValue(v any) string {
	switch t := v.(type) {
	case bool:
		if t {
			return "true"
		}
		return "false"
	case int:
		return strconv.Itoa(t)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	default:
		return fmt.Sprintf("%v", v)
	}
}

// memoryRootPatchPath profile 根 cordis.patch.yml。
func memoryRootPatchPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".dsh", "profiles", "web", "cordis.patch.yml")
}

// patchMemoryRowDisabled 在根 patch 中确保某行处于指定 disabled 状态。
func patchMemoryRowDisabled(row string, disabled bool) error {
	path := memoryRootPatchPath()
	if path == "" {
		return fmt.Errorf("no home dir")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	content := string(data)
	re := regexp.MustCompile(`(?m)^\s*-\s*id:\s*` + regexp.QuoteMeta(row) + `\s*\n(?:\s+disabled:\s*(?:true|false)\s*\n)?`)
	content = re.ReplaceAllString(content, "")
	if disabled {
		// 顶层数组位置插入（替换空数组 []，或追加到已有内容）
		entry := fmt.Sprintf("- id: %s\n  disabled: true\n", row)
		if strings.Contains(content, "[]") {
			content = strings.Replace(content, "[]", entry, 1)
		} else {
			content = strings.TrimRight(content, "\n") + "\n" + entry
		}
	} else if !strings.Contains(content, "- id:") {
		// 启用后数组已无任何条目 → 恢复 []（防止 YAML 顶层解析为 null）
		content = strings.TrimRight(content, "\n") + "\n[]\n"
	}
	return os.WriteFile(path, []byte(content), 0644)
}

// ensureMemoryDefaultOff 首次启动确保三个记忆插件默认关闭（根 patch disabled）。
// 幂等：memory-plugins.json 的 defaultOffApplied 标记只应用一次，之后由用户开关控制。
func ensureMemoryDefaultOff() {
	loadMemoryPluginState()
	memPluginMu.Lock()
	if memPluginState.UpdatedAt != 0 && memPluginState.Enabled == nil {
		// 已初始化过（有开关记录）——不干预
		memPluginMu.Unlock()
		return
	}
	memPluginMu.Unlock()
	// 读标记（独立于 enabled map，避免误判）
	path := filepath.Join(reasonixDataDir(), "memory-plugins-default-off.json")
	if _, err := os.Stat(path); err == nil {
		return
	}
	for _, row := range []string{"openviking-memory", "hindsight", "memos-local-memory"} {
		_ = patchMemoryRowDisabled(row, true)
	}
	_ = os.MkdirAll(reasonixDataDir(), 0755)
	_ = os.WriteFile(path, []byte("{\"applied\":true}\n"), 0644)
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

// memoryPluginPrereq 插件的前置依赖（先装前置再装插件；与插件一起伴随项目预装）。
// @memtensor/memos-local-plugin 的 peerDependencies 要求 @deepseek-ai/cordis（DSH 编排框架，
// web profile 默认未装——headless profile 有 cordis.yml 为证）。
var memoryPluginPrereq = map[string][]string{
	"@memtensor/memos-local-plugin": {"@deepseek-ai/cordis"},
}

// ---------- 预装：记忆插件伴随项目安装（首次启动后台自动安装） ----------

// memoryPreinstallMark 预装标记文件（~/.reasonix/memory-plugins-preinstalled.json）。
const memoryPreinstallMark = "memory-plugins-preinstalled.json"

// memoryPreinstallState 预装状态。
type memoryPreinstallState struct {
	Done      bool              `json:"done"`
	Installed map[string]string `json:"installed"` // id -> 状态(ok/error摘要)
	At        int64             `json:"at"`
}

// preinstallMemoryPlugins 首次启动时后台安装推荐记忆插件（默认禁用，装完不启用）。
// 幂等：标记文件存在即跳过；失败记录到标记，下次启动重试失败的。
func (a *App) preinstallMemoryPlugins() {
	path := filepath.Join(reasonixDataDir(), memoryPreinstallMark)
	st := memoryPreinstallState{Installed: map[string]string{}}
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &st)
		if st.Done {
			return
		}
		if st.Installed == nil {
			st.Installed = map[string]string{}
		}
	}
	// 仅重试未成功的
	pending := make([]map[string]any, 0, len(memoryRecommend))
	for _, r := range memoryRecommend {
		id := r["id"].(string)
		if st.Installed[id] == "ok" {
			continue
		}
		pending = append(pending, r)
	}
	if len(pending) == 0 {
		st.Done = true
		st.At = time.Now().UnixMilli()
		data, _ := json.MarshalIndent(st, "", "  ")
		_ = os.WriteFile(path, data, 0644)
		return
	}
	// 后台逐个安装（不阻塞启动）；先装前置依赖，再装插件本身
	go func() {
		cli := dshCliPath()
		installOne := func(spec string) string {
			if cli == "" {
				return "dsh CLI not found"
			}
			if out, err := runDshPlugin("add", spec); err != nil {
				return tail(err.Error()+" :: "+out, 200)
			}
			return "ok"
		}
		for _, r := range pending {
			id := r["id"].(string)
			// 前置依赖（未装才装）
			installedSet := memoryInstalledSet()
			for _, pre := range memoryPluginPrereq[id] {
				if installedSet[pre] {
					continue
				}
				if preStatus := installOne(pre); preStatus != "ok" {
					st.Installed[id] = "prereq " + pre + " failed: " + preStatus
					continue
				}
			}
			if st.Installed[id] != "" {
				continue // 前置失败已记录
			}
			spec := strings.TrimPrefix(r["install"].(string), "dsh plugin --profile web add ")
			if spec == "" {
				spec = id
			}
			st.Installed[id] = installOne(spec)
		}
		allOK := true
		for _, s := range st.Installed {
			if s != "ok" {
				allOK = false
				break
			}
		}
		st.Done = allOK
		st.At = time.Now().UnixMilli()
		data, _ := json.MarshalIndent(st, "", "  ")
		_ = os.MkdirAll(reasonixDataDir(), 0755)
		_ = os.WriteFile(path, data, 0644)
	}()
}

// MemoryPreinstallStatus 预装进度（前端设置-记忆显示）。
func (a *App) MemoryPreinstallStatus() map[string]any {
	path := filepath.Join(reasonixDataDir(), memoryPreinstallMark)
	st := memoryPreinstallState{Installed: map[string]string{}}
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &st)
	}
	return map[string]any{
		"done":      st.Done,
		"installed": st.Installed,
		"at":        st.At,
	}
}
