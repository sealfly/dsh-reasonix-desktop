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

### 2.1 品牌图标改动只限操作系统层，一律不碰项目内部 UI 图标

- 本项目语境里的"图标 / logo"**默认指操作系统层面**：窗口图标、任务栏图标、
  桌面与开始菜单快捷方式（`.lnk`）图标、安装包与卸载器图标。
  改动位置只有三处：`package.json` 的 `build.win.icon`、`build.nsis.installerIcon/uninstallerIcon`，
  以及 `src/main.js` 里 `BrowserWindow` 的 `icon`。
- **禁止**在桥接层注入或替换项目内部 UI 的图标（侧栏、设置导航、快捷栏等任何前端组件）。
  `src/preload.js` 的 `applyBranding()` 历史上出现过两次越界注入，均已移除：
  - `.sidebar__utility-button svg.lucide`（e8bd31a）——通配选择器，一次把侧栏快捷栏的
    回收站 / 定时任务 / 设置三个按钮的图标全部换掉；
  - `.settings-center__navitem svg.lucide-keyboard`（1f4a7d9、e5ddbf0）——把「设置-快捷键」
    项的键盘图标换掉。
- **术语对齐**：作者说"快捷键"指的是 **Windows 快捷方式（`.lnk`）**，不是应用内的
  「设置-快捷键」页；说"桌面 / 任务栏"指的是 OS 图标。有歧义时**先问**，
  不要按 UI 组件去猜，更不要扩大改动范围。
- 唯一保留的 UI 内品牌化是 `applyBranding()` 的文字标识（`.dsr-wordmark`：`D_LOGO` + `DSH-ReasonixUI`）
  与品牌位替换，属"品牌更新为 DSH-Reasonix"（c17d206）的既有需求，与"图标"无关，不受本条约束。

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
