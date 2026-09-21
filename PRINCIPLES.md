# 项目原则（PRINCIPLES）

## 原则 1（最高优先）：本项目只是 DSH 的前端 UI，不限制 DSH 的任何能力

> 本项目（dsh-reasonix-desktop）存在的唯一目的：为 DSH（DeepSeek Harness）提供一个
> 方便人使用的前端界面。DSH 才是能力提供方（多客户端、多路复用的独立服务），
> 本项目只是它的"皮肤"——**永远不要让桥接层成为 DSH 能力的瓶颈或闸门**。

由此派生四条不可违背的约束：

### 1.1 桥接层是通用透传，不是白名单

- `window.dsh.rpc` 透传任意 DSH 方法（`session.*`、`goal.*`、插件动态注册的 `namespace.method`），
  不设白名单、不做方法级过滤。
- 前端/插件需要什么方法就透传什么；"未实现"只允许表现为安全兜底（返回 `undefined` / 空对象），
  不允许表现为拦截或拒绝。

### 1.2 事件流全量透传

- `events.mux` 的**全部原始帧**无筛选转发给渲染层（`dsh:raw-event` → `window.dsh.onEvent`）。
- WireEvent 语义转换（`dshEventToWire`）只是"额外的便捷通道"；转换不了的帧照样从原始通道送达，
  绝不丢弃、绝不篡改。

### 1.3 完全授权时解除桥自身的防御

- yolo / danger-full-access（`bridgeFullAccess`）下，解除桥自身的防御性校验
  （RPC 方法名校验、文件路径校验），让前端对 DSH 及其 AI **零限制**。

### 1.4 桥只做"方便人使用"的适配

- 桥允许做的是**展示与持久化适配**：设置项（语言/主题/布局/币种/思考显示）、
  会话元数据转换、费用估算、事件重放等。
- 桥**禁止**做：拦截、篡改、节流、屏蔽 DSH 的输入输出、工具调用、上下文或任何能力数据。

## 原则 2：默认不改前端，除非作者要求

- 前端是 Reasonix 的 dist 构建产物，默认保持原样；常规功能适配都走桥接层（`src/preload.js` / `src/main.js`）。
- 默认需要新功能时，在桥里补齐 Reasonix 前端期待的方法/事件，而不是改前端。
- **例外**：作者（项目所有者）明确要求时，可以直接修改前端。

## 原则 3：失败留痕、兜底不崩溃

- 未实现的方法返回安全兜底，前端不崩溃；需要时逐个映射。
- 提交/调用失败不静默：console 有痕迹，调用方拿得到结果（`{ ok: false, error }` 或抛错）。

## 原则 4：遵循 dsh-std 协议（DSH 标准协议）

- 本项目作为 DSH 生态前端，桥接层必须遵循 **dsh-std 协议**（DSH 标准协议）：
  能力协商（`DshStdNegotiate`）、准入（`DshStdAdmit`）、能力清单（`DshStdCapabilities`）、
  宿主描述（`DshStdHostDescriptor`）、清单解析（`DshStdParseManifest`）、自描述（`DshStdSelfManifest`）。
- 协议版本升级时，桥接层需同步对齐；新增 DSH 能力不得绕过协议直接硬编码。
- 协议相关实现集中在 `app_dshstd.go`，改动必须带测试（`app_dshstd_test.go`）。

## 原则 5：社区准入性（dsh-ecosystem-spec Admission）

- 本项目遵循 **dsh-ecosystem-spec 的 Admission v0.15 规范**，保持社区生态准入合规：
  - 宿主/前端元数据（名称、版本、协议版本、能力声明）必须与 Admission 要求一致；
  - 接入 DSH 生态时必须通过 Admission 校验（自描述 + 能力协商）；
  - 规范升级时评估并同步，不落后于社区准入要求。
- 社区准入性不是一次性动作：每次协议/规范升级、每次对外发布都要复核。

## 原则 6：本项目的 DSH 适配不参与官方对照覆盖（升级红线）

> 本项目（dsh-reasonix-wails）本身就是 **DSH 的前端适配项目**（见原则 1）：Reasonix 前端 +
> DSH 后端。对照 Reasonix 官方源码（esengine/DeepSeek-Reasonix）升级到最新版时，
> **本项目的一切 DSH 适配都不允许被官方版本覆盖掉**。
> 官方源码是"Reasonix 自身 + 它的后端"的实现；本项目是"Reasonix 前端 + DSH 后端"的实现——
> 两者桥接层本质不同。官方升级只应带来"前端 UI 与桥方法面的更新"，本项目的 DSH 适配是
> 与 DSH 共存的基础，覆盖即断链。

以下类别**一律保留本项目版本，不参与官方对照/替换**（升级后须从旧版本/备份恢复或重新应用）：

