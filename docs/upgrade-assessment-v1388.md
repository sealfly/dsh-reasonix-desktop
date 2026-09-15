# Reasonix 桌面端升级评估报告：v1.31.4 → v1.38.8

> ## 📌 决策记录（作者，2026-09-14）：**暂不升级**
> 理由：v1.38.x 是架构级重构（宿主契约层 + 退役 Wails shell + 前端大重构 + 248 命令缺口），
> **改得太大，必须谨慎考虑**。当前继续以 **v1.31.4 为基础**推进；
> 本报告与缺口清单存档备用（官方源码已下载，将来决策时可直接复用）。
> 重新评估的触发条件建议：①v1.38.x 出稳定小修版 ②我们完成契约适配层原型 ③有明确的新版功能需求。

> 评估日期：2026-09-14 ｜ 评估人：DSH-Reasonix 适配工程
> 评估对象：本项目当前挂载的 Reasonix 前端 **v1.31.4** → 官方最新 **desktop-v1.38.8**（2026-09-14 发布）
> 证据：官方源码两份（本地已下载，见附录）、契约文件、实测构建、脚本化对照

---

## 0. 结论先行（TL;DR）

| 判断 | 结论 |
|---|---|
| **值不值得升** | **值得**，但不是"换个 dist"那么简单 |
| **性质** | **架构级重构版本**（不是常规功能迭代）：前端源码 600→1145 文件、新增整套 `app-runtime/` 体系、右栏改为**可增删 tab 容器**、引入**宿主契约层**并明确**退役 Wails shell** |
| **最大工作量** | **宿主契约适配**：官方 **644 命令** vs 我们桥 **524 方法**（覆盖 396，**缺口 248**）+ **事件面**（前端监听 11 个事件，我们只 emit 2 个）+ 新增 `window.reasonixDesktop` 适配层 |
| **最省心的部分** | ①我们的 **DOM 注入锚点全部存活**（逐项验证）②品牌定制依赖的 **boot-shell 结构与 v1.31.4 完全一致** ③**构建链已验证可跑**（install 1分6秒 + vite build 1.2 分钟） |
| **建议时机** | **不建议立刻升**。建议：等官方出 v1.38.x 的稳定小修版 → 在**独立 worktree** 里先做"契约适配层 + 事件面" → 再换 dist → 顺势把「子代理」页**焊成官方 tab**（那时锚点最干净） |
| **建议分级** | P0（必须）1–2 天；P1（日常路径命令补齐）2–4 天；P2（官方桌面端专属能力）按需；P3（不需要）走安全兜底 |

---

## 1. UI 与架构变化（v1.31.4 → v1.38.8）

### 1.1 架构级重构（源码文件清单实测）
| 指标 | v1.31.4 | v1.38.8 |
|---|---|---|
| 前端源码文件数 | 600（v1.29 基线） | **1145** |
| `components/` | 141 | **263** |
| `lib/` | 204 | **345** |
| 关键新增 | — | **整套 `app-runtime/`**（owner / adapter / projection 模式，30+ 模块） |

### 1.2 右栏（我们主要适配区）重构
- 从"固定视图" → **可增删的 tab 容器**：
  `components/TabContainer/`（TabContainer / TabBar / TabContent / **TabAddMenu** / DockTabPicker / TabOverviewMenu）
- **类型化的 tab 系统**：`store/activityBar.ts` → `type TabType = "file" | "changed" | "context" | "remote" | "browser"`，配 `addTab / openEntry` API
- **新增 browser（浏览器）tab** + 「**+**」按钮下拉菜单添加 tab
- 注释明示：terminal / browser / remote 尚未全部暴露（tab 系统仍在演进）

### 1.3 新功能（由 i18n 文案差异实证，增删各约 600 条 key）
- **工具调用详情**：查看执行过程、子调用 / 父调用、参数、结果、原始记录、加载全文 / 复制全文
- **对话轮次导航**：对话轮次、跳转到第 N 轮、返回最新、加载更早消息
- **用量与速度**：本轮用量、未缓存输入、缓存读取、**输出速度 TPS**、本轮用时 / 总用时
- **官方自带 `{count} 个子代理`**（与我们自建子代理页概念重合，可对照）
- **worktree 分支**（在新对话中分支）、**网页搜索 / 网页抓取**、**文件成果**（本轮修改的文件）
- **远程向导（remoteWizard）**、**接管（takeover）**、**恢复 / 回滚（recovery / toolRecovery）**
- 移除 / 重构：`settings.*` 大规模重命名（−343 key）、heartbeat 面板（−34）、composer 片段（−63）

