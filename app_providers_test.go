package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// ===== 纯逻辑单测（不需要 DSH）=====

func TestProviderRouteName(t *testing.T) {
	cases := map[string]string{
		"OpenAI":              "openai",
		"T.Y.M. ModelHub":     "t-y-m-modelhub",
		"my provider/x":       "my-provider-x",
		"__weird__name__":     "weird-name",
		"":                    "provider",
		"  ":                  "provider",
		"Kimi-CN":             "kimi-cn",
	}
	for in, want := range cases {
		if got := providerRouteName(in); got != want {
			t.Errorf("providerRouteName(%q) = %q, want %q", in, got, want)
		}
	}
	// DSH 要求 route 能作为凭据记录 id 的段（小写连字符标识符）：不能有空格/大写/下划线。
	for _, bad := range []string{"A B", "ABC_DEF", "a.b"} {
		got := providerRouteName(bad)
		if strings.ToLower(got) != got || strings.ContainsAny(got, " _.") {
			t.Errorf("providerRouteName(%q) = %q 不是合法 route", bad, got)
		}
	}
}

func TestProviderProtocolFromRequest(t *testing.T) {
	cases := []struct{ format, kind, want string }{
		{"", "", protoOpenAICompletions},                        // 兜底
		{"openai", "", protoOpenAICompletions},                  // kind
		{"", "anthropic", protoAnthropicMessages},               // kind
		{"", "responses", protoOpenAIResponses},                 // kind
		{"anthropic-messages", "", protoAnthropicMessages},      // 精确协议名
		{"openai-responses", "", protoOpenAIResponses},          // 精确协议名
		{"", "claude", protoAnthropicMessages},                  // 别名
		{"", "openai-completions", protoOpenAICompletions},      // 精确优先
		{"bogus", "bogus", protoOpenAICompletions},              // 未知 → 兜底
	}
	for _, c := range cases {
		if got := providerProtocolFromRequest(c.format, c.kind); got != c.want {
			t.Errorf("providerProtocolFromRequest(%q,%q) = %q, want %q", c.format, c.kind, got, c.want)
		}
	}
}

func TestProfileFromProviderView(t *testing.T) {
	// baseURL 末尾的聊天路径要去掉（DSH 自己拼 /chat/completions）
	p := map[string]any{
		"name":        "OpenAI",
		"kind":        "openai",
		"apiKeyEnv":   "OPENAI_API_KEY",
		"baseUrl":     "https://api.openai.com/v1/chat/completions/",
		"models":      []any{"gpt-4o", "gpt-4o-mini", "gpt-4o"},
		"displayName": "OpenAI",
	}
	prof := profileFromProviderView(p, "", "")
	if got := strAt(prof, "baseURL"); got != "https://api.openai.com/v1" {
		t.Errorf("baseURL = %q, want https://api.openai.com/v1", got)
	}
	if got := strAt(prof, "api"); got != protoOpenAICompletions {
		t.Errorf("api = %q", got)
	}
	if got := strAt(prof, "apiKeyEnv"); got != "OPENAI_API_KEY" {
		t.Errorf("apiKeyEnv = %q", got)
	}
	models := providerModelsFromDSH(prof["models"])
	if len(models) != 2 { // 去重
		t.Errorf("models = %v, want 2 条", models)
	}

	// override 优先于视图里的值
	prof2 := profileFromProviderView(p, "https://gateway.example/v1", "anthropic")
	if got := strAt(prof2, "baseURL"); got != "https://gateway.example/v1" {
		t.Errorf("override baseURL = %q", got)
	}
	if got := strAt(prof2, "api"); got != protoAnthropicMessages {
		t.Errorf("override api = %q", got)
	}
}

func TestProfilePrefersRequestURLFromCurrentUI(t *testing.T) {
	// 回归：当前设置 UI 保存的是 requestUrl（baseUrl 是 legacy）。
	// 曾优先读 baseUrl → 用户在「模型服务」页改地址后保存被静默丢弃（实测 UI 测试抓到）。
	p := map[string]any{
		"name":       "probe",
		"baseUrl":    "https://old.example/v1",
		"requestUrl": "https://new.example/v1?ui=1",
		"chatUrl":    "https://new.example/v1?ui=1",
	}
	prof := profileFromProviderView(p, "", "")
	if got := strAt(prof, "baseURL"); got != "https://new.example/v1?ui=1" {
		t.Errorf("baseURL = %q, want 以 requestUrl 为准 https://new.example/v1?ui=1", got)
	}
	// 只有 legacy baseUrl 时仍要能用（兼容旧配置）
	legacy := profileFromProviderView(map[string]any{"name": "p", "baseUrl": "https://legacy.example/v1"}, "", "")
	if got := strAt(legacy, "baseURL"); got != "https://legacy.example/v1" {
		t.Errorf("legacy baseUrl = %q", got)
	}
	// 显式 override 优先级最高（添加/编辑时的地址覆盖）
	over := profileFromProviderView(p, "https://override.example/v1", "")
	if got := strAt(over, "baseURL"); got != "https://override.example/v1" {
		t.Errorf("override = %q", got)
	}
}