1. **品牌与视觉定制**：logo 文件（wordmark/square）、`index.html` 的 boot-shell 内联 SVG 与名称
   （见 `LOGO-NOTES.md` 的升级规则）。
2. **前端注入脚本**：插件市场（`__DSH_PLUGIN_MARKET`，现位于 `index.html`）、错误捕获、
   就地代码编辑器（`scripts/dsh-inline-editor.js`，含"编辑按钮只在文件内容预览出现"的门禁）、
   **右侧栏「子代理」页**（`scripts/dsh-subagent-panel.js`：显示当前会话子智能体 + 相关后台进程）
   等本项目注入，升级后按 `scripts/dsh-plugin-market-inject.js` 与幂等重放脚本
   `node scripts/apply-inline-editor.js`、`node scripts/apply-subagent-panel.js` 重新注入。

   > **2026-09-14 起已自动化**：`build-deploy.ps1` 与 `build-installer.ps1` 在 wails build **之前**
   > 统一执行 apply 脚本（幂等可重复）——上游 dist 覆盖后**重新构建即自动恢复**注入，
   > 不需手工处理。注入步骤缺 node 会直接失败（宁可构建失败，也不静默产出缺功能的包）。
   >
   > **2026-09-17 起升级为「清单驱动 + 语法门禁 + 品牌一并重放」**（v1.31.4→v1.38.2 升级时重建）：
   > - `scripts/injections.json` 是**唯一注入清单**（顺序 = 依赖顺序）；
   >   `node scripts/apply-all-injections.js` 按清单把**全部**注入内联进 dist，
   >   用区域哨兵 `<!-- dsh-inject:begin/end -->` 整段替换，因此**幂等**（三次运行字节一致）。
   > - **语法门禁**：每个源脚本内联前先 `new Function(src)` 解析，不通过就**整体失败退出**。
   >   这条规则来自一次真实缺陷——旧 `dsh-plugin-market-inject.js` 首行是一段 `export {…}` 残片
   >   （`ce as T,…};`），作为经典脚本是 `SyntaxError`，**整块从未执行**：插件市场功能
   >   自上线起就是死的，且没有任何报错痕迹。语法门禁让这类错误在构建期就炸出来。
   > - **品牌改造同样必须可重放**：`scripts/apply-branding.js` 读 `branding/`
   >   （由 `scripts/extract-branding.js` 从既有 dist 反向固化）幂等写回启动壳名称/位图/
   >   品牌 CSS/被 CSS 引用的 logo SVG/标题错误钩子。
   > - **升级动作收敛为一条命令**：`node scripts/sync-upstream-dist.js <上游 dist>`——
   >   形态前置校验（boot-shell / 外部 module bundle / wails-spinner 锚点）→ 复制 → 重放品牌+注入。
   > - **升级前先盘点**：`node scripts/dist-inventory.js` 看"哪些注入只活在 dist 里（无源脚本、
   >   无 applier）"，`node scripts/dist-block-extract.js promote` 可把丢失的源反向提取回来
   >   （带同一道语法门禁），`node scripts/injection-anchors.js <上游 src>` 判断注入能否平移。
   >
   > 注入脚本的验证：`node scripts/subagent-panel-domtest.js`——在最小假 DOM 里执行
   > **dist 中的真实内联块**，断言 tab 注入 / overlay 挂载 / 定位加固 / 卡片与进程行渲染 /
   > React 节点未被动 / 幂等（**当前 37 项**，随功能增长递增）。
   >
   > **运行期留痕**：注入脚本把关键诊断经桥 `LogFromFrontend(msg)` 写进 `%TEMP%\resume-debug.log`
   > （`frontend: [subagent-panel] tab-bar-found …` / `no-tab-bar`）。生产构建的 WebView2
   > **不带远程调试端口（CDP 不可用）**，所以"注入是否找到锚点"只能靠这条通道验证；
   > 排查升级回归时先看这个日志。
   >
   > **真界面自动化验证（2026-09-17 新增，因 CDP 被封）**：Wails 在创建 WebView2 环境时会
   > **覆盖** `WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS`（go-webview2 的
   > `preventEnvAndRegistryOverrides` 里 `os.Setenv(..., additionalBrowserArgs)`），
   > 所以 `--remote-debugging-port` 这条路走不通。替代方案是本项目的 **UI 测试钩子**：
   > 启动时设 `DSH_UI_TEST_PORT=<port>`（可选 `DSH_UI_TEST_TOKEN`）即在本机 127.0.0.1 开
   > `GET /health`、`POST /eval {"js"}`、`POST /click {"selector"}`，用 Wails 的
   > `WindowExecJS` 在真页面里求值并把结果经桥方法 `UiTestReport` 回传（支持 await Promise）。
   > 工具：`scripts/ui-test-eval.js`（探针）、`scripts/fake-openai-provider.js`（本机假供应商端点）、
   > `scripts/ui-test-provider-flow.js`（「模型服务」全链路：添加→刷新模型→改动→保存→复核 DSH）。
   > 规则：**功能宣称"能用"之前，先用这条链路在真界面上跑一遍**，并断言落库结果（读 DSH），
   > 而不是只看界面显示。写 UI 断言时注意三条实测教训：①查询必须限定在目标子树
   > （`document.body.innerText` 会撞到会话区的文本）；②React 受控输入要用原生 setter + `input` 事件；
   > ③面板只在挂载时拉一次设置，**复用它可能拿到陈旧列表**（测试前重启应用或强制重挂载）。
   >
   > ⛔ **注入脚本硬约束（因 2026-09-14 冻结事故新增）**：
   > 1. **禁止 `MutationObserver` 观察整个文档**（`document.documentElement` + `subtree`）。
   >    它与"改动自己的 DOM"组合会形成自激死循环（改 DOM → 回调 → 再改 DOM），把渲染主线程
   >    打满、界面彻底冻结。需要感知布局变化时用**低频轮询**（≥1.5s），它不会自激。
   > 2. **周期性逻辑不得重建 DOM**：轮询里只做"轻量维持"（补回自己的 tab、宿主失联时收掉
   >    overlay）；面板内容按**内容签名比对**决定是否重建，签名不变时一个 DOM 都不动。
   > 3. 任何写入 DOM 的操作都要**先比对再写**（如徽标：值未变则不写）。
   > 4. 上线前必须做 CPU 静默验证：空闲时 app 进程 CPU 增量为 0（`Get-Process` 采样 4s）。
