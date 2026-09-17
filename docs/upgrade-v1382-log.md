# 前端升级执行记录：v1.31.4 → v1.38.2

> 执行日期：2026-09-17 ｜ 目标版本：`desktop-v1.38.2`（上游检出 `esengine-DeepSeek-Reasonix-f5745ba`）
> 前置评估：`docs/upgrade-assessment-v1388.md`（v1.38.8 存档评估 + 本次决策修订）
> 结论：**升级完成并已实测运行**（启动无错、会话/事件流正常、子代理页注入成功）

---

## 0. 为什么是 1.38.2 而不是 1.38.8

| 维度 | v1.38.2（已采用） | v1.38.8（继续搁置） |
|---|---|---|
| 宿主调用模式 | `lib/bridge.ts` **304KB，运行时解析 `window.go?.main?.App`** —— 与 v1.31.4 同源 | 引入 `lib/desktopHost.ts` + `window.reasonixDesktop` + 协议版本 6 + digest |
| 契约文件 | **无**（不需要适配层） | `generated/desktopContract.generated.ts`，644 命令 |
| 边界校验 | **无** | `scripts/check-desktop-host-boundary.mjs` 禁 `window.go`/`wailsjs` |
| 右栏 tab 容器 | **无** `components/TabContainer/`，tabs 仍为内联渲染 | TabContainer + TabAddMenu |
| 事件通道 | **仍是 `agent:event`**（与我们 Go 侧 emit 一致） | 11 个事件名 |
| 新增宿主方法 | **32 个（移除 0）** | 相对 1.38.2 再增一批 |
| 「模型服务」独立页 | ✅ 已移除 `providers→models` 重定向（SettingsPanel.tsx L152） | ✅ |

**判定依据（源码级）**：`SettingsPanel.tsx` L152 `useState<SettingsTab>(initialTab ?? "general")`
+ L322 `tab === "providers"` 作为一等页签 —— 而 v1.31.4/v1.38.1 是
`initialTab === "providers" ? "models" : ...` 重定向（UI 无独立页签）。

---

## 1. 方法面缺口（本项目桥 524 方法 vs 1.38.2 前端 468 接口方法）

用 `scripts/bridge-surface-diff.js` 对照两版 `bridge.ts` 的接口方法面：

```
base   (v1.31.4): 436
target (v1.38.2): 468
target 新增: 32      移除: 0
我们桥期望但缺失: 102   （其中 v1.31.4 基线就缺 70，新增缺口 = 32）
```

**关键容错机制**（`bridge.ts` L1149-1153，决定升级可行性的核心事实）：

```js
export const app: AppBindings = new Proxy({}, {
  get(_t, prop) {
    const target = realApp() ?? getMock();
    const v = target[String(prop)];
    if (typeof v !== "function") return v;   // ← 缺失方法返回 undefined，不抛错
    ...
```

即：宿主缺方法时**启动不受影响**，只有真正调用到该功能时才在调用点 TypeError。
这正是本项目带着 70 个基线缺口仍能正常运行的机制，也说明「换 dist」不会整体崩。

### 1.1 1.38.2 新增的 32 个方法（分类）

| 类别 | 方法 | 影响 |
|---|---|---|
| **供应商/模型目录（本页核心）** | `FetchProviderModelCatalog`、`FetchProviderModelCatalogDraft`、`FetchAllProviderModelCatalogs`、`AddProviderConnection`、`AddProviderConnectionWithURL`、`AddProviderConnectionWithOptions`、`SetConnectionKey`、`TestProviderModel` | **「模型服务」页功能所必需** |
| 会话版本/恢复 | `GetSessionVersionState`、`SetActiveSessionVersion`、`RenameSessionHead`、`QuerySessionTakeover`、`TakeoverSession`、`RetrySessionRecovery`、`ReconcileRecoveryVersions` | 会话恢复相关页面 |
| worktree 合并 | `GetWorktreeStatus`、`InspectWorktreeMerge`、`PrepareWorktreeMerge`、`MergeWorktreeBack`、`FinalizeWorktreeMerge`、`ForkWorktreeForTab`、`CloseMergedWorktreeTab` | 隔离 worktree 合并流程 |
| shell 集成 | `InstallShellSupport`、`CancelShellInstall`、`SetShellPreference` | 设置-终端 |
| MCP | `MCPCapabilityMatrix`、`AnswerMCPInteractionForTab` | MCP 能力矩阵 |
| 其他 | `CheckRemotePlatform`、`PickRemoteIdentityFile`、`RegisterNavigationIntent`、`SetSessionExperience`、`SetWebSearchModel` | 按需 |

