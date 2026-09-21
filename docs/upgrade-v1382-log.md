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

### 6.5 「审批模式」按钮反应慢（询问/自动/yolo）—— 已修

**现象**：Composer 里切换审批模式（询问/自动/yolo）后要等 1~3.6s 才生效。

**定位方法（用 UI 测试钩子逐个量桥调用）**：

| 点一次按钮实际触发的调用 | 耗时 |
|---|---|
| `SetToolApprovalModeForTab`（按钮自身） | **2~4ms** —— 按钮不慢 |
| `MetaForTab`（点完必调） | **553~1569ms** |
| `ContextUsageForTab`（点完必调） | **495~2069ms** |
| `EffortForTab` | 4~7ms |

链路来自 1.38.2 的 `useController.setToolApprovalModeForTab`：
`app.SetToolApprovalModeForTab(...)` → `refreshMetaForTab(tabId)` →
`MetaForTab` + 并行 `ContextUsageForTab`/`EffortForTab`。

**根因**：`MetaForTab` 与 `ContextUsageForTab` 都走 `findSession(tabID)` → **`fetchSessions()`**
→ 裸调 `session.list`（DSH 对巨型会话 0.5~1.3s），于是点一次按钮付了 **2 次** 这个代价。
`fetchSessions()` 被 8 处调用（项目树、历史、上下文用量、meta…），是普遍性浪费。

**修法**（沿用项目既有的 `tabsCache` 模式）：
- `app_tree.go` 新增 `sessionsCache`（`session.list` 原始结果，**TTL 3s**）；
- 失效统一挂在 `invalidateTabsCache()` 上 → create/rename/archive/git 等既有调用点自动覆盖；
- 另外给 `session.prompt`（发消息）补上失效：刚提交时会话 projections 马上变，
  下一次读取应拿新值。失效测试：`TestSessionsCacheInvalidation` / `TestSessionsCacheTTL`。

**实测（同探针、同会话）**：

| 方法 | 优化前 | 优化后 |
|---|---|---|
| `MetaForTab` | 553~1569ms | **225ms（首次填充）→ 3ms / 2ms** |
| `ContextUsageForTab` | 495~2069ms | **3ms / 3ms / 2ms** |
| 点一次按钮合计 | 1~3.6s | **约 5ms** |

**TTL 取 3s 的取舍**：空闲点击秒回；回合进行中最多 3s 陈旧（界面本就按事件流持续刷新），
同时把 DSH 的重复投影计算压到每 3s 最多一次。回归：Go 全量测试 + UI-FLOW 19/19 + DOM-TEST 37/37 全绿。
---

## 7. 仓库完整性：dist 资产静默丢失（2026-09-20 修复）

**症状**：同步远端新提交后，别的会话新增的门禁 `scripts/verify-dist-assets.js` 在本机 **exit 1**——
磁盘上 555 个 dist 文件，git 只跟踪 140 个，**415 个未跟踪（23.3 MB）**，其中包括
`assets/monaco/vs/**`（143 个）、`provider-icons/**`（35 个）与 1.38.2 主入口
`assets/index-oFeqbpn3.js`。

**根因**：`.gitignore` 第 7-8 行的 `frontend/dist/`（注释写"构建产物不提交"）。
`d72a110`（v1.31.4→v1.38.2）换 dist 时，被忽略的路径不会进 `git add -A`，
于是新 chunk 只存在于构建机磁盘——**任何干净 checkout 首屏 JS 缺失，永久卡「加载中」**，
而构建机上一切正常、肉眼无从察觉。

**修法**：

1. 补实物：`git add -f frontend/dist` → 415 个全部入库（380 个 09-17 构建产物 +
   35 个 09-08 provider-icons 图标包；后者按 id 拼路径在运行时加载，静态引用扫描会把它
   误报成孤儿，实为必需资产。与 `d72a110` 的 291 个删除项重叠的 143 个正是无哈希的 monaco
   路径，同名不同内容，属正常替换）。
2. 修根因：**删掉 `frontend/dist/` 忽略规则**，改为注明「必须入库 + 事故溯源」的注释。
   此后新增 chunk 会直接以 untracked 暴露在 `git status`，配合门禁硬失败，
   不再依赖「人工记得加 `-f`」这种约定。
3. 同步订正四处因之失效的表述：门禁未跟踪提示（不再要求 `-f`，并把它变成对规则回退的探测）、
   PRINCIPLES P6 检查清单、`LOGO-NOTES.md`、`MIGRATION.md`。

**验证**（全部实测，非纸面推演）：

| 判据 | 结果 |
|---|---|
| `git ls-files frontend/dist` vs 磁盘 | **555 / 555** |
| `node scripts/verify-dist-assets.js` | **exit 0**（入口 39、闭包 291、入口缺失 0、未跟踪 0）|
| `git archive HEAD frontend/dist` 解出 vs 磁盘 | 逐名一致 **555/555**，index.html 的 39 个入口缺失 0 |
| 远端实测（GitHub API 读远端 tree） | frontend/dist 555、monaco 143、provider-icons 35、主入口在 |
| 三端 tree 哈希 | 本地 = CNB = GitHub = `bd21cf71` |
| 同步进来的上游代码 | `go build ./...` exit 0；`go test ./...` ok 46.5s |

**防复发**：门禁已接进两个构建脚本（任何缺失即中止构建）；`.gitignore` 不再忽略 dist；
PRINCIPLES P6 检查清单保留「dist 资产完整且可复现」一项。

### 7.1 追加：byte 级可复现（同日继续挖）