3. **桥方法适配**：`app_*.go` 中所有"DSH 语义"实现（持久化桥：MCP、子智能体、技能偏好、
   插件市场等）——官方实现基于 Reasonix 自身后端（skill 文件、配置服务），与 DSH 桥不同，
   升级时**不得用官方实现覆盖本项目实现**，只吸收官方新增的方法面。

   > **载荷字段面与方法面同等重要（2026-09-17「主题跳变」事故）**：宿主方法缺失时会因
   > `app` Proxy 返回 `undefined` 而**静默不崩**，但**返回载荷里缺字段同样静默**——
   > 前端一律 `normalize*(undefined)` 回落默认值。真实案例：前端有**两套键名**的主题契约
   > （themeExperience 用 `themeMode`/`baseStyle`；设置快照用 `desktopTheme`/`desktopThemeStyle`/
   > `conversationWidth`/`sessionExperience`），`Settings()` 曾只给第一套，于是每次设置面板
   > 重读都 `normalizeThemePreference(undefined)` → `DEFAULT_THEME("auto")` → **用户选的深色
   > 被冲掉（按任意按钮就"跳"）**；而 `DesktopStartupSettings()` 给对了键名，所以表现为
   > "启动正确、之后跳变"，看起来像升级新引入的 bug（缺口其实长期存在，
   > v1.38.2 新增的 `app-runtime/` 偏好适配器让重放时机变多才暴露）。
   > 规则：**同一份语义只允许一个来源函数**（本项目为 `desktopPreferenceKeys()`），
   > `Settings()` 与 `DesktopStartupSettings()` 必须共用它，并用
   > `node scripts/settings-contract-check.js <上游 lib/types.ts>` 对照契约字段；
   > 改外观/会话类字段必须同时补回归测试（见 `app_settings_theme_test.go`）。
4. **持久化用户数据**：`~/.reasonix/` 下的用户数据（`mcp-servers.json`、
   `subagent-profiles.json`、`skill-preferences.json`、`plugins/`）是运行时数据，
   升级不涉及，也禁止升级流程触碰。
5. **自有前端资产（vendor 目录类）**：上游**不提供**、由本项目自己放进 `frontend/dist` 的资源
   ——例如就地编辑器用的 **Monaco**（`third_party/monaco/vs/**`，143 文件 15.7MB；
   实测上游 1.31.4 与 1.38.2 的 `package.json` 都不依赖 monaco）。
   这类资产换 dist 时会被整体冲掉，因此**必须**：
   - 仓库内留存（本项目的 `third_party/`，**不能叫 `vendor/`** —— Go 会把模块根下的 vendor
     当依赖目录，直接 `inconsistent vendoring` 构建失败，实测踩过）；
   - 由幂等 applier 复制进 dist（`node scripts/apply-monaco-vendor.js`，逐文件比对 + 断言
     `loader.js`/`editor/editor.main.js`/`editor/editor.main.css` 在位，失败即停）；
   - 接入 `build-deploy.ps1` 与 `build-installer.ps1` 的 applier 列表；
   - 纳入发布校验 `scripts/verify-packaged-app.js` 的标记表（缺失即构建/发布期报错）。
   事故记录（2026-09-18）：Monaco 资产在 v1.38.2 换 dist 时被冲掉且当时**不在**任何 applier 里，
   于是编辑器退回 textarea——而回退 textarea 又被放进 `display:block` 的宿主里、`flex:1` 失效，
   只剩 ~70px 高，用户看到"点编辑后编辑窗口只剩上面一部分"。

   > **同类风险排查法**：升级前后做一次**目录级**对比（只比目录、排除带 hash 的文件名）：
   > 旧 dist 有、新 dist 没有的目录 = 被冲掉的自有资产。本次结果只有 `assets/monaco` 一处
   > （品牌 SVG 因已纳入 `branding/` applier 而未丢）。
   > 可视化验证同理：真界面上量几何（覆盖层/编辑器宿主/内部编辑器的宽高），
   > 别只看"功能还在不在"——布局塌陷时功能是"在"的，只是看不见。