### 1.2 已知数据契约缺口（P3 待补）

1. **`settings.providerPresets` 未提供**：1.38.2 的 `SettingsPanel.tsx` L1409/L4944/L4959/L5106
   均读 `s.providerPresets`；我们 `Settings()` 只返回 `providers` / `officialProviders`。
   前端用 `asArray()` 兜底 → **不崩，但「模型服务」页的可添加预设列表为空**。
2. **`FetchAllProviderModels` 返回形状不符**：契约要求 `Record<string, string[]>`（按 provider 键），
   我们返回扁平 `[]any`。
3. **`FetchProviderModels` 契约**：`(p: ProviderView) => Promise<string[]>`，我们现在忽略入参、
   恒返回当前 modelsRefs（功能可用但不随入参变化）。
4. 若要让「模型服务」页真正可加连接/可测模型，需实现 §1.1 第一类 8 个方法。

---

## 2. 升级前置加固（P0）：让注入与品牌改造可重放

这次升级暴露了一个**此前就存在的真实缺陷**：项目有 8 处前端注入，但只有 2 个
（inline-editor、subagent-panel）有可重放的 apply 脚本，其余靠**手工内联**留在
已提交的 `frontend/dist/index.html` 里 —— 一旦换 dist 就会静默丢失。

### 2.1 发现：插件市场注入块是**语法错误**，从未执行

`scripts/dsh-plugin-market-inject.js` 第 1 行是：

```js
ce as T,clearLegacyThemePreference as U,h as V,...themePackKind as z};
```

这是某个 `export { ... }` 语句的**尾部残片**（推测是早期想给内联 module 的导出表打补丁），
被当成**普通 `<script>`** 内联进 index.html。`node --check` 判定：

```
SyntaxError: Unexpected identifier 'as'
```

**经典脚本一旦语法错误，整块都不执行** → 该块内的 `__DSH_PLUGIN_MARKET v3`（18KB 插件市场代码）
以及 `__DSH_ACT_WRAP` 兜底**从来没有生效过**。证据：`frontend/dist/index.html` 中
`__dshPluginMarketInjected` 出现 1 次（只在死块里），别处无副本。

处理：用 `scripts/dist-block-extract.js promote 3 scripts/dsh-plugin-market.js --slice ...`
把市场部分（可解析的自包含 IIFE）反向提取成源脚本，**丢弃 ACT_WRAP 残片**
（主路径已由 Go `StartTopicActivationImpl` 用 `runtime.EventsEmit("topic:activation")` 承担）。

### 2.2 发现：`dsh-agent-presets.js` 源脚本丢失

「Agent 预设」注入只在 dist 块 #6 与 `dist/assets/dsh-agent-presets.js`（两者内容还不一致）中存在，
`scripts/` 下无源文件。用 `dist-block-extract.js promote 6` 反向提取回源（含语法门禁）。

### 2.3 发现：`dsh-conn-banner.js` 源与内联版已漂移

dist 块 #5（9384B，实际生效）vs `scripts/dsh-conn-banner.js`（9193B）不一致 —— 谁新谁旧无记录。
本次以 **dist 为准**（它是实际运行的那个）。

### 2.4 新增的可重放工具链