补齐资产后，用**真正的干净 clone** 复验时发现更隐蔽的一层：**「提交字节 ≠ 运行字节 ≠ clone 字节」三方不一致**。
根因是仓库级 `core.autocrlf=true` 且此前没有 `.gitattributes`：

| 失真 | 实测 |
|---|---|
| blob 被归一化，工作区不是 | **132 个文件**（index.html + 131 个 monaco 等）工作区是 CRLF、blob 是 LF；因 index 的 stat 缓存，`git status` 一直显示干净 |
| clone 检出又被反向改写 | `frontend/dist/**` 42 个文件在 clone 里变成 CRLF；index.html 从 160446 → 161569 字节（多 1123 个 CRLF）|

修法：`frontend/dist/** -text`（关闭 EOL 转换、保留文本 diff）+ `git add --renormalize frontend/dist`
把现有 **原始字节**重新入库 → dist 现存字节成为唯一权威版本，任何平台/任何 autocrlf 设置的
clone 都逐字节相同。

接着验证「clone 重放构建链 == 提交产物」，又抓出两处 EOL 漂移（内容逐字符一致，纯换行）：

| 步骤 | 漂移 | 修法 |
|---|---|---|
| 注入（`apply-all-injections.js`）| 载荷脚本换行直接进入 index.html：构建机上 6 个载荷是 LF、4 个是 CRLF，clone 侧却是清一色 CRLF → 重放多出 1015 个 CRLF | `scripts/dsh-*.js`、`scripts/injections.json` 钉 `text eol=lf`，归一化工作区，重生成（160446 → 158702）|
| 品牌（`apply-branding.js`）| 内联 `branding/brand.css` 与 `head-error-hook.js`，其换行随 autocrlf 变化 → 品牌区 8 行漂移 | 同样钉 `text eol=lf`，重生成（158702 → 158694，CRLF=0）|

**最终验证**：

| 判据 | 结果 |
|---|---|
| clone A（默认 autocrlf=true）重放构建链 | index.html **与构建机逐字节相同**，`git status` **0 项变更** —— 重放完全复现提交产物 |
| clone B（`core.autocrlf=false`）重放 | index.html 与构建机逐字节相同；另有 **130 项**差异，全部落在 `frontend/dist/assets/monaco/**` |
| 门禁 / 注入自检 / DOM 回归 | exit 0 / `--check` 幂等 / DOM-TEST 37/37 |
| 内容等价性 | 重生成前后归一化换行后**逐字符一致**（纯 EOL 变更，无语义变化）|

**已闭合（同日）**：`third_party/monaco` 也已钉住 —— `.gitattributes` 加 `third_party/** -text`
（该树只有 monaco，143 个纯文本文件，无二进制损坏风险；写成目录级规则是为了将来新增 vendor
资产默认就 byte-exact），归一化工作区到 LF 后重跑 `apply-monaco-vendor.js` 同步 130 个文件。
逐文件比对：原始 SHA 变化 130 个、**归一化换行后变化 0 个**（纯 EOL）。

**最终结论**：两个全新 clone（`core.autocrlf=true` 与 `false`）各自重放「品牌 + monaco + 注入」
三件套后，`git status` **均为 0 项变更**、index.html 与构建机逐字节相同 —— 即
「clone → 重放 → 产物 == 提交产物」在两种换行配置下都成立。

---

## 7.2 安装包重建：同步进来的提交把构建打断了（2026-09-20）

背景：`e80d810`（内置 skill 随包分发）之后没人重建过安装包，所以它引入的两个缺陷一直没暴露。

**症状**：`makensis` 在经典安装包步骤直接失败 —— `Bad text encoding: project.nsi:109`
（109 行正是第一处中文注释）。

**根因 1（BOM 丢失）**：`e80d810` 用会丢 BOM 的编辑器重写了 `project.nsi`。`git cat-file` 实测：
`e80d810~1` BOM=True（10474 字节）→ `e80d810` BOM=False（10648 字节）。makensis 对含非 ASCII
的脚本要求 UTF-8 BOM，否则报上面那条晦涩错误 —— 即**从 e80d810 起安装包构建一直是坏的**。

**根因 2（路径错误）**：同一提交新增的 `File /r "skills"` 路径也不对。NSIS 的 `File` 按
**脚本所在目录**解析（与同文件里的 `OutFile "..\..\bin\..."`、`File /r "plugins-offline"`
同一约定），而脚本在 `build\windows\installer\`，那里并没有 `skills\` —— 仓库根的 skills 是
`..\..\..\skills`。只修 BOM 的话这一步会以 `no files found` 硬失败；即便侥幸放过，内置 skill
也进不了安装包（`app_skill_seed.go` 找的正是 exe 同级的 `skills\`）。

**修法**：补 BOM（`node scripts/ensure-bom.js`）+ 路径改 `..\..\..\skills` + 在
`build-installer.ps1` 的 makensis 之前加**编码守卫**（校验前 3 字节是 UTF-8 BOM，缺失即中止
并打印修复命令）—— 把「人记得补 BOM」换成机器判定，与 `verify-dist-assets.js` 同一思路。

**验证（全部实测）**：

| 判据 | 结果 |
|---|---|
| `makensis -V4` 直接证据 | 日志出现 `Descending to: "..\..\..\skills\dsh-std-plugin-gen\"` 与 `File: "SKILL.md" [compress] 1972/4017 bytes` |
| `no files found` | 0 行 |
| 经典安装包 | **101 MB**，签名 **Valid**，已复制到桌面 |
| 懒人包（`-Bundle`）| **177.7 MB**，签名 **Valid**，已复制到桌面 |
| exe 发布校验 `verify-packaged-app.js` | **14/14**（注入内联、桥方法、Monaco、就地编辑器铺满规则）|
| appliers 幂等 | 重建后 `git status` 只剩本轮 3 处预期改动，dist 未被改写 |
| 安装包本体标记扫描 | 0/14 —— 属预期：内层 exe 被 NSIS 压缩，标记不可直搜；由上面两条 + 同一轮内 `Sign exe → makensis` 的顺序保证 |