6. **源码级焊接（未来形态，作者要求时）**：当作者要求把本项目页面**焊成 Reasonix 原生
   组件/tab**（例如按官方 `TabContainer` / `TabAddMenu` 的 tab 体系把「子代理」页实现为
   原生 tab）时，该焊接改动**同样属于"不参与官方对照覆盖"的本项目适配**——
   **官方升级覆盖源码/dist 后，焊接改动必须能够恢复，不允许被冲掉**（丢页面即断功能）。
   焊接必须满足以下硬要求（否则不予采用）：

   - **可重放**：改动以**锚点补丁脚本**（如 `scripts/apply-frontend-patches.js`）表达，
     幂等、可重复执行；**禁止**只手工散改上游文件（官方覆盖后无法恢复，也无法审计差异）。
   - **改动收敛**：所有自有代码放**独立目录**（如 `desktop/frontend/src/our/`），
     上游文件里只保留**极小锚点**——例如 `TabType` 联合类型加一项、`ADDABLE_TABS` 加一行、
     渲染 switch 加一个分支；锚点数量与位置必须在补丁脚本里显式列出（当前参考：
     v1.38.8 形态为 3–4 行锚点）。
   - **失败即停**：补丁脚本找不到锚点必须**报错退出**——构建期失败优于静默产出缺页面的错版
     （与注入脚本"缺 node 直接失败"同一原则）。
   - **构建链纳入本项目流程**：前端 `pnpm install` + `vite build` 属本项目构建步骤；
     若官方 `pnpm build` 串联的校验（lint/waapi/css/z-index/theme-token/bundle-budget）
     与我们的改动冲突，允许只跑 `vite build`，但需在文档记录理由。
   - **升级时必须复核**：官方升级后重放补丁；锚点失配则**修锚点**（这是本项目的维护动作），
     **绝不允许"因为上游重构所以放弃我们的页面"**。
   - **与原则 2 的关系**：源码焊接属原则 2 的"作者明确要求"例外；未获作者要求时，仍优先走
     注入方案（升级迁移成本更低）。

升级流程检查清单（对照官方 diff 时逐项勾选）：
- [ ] **先跑漂移对照**：`node scripts/ui-drift-report.js --upstream-root <官方源码目录> --history`
      （看各特征从哪版开始变）与 `--baseline-version <我们挂载的版本> --version <新版本> --doc docs/upstream-ui-drift.md`
      （出本次升级评估：锚点是否还在、契约层是否出现、哪些 UI 变了）。**有 ★破坏/★需评估 时先读报告再动手。**
- [ ] 升级前先盘点：`node scripts/dist-inventory.js`（有无"只活在 dist 里"的注入）
- [ ] 升级前先判锚点：`node scripts/injection-anchors.js <上游 frontend/src>`（未命中项逐个人工确认）
- [ ] dist 中 logo/boot 品牌是否仍是本项目版（`node scripts/apply-branding.js` 幂等重放，5 项断言）
- [ ] 全部注入是否就位：`node scripts/apply-all-injections.js`（语法门禁 0 失败 + 每个 id 哨兵恰好 1 个）
- [ ] 自有资产是否就位：`node scripts/apply-monaco-vendor.js`（Monaco 等 vendor 资源；缺 loader 即失败）
- [ ] 目录级对比旧/新 dist：旧有新无的自有目录（排除 hash 文件名）= 被冲掉的资产
- [ ] **dist 资产完整且可复现**：`node scripts/verify-dist-assets.js`（index.html 入口引用 + chunk 依赖闭包必须**存在**，且必须**全部被 git 跟踪**）。未跟踪 = 别人 clone 不到 = 首屏白屏（2026-09-18 卡「加载中」事故：升级只提交了 index.html，415 个 assets 被 `.gitignore` 的 `frontend/dist/` 吃掉。**根因已修**：该忽略规则已删除，dist 必须入库，新增 chunk 会直接以 untracked 暴露 → `git add frontend/dist`）。两个构建脚本已内置该门禁。
- [ ] 运行期确认注入生效：`%TEMP%\resume-debug.log` 里有对应的 `frontend: [<脚本>] …` 成功诊断
- [ ] **载荷字段面对照**：`node scripts/settings-contract-check.js <上游 frontend/src/lib/types.ts>`
      （缺字段会被前端静默归一化为默认值 —— 主题跳变即此类）
- [ ] 桥方法：本项目持久化实现（MCP/子智能体/技能偏好/插件市场）未被官方实现替换
- [ ] **桥方法参数个数与前端一致**：`node scripts/bridge-arity-check.js`（真实 UI 调用点错位必须为 0）。
      Wails 绑定按**参数个数**校验，形参 0 而前端传 2 个就抛
      `error parsing arguments: received 2 arguments to method 'main.App.X', expected 0`，
      而这类错只在界面角落显示一行小字 —— 2026-09-20 就是这样让右栏「远程」页签的文件树恒"加载失败"，
      顺着查出并修掉 **57 处**同类错位（含 1 处参数**顺序**颠倒）。新增了零参桩批量对齐脚本
      `node scripts/align-stub-arity.js`；检查器里的"已审阅例外表"必须写清原因，不得用来消音。
      ⚠️ 改签名时注意：**`any` 形参收到 JS 的 `null`/`undefined` 会让 Wails 既不 resolve 也不 reject**
      （Promise 永久挂起且无报错）—— 该传 null 的位置要用具体类型（如 `map[string]any`）。
- [ ] 本项目独有桥方法（`DshStd*`、`MarketPage`、`Terminal*` 等 23 个）未被删除
- [ ] **`session.history` 的字段位置与分页语义**（问题导航 questionNav 依赖它；错了会"点位全显示
      「第 {n} 个问题（点击加载）」且点击毫无反应"）：user/message 的内容在 **`data.content`**
      （不是 `data.message.content`，那是 assistant 的位置）；点位总数要用
      **`projections.values.sessionStats.turns`（会话真实轮次）**，不能数尾页消息条数
      （DSH 一次只回尾部一页，实测 34248 事件 / `hasMore:true`，真实 211 轮而尾页只有 5 条提问）；
      翻更早的页用 **`beforeSeq`**（实测有效，已用不透明 cursor 承载）。回归：`app_session_history_test.go`。
- [ ] **连接横幅的行为对称**：`node scripts/verify-conn-banner.js`。横幅必须**连上就收**、
      掉线能再弹（注入脚本里既要有 showBanner 也要有 hideBanner + 周期复检）；给桥调用加超时竞速，
      否则遇到"永不 settle 的 Promise"会让轮询永久停摆（2026-09-21 实测：`connected:true` 时横幅
      一直挂着）。排障看页面里的 `window.__dshBanner`。
      还必须保留**「断开 → 连上」自动重载**：前端有组件只在挂载时取一次数（项目树就是），
      应用先于 DSH 后端启动时会把空树记下来并一直显示「还没有项目」——2026-09-21 实测 DSH 里
      12 个项目 / 39 个会话，界面却是空的，刷新才恢复。重载需带冷却，避免抖动反复刷新。
      ⚠️ 该脚本含中文注释：**不要用 PowerShell 读写它**（PS 5.1 按 ANSI 读 UTF-8 会把注释改坏），
      用编辑工具或 Node；万一改坏，可从 `frontend/dist/index.html` 的注入块里原样提取回来。
- [ ] **「启动 DSH」尊重配置**：`DshLaunch` 必须用「连接设置」里的 host:port（不得写死 3080）；
      端口被占但 ping 不通时**明确报错**（典型：占用者是要求 token 的 DSH 实例，而本应用 `DshClient`
      没有 token 支持）；profile 先试 `web`，缺 bundle 时自动回退 `tauri`；拉起后轮询等待就绪并把
      子进程 stderr 尾部带进错误信息。回归：`app_dsh_launch_test.go`。
- [ ] 若已做**源码级焊接**：补丁脚本已重放且锚点校验通过（未通过则先修锚点，**不得静默丢失我们的页面**）
- [ ] `~/.reasonix/` 用户数据完整
- [ ] `.ps1` 构建脚本与 `build/windows/installer/project.nsi` 仍带 UTF-8 BOM
      （`node scripts/ensure-bom.js --check build-deploy.ps1 build-installer.ps1 build/windows/installer/project.nsi`）。
      **`.nsi` 丢 BOM 会让 makensis 直接报 `Bad text encoding: project.nsi:109`** —— 2026-09-20
      e80d810 就是这么把安装包构建弄坏的（BOM=True → False），直到重建才暴露；`build-installer.ps1`
      现已内置前置守卫，缺失即中止并打印修复命令。改完 `.ps1`/`.nsi` 一律先补 BOM 再构建。