| 脚本 | 作用 |
|---|---|
| `scripts/dist-inventory.js` | 盘点 dist 内联块 + 源脚本↔dist 标记对照，一眼看出「哪些注入只活在 dist 里」 |
| `scripts/dist-block-extract.js` | 从 dist 反向提取注入块为源脚本（`promote` 带**语法门禁**，语法不过拒绝写入） |
| `scripts/extract-branding.js` | 从既有 dist 固化品牌资产到 `branding/`（启动壳 logo、品牌 CSS、head 错误钩子、被 CSS 引用的 SVG） |
| `scripts/apply-branding.js` | **幂等**写回品牌改造（DSH-Reasonix 名称/启动位图/橙色深色 logo/启动光晕/标题错误钩子） |
| `scripts/apply-all-injections.js` | **清单驱动**（`scripts/injections.json`）内联全部注入，带**语法门禁**与唯一性断言，区域哨兵保证幂等 |
| `scripts/sync-upstream-dist.js` | 上游产物 → `frontend/dist` 一键同步并重放品牌+注入（含形态前置校验） |
| `scripts/bridge-surface-diff.js` | 两版 `bridge.ts` 方法面差集 + 与本地 Go 桥的缺口 |
| `scripts/injection-anchors.js` | 注入依赖的 DOM 锚点在目标前端里是否还存在（可平移 vs 必须改锚点） |
| `scripts/ensure-bom.js` | 保证 `.ps1` 带 UTF-8 BOM（PS 5.1 无 BOM 会按 GBK 读坏中文路径） |

**幂等性实测**（三次运行字节一致）：

```
apply-branding.js         run1 = run2 = run3  SHA256 7F213DEE152B9F08…
apply-all-injections.js   run1 = run2 = run3  SHA256 C383D4A8E5CAF145…
```

**语法门禁价值**：`apply-all-injections.js` 对每个源脚本先 `new Function(src)` 解析，
上面那个插件市场残片这类错误会在**构建时**直接炸出来，而不是静默不执行。

构建脚本已改为调用这两个统一 applier（`build-deploy.ps1` L45、`build-installer.ps1` L74）。

---

## 3. 升级执行

```powershell
# 1) 上游构建（在 upstream 检出内）
pnpm install --frozen-lockfile          # node v24.19.0 / pnpm 10.34.5，engines: node>=24, pnpm>=10<11
npx vite build                          # ✓ built in 1m 11s

# 2) 同步进本项目 + 重放品牌与注入
node scripts/sync-upstream-dist.js <upstream>\desktop\frontend\dist
#   [sync] ok boot-shell 启动壳 / 外部 module bundle / wails-spinner 锚点
#   品牌改造 5 项断言全过；注入语法门禁 8/8 过；注入区 130800 bytes

# 3) 构建部署
.\build-deploy.ps1                      # wails build ✓ / winres 资源 ✓ / 签名 ✓
```

dist 规模变化：535 文件 / 34.0MB → **410 文件 / 14.1MB**（顺带清掉了历史构建堆积的过期 hash 资产）。

### 3.1 DOM 锚点存活核对（升级前）

`scripts/injection-anchors.js` 对 1.38.2 源码（6.4MB ts/tsx/css）核对：agent-presets 4/4、
memory-plugins 2/2、subagent-panel 2/2、inline-editor 4/5（唯一"未命中"是
`.workspace-tree__row--active[data-workspace-path]` 这种复合选择器被检查器拆错，非真缺失）。

1.38.2 里右栏 tab 的 class 完全没变（`app-shell/WorkspaceDockRegion.tsx` L57/L81/L82）：
`workbench-dock__tabs` / `workbench-dock__tab` / `workbench-dock__tab--active` / `workbench-dock__tab-label`
—— 注意**渲染者从 `App.tsx` 搬到了 `src/app-shell/`**，我们是 DOM 选择器注入，因此不受影响。

### 3.2 运行实测（PID 57544，1.38.2 dist）

**启动无 JS 错误**：注入的 head 错误钩子会把 `ERR:`/`REJ:` 写进窗口标题，实测标题为
`DSH-ReasonixUI`，无错误标记。

**前端↔桥协作正常**（`%TEMP%\resume-debug.log`）：

```
Tabs: 38 sessions (29 archived filtered)
ListTabs called / Tabs called / Tabs: cache hit (38 sessions)
agent:event emit reasoning x1801 (tabId=session-30cbaa1d-…)
agent:event emit kind="tool_dispatch" / kind="tool_result"
```

即会话列表、事件流、回合推进都在跑。

**注入生效**（同一日志的 `frontend:` 诊断）：

```
11:14:47.674: frontend: [subagent-panel] no-tab-bar
11:14:49.679: frontend: [subagent-panel] tab-bar-found .workbench-dock__tabs
11:14:49.680: frontend: [subagent-panel] tab-injected .workbench-dock__tabs
```

「子代理」页 tab 在 1.38.2 DOM 上注入成功（2 秒补挂载重试逻辑按预期工作）。