**教训**：`edit` 类工具会丢 BOM（本仓库已记录同类事故），所以「改完 `.ps1`/`.nsi` 必须
`ensure-bom.js` 补回」不能只写在清单里，得有门禁兜住 —— 本次已加。巡检顺带确认
`prepare-dsh-runtime.ps1` 虽缺 BOM，但**纯 ASCII（非 ASCII 字节数 0）**，无需处理。

---

## 7.3 重建后的真机实测（2026-09-20，安装包 → 安装 → 运行 → 功能）

**方法**：把经典安装包静默装到临时目录（`/S /D=<tmp>`，非 Program Files、无需提权），用
`DSH_UI_TEST_PORT=9310` 启动**装好后的 exe**，通过 `/eval`、`/click` 驱动**真实界面**；模型链路
另起假端点 `scripts/fake-openai-provider.js`(9411) 配合。全程用真机结果判定，不靠读代码。

| 判据 | 结果 |
|---|---|
| 静默安装 | 退出码 0，44.3 秒；DSH 后端分区探测到 3080 已占用 → 走「已存在，跳过」，**未触发 npm 安装** |
| 安装落地 | `$INSTDIR\{DSH 客户端, skills, plugins-offline, start-dsh.cmd, uninstall.exe}` |
| **skills 落地** | `skills\dsh-std-plugin-gen\SKILL.md` 5741B/104 行，与仓库 **SHA 一致** ← NSIS 路径修复的实证 |
| 首屏 | `readyState=complete`、`.boot-shell` 已消失、`#root` 有子节点、正文 30k 字符 |
| 前端注入 | DOM 里 **8 个** `dsh-inject:*` 脚本块；`__DSH_SUBAGENT_PANEL__`/`__DSH_MEMORY_PLUGINS__`/`__DSH_VERSION_MANAGE__`/`__DSH_INLINE_EDITOR__`/`__DSH_UPDATE_BANNER__` 全为 true |
| 关键资产 | 主入口 `assets/index-oFeqbpn3.js`、`monaco/vs/loader.js`、`editor.main.js`、`provider-icons/openai.svg` 全部 **HTTP 200** |
| 设置页 | 20 个页签齐全；**MCP 与工具 / 远程 SSH / Agent Skills / 子智能体 / 插件 / 记忆 / 诊断 / 权限** 8 个页面逐个切到、页面组件（`settings-page--mcp|remote|skills|...`）出现且正文非空 |
| 供应商链路 | `scripts/ui-test-provider-flow.js` **19/19**（真实界面点选 → 刷新模型 → 勾选 → 保存 → 读回 DSH 校验 → 清理）|
| Monaco | 页内真实加载 loader + `editor.main`：**91 种语言**、编辑器高度 600 = 容器 600、4 行/25 高亮 token、行号与取值回读正确 ← LF 归一化后资产完好 |
| 就地编辑器 | 点「✏️ 编辑」进入编辑态：覆盖层 **768 = 父容器 768**（原「只剩上半部」形态不存在）、Monaco 665px/37 行/**386** token，按钮切为「退出编辑」，Esc 可退出 |
| 技能播种 | app 首启后 `~/.dsh/skills/dsh-std-plugin-gen/{SKILL.md,.dsh-seeded}` 出现，与安装包内文件 **SHA 一致**；app 内 `SkillsSettings` 里能看到该技能，**Harness 自身的技能目录也实时出现了它** |
| 桥延迟 | `Settings` 14ms、`RemoteHosts` 1ms、`MCPServers` 656ms（桥共 478 个方法）|

**这轮暴露并修掉的工具缺陷**（已写进 `ui_test_hook.go` 注释，防下次再踩）：

1. **测试标记陈旧会让「点错元素」伪装成「点击无效」**（我先误判成 React 不理 `el.click()`，已更正）：
   先用一次 `/eval` 打 `data-uit-nav` 标记、再单独调 `/click` 时，若上一轮标记没清掉，`querySelector`
   命中的是**文档序靠前的旧元素**（实测：想点「MCP 与工具」，实际一直在点「模型服务」，现场表现就是
   "页签点了不切换"）。随后做对照实验（同一目标轮流用四种方式：原生 `el.click()`、`pointerdown+click`、
   `mousedown+click`、五事件序列）——**四种全部正确切换且 3 秒内不漂移**，证明 `el.click()` 本身没问题，
   错的是标记。`/click` 端点已回归最简的原生 `click()`（避免一次点击产生多个事件），注释写明
   "打标记前先清 `[data-uit-*]`，或干脆在同一次 `/eval` 里定位并点击"。
2. **调不存在桥方法会静默挂死**：写成 `App.Skills()`（真名是 `SkillsSettings`）时返回的是**永不 settle**
   的 Promise，`/eval` 只能报「求值超时（20s）」，完全看不出原因；探针须先判 `typeof … === 'function'`。
   桥共 478 个方法，先 `Object.keys` 列一遍最稳。
3. 另记：`wails build` 要求 `go` 在 PATH 上（本会话 `C:\Go\bin` 不在），否则报
   `unable to find compiler: go` —— 与代码无关，属环境。

**截图取证的坑（同一轮，值得复用）**：

- **`PrintWindow(PW_RENDERFULLCONTENT)` 对本应用的主界面状态会返回旧帧**：四个不同界面状态截出
  **逐字节相同**的 PNG（哈希一致、71 色桶、24KB），而设置页状态却能拿到真实帧 —— 只靠它取证会
  "看起来截到了，其实是上一张"。
- **`SetForegroundWindow` 在后台进程里常被系统拒绝**（窗口不在前台时抓屏通道直接是空帧）；
  改用 `SetWindowPos(HWND_TOPMOST)`（**不需要抢焦点**）把窗口临时提到最上层再抓屏，成功率高得多，
  抓完 `HWND_NOTOPMOST` 复位。
- 因此截图工具做成**双通道 + 自校验**：PrintWindow 与前台抓屏各取一帧、按色桶数择优，并输出
  `尺寸/方法/是否前台/色桶数/主色占比`；调用侧再校验「**截图前后的界面状态一致**」+「**帧哈希不与
  已交付的图重复**」—— 正是这两条把上面的废图挡了下来（某一轮 10 张里挡掉 3 张，宁可少交付也不交付错图）。

---

## 8. 后续（P3）待办

> 下列为**当前仍未做**的事项；已完成项（providerPresets、8 个 provider catalog 方法、
> `FetchAllProviderModels` 形状、14 个契约字段、`Settings()` 性能）见 §5 / §6.4。

1. **给其余注入脚本补 `LogFromFrontend` 诊断**（目前只有 subagent-panel 有）：
   让"锚点消失"这类升级回归能自动留痕，而不是靠肉眼或事后发现。
2. **`dsh-file-preview.js` 是否启用**：源脚本在、清单里禁用；启用前需确认与原生预览不冲突。
3. **可选：把「子代理」页在 TabContainer 时代（≥1.38.8）焊成官方 tab**（届时锚点最干净，
   按 PRINCIPLES 原则 6 类别 5 的硬要求执行）。
4. **注入脚本运行期诊断的 UI 化**：把 `resume-debug.log` 的成功/失败诊断做成设置页里的一行状态，
   用户能自查"某个注入是否还活着"。

---

## 9. 桥方法参数错位：从「远程页签打不开」查到 57 处并全部修掉

### 9.1 现象与追问

用户看到右栏除「概览 / 文件 / 改动 / 子代理」外**多了一个「远程」页签**，问它从哪来。查证结论：

- **是上游 1.38.2 自带的、条件渲染的页签**，bundle 里原文：
  `[s&&!o&&DockTab{i("rightDock.overview")}] [DockTab{i("workspace.filesTab")}] [DockTab{i("workspace.changedTab")}] [a&&DockTab{i("rightDock.remote")}]`
  —— 只有条件 `a` 为真才渲染，视图体是远程工作区组件。
- **条件为什么成立**：`~/.reasonix/remote-hosts.json` 里有 **2 台主机**（`桌面虾` root@172.16.65.70 等）。
  没配主机就不会出现该页签。
- **不是本项目的注入**：`scripts/dsh-*.js` 里没有任何创建"远程/ssh"页签的代码，注入只加「子代理N」
  （DOM 里 class 是 `dsh-sp-tab workbench-dock__tab`，与上游的纯 `workbench-dock__tab` 可分）。

**但该页签的内容是坏的**：面板里写着
`加载失败: Error: error parsing arguments: received 2 arguments to method 'main.App.ListRemoteDir', expected 0`
—— 前端按 2 个实参调用（懒加载 chunk `RemotePanel-*.js`），而 Go 侧那四个方法还是
`app_stubs2.go` 里的**零参占位桩**；Wails 绑定按参数个数校验，直接拒绝。

### 9.2 顺着这条线查出 57 处同类错位

同一类问题**远不止这 4 个方法**。新增 `scripts/bridge-arity-check.js`（前端调用实参个数 ↔
Go 签名形参个数）后，真实 UI 可达路径上共查出 **57 处**错位，覆盖会话、任务、收件箱、
记忆、主题包、供应商、提交链路等。

- 写完第一版检查器时得到 62 处，其中若干是**假阳性**：切分实参时把 `<` `>` 当括号（Go 泛型用），
  于是箭头函数 `f(e=>e.name)` 与比较运算把深度算错。修正为 **JS/Go 两套切分规则**后，
  `AddProviderConnectionWithOptions`、`RenameProviderConnections` 等 5 处假错位消失 ——
  **若不先修检查器，就会去"修"本来正确的代码**。
- 也确认了开发态 mock 桥（`bridge-*.js`）自身签名偏松（`HooksSettings` 在 mock 里是 0 参、
  前端却传 1 参），所以**判定标准取"真实 UI 调用点"**，不取 mock。

### 9.3 修法（保留诚实语义，不造假数据）

| 类别 | 数量 | 处理 |
|---|---|---|
| 零参占位桩 | 49 | `scripts/align-stub-arity.js` 按前端实参个数补齐形参（`any`），行为仍为"安全空实现" |
| 已实现但签名不符 | 8 | 人工按调用语义改：`AttachDropped`、`SavePastedImage`、`AttachmentDataURL`、`ResolveWorkspacePathForTab`、`ReportCrash`、`UpgradeDeepSeekProviderAccess`、`RecordUIPerf`、`ResizeTerminalForTab` |
| 参数**顺序**颠倒 | 1 | `SubmitInvocationsToTabWithID`：前端是 `(tabID, display, input, invocations, submissionId)`，Go 是 `(tabID, display, invocations, input)` —— 不仅少一个，第 3/4 个还是反的，会把 input 对象当 `[]any` 解码 |
| 有意例外 | 1 | `Cancel`：真实 UI 从不零参调用它；Go 的 `Cancel(tabID)` 是 `CancelForTab` 的基础，改 0 参会破坏按标签页取消。已写进检查器的**已审阅例外表**并附原因 |

本轮顺带**真实现**了两条链路（原来是静默失效的空桩）：

- **粘贴图片**：`SavePastedImage(dataURL)` 落盘到 `~/.reasonix/attachments/` 并返回路径，
  `AttachmentDataURL(path)` 反向转 data URL 供预览；`AttachDropped(path)` 返回
  `{kind,path,isDir,displayPath,previewUrl}`（目录 → `kind:"workspace"`，前端据此加工作区引用）。
- **远程文件读写**（`app_remote_files.go`）：列目录 = 一次 ssh 会话来跑 POSIX sh 循环
  （`ls -A` 逐行读，带空格的目录名不会被拆；`stat -c %Y` → 退回 `stat -f %m` 兼容 BSD）；
  读文件 = 一次往返先给 mtime/大小再给正文（NUL 判二进制、2MiB 截断标记）；
  写文件 = 正文走 stdin 给 `cat >`，传了 expectedMtime 时先比对、不一致返回 `conflict` 而不是冲掉别人的修改。
  `OpenRemoteWorkspace` 仍**明确报错**（远端 serve 本项目不做）。

### 9.4 真机验证

- **逐方法真机调用 58 项**：`参数错位 0`。原先必失败的 57 个方法全部走到真实语义；
  `SavePastedImage` 真的落盘（`~/.reasonix/attachments/paste-….png`），
  `AttachDropped` 真的描述了 `C:\Windows\win.ini`。
- **探针自己踩的坑（记下来）**：`App.X([a,b])` 等于把整个数组当第 1 个实参，会得到
  "received 1 arguments … expected N" —— 那是**探针的错**，不是产品的错；必须 `App.X(...args)`。
  首轮 36 项"参数错位"全是这个原因，差点误报成产品缺陷。
- 远程方法在本机**连不上那台主机**（`172.16.65.70:22` 超时，用户确认测试机已下线），
  从 SSH 层返回的是诚实错误：
  `Warning: Identity file Clawbot@168 not accessible: No such file or directory`
  —— 这同时暴露该主机的 `identityFile` 填的是**文件名而非密钥路径**，建议用户修正。
  因此远程读写的**成功路径**改由离线测试覆盖（12 个用例，用可替换的执行钩子模拟远端 shell）。

### 9.5 又一个真机级的坑：`any` 形参 + JS `null` = Promise 永久挂起

`AnswerMCPInteractionForTab` 逐个实参试探后发现：**只要某个 `any` 形参收到 `null`/`undefined`，
Wails 绑定既不 resolve 也不 reject**（无任何报错，调用方永远等下去）。而前端真实调用恰恰是
`AnswerMCPInteractionForTab(tabID, interactionID, action, r.content ?? null)` —— **默认就传 null**。

改为 `map[string]any` 后 null 正常解码为 nil、REJECT 正常返回。同时扫了真实 UI 的全部调用点，
确认没有别的地方把字面 `null`/`undefined` 传给桥方法（命中的 5 处全是 Monaco/vendor 内部函数），
所以本轮批量补的 `any` 形参没有引入挂死风险。

> 结论沉到工具里：检查器把"任何 `any` 形参 + 调用点可能传 null"列为需人工确认项，
> 避免下一次批量改签名时把"抛错"换成"静默挂死"（后者更难查）。

### 9.6 结果

```
bridge-arity-check:  真实 UI 错位 0、仅 mock 错位 0、已审阅例外 1
go build / go vet:   exit 0
go test ./...:       ok（含新增远程文件 12 例 + 附件 6 例）
真机逐方法调用:       OK 57 / 参数错位 0 / 1（MCP 交互应答按设计返回明确错误）
安装包重建:          安装包 101MB / 懒人包 177.7MB，签名 Valid，verify-packaged-app 14/14
```

---

## 10. 「第 x 个问题」导轨（questionNav）失效：根因在后端与环境，修了三处

### 10.1 现象

用户反馈：对话栏右侧的「第 x 个问题」波浪快捷栏**失效**（点位悬停显示「第 {n} 个问题（点击加载）」，点击无反应）。

### 10.2 先确认它是什么

它是本应用自己的 `questionNav`（中文文案在 `frontend/dist/assets/zh-DrhyLgkz.js`：
`questionNav.notLoaded = "第 {n} 个问题（点击加载）"`、`progress = "问题 {current} / {total}"`），
组件是 `TranscriptQuestionNavigator` + `QuestionJumpBar`，数据来自 transcript store：

- 点位总数 `totalQuestions = max(历史总轮次, 已载入提问数)`，`≥2` 才渲染；
- 每个点位 `data-turn = 提问序号-1`，`data-loaded` 由「该提问是否在已载入条目里」决定；
- 点未加载的点位 → 走 `requestOlder` 分页；点已加载的 → 本地跳转。

### 10.3 根因链（真机实测）

| # | 事实 | 证据 |
|---|---|---|
| 1 | 应用需要 DSH 后端，配置 `127.0.0.1:3080` | `DshConnStatus() → connected:false` |
| 2 | 3080 被**另一个要求 token 的 DSH 实例**占着 | `TestDshConn(3080) → rpc session.list bad response: invalid character 'u'`（unauthorized） |
| 3 | 本应用客户端**没有 token 支持**，结构上连不上它 | `type DshClient struct{ host; port; http }` |
| 4 | 应用自己的「启动 DSH」也起不来：`DshLaunch` 写死 3080、忽略配置端口 | 代码 `app_dsh_conn.go`（旧实现） |
| 5 | 即便去起，全局 `dsh web` 也**必然失败** | `cannot resolve profile bundle "dsh-tauri" from … ~/.dsh/profiles/web` |
| 6 | 因为该 profile 里 9 个 `dsh-tauri*` 是**死链** | junction 指向 `%LOCALAPPDATA%\Deepseek Harness Desktop\resources\node_modules\…`，而该目录已随 Harness Desktop 更新/卸载消失（resources 里只剩一个 `.bak`） |
| 7 | 于是应用没有会话/历史数据 → 导轨无点位或点位全"未加载"且点不动 | 现场 DOM：`jump-item` 25 个、`data-loaded` 全 false |

### 10.4 定位到的**我们自己的**两个解析缺陷（这才是"点了没反应"的直接原因）

用 `session.history` 的原始响应比对，发现 `sessionMessages` 读错了字段：

```jsonc
// DSH 真实事件：内容在 data.content（同级 data.role / data.id）
{"event":{"type":"user/message","seq":2360314,"data":{"content":[{"type":"text","text":"…"}],"role":"user","id":"…"}}}
```

- 旧结构只读 `data.message.content`（那是 assistant 的位置）→ **每条用户提问都被判为空串丢弃**；
- 连带 `mockHistoryPage` 的 `userCount` 恒为 0 → `totalTurns` 退化成"消息条数"（实测 25），
  导轨据此画 25 个点位，却没有任何一条对应真实提问；
- `hasOlder` 旧实现**硬编码 false** → 前端要点位未加载就走分页，直接被拒 → 点击无反应；
- 且 `turn` 用的是 DSH 会话级轮次号（实测同一 payload 全是 207/208…），与点位 `0..N-1` 对不上。

另外 DSH 的 `session.history` 一次只回**尾部一页**（实测 34248 事件、`hasMore:true`），
真实总轮次在 `projections.values.sessionStats.turns`（实测 211，而尾页只有 5 条提问）——
旧实现把尾页条数当总数。分页游标实测是 **`beforeSeq`**（`beforeSeq=2360310` 会返回更早的一页）。

### 10.5 三处修复

1. **`app_session_resume.go` 解析修正**：user/message 读 `data.content`（并回退 `data.message.content`）；
   提问按**顺序问题号**编号（而非 DSH 轮次号），使点位、问题文本、跳转三者对齐。
2. **真实总数 + 分页**：新增 `fetchHistoryWindow(sid, beforeSeq)`（带 `beforeSeq` 拉更早一页）、
   `historyPayload.sessionTotalTurns()`（取 `projections.sessionStats.turns`）、
   以及不透明游标 `encodeSeqCursor/decodeSeqCursor`；`HistorySliceForTab` 现在返回
   `totalTurns`=真实轮次、`startTurn`=本页首问的全局序号、`hasOlder`=`hasMore`、`nextCursor`。
3. **`DshLaunch` 重写（`app_dsh_conn.go`）**：尊重配置的 host:port（旧实现写死 3080）；
   端口被占但 ping 不通时**明确报错**而不是起个注定失败的进程；
   启动参数 `--profile <p> --no-open --port <port>`，profile 先试 `web`、
   失败自动回退最小组合 `tauri`；拉起后**轮询等待就绪**，失败时把子进程 stderr 尾部带出来。

### 10.6 真机验证

用应用**自带的 DSH**（`%APPDATA%\…\dependencies\dsh`，`0.1.1-rc.1`，实测**不要求 token**）
在 3092 起后端后：

```
DshLaunch() → {ok:true, started:true, port:3092, profile:"tauri"}   5.2s（web 因死链失败 → 自动回退 tauri）
DshConnStatus() → connected:true
```

导轨（同一会话，211 轮）：

| | 修复前 | 修复后 |
|---|---|---|
| 点位 | 25 个，**0 已加载** | **120 个**（覆盖 0…210，即真实 211 轮采样） |
| 已加载 | 0 | 5（尾部一页） |
| 点击已加载点 | 无反应 | **跳转生效**：scrollTop `5691 → 24` |
| 点击未加载点 | 无反应 | **分页生效**：已加载 3 → 5 |

新增测试：`app_session_history_test.go`（4 例：data.content 解析、真实总数/游标、游标往返、无 user 兜底）、
`app_dsh_launch_test.go`（5 例：启动参数用配置端口、profile 回退顺序、端口占用探测、stderr 取尾、坏 exe 快速失败）。

### 10.7 顺带修掉与仍未做

- 修：`ws_probe_test.go` 的哨兵写死 3080 且只做 TCP 可达判断 → 3080 被 token 实例占用时报**假失败**；
  现在跟随「连接设置」的 host:port，且只在 DSH **真的可用**（RPC 通）时才探测。
- **已修（本轮补做）**：注入的连接横幅 `#dsh-conn-banner` **连上后不自动消失**。详见 §11。
- **未做**：`~/.dsh/profiles/web` 里 9 条 `dsh-tauri*` 死链没有清理（那是 Harness Desktop 留下的，
  清理会影响它的 profile），所以「启动 DSH」目前靠回退 `tauri` 工作。
- **未做**：`DshClient` 仍无 token 支持，无法连接需要鉴权的 DSH。

---

## 11. 连接横幅「连上后不消失」修复

### 11.1 现场事实

`DshConnStatus()` 明明返回 `connected:true`（402ms），但 `#dsh-conn-banner` 仍是
`class=show / display:block / opacity:1` —— 一块持续误导用户的提示。

### 11.2 根因（旧实现）

```js
function check() {
  realApp().DshConnStatus().then(s => {
    if (!s.connected) { …showBanner()… }   // ← 只有"显示"分支，没有"隐藏"分支
  }).catch(() => {});
}
// 且 boot() 里只有 setTimeout(check, 4000)：**只查一次，没有轮询**
```

于是：启动时未连接 → 弹出 → 之后再也不会收（除非用户手点 ✕ 或整页刷新）。

### 11.3 修法

1. `check()` 变成对称：`connected` → `hideBanner()`；未连接 → 按 24h 抑制规则 `showBanner()`；
2. **周期轮询**：未连接 8s 一次（用户刚点完"启动 DSH"要尽快看到恢复），已连接 15s 一次
   （每次 `DshConnStatus` 真的会 ping 一次 DSH，实测 0.2~0.5s，成本可接受）；
3. **给桥调用加超时竞速**（`withTimeout`，10s）：Wails 桥上异常/不存在的方法可能返回
   **永不 settle 的 Promise**（本项目在 `ui_test_hook.go` 记过这个坑），不设防会让轮询永久停摆
   —— 实测症状正是"横幅显示后再也不会变"；
4. `showBanner()` 改为幂等（已在显示则不重建），否则轮询会把「正在启动 DSH 后端…」的进度文字清掉；
5. 窗口回到前台/获得焦点时立即复检一次；
6. 暴露诊断出口 `window.__dshBanner = {ticks, connected, timeout, at, pollMs}`，排障先看它。

### 11.4 真机验证（不刷新页面）

| 阶段 | 期望 | 实测 |
|---|---|---|
| 后端未连接 | 横幅出现 | `shown=true`，`ticks` 每 8s +1 |
| 外部拉起后端 | 横幅**自动消失** | **+10s** `shown=false, connected=true` |
| 再停掉后端 | 横幅**自动回来** | **+16s** `shown=true, connected=false` |

`performance.timeOrigin` 前后一致 → **全程没有页面重载**，证明是轮询生效而非刷新。

### 11.5 顺带：守卫与一次自伤

- 新增 `scripts/verify-conn-banner.js`：校验源码里的 hide/轮询/超时竞速/诊断出口等不变量，
  以及注入产物里横幅脚本**恰好 1 份且含修复标记**；做了**负向测试**（故意删掉 `hideBanner();`
  必须报失败）。
- **自伤记录**（避免重犯）：为做负向测试，我用 PowerShell `Get-Content -Raw` + `Set-Content`
  往返那个含中文注释的 `.js`，**PowerShell 5.1 按 ANSI 读 UTF-8**，把中文注释读成乱码、并把
  注释后的换行吞掉 → 源码被改坏（代码侥幸还在，注释已损）。恢复办法：**从注入产物
  `frontend/dist/index.html` 里把该脚本原样提取回来**，再归一化结尾换行（项目约定单个 LF）。
  证明恢复正确的方式：重新注入后 `index.html` 的 sha256 **逐字节等于损坏前那一份**
  （`8522956CE664FE54`）。
  → 教训：**不要用 PowerShell 读写含中文的 UTF-8 文件**；要么用编辑工具，要么用 Node。

---

## 12. 「历史项目与会话看不见了」：应用先于后端启动导致项目树停在空态

### 12.1 现场与排除

用户反馈：应用里看不到历史项目与会话。逐层取证后确认**数据一条没丢**：

| 检查 | 结果 |
|---|---|
| 桥 `ListTabs()` | **39 个会话**，标题/cwd 都是真实值（`issue回复问题`、`工作区内容与项目精神了解` …） |
| 桥 `GetProjectTreeSnapshot()` | **12 个项目**（`chenz`、`dsh-reasonix-desktop`、`dsh`、`12580业务流程`、`DSH-deskop`、`DeepSeek WORK!`、`IVR skill` …） |
| 界面 DOM | 项目树 `还没有项目`（`projectTree.emptyNoProjects`），行数 0 |
| 应用日志（当前实例） | 只有 `Tabs/ListTabs` 在跑，**没有任何项目树/目录相关调用** |

关键插桩（把项目树可能用到的桥方法全部计数）：`GetProjectGroups` / `ListProjectGroups` /
`GetProjectTreeSnapshot` / `GetProjectTreeRuntimeSnapshot` / `ListProjectTree` / `ListProjectTopics`
**全部 0 次调用**（对照项 `ListTabs` 有调用，证明插桩有效）。也就是说：前端根本没去取项目树。

### 12.2 根因

- 项目树（`ProjectTree` 组件）**只在挂载时取一次数**（`useEffect(() => { fr() }, [fr, me])` 里调
  `GetProjectTreeSnapshot`），之后既不轮询，也要等**后端的元数据事件**才刷新；
- 而本项目不会发那类事件 → 于是只要"应用启动时 DSH 后端还没起来"，这次取数就拿到空树并**一直**显示
  「还没有项目」；
- 本次正好踩中：应用先起（后端 3092 还没拉起来）→ 空树 → 我随后用桥方法直接 `DshLaunch`
  （**没走横幅按钮**，因此也没有那 3.5s 后的 `location.reload()`）→ 树就一直空着。
- 反证：手动刷新页面后，12 个项目 + 39 个会话**立刻全部回来**。

### 12.3 修法（自愈）

在连接横幅的复检里识别**「断开 → 连上」跃迁**，并自动重载一次页面（`reloadForReconnect`）：

- 跃迁时才重载：首次判定为已连接（`wasConnected === null`）不重载，避免刷新死循环；
- 冷却：`sessionStorage` 记录上次自动重载时间，20s 内不再重载，避免后端抖动反复刷新；
- 诊断出口扩展为 `window.__dshBanner = {ticks, connected, wasConnected, transition, timeout, at, pollMs}`。

这样"后端稍后才可用"（用户点启动、外部拉起、后端重启）都会自愈，而不是留一个空白侧栏让人以为数据没了。

### 12.4 真机验证

```
阶段1 应用已启动、后端未起：树="还没有项目" 横幅=true dbg={connected:false, wasConnected:false}
阶段2 外部拉起后端（不做任何手动操作）：
  +6s → 页面自动重载，横幅收起，树="正在整理历史 …"
  +9s → 树="chenz dsh-reasonix-desktop dsh 12580业务流程 DSH-deskop DeepSeek WORK! IVR skill …
            issue回复问题 …"
结论：自动重载=true，项目树已恢复=true
```

`verify-conn-banner.js` 相应新增三条不变量（跃迁识别、自动重载、冷却），缺失即报失败。

---

## 13. 项目树事件化：不整页重载也能刷新（顺带挖出并修掉一个"null 崩溃"隐雷）

### 13.1 为什么要做

§12 的自愈靠**整页重载**，能救回来但代价大（整页白一下、丢当前滚动/选中）。更彻底的做法是
让后端在数据变化时发前端**本来就在订阅**的事件 —— 查前端代码可确认它订阅的是：

```
project-tree:changed / project-tree:changed-v2   ← 外壳树（项目/会话结构），payload {revision, reason}
project-tree:runtime-changed                     ← 运行态装饰（open/running/status），payload {revision, topics}
```

而本项目此前**一个都没发过**（只发 `agent:event` / `topic:activation` / `terminal:*`），
这就是"树只在挂载时取一次数、之后永不刷新"的根因。

### 13.2 实现（`app_tree_events.go`）

- `emitProjectTreeChanged(reason)`：发 `project-tree:changed-v2` 与 `project-tree:changed`，payload `{revision, reason}`；
- `emitProjectTreeRuntimeChanged()`：发 `project-tree:runtime-changed`，payload 与 `GetProjectTreeRuntimeSnapshot()` 同形；
- 触发点：① `DshConnStatus()` 检测到**离线 → 在线跃迁**；② `DshLaunch()` 成功（含 alreadyRunning）；
  ③ `fetchSessions()` 拉到会话后**内容签名变化**（sha1，顺序无关）——避免把 3s 轮询变成事件噪声；
- **revision 统一**：`GetProjectTreeSnapshot` 原先**恒定返回 1**，而前端会把事件里的 revision 记在
  `Ce.current`（只增不减）——一旦事件带了更大的 revision，常数 1 的快照就会被判"不新鲜"而丢弃。
  现在快照与事件共用同一个单调递增计数（`currentTreeRevision()`）；运行态快照同理
  （前端只接受 `revision >= 当前值` 的快照，旧实现用"会话条数"，条数不变而运行态变化时更新会被静默忽略）。

### 13.3 事件化当场炸出一个隐雷（值得单独记）

事件路径**此前从未执行过**（因为从来没发过事件），一开启就崩：

```
[react] TypeError: Cannot read properties of null (reading 'state')
  at ProjectTree-DusZdLr0.js … at Object.updateMemo [as useMemo]
```

链路：事件回调里 `c.GetSessionCatalogStatus().then(e => Me(e))`，而我们的
`GetSessionCatalogStatus` 是零值桩 **`return nil`** → catalog 状态被设成 `null` →
随后 `useMemo` 读 `catalog.state` → 抛错 → **React 错误边界把整个界面替换成错误页**。
（真机现象：侧栏连同项目树整块消失，只剩"Reasonix 遇到错误"。）

修法：`GetSessionCatalogStatus` 改为真实现（返回非 nil 的 catalog 状态，含 `state` / `canRebuild` /
`indexed` / `total` …），并把 `buildProjectTree` 的内联 catalog 抽成同一个 `sessionCatalogStatus()`。

**同类隐雷排查**（返回 nil 的 map 桩，看前端是否立刻取属性）：

| 方法 | 前端用法 | 结论 |
|---|---|---|
| `GetSessionCatalogStatus` | `Me(结果)` 后读 `.state` | **崩** → 已改真实现 |
| `WorkspaceConflictForTab` | `"none"===e.state ? null : e` | **崩** → 已改为返回 `{state:"none"}`（本项目不做冲突检测） |
| `PickExportFile` | `n && await SaveExportFile(n,…)` / `if(!e)return` | nil 是**正确语义**（用户取消），保留并加注释 |
| `GetRecoveryLineage` | 经规范化函数 `N(t)` 再取长度 | 暂安全，未改（已记入文档） |
| `GetTopicSummary` | 仅出现在桥包装层，无真实消费点 | 暂安全，未改 |
| `PreviewSession` | 调用点在 `try/catch` 内 | 暂安全，未改 |

新增回归守卫 `app_bridge_contract_test.go`：把"这些结果必须非 nil 且带必要字段"固定下来
（`GetSessionCatalogStatus` 的 `state`/`canRebuild`、快照的 `catalog`/`revision`、
`WorkspaceConflictForTab` 的 `state=none`、运行态快照的 `revisions`/`topics` 形状）。

### 13.4 真机验证

```
插桩 GetProjectTreeSnapshot 计数 + performance.timeOrigin：
  基线（等 6s）            → calls=0
  调用 DshLaunch()（已连接）→ Go 发 project-tree:changed
  +1.5s                    → calls=1，页面重载过=false
结论：事件触发树重载=true；页面未重载=true
      树内容="项目 chenz dsh-reasonix-desktop dsh 12580业务流程 DSH-deskop DeepSeek WORK! IVR skill …"
      错误界面=false
```

新增测试：`app_tree_events_test.go`（revision 单调、签名去重且顺序无关、连接跃迁语义、
无 ctx 不发也不崩、跃迁只发一次）。全量 `go test` 绿；`bridge-arity-check` 真实 UI 错位 0。