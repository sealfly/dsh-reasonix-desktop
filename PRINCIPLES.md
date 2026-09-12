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
   就地代码编辑器（`scripts/dsh-inline-editor.js`，含"编辑按钮只在文件内容预览出现"的门禁）等
   本项目注入，升级后按 `scripts/dsh-plugin-market-inject.js` 与
   `node scripts/apply-inline-editor.js`（幂等重放）重新注入。
3. **桥方法适配**：`app_*.go` 中所有"DSH 语义"实现（持久化桥：MCP、子智能体、技能偏好、
   插件市场等）——官方实现基于 Reasonix 自身后端（skill 文件、配置服务），与 DSH 桥不同，
   升级时**不得用官方实现覆盖本项目实现**，只吸收官方新增的方法面。
4. **持久化用户数据**：`~/.reasonix/` 下的用户数据（`mcp-servers.json`、
   `subagent-profiles.json`、`skill-preferences.json`、`plugins/`）是运行时数据，
   升级不涉及，也禁止升级流程触碰。

升级流程检查清单（对照官方 diff 时逐项勾选）：
- [ ] dist 中 logo/boot 品牌是否仍是本项目版
- [ ] `index.html` 是否仍含插件市场注入脚本
- [ ] `index.html` 是否仍含就地编辑器内联（`node scripts/apply-inline-editor.js` 幂等重放，脚本内会校验 `__DSH_INLINE_EDITOR__` 标记）
- [ ] 桥方法：本项目持久化实现（MCP/子智能体/技能偏好/插件市场）未被官方实现替换
- [ ] 本项目独有桥方法（`DshStd*`、`MarketPage`、`Terminal*` 等 23 个）未被删除
- [ ] `~/.reasonix/` 用户数据完整

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
- **磁盘清理 ≠ DSH 内存清理**：`session.list` 返回 DSH 内存索引，历史测试会话
  需**重启 DSH（Harness Desktop）重建索引**才会从界面消失（磁盘已净 → 重建后只剩真实会话）。
- 界面残留不代表磁盘残留：先查 `~/.dsh/sessions/` 磁盘（`001|Temp-Test|TestSet|TestSubmit` 工作区），
  磁盘为 0 即清理达标，界面残留属 DSH 内存态，走重启。

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

| 插件 | 版本 | defaultEnabled | dsh-plugin.json | 形态判定 |
|---|---|---|---|---|
| @openviking/dsh-memory-plugin | 0.3.0 | 禁用 | 无 | cordis 形态——**不符 dsh-std 准入形态（已按 8.2 提醒）** |
| @vectorize-io/hindsight-coding-agents | 0.4.3 | 禁用 | 无 | cordis 形态——同上 |
| @memtensor/memos-local-plugin | 2.0.18 | 禁用 | 无 | cordis 形态——同上 |
| @nanmicoder/dsh-agent-teams | 0.1.15 | 启用 | 无 | cordis 形态——同上 |