## 原则 7：测试会话/工作区清理准则（磁盘零残留）

> 集成测试每次 `session.create` 都会在 DSH 磁盘留一个临时工作区会话
> （`Temp\TestXxx/001`）。测试结束必须清理干净，**磁盘零残留**是硬性要求。

### 7.1 清理流程（按 test_helpers_test.go 的现行做法，别走捷径）

1. **先归档**：`workspace.archiveSession` RPC（DSH 运行时持有会话文件句柄，
   直接删磁盘会被占用；归档让 DSH 释放管理后再删才可靠）。
2. **等落盘**：`time.Sleep(~2s)`——DSH 会话目录是**延迟落盘**（cleanup 时可能尚未创建）。
3. **按 cwd 编码删整个工作区目录**：DSH 工作区目录名 = `--` + cwd 编码 + `--`
   （反斜杠→`-`、删除冒号）。按 cwd 删**不依赖 sid 落盘时机**，是根治残留的关键。
4. **延迟再删一次兜底**（等 DSH 写完/释放句柄后再清一次）。
5. **清理 t.TempDir 临时目录**（带重试；DSH 句柄占用时 3 次重试后仅警告，
   不视为失败——系统临时目录可自动回收）。

### 7.2 边界
- 清理逻辑只存在于**测试辅助**（`test_helpers_test.go`，不进产品代码）；
  **产品功能不做任何自动清理**（用户明确要求）。
- **测试不得污染用户配置目录（事故条款，2026-09-18）**：桥代码会写到用户目录
  （`~/.dsh/profiles/*/cordis.patch.yml`、`~/.reasonix/*.json`），因此**凡触碰这些路径的测试
  必须隔离 home**（`t.Setenv("DSH_HOME", t.TempDir())` / `USERPROFILE`），
  且由 `app_testmain_test.go` 的 `TestMain` **全局兜底**：未设 `DSH_HOME` 时自动指向临时目录，
  拿不到临时目录就拒绝运行。事故经过：一组旧 MCP 测试断言的是"写本地 JSON"的已废弃契约、
  又没隔离 home，于是把 4 条测试数据写进了**真实的** profile 配置；
  靠写入器的"写前备份"才逐字节恢复（551 字节 / SHA `C4C526494F29`）。
  推论：**任何改写用户配置的功能，都必须自带备份 + 写后校验 + 可恢复**（本次正是靠这条救回）。
- **磁盘清理 ≠ DSH 内存清理**：`session.list` 返回 DSH 内存索引。归档
  （`workspace.archiveSession`）会立刻把会话从所有走 `fetchTabs`/`fetchSessions` 的列表里去掉；
  DSH 自身在索引维护时也会丢弃磁盘已不存在的会话。真正的坑是**列表路径漏过滤**（见 7.3），
  而不是"非得重启 DSH"。
- 界面残留不代表磁盘残留：先查 `~/.dsh/sessions/` 磁盘（`001|Temp-Test|TestSet|TestSubmit` 工作区），
  磁盘为 0 即磁盘达标；界面仍残留则按 7.3 查过滤路径，最后才考虑重启 DSH。

### 7.3 归档过滤：每条"列会话"的路径都必须过滤

`workspace.archiveSession` **只是把 sessionId 记进** `workspace.list` 的 `archivedSessionIds`；
`session.list` 与 DSH 内存态**仍然返回该会话**。因此：

- 任何列举会话的桥方法都必须走 `fetchTabs()` / `fetchSessions()`（两者内部都过滤归档），
  **不要自己直接调 `session.list`**——漏一处就是界面长期残留。
- 事故记录（2026-09-13）：集成测试留下的 14 个 `Temp\...\TestXxx\001` 会话，磁盘目录早被
  `createTempSession` 的清理删干净、归档也做了，但 `app_tree.go` 的 `fetchSessions()` 当时没有
  过滤归档 → 侧栏项目树长期显示 6 个 "001" 项目，被作者发现。已修 + 加回归测试。
- 回归测试：`app_tree_archived_live_test.go`（**只读，不建会话**）断言 `fetchSessions`/`Tabs`/
  项目树里不出现归档会话与测试工作区；`app_tree_archived_test.go` 覆盖纯过滤函数。

## 原则 8：作者要求集成的 dsh 插件——随包必带 + dsh-std 合规（作者明示的项目精神）

> 凡作者（项目所有者）要求**集成到本项目**的 dsh 插件，构建安装包时必须带上：
> **懒人包与普通版都要有**（默认附加 或 置源在线下载），小白开箱即得、零操作。
> 同时集成插件必须符合 **dsh-std 协议**；不符合时作者要求集成**当下必须提醒**，
> 并附带 **dsh-std 化该插件**的选项。

### 8.1 集成与随包的机械规则（不要遗漏）

