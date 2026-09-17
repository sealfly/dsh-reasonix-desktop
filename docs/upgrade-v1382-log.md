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

## 4. 后续（P3）待办

1. `Settings()` 补 `providerPresets`（对齐 `ProviderPresetView`），让「模型服务」页预设列表有内容。
2. 实现 `FetchProviderModelCatalog` / `FetchProviderModelCatalogDraft` / `FetchAllProviderModelCatalogs`
   / `TestProviderModel` / `AddProviderConnection{,WithURL,WithOptions}` / `SetConnectionKey` / `DeleteProvider`。
3. 修 `FetchAllProviderModels` 返回形状为 `Record<string, string[]>`；`FetchProviderModels` 按入参 provider 过滤。
4. 给其余注入脚本补 `LogFromFrontend` 诊断（本次只有 subagent-panel 有），
   让"锚点消失"这类升级回归能自动留痕，而不是靠肉眼发现。
5. 可选：把「子代理」页在 TabContainer 时代（≥1.38.8）焊成官方 tab。