func TestProviderModelsConversion(t *testing.T) {
	dsh := providerModelsToDSH([]string{"a", "b", "a", "  "})
	if len(dsh) != 2 {
		t.Fatalf("providerModelsToDSH 去重/去空失败: %v", dsh)
	}
	if m, ok := dsh[0].(map[string]any); !ok || m["id"] != "a" {
		t.Fatalf("providerModelsToDSH 形状错误: %v", dsh[0])
	}
	for _, in := range []any{
		[]any{map[string]any{"id": "x"}, "y"},
		[]string{"x", "y"},
	} {
		got := providerModelsFromDSH(in)
		if strings.Join(got, ",") != "x,y" {
			t.Errorf("providerModelsFromDSH(%T) = %v, want [x y]", in, got)
		}
	}
}

func TestProviderPresetCatalog(t *testing.T) {
	seen := map[string]bool{}
	for _, p := range providerPresetCatalog {
		if p.ID == "" || p.Label == "" || p.Route == "" {
			t.Errorf("预设字段不全: %+v", p)
		}
		if seen[strings.ToLower(p.ID)] {
			t.Errorf("预设 id 重复: %s", p.ID)
		}
		seen[strings.ToLower(p.ID)] = true
		if p.API != protoOpenAICompletions && p.API != protoOpenAIResponses && p.API != protoAnthropicMessages {
			t.Errorf("预设 %s 的协议 %q 不是 DSH 支持的协议", p.ID, p.API)
		}
		if p.BaseURL == "" {
			t.Errorf("预设 %s 缺少 baseURL", p.ID)
		}
		// route 必须能通过 DSH 的 id 规则
		if got := providerRouteName(p.Route); got != p.Route {
			t.Errorf("预设 %s 的 route %q 不规范（会变成 %q）", p.ID, p.Route, got)
		}
	}
	if len(providerPresetCatalog) == 0 {
		t.Fatal("预设目录为空")
	}
	if _, ok := lookupProviderPreset("openai"); !ok {
		t.Error("lookupProviderPreset(openai) 未命中")
	}
	if _, ok := lookupProviderPreset("不存在的预设"); ok {
		t.Error("lookupProviderPreset 对未知 id 应返回 false")
	}
}

func TestProviderKindsMatchMockContract(t *testing.T) {
	// 值取自 v1.38.2 lib/bridge.ts 的 mock：providerKinds: ["openai","anthropic","responses"]
	want := []string{"openai", "anthropic", "responses"}
	if strings.Join(providerKinds, ",") != strings.Join(want, ",") {
		t.Errorf("providerKinds = %v, want %v（前端自定义供应商表单按这些值渲染）", providerKinds, want)
	}
	for _, k := range providerKinds {
		if providerProtocolFromRequest("", k) == "" {
			t.Errorf("kind %q 无法映射到协议", k)
		}
	}
}

func TestMergeModelIDsAndCapabilityViews(t *testing.T) {
	got := mergeModelIDs([]string{"b", "a"}, []string{"a", "c"})
	if strings.Join(got, ",") != "a,b,c" {
		t.Errorf("mergeModelIDs = %v", got)
	}
	views := capabilityViews([]string{"m1"}, "configured", map[string]bool{"m1": true})
	if len(views) != 1 {
		t.Fatalf("capabilityViews 长度 = %d", len(views))
	}
	v := views[0].(map[string]any)
	if v["model"] != "m1" || v["state"] != "supported" || v["source"] != "configured" {
		t.Errorf("capabilityView 字段不契约: %v", v)
	}
	mods, _ := v["inputModalities"].([]any)
	if len(mods) != 2 {
		t.Errorf("已知视觉模型的 inputModalities 应含 image: %v", mods)
	}
}