1. **清单唯一入口**：默认附加插件集 = `build/windows/installer/prepare-plugin-offline.ps1` 的
   `$plugins` 数组（name / spec 锁版 / defaultEnabled）。新插件在这里**加一行**即自动进入：
   - `prepare-plugin-offline.ps1` → 离线源 `plugins-offline/`（含 manifest.json）
   - `project.nsi` → 两种安装包都 `File /r plugins-offline`
   - `app_plugin_seed.go` → 首启自动注入 DSH profile（离线优先，无源时在线 `dsh plugin add` 回退）
2. **清单变更必须走完闭环**：改清单 → 重跑 prepare（联网机）→ 单测/冒烟 → 重建两种安装包
   （懒人包 `-Bundle` + 普通版）→ 体积确认 → 双端推送。
3. **镜像原则 6**：插件集成相关文件（prepare-plugin-offline.ps1、app_plugin_seed.go、
   project.nsi 打包段）属本项目独有实现，官方升级不得覆盖。
4. **内置 skill 与插件同等对待**：`skills/<name>/SKILL.md` 随包分发并在首启播种到
   `~/.dsh/skills`（`app_skill_seed.go`，幂等）。新增 skill 必须：
   - 放进仓库 `skills/`（`project.nsi` 的 `File /r "skills"` 自动带上；`build-installer.ps1`
     在打包前校验该目录存在，缺失即失败）；
   - frontmatter 必须含 `name`（**与目录名一致**）与 `description`；
   - `app_skill_seed_test.go` 的 `TestRepoSkillsArePackagable` 会在缺失/命名不符时报错。
   **播种的保护语义（不要改成无脑覆盖）**：播种时在 skill 目录写 `.dsh-seeded`（内容 =
   播入内容的 sha256）。目标不存在 → 写入；内容一致 → 跳过（缺标记则补上，使手工拷过的
   也能跟随升级）；内容不同但标记 == 当前内容 → 用户没动过，更新为新版；
   **内容不同且哈希对不上标记 → 判定用户改过，保留用户版本**（`kept-user`，只留日志）。
   卸载不删 `~/.dsh/skills`（用户数据）。

### 8.2 dsh-std 合规检查与提醒（每次要求集成插件时执行）

1. **先查形态**：`<插件>/dsh-plugin.json` 是否存在（或要求时先跑
   `DshStdParseManifest` / 官方 conformance）。
   - 有 dsh-plugin.json → 过 `AdmitPlugin` 五态准入，记录进档；
   - **无 dsh-plugin.json（cordis 形态——DSH 官方生态插件普遍如此）→ 即为"不符 dsh-std
     manifest 准入形态"，必须当下提醒作者**，不得静默集成。
2. **提醒措辞要点**：说明该插件是 cordis 加载器形态（`apply(ctx)` 服务/UI 插件）、
   dsh-plugin.json 缺失意味着准入器只能返回 manifestFound=false / unknown，
   无法做 dsh-std 能力协商（requires/permissions/facets 声明）。
3. **dsh-std 化选项（提醒时附带，作者可选）**：
   - **选项 A——生成配套 dsh-plugin.json**（声明层 dsh-std 化）：按官方
     `dsh-plugin-0.15.schema.json` 为该 cordis 插件写 manifest（id/版本/facets.host/
     requires/permissions/contributes/subscriptions），放在插件包内；效果 = 准入器能读到
     manifest 做五态评估（cordis 运行时行为不变，manifest 是能力声明层）。
   - **选项 B——桥层 adapter 声明**：本项目准入/协商记录"cordis loader + 该插件的
     package.json 能力映射"，以 degraded/兼容类别入库（不写插件侧文件）。
   - **选项 C——保持 cordis 原生**：作者明确接受 cordis 形态（官方生态现状），
     但每次集成仍走 8.2 的提醒流程，状态记入 `docs/dshstd-official-evidence.md` 或集成记录。
4. **状态留痕**：每次集成/提醒的结论（形态、是否 dsh-std 化、选项选择）写入
   `docs/plugin-integration-log.md`（新增条目追加），失败留痕兜底原则同样适用。

### 8.3 当前默认附加清单的 dsh-std 状态（原则 8 生效时的基线）

> 2026-09-14 更新：作者拍板**选项 A**，4 款均已补配套 `dsh-plugin.json`（声明层 dsh-std 化），
> 真实安装链准入全部 `compatible`。声明文件在 `build/windows/installer/plugin-manifests/`，
> 依据与边界见 `docs/plugin-integration-log.md`。

