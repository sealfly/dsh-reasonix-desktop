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
2. **前端注入脚本**：插件市场（`__DSH_PLUGIN_MARKET`，现位于 `index.html`）、错误捕获等
   本项目注入，升级后按 `scripts/dsh-plugin-market-inject.js` 重新注入。
3. **桥方法适配**：`app_*.go` 中所有"DSH 语义"实现（持久化桥：MCP、子智能体、技能偏好、
   插件市场等）——官方实现基于 Reasonix 自身后端（skill 文件、配置服务），与 DSH 桥不同，
   升级时**不得用官方实现覆盖本项目实现**，只吸收官方新增的方法面。
4. **持久化用户数据**：`~/.reasonix/` 下的用户数据（`mcp-servers.json`、
   `subagent-profiles.json`、`skill-preferences.json`、`plugins/`）是运行时数据，
   升级不涉及，也禁止升级流程触碰。

升级流程检查清单（对照官方 diff 时逐项勾选）：
- [ ] dist 中 logo/boot 品牌是否仍是本项目版
- [ ] `index.html` 是否仍含插件市场注入脚本
- [ ] 桥方法：本项目持久化实现（MCP/子智能体/技能偏好/插件市场）未被官方实现替换
- [ ] 本项目独有桥方法（`DshStd*`、`MarketPage`、`Terminal*` 等 23 个）未被删除
- [ ] `~/.reasonix/` 用户数据完整