---

## 4. 升级后回归修复：深色模式按别的按钮会「跳」

**现象**：深色模式下按任意其他按钮，主题/风格/会话宽度被重置。

**根因（源码级）**：前端有**两套不同键名**的主题契约，我们的载荷混用了：

| 契约 | 键名 | 读它的代码 | 我们的来源 |
|---|---|---|---|
| themeExperience | `themeMode` / `baseStyle` | `lib/themeExperience.ts` | `GetThemeExperience()` ✅ 正确 |
| 设置快照 | `desktopTheme` / `desktopThemeStyle` / `conversationWidth` / `desktopLanguage` / `sessionExperience` | `SettingsPanel.tsx` L200-210、`app-runtime/desktopPreferencesAdapter.ts` L19-26 | `Settings()` ❌ 曾只给第一套键名 |

链路：

```
SettingsPanel.tsx L173  normalizeSettingsView(await app.Settings())
                L202  normalizeThemePreference(s.desktopTheme)   ← 我们没给 → 缺失值
lib/theme.ts    L38   DEFAULT_THEME = "auto"                    ← 回落成这样
                L204  setThemeState("auto"); setThemeStyleState("graphite")
                L209  setConversationWidth(默认)
依赖数组        L210  [s?.conversationWidth, s?.desktopTheme, s?.desktopThemeStyle, ...]
```

即：**每次设置面板重读（点任意按钮都会触发 `reload`）就重放一次外观**，把用户选的值冲掉。
关键对照事实：`DesktopStartupSettings()` 一直给的是契约键名（14/14 完整）→
**启动时主题正确、之后才跳**，这正是它看起来像"升级引入的新 bug"的原因。

**为什么升级后才明显**：v1.38.2 新增了 `app-runtime/` 桌面偏好适配器
（`useDesktopPreferences` → `synchronizeDesktopPreferences` → `publish` →
`applyPreferencesAppearance`），设置快照的重读/重放时机远多于 v1.31.4 的 App.tsx 路径；
键名缺口本身是长期存在的。

**修复**：`app_settings.go` 的 `desktopPreferenceKeys()` 按 v1.38.2 `lib/types.ts` 给出契约键名，
`Settings()` 与 `DesktopStartupSettings()` **共用同一套值**（两处一致是防跳变的不变量），
旧键名保留兼容；补 `SetSessionExperience` + `settings.go` 的 `SessionExperience` 持久化。
回归守卫：`app_settings_theme_test.go` 锁住"两载荷键名齐全且取值一致 + 用户设置可往返"。

**可复用工具**：`scripts/settings-contract-check.js <types.ts>` 静态对照前端设置契约与
Go 返回键，列出"前端读到缺失值因而静默回落"的字段。本次结果：
`DesktopStartupSettings` 14/14；`Settings()` 曾缺 14 项，其中主题类 4 项已修，
其余属 P3（`providerPresets` / `webSearchModel*` / `network` / `agent` / `providerKinds` /
`autoApproveTools` / `bypass` / `visionModel` / `shadowedByPath` / `effectiveWebSearchModel`）。

> **教训（已入 PRINCIPLES 升级清单）**：宿主方法缺失会因 Proxy 返回 `undefined` 而"静默不崩"，
> 但**返回载荷里缺字段**同样静默——前端一律 `normalize*(undefined)` 回落默认值。
> 升级后除了对照方法面，还必须对照**载荷字段面**。

---

## 5. 「模型服务」页供应商管理（已实现）

### 5.1 数据源：DSH 的 `llm-pi-ai` settings 命名空间

DSH 的第三方供应商配置本来就在 `~/.dsh/settings.yaml`：

```yaml
llm-pi-ai:
  providers:
    xtoken:
      apiKeyEnv: XTOKEN_API_KEY      # 凭据引用 → .credentials.yaml
      api: openai-completions        # 见 5.3 协议枚举
      baseURL: https://xtoken.paylf.com/v1
      models: [{ id: z-image-turbo }, ...]
```

本桥**通过 DSH RPC 读写**（不直接改 YAML，避免绕开 DSH 的校验器与 revision）：

| RPC | 用途 |
|---|---|
| `settings.describe {}` | 读全部命名空间，取 `llm-pi-ai.providers` |
| `settings.mutate {ns, ops:[{op,path,value}]}` | 写：`op=set/unset`，`path` 为**分段数组** |