### 1.4 新增宿主边界约束（⚠️ 升级关键）
官方新增 `scripts/check-desktop-host-boundary.mjs`，**禁止**前端访问：
```
HOST_GLOBALS  = ["go", "runtime", "reasonixDesktop"]
SHELL_MODULES = [/wailsjs/, /@wailsapp/]
// 注释原文："The Wails shell is retired; no import of its generated bindings or
//           runtime package is legitimate anymore, type-only included."
```
→ 即：**v1.38.8 前端不再认 `window.go`（我们的 Wails 桥入口）**，只认 **`window.reasonixDesktop`**（Electron preload 契约）。

---

## 2. 桥方法面对照（核心工作量）

### 2.1 官方契约（权威来源）
`desktop/frontend/src/generated/desktopContract.generated.ts`（135 KB，由官方 Go 侧 `-emit-contract` 生成）：

```
DESKTOP_PROTOCOL_VERSION = 6
DESKTOP_CONTRACT_DIGEST  = sha256:36d13e77ffca2b587b4f0b57a3a4618f1d4c2540e5228530a94372dde7d58297
DESKTOP_COMMANDS         = 644 项（PascalCase 命令 + agent:event 等事件名）
```

### 2.2 对照结果（脚本严格集合运算，大小写不敏感）
| 指标 | 数值 |
|---|---|
| 官方契约命令 | **644** |
| 我们桥导出方法（42 个 `app*.go`） | **524** |
| **已覆盖（同名匹配）** | **396（61.5%）** |
| **缺口** | **248** |
| 我们独有（官方契约无，DSH 桥特有） | 124 |

### 2.3 缺口分布（Top 能力域）
```
Set×28  Remote×13  Resolve×10  Cancel×8  Get×8  Submit×7  Git×6
Save×5  Add×4  Answer×4  Delete×4  Open×4  Rename×4  Transcript×4  Workspace×4 …
```
典型缺口样例：`ActivateTopic`、`AddProviderConnection(WithOptions/WithURL)`、`AddRemoteProject`、
`AnswerMCPInteractionForTab/ForTurn`、`AnswerPromptForTab`、`AnswerRemoteTab`、`ApplyModelSettings`、
`ApproveTabForTurn`、`AuthorizeAndConnectMCPServer`、`CancelJobsForTab`、`CancelRemoteTab`、
`CancelTask(ByKey/ForTab)`、`CaptureInboxTarget`、`CheckRemotePlatform` …
→ 集中在 **Remote（远程项目/转发）**、**Provider（模型供应商）**、**MCP 交互**、**`*ForTab` / `*ForTurn` 变体**、**Task/Job 管理** 等域。

完整清单：`docs/upgrade-v1388-contract-gap.txt`

### 2.4 事件面对照
| 项 | 情况 |
|---|---|
| v1.38.8 前端监听的事件（11） | `app:open-settings`、`config:load-warnings`、`desktop:resync`、`desktop:shell-status`、`history-index:changed-v1`、`InboxChanged`、`project-tree:changed-v2`、`project-tree:runtime-changed`、`remote-tab:updated`、`runtime-state:changed`（+ `unhandledRejection`） |
| 我们桥 emit | `agent:event`、`topic:activation` |
| **缺口** | 上表绝大部分事件未 emit → 升级后相关 UI 不会自动刷新（属需补的中等工作量） |

---

## 3. 品牌资产盘点（升级后需重做的部分）

| 资产 | v1.31.4 | v1.38.8 | 结论 |
|---|---|---|---|
| `boot-shell` / `boot-shell__mark` / `boot-shell__name`（加载页） | index.html 有 | **index.html 有（结构一致）** | ✅ **我们的品牌定制可直接复用** |
| `src/assets/logo-wordmark.svg` | 有（4 KB） | **有（4 KB）** | ✅ 同名，可按 `logo-wordmark-*.svg` 模式替换 dist 产物 |
| `src/assets/logo-symbol.svg` | 有（2 KB） | **有（2 KB）** | ✅ 同上 |
| `src/assets/logo.svg` | 有（5 KB） | **无** | ⚠️ 新版移除，检查我们是否引用 |
| 供应商图标 | — | 新增 `public/provider-icons/*` | 新增资源（官方功能） |
| 侧边栏 logo 反白 `filter` 覆盖 | 我们已覆盖 `filter:none` | `styles.css` 仍存在 | ⚠️ 升级后需复核该覆盖是否仍命中 |
| dist 体积 | 23.29 → 剪裁后 15.67 MB（含 Monaco 剪裁） | **构建产物 13.8 MB / 377 文件** | ⚠️ **Monaco 剪裁工作需重新评估**（新版结构不同，可能已不需要剪裁） |