func TestUnknownPresetIsRejectedNotFaked(t *testing.T) {
	// 未知预设必须报错返回（不静默造一个假配置）——这是"不猜端点"原则的守卫。
	a := newTestApp()
	got := a.AddProviderConnectionWithOptions("no-such-preset", "", "k", "", "")
	if !strings.Contains(got, "未知的供应商预设") {
		t.Errorf("未知预设应返回明确错误，实际 = %q", got)
	}
}

// ===== 对真实 DSH 的集成测试（需显式开启）=====
//
// 为什么需要它：写入路径依赖 DSH 的 settings 校验器与 path 语义，
// 单测无法覆盖（例如"分段路径 vs 点号路径"这个坑就是实测才发现的）。
// 默认跳过，避免普通 go test 依赖外部进程；开启方式：
//
//	$env:DSH_LIVE_TEST=1; go test -run TestLiveProviderRoundTrip ./...
//
// 测试只写一个 example.invalid 的临时 route，并在结束前删除。
func TestLiveProviderRoundTrip(t *testing.T) {
	if os.Getenv("DSH_LIVE_TEST") != "1" {
		t.Skip("需要 DSH 在 127.0.0.1:3080 运行；设置 DSH_LIVE_TEST=1 开启")
	}
	a := &App{dsh: NewDshClient(3080), st: NewSettings()}

	const route = "dsh-live-probe"
	// 清理既有残留（上次异常退出）
	_ = a.deleteProviderProfile(route)
	defer func() { _ = a.deleteProviderProfile(route) }()

	profile := map[string]any{
		"displayName": "DSH live probe (临时)",
		"api":         protoOpenAICompletions,
		"baseURL":     "https://example.invalid/v1",
		"models":      providerModelsToDSH([]string{"probe-model-1", "probe-model-2"}),
	}
	if err := a.writeProviderProfile(route, profile); err != nil {
		t.Fatalf("写入供应商失败: %v", err)
	}

	profiles, err := a.providerProfiles()
	if err != nil {
		t.Fatalf("读回失败: %v", err)
	}
	got, ok := profiles[route]
	if !ok {
		t.Fatal("写入后读不到该供应商")
	}
	if strAt(got, "api") != protoOpenAICompletions {
		t.Errorf("api = %q", strAt(got, "api"))
	}
	if strAt(got, "baseURL") != "https://example.invalid/v1" {
		t.Errorf("baseURL = %q", strAt(got, "baseURL"))
	}
	models := providerModelsFromDSH(got["models"])
	if strings.Join(models, ",") != "probe-model-1,probe-model-2" {
		t.Errorf("models = %v", models)
	}

	// 校验器必须拒绝非法协议 —— 证明我们确实经过了 DSH 的校验，而不是自说自话写入。
	bad := map[string]any{"api": "bogus-protocol", "baseURL": "https://example.invalid/v1"}
	if err := a.dshSettingsMutate([]providerOp{{Op: "set", Path: []string{"providers", route + "-bad"}, Value: bad}}); err == nil {
		t.Error("非法协议应被 DSH 校验器拒绝，但写入成功了")
	}

	if err := a.deleteProviderProfile(route); err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	profiles, _ = a.providerProfiles()
	if _, ok := profiles[route]; ok {
		t.Error("删除后仍存在")
	}
}

// TestLiveProviderAutoResolvesModels 锁住一个实测发现的 DSH 约束的修复：
// 不在 DSH 内置目录里的路由**必须**在配置里列出 models，否则写入被拒
// （settings-rejected: "... resolves no models; the installed catalog does not describe this route"）。
// 添加连接时用户只填 key + 地址，所以写入被拒后必须用端点探测补齐模型再重试。
func TestLiveProviderAutoResolvesModels(t *testing.T) {
	if os.Getenv("DSH_LIVE_TEST") != "1" {
		t.Skip("需要 DSH 在 127.0.0.1:3080 运行；设置 DSH_LIVE_TEST=1 开启")
	}
	// 本机假端点：模型列表是确定的，测试不依赖外网。
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/models") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"auto-alpha"},{"id":"auto-beta"}]}`))
	}))
	defer server.Close()

	a := &App{dsh: NewDshClient(3080), st: NewSettings()}
	const route = "dsh-live-automodels"
	_ = a.deleteProviderProfile(route)
	defer func() { _ = a.deleteProviderProfile(route) }()

	// models 故意留空：模拟预设添加（用户不填模型）
	profile := map[string]any{
		"displayName": "DSH live automodels (临时)",
		"api":         protoOpenAICompletions,
		"baseURL":     server.URL + "/v1",
	}
	if err := a.writeProviderProfileResolvingModels(route, profile, ""); err != nil {
		t.Fatalf("写入失败（应通过端点探测自动补模型）: %v", err)
	}
	profiles, err := a.providerProfiles()
	if err != nil {
		t.Fatalf("读回失败: %v", err)
	}
	got := providerModelsFromDSH(profiles[route]["models"])
	if strings.Join(got, ",") != "auto-alpha,auto-beta" {
		t.Errorf("自动补齐的模型 = %v, want [auto-alpha auto-beta]", got)
	}

	// 负例：端点不可达时必须明确失败，且**不得**留下半成品配置
	badProfile := map[string]any{
		"displayName": "DSH live automodels bad (临时)",
		"api":         protoOpenAICompletions,
		"baseURL":     "http://127.0.0.1:9/v1",
	}
	if err := a.writeProviderProfileResolvingModels(route+"-bad", badProfile, ""); err == nil {
		t.Error("端点不可达时应当报错")
	}
	remaining, _ := a.providerProfiles()
	if _, ok := remaining[route+"-bad"]; ok {
		t.Error("探测失败时不应留下供应商配置")
	}
}