### 5.2 踩坑：路径必须分段，且写入必须读回校验

实测发现：`path: ["providers.<route>"]`（点号单元素）会返回 **ok=true 但实际不生效**；
改用 `path: ["providers","<route>"]` 才落盘。因此 `writeProviderProfile()` / `deleteProviderProfile()`
**每一次写入后都 read-back 校验**，失败即返回错误——只信 `settings.mutate` 的返回值会静默丢配置。

### 5.3 协议枚举由 DSH 校验器强制

`api` 只允许三者（`dsh-llm-pi-ai` 的 `supportedProtocols()`）：
`openai-completions` / `openai-responses` / `anthropic-messages`。
非法值会被拒绝并给出可读报文：

```
settings-rejected: $.providers.dsh-probe-tmp.api
  expected "openai-completions" | "openai-responses" | "anthropic-messages" but got "bogus-protocol"
```

另外**只有前两者支持从端点列举模型**（`LISTABLE_PROTOCOLS`）——`anthropic-messages`
无法自动拉模型，只能看已配置的 `models`（`TestProviderModel` 据此分两条判据）。

### 5.4 预设目录（`app_providers_presets.go`）

新增 16 个可一键接入的预设（DeepSeek API / OpenAI / OpenAI Responses / Anthropic /
Kimi 中·国际 / GLM / Z.AI / 通义 中·国际 / MiniMax / StepFun / SiliconFlow / OpenRouter /
Novita / 本机 Ollama）。设计取舍：

- 只收录**协议 + 端点 + key 环境变量**三元组明确的网关，不猜端点；
- `models` 留空：模型列表由 `FetchProviderModelCatalog` 从端点实时拉（或用户填），
  不把会过期的模型清单硬编码；
- `added` / `keySet` 是**实时**状态（route 是否在 settings 里、凭据是否已配置）；
- 未知预设 id **报错返回**而不是静默造一个假配置。

### 5.5 实现的方法

| 契约方法 | 实现要点 |
|---|---|
| `SaveProvider(p)` | ProviderView → DSH profile（kind→协议、baseUrl→baseURL、models[]→[{id}]），写 `providers.<route>` |
| `SaveProviderWithKey(p, key)` | 同上 + `credentials.set(apiKeyEnv, key)`；返回空串=成功、非空=警告 |
| `AddProviderConnection{,WithURL,WithOptions}` | 预设/复制/官方三种调用形态；route 唯一化；返回警告串 |
| `AddProviderPresetAccess` / `AddOfficialProviderAccess` | 复用上面（原先都是 `return nil` 的空 stub） |
| `DeleteProvider` / `RemoveProviderAccess{,es}` | unset profile（保留凭据，避免误删共用 key） |
| `RenameProviderConnections` | 改 `displayName`，保留 route 与凭据引用 |
| `SetConnectionKey(name, value)` | 按 route/displayName 找到 apiKeyEnv 后写/清凭据 |
| `FetchProviderModelCatalog{,Draft}` | 配置模型 ∪ 端点实时列表（拉取失败不影响返回） |
| `FetchAllProviderModelCatalogs` / `FetchAllProviderModels` | 后者形状修正为 `Record<string, string[]>` |
| `TestProviderModel(p, model, key)` | 端点可列举→必须在列表里；否则必须在配置里；不发起真实推理（会花钱且慢） |
| `SaveProviderModelCatalogs` | 批量保存模型选择，返回逐条警告 |
| `SetWebSearchModel` | 真写 DSH 的 `web-search-deepseek.model`（并读回校验） |
| `SetVisionModel` / `SetAgentParams` / `SetNetwork` / `SetCompactRatio` / `SetReasoningLanguage` / `SetAutoApproveTools` / `SetBypass` | 本项目桌面偏好（真实持久化） |

`Settings()` 同时补齐 **41/41 契约字段**（`scripts/settings-contract-check.js` 可回归验证），
其中联网搜索相关 6 个字段值来自 DSH 的 `web-search-deepseek`，`providerPresets`/`providerKinds`
来自 5.4。`autoApproveTools` 与 `bypass` 拆成两个独立开关（原先都写 yolo，界面无法分别反映状态）。