---

## 4. 注入锚点盘点（**全部存活** ✅）

| 我们的锚点 | 用途 | v1.31.4 位置 | v1.38.8 位置 | 结论 |
|---|---|---|---|---|
| `workspace-preview__body` | 内联编辑器预览容器 | WorkspacePanel.tsx | **WorkspacePanel.tsx**（+ styles.css） | ✅ 存活 |
| `workspace-tree__row(--active)` | 文件树选中行 | WorkspaceTreeRow.tsx | **WorkspaceTreeRow.tsx** | ✅ 存活 |
| `data-workspace-path` | 文件路径属性 | WorkspaceTreeRow.tsx | WorkspaceTreeRow.tsx（bench 夹具也引用） | ✅ 存活 |
| `workbench-dock__tabs` | 子代理页 tab 栏注入点 | app-chrome 区 | **TabBar.tsx**（`className={["workbench-dock__tabs", …]}`） | ✅ 存活 |
| `workbench-dock__body` | 子代理页面板挂载点 | workspace 布局 | **styles.css**（+ BrowserPanel.css 引用） | ✅ 存活（元素需实测） |
| `workspace-files__tabs` / `_tab` / `workspace-iconbtn` | 旧布局兜底候选 | WorkspacePanel.tsx | **WorkspacePanel.tsx** | ✅ 存活 |
| `monaco`（HljsCode.tsx） | 代码高亮/编辑器 | HljsCode.tsx | HljsCode.tsx | ✅ 存活 |
| `__DSH_PLUGIN_MARKET` | 插件市场注入点 | 两版均无 | 两版均无 | ➖ 已废弃项，无需处理 |

**结论**：三个前端注入脚本（内联编辑器 / 子代理页 / 品牌）在 v1.38.8 上**大概率可直接工作**，升级后只需实测微调选择器。

---

## 5. 构建可行性（已实测通过 ✅）

| 步骤 | 结果 |
|---|---|
| 环境 | node v24.19.0 ✓ / pnpm 10.34.5 ✓ / corepack ✓ / 磁盘可用 14.7 GB |
| `pnpm install` | **成功，1 分 6 秒**（有缓存/镜像；装出 vite 8.2.2、typescript 6.0.3、eslint 10、playwright 等） |
| `npx vite build` | **成功，1.2 分钟** → `dist/` **377 文件 / 13.8 MB** |

⚠️ **注意**：官方 `pnpm build` 脚本串联了一长串校验，任一失败即中断构建：
```
lint:hooks → check:waapi → check:scroll-writer → check:app-layers → check-css-syntax
→ check-z-index-tokens → check-theme-token-contract → tsc --noEmit → vite build
→ check-bundle-budget
```
→ 我们做源码级改动（焊接）时，**建议只跑 `vite build`**（或 `build:electron`），避免被与我们无关的校验卡住。

---

## 6. 新增关键工作：宿主契约适配层（v1.38.8 特有）

因为前端"退役 Wails shell"，我们需要提供一个**注入式适配层**，把官方契约接到现有 Wails 桥上：

```js
// 注入脚本（新增，约 150–250 行）
window.reasonixDesktop = {
  contract: {
    version: 6,                      // DESKTOP_PROTOCOL_VERSION
    digest: "sha256:36d13e…",        // DESKTOP_CONTRACT_DIGEST（前端可能校验）
    commands: [ /* 644 项，来自 generated 契约 */ ]
  },
  invoke(method, args) {             // → 转发到现有 Wails 桥
    const app = window.go?.main?.App;
    if (app && typeof app[method] === "function") return Promise.resolve(app[method](...args));
    return Promise.resolve(null);    // 原则 3：安全兜底 + 留痕（不崩溃）
  },
  on(name, cb) { /* 订阅 Wails 事件并映射到契约事件名 */ return () => {}; },
  native: { /* window/clipboard/getPathForFile… 小工具 */ },
  window: { setTheme, setBackgroundColour, getBounds, isMaximised, … },
  clipboard: { writeText, readText }
};
```