| 插件 | 版本 | defaultEnabled | dsh-plugin.json | 准入状态 |
|---|---|---|---|---|
| @openviking/dsh-memory-plugin | 0.3.0 | 禁用 | ✅ 配套（本项目补写） | compatible_degraded |
| @vectorize-io/hindsight-coding-agents | 0.4.3 | 禁用 | ✅ 配套 | compatible |
| @memtensor/memos-local-plugin | 2.0.18 | 禁用 | ✅ 配套 | compatible_degraded |
| @nanmicoder/dsh-agent-teams | 0.1.15 | 启用 | ✅ 配套 | compatible |

**新增插件时的强制动作**（原则 8.2 落地）：集成新插件必须同时补一份配套 manifest 到
`plugin-manifests/<dir>/`，并在 `prepare-plugin-offline.ps1` 的 `$plugins` 条目里填 `manifestDir`；
`app_plugin_manifests_test.go` 会在缺失时报错（声明与集成清单必须一一对应）。

## 原则 9：双端推送与同步（GitHub / CNB）

> 双远程（GitHub + CNB）同步是本项目的日常动作，但踩过两次同样的坑。以下三条是硬要求。

### 9.1 GitHub 推送必须用 HTTP/1.1（本网络环境）

- **症状**：`git push origin master` 连续失败，报
  `Failed to connect to github.com port 443 after 21064 ms: Could not connect to server`；
  重试 20 次全败，看起来像"GitHub 挂了"。**实际不是网络断，是 HTTP/2 被阻断。**
- **判据（区分"真断网"与"协议被卡"）**：
  - `api.github.com`（200）、`codeload.github.com`（200）可达；
  - `curl --resolve github.com:443:20.205.243.166 https://github.com/` 也返回 200，
    连 `/info/refs?service=git-upload-pack` 都是 200；
  - 说明 TCP+证书+端点都通，只有 git 走的 HTTP/2 连接建立失败。
- **修复（仓库级即可，不必动全局）**：`git config http.version HTTP/1.1`。
  改完第一次 push 就从 `exit=128（连接失败）` 变成 `exit=1（已进入协商阶段）`。
- 另：本机 `~/.ssh` 无私钥，所以 SSH-over-443（`ssh.github.com:443` 可达）这条退路走不通；
  `.tools/bin/gh.exe` 已认证（scopes 含 `repo`）可作 API 兜底。
- **⚠️ 实测补充（2026-09-15，开发机 chenz 桌面机）**：上述 `http.version=HTTP/1.1` 修复
  **不是万能的**——在本机配置后 `git fetch origin` **仍然** 443 超时
  （`Failed to connect to github.com:443 after 21067 ms`），说明该环境的障碍不只是 HTTP/2。
  本机可用的替代路径（已验证）：
  1. GitHub 推送继续走 **API 通道**（`upload.ps1`，`gh auth token` 认证）；
  2. GitHub 侧因此产生**平行链**（SHA 与本地/CNB 不同，见 9.2），**不要再尝试强推统一**
     （直推不通时无法统一，强推只会白等一轮超时）；
  3. 内容等价性改用 **tree 哈希比对**——比 `git diff --name-only` 更严谨（逐字节等价）：
     ```powershell
     git rev-parse master^{tree}          # 本地 / CNB
     # GitHub 侧：GET /repos/<owner>/<repo>/commits/master → commit.tree.sha
     ```
     两者相同即内容一致。2026-09-15 实测：本地 `261ccbb` / CNB `261ccbb` / GitHub `9f1bf282`
     ——SHA 不同，但 **tree 均为 `3572819c852f…`，内容完全等价**。

### 9.2 并行链：内容相同、SHA 不同

同一份改动在 GitHub 与 CNB 各自的 clone 里分别提交，会产生**两条内容等价但 SHA 不同的链**
（提交标题逐字相同）。此时 push 报 `! [rejected] master -> master (fetch first)`。

- **判定**（必须先做，这一步决定能不能强推）：
  `git diff --name-only <remote>/master master` **为空** = 内容零差异。
- **统一**：`git push --force-with-lease origin master`。
  用 `--force-with-lease` 而不是 `--force`——前者在远程被他人更新时会拒绝，不会误伤。
- **禁止**：本地存在**未推送的独有提交**时强推（会丢提交）。
  先用 `git rev-list --left-right --count <remote>/master...master` 确认右侧（本地独有）的来源可解释。

### 9.3 标准同步动作（照抄执行）

```powershell
git fetch origin --prune; git fetch cnb --prune          # 1) 两端都拉
git rev-parse origin/master cnb/master master            # 2) 三方 SHA 对比
git diff --name-only origin/master cnb/master            # 3) 空 = 两条链内容等价
git merge --ff-only <较新的那一侧>                        # 4) 本地无独有提交时快进
git push --force-with-lease <落后的一侧> master            # 5) 统一 SHA（见 9.2）
git push <已同步的一侧> master                            # 6) 确认 Everything up-to-date
```

同步完成后，三方 `git rev-parse` 必须完全一致；`git status --porcelain` 必须为空。