### 5.6 测试

- 单测：route 规范化、协议映射、profile 映射（含去掉 baseURL 尾部聊天路径）、models 双向转换、
  预设目录自检（id 唯一 / 协议合法 / route 规范）、`providerKinds` 对齐前端 mock 契约。
- **live 集成测试**（`DSH_LIVE_TEST=1`，默认跳过）：对真实 DSH 做 写入 → 读回 → 校验器拒绝非法协议 → 删除
  的完整往返，以及预设视图字段完整性。这条测试才覆盖得住"分段路径"这类只有真后端才暴露的坑。

---

## 6. 真界面自动化验证（UI 测试钩子）与它抓到的缺陷

### 6.1 为什么需要钩子

Wails v2 在创建 WebView2 环境时会**覆盖** `WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS`
（go-webview2 `preventEnvAndRegistryOverrides` 里的 `os.Setenv(..., additionalBrowserArgs)`），
所以 `--remote-debugging-port` 被上游封死、CDP 连不上（实测：环境变量设了，WebView2 进程
命令行里没有该 flag）。而"供应商能不能加、模型能不能拉"这类问题必须在真界面上点一遍，
于是给应用加了环境变量开关的 UI 测试钩子（`ui_test_hook.go`）：

| 端点（仅 127.0.0.1） | 作用 |
|---|---|
| `GET /health` | 就绪检查 |
| `POST /eval {"js"}` | 在页面里求值（**支持 await Promise**）并回传结果 |
| `POST /click {"selector"}` | 便捷点击 |

回传机制：Wails 的 `WindowExecJS` 没有返回值，所以注入的 JS 把结果经桥方法 `UiTestReport`
送回 Go，`/eval` 按 nonce 匹配。默认关闭，仅 `DSH_UI_TEST_PORT` 设置时启动。

配套工具：`scripts/ui-test-eval.js`（探针）、`scripts/fake-openai-provider.js`（本机假供应商端点，
返回固定 fake-alpha/fake-beta/fake-vl-vision）、`scripts/ui-test-provider-flow.js`（全链路测试）。

### 6.2 它抓到的 4 个真实缺陷（都已修）

1. **未知路由必须显式列出模型**：DSH 校验器拒绝 `models: []` 的供应商
   （`settings-rejected: … resolves no models; the installed catalog does not describe this route`），
   而"添加连接"时用户只填 key + 地址 → **预设添加会直接失败**。
   修：写入被拒且原因是缺模型时，用给定 key 去端点探测模型后重试；探测失败则明确报错
   （不写半个配置、不塞占位模型）。回归测试 `TestLiveProviderAutoResolvesModels`。
2. **`ProviderView.baseUrl` 恒为空** → 前端「刷新模型」按钮条件是
   `disabled={busy || fetching || !p.baseUrl || !providerIsConfigured(p)}`，
   于是**按钮永久禁用、地址栏空白**，用户根本没法拉模型。
   修：从 DSH profile 回填 `baseUrl`/`apiKeyEnv`/`keySet`/`configured`（并在测试里断言按钮可用）。
3. **保存时改的是 `requestUrl`，我们却优先读 `baseUrl`** → 用户在「模型服务」页改了地址再保存，
   **改动被静默丢弃**。证据：桥调用探针记录到
   `SaveProvider(baseUrl=…/v1, requestUrl=…/v1?ui=1)`，写入后 DSH 仍是旧值。
   修：映射优先级改为 `requestUrl` → `baseURL` → `baseUrl` → `chatUrl` → `modelsUrl`。
   回归测试 `TestProfilePrefersRequestURLFromCurrentUI`。
4. **测试侧也会骗人**（记下来避免重犯）：① `document.body.innerText` 断言会撞到会话区文本
   （我那句话的文本被当成界面提示）；② 程序化 `.click()` 对 React 受控 checkbox 不生效；
   ③ 设置面板只在挂载时拉一次设置，**复用已开面板会拿到陈旧供应商列表**（出现过"DSH 里没有的
   供应商显示在列表里"）；④ 清理若走很重的 `app.Settings()` 会在钩子 20s 超时里跑不完，
   导致残留累积 → 改为直接查 DSH。

### 6.3 最终结果

```
UI-FLOW OK (19/19)
```