要点：
1. **命令转发**：524 个已有方法**直接按名映射**；248 个缺口走兜底（功能不可用但不崩，按需补）
2. **事件补齐**：把我们的 `agent:event` 等映射/补充为契约事件名（`project-tree:changed-v2`、`runtime-state:changed`、`desktop:shell-status` …）
3. **协议版本与摘要**：如实声明 v6 + digest（若前端校验，必须一致）
4. **新能力域**：`BrowserControlApi`（浏览器控制）、`GraphicsSettings`、`ServiceState`（服务生命周期，与我们"启动 DSH 后端"语义相近）、`window/clipboard/native` 工具集

---

## 7. 工作量分级

| 级别 | 内容 | 估算 |
|---|---|---|
| **P0 必须** | 契约适配层骨架 + 命令转发 + 事件面补齐 + 品牌资产重做（boot-shell/logo/filter 复核）+ 三个注入脚本实测复核 + 构建链接入 | **1–2 天** |
| **P1 重要** | 缺口中的日常路径命令（会话/审批/工作区/子代理/任务/设置相关，约 60–120 个） | **2–4 天** |
| **P2 可选** | Remote（远程项目/转发）、Provider 管理、主题包、浏览器控制、图形设置等官方桌面端专属能力 | 按需，可长期并行 |
| **P3 不需要** | 官方自动更新、Chrome 登录导入、遥测/更新通道等 | 走安全兜底，永不实现 |

---

## 8. 建议：值不值得升 / 什么时候升 / 怎么升

### 值不值得升 → **值得**
- **用户体验收益明确**：工具调用详情面板、TPS/用量指标、对话轮次导航、网页搜索/抓取、可增删的右栏 tab
- **架构上更适配我们**：v1.38.8 的 **tab 容器**让我们的「子代理」页可以**焊成官方 tab**（锚点仅 3 处：`TabType` 联合类型 + `ADDABLE_TABS` 常量表 + 渲染 switch），观感 100% 原生且升级可维护
- **我们的既有资产大部分可复用**：注入锚点全部存活、boot-shell 结构一致、构建链跑得通

### 什么时候升 → **不立刻升**
理由：缺口 248 命令 + 事件面 11 项 + 契约适配层都还没做；现在硬换 dist 会出现"部分功能点了没反应"（虽不崩，但体验差）。
建议时机：**官方 v1.38.x 出稳定小修版后**（或我们主动钉住某个 tag），在独立分支上按下面步骤做。

### 怎么升（建议步骤）
1. **独立 worktree** 建升级分支（不污染现网）
2. 先写**契约适配层**注入脚本 + 事件面映射（此时可先用 v1.31.4 跑通，风险低）
3. 换 **v1.38.8 dist** → 跑三个注入脚本（预期可用）+ 重做品牌资产 → 冒烟测试
4. **顺势焊接「子代理」页**为官方 tab（`TabType` + `ADDABLE_TABS` + switch 三处锚点，配补丁脚本 `apply-frontend-patches.js`，锚点失配即构建失败）
5. 补齐 P1 命令 → 回归（现有 30 项 DOM 测试 + 桥单测 + 端到端）
6. 重新评估 **Monaco 剪裁**是否仍需要（新版 dist 13.8MB，结构已不同）

### 风险提示
- 未实现的 248 个命令会走兜底 → 官方部分能力不可用（**可接受，按需补**）
- 官方协议版本号（当前 6）与 digest 后续会变 → 适配层需跟随（我们已把它参数化即可）
- `settings.*` 大规模重命名 → 若我们曾依赖 settings 相关 key/结构需重新对照

---

## 附录：证据与产物

| 内容 | 位置 |
|---|---|
| 官方 v1.38.8 源码（已下载，含依赖安装与构建产物） | `%USERPROFILE%\Desktop\dsh-upstream-v1388\src\esengine-DeepSeek-Reasonix-7278072\` |
| 官方 v1.31.4 源码（对照基线） | `%USERPROFILE%\Desktop\dsh-upstream-v1388\src1314\esengine-DeepSeek-Reasonix-97411a3\` |
| 官方契约文件 | `<v1.38.8>\desktop\frontend\src\generated\desktopContract.generated.ts` |
| 缺口完整清单（248 + 独有 124） | `docs/upgrade-v1388-contract-gap.txt` |
| 对照脚本 | `scripts/contract-gap.js`（契约 ↔ 桥方法）、`scripts/i18n-diff.js`（UI 文案差异）、`scripts/ws-capture.js`（DSH 事件帧抓取） |
| 构建实测 | `pnpm install` 1m6s ✓ / `npx vite build` 1m12s ✓ → dist 377 文件 13.8 MB |