// TestLiveSettingsLatency 是设置快照的性能回归守卫。
//
// 为什么需要：前端每次保存/应用操作后都会 reload app.Settings()，期间 busy=true
// → 「保存更改」等按钮临时禁用；此前一次约 2.9s（重复的 session.list/session.models/
// settings.describe + 按 provider 逐个 credentials.describe），用户能明显感觉"点了没反应"。
// 优化后（app_settings_reads.go 的读取复用）应显著低于阈值。阈值取得宽松，只拦"明显退化"。
func TestLiveSettingsLatency(t *testing.T) {
	if os.Getenv("DSH_LIVE_TEST") != "1" {
		t.Skip("需要 DSH 在 127.0.0.1:3080 运行；设置 DSH_LIVE_TEST=1 开启")
	}
	a := newTestApp()
	a.dsh = NewDshClient(3080)

	// 预热（首次包含会话列表缓存预热等一次性开销）
	_ = a.Settings()

	const budget = 1500 * time.Millisecond
	start := time.Now()
	out := a.Settings()
	took := time.Since(start)
	if len(out) == 0 {
		t.Fatal("Settings() 返回空")
	}
	// 顺带确认关键载荷仍在（优化不能悄悄丢字段）
	for _, key := range []string{"providers", "providerPresets", "providerKinds", "webSearchModel", "agent", "network"} {
		if _, ok := out[key]; !ok {
			t.Errorf("Settings() 缺字段 %s", key)
		}
	}
	if took > budget {
		t.Errorf("Settings() 耗时 %v，超过预算 %v（读取复用被破坏？见 app_settings_reads.go）", took, budget)
	} else {
		t.Logf("Settings() 耗时 %v（预算 %v）", took, budget)
	}
}

// TestLiveProviderPresetViews 验证「模型服务」页拿到的预设目录是真实可用的视图。
func TestLiveProviderPresetViews(t *testing.T) {
	if os.Getenv("DSH_LIVE_TEST") != "1" {
		t.Skip("需要 DSH 在 127.0.0.1:3080 运行；设置 DSH_LIVE_TEST=1 开启")
	}
	a := &App{dsh: NewDshClient(3080), st: NewSettings()}

	views := a.providerPresetViews()
	if len(views) != len(providerPresetCatalog) {
		t.Fatalf("预设视图 %d 条，目录 %d 条（DSH 读不到时应返回空表，不该少给）",
			len(views), len(providerPresetCatalog))
	}
	ids := map[string]bool{}
	for _, raw := range views {
		v, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("预设视图类型错误: %T", raw)
		}
		id, _ := v["id"].(string)
		ids[id] = true
		for _, key := range []string{"id", "label", "description", "keyEnv", "providerNames", "models", "added", "keySet", "routeKind", "catalog"} {
			if _, present := v[key]; !present {
				t.Errorf("预设 %s 缺字段 %s（前端 ProviderPresetView 需要）", id, key)
			}
		}
		if routeKind, _ := v["routeKind"].(string); providerProtocolFromRequest(routeKind, "") == "" {
			t.Errorf("预设 %s 的 routeKind %q 无法映射协议", id, routeKind)
		}
	}
	for _, p := range providerPresetCatalog {
		if !ids[p.ID] {
			t.Errorf("预设 %s 未出现在视图里", p.ID)
		}
	}
}