覆盖：经应用自身桥方法添加供应商（并自动补齐模型）→ 打开设置-模型服务 →
精确选中该供应商 → 「刷新模型」可用并拉到假端点的 3 个模型 → 改动表单出现「未保存更改」→
「保存更改」可用并点击 → **DSH 侧 baseURL 变为带 ?ui=1 的新值**（读回校验）→
负例：端点不可达时卡片显示"没有自动获取到模型"（不静默）→ 清理无残留。

### 6.4 性能：`Settings()` 从 ~2.9s 优化到 ~0.5s（已修）

**问题**：`app.Settings()` 一次约 **2.9s**，而前端每次保存/应用操作后都会 reload 它
（reload 期间 `busy=true` → 「保存更改」等按钮临时禁用，用户感觉"点了没反应"）。

**原因：一趟快照里同一份数据被反复取**

| 重复点 | 次数 |
|---|---|
| `session.list`（DSH 对巨型会话要 0.5~1.3s） | 3 次（providerViews / officialProviderViews / webSearchState 各一次） |
| `session.models` | 3 次（同上） |
| `settings.describe` | 3 次（providerProfiles / providerPresetViews / webSearchState 各一次） |
| `credentials.describe` | **N+1 次**（`providerViewFromGroup` 对每个 provider 各查一次） |

**修法**（`app_settings_reads.go`）：新增一趟快照的读取缓存 `settingsReads`，
`Settings()` / `DesktopStartupSettings()` 内只取一次 session 列表、一次 `session.models`、
一次 `settings.describe`、一次批量 `credentials.describe`；`providerViews` /
`officialProviderViews` / `providerViewFromGroup` / `providerPresetViews` / `webSearchState`
都改为可复用缓存的 `*Reads(r)` 变体（非缓存调用点传 `nil`，行为不变）。

另外 `activeSessionID("")` 改为**优先读左侧任务栏的会话列表缓存**（`Tabs`，10s TTL）：
它本来就要那份 `session.list` 结果，且会话增删会 `invalidateTabsCache()`，语义不变 ——
这次改动把最慢的那个 RPC 从设置快照路径上彻底去掉了。

**实测**（同一台机器，DSH 已预热）：

| 测量点 | 优化前 | 优化后 |
|---|---|---|
| 前端（UI 钩子，连测 3 次） | 2948ms / 2558ms | **9ms / 7ms / 7ms** |
| Go 集成测试（`TestLiveSettingsLatency`，直接调用） | ~2.9s | **490ms** |

> 两个数字的差别来自会话列表缓存是否已预热（前端路径上 `Tabs` 早被 UI 调过）；
> 无论哪条路径都远低于原来的 2.9s。

**回归保障**：`TestLiveSettingsLatency`（live，预算 1.5s）拦"明显退化"，并顺带断言
`providers`/`providerPresets`/`providerKinds`/`webSearchModel`/`agent`/`network` 仍在，
防止为了提速把字段优化没了；载荷正确性另由 UI 全链路测试（19/19）端到端复核。

**关键设计约束**：**写路径不共用这个缓存** —— 写操作必须读新鲜数据（写完还要 read-back 校验），
所以 `settingsReads` 只在一趟快照内存在、绝不跨请求复用（否则会出现"写完读回还是旧值"的假成功）。

---

## 7. 后续（P3）待办

> 下列为**当前仍未做**的事项；已完成项（providerPresets、8 个 provider catalog 方法、
> `FetchAllProviderModels` 形状、14 个契约字段、`Settings()` 性能）见 §5 / §6.4。

1. **给其余注入脚本补 `LogFromFrontend` 诊断**（目前只有 subagent-panel 有）：
   让"锚点消失"这类升级回归能自动留痕，而不是靠肉眼或事后发现。
2. **`dsh-file-preview.js` 是否启用**：源脚本在、清单里禁用；启用前需确认与原生预览不冲突。
3. **可选：把「子代理」页在 TabContainer 时代（≥1.38.8）焊成官方 tab**（届时锚点最干净，
   按 PRINCIPLES 原则 6 类别 5 的硬要求执行）。
4. **注入脚本运行期诊断的 UI 化**：把 `resume-debug.log` 的成功/失败诊断做成设置页里的一行状态，
   用户能自查"某个注入是否还活着"。