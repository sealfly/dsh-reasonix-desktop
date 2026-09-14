# 插件集成日志（plugin-integration-log）

> 依 PRINCIPLES.md 原则 8.2：每次作者要求集成 dsh 插件 → 形态判定 → 不符 dsh-std 必须提醒 →
> 附带 dsh-std 化选项 → 结论留痕。追加式维护，不改历史条目。

## 2026-09-08 基线：默认附加插件集（懒人包 + 普通版随包）

集成清单入口：`build/windows/installer/prepare-plugin-offline.ps1` 的 `$plugins` 数组
（本次变更 commit bb4c7ca 建立机制）。随包闭环：离线源 plugins-offline（221MB，win32-only 裁剪）
→ project.nsi 两包同带 → app_plugin_seed.go 首启注入（离线优先/在线回退，幂等，留痕
`~/.reasonix/plugin-seed.json`）。

| 插件 | 版本 | defaultEnabled | 形态判定 | dsh-std 提醒 | 作者决策 |
|---|---|---|---|---|---|
| @openviking/dsh-memory-plugin | 0.3.0 | 禁用（省 token，设置-记忆一键启用） | **cordis 形态（无 dsh-plugin.json）——不符 dsh-std 准入形态** | ✅ 已提醒（随原则 8 生效） | 待定（A 生成 manifest / B adapter / C 保持原生） |
| @vectorize-io/hindsight-coding-agents | 0.4.3 | 禁用 | **cordis 形态——不符** | ✅ 已提醒 | 待定 |
| @memtensor/memos-local-plugin | 2.0.18 | 禁用 | **cordis 形态——不符** | ✅ 已提醒 | 待定 |
| @nanmicoder/dsh-agent-teams | 0.1.15 | 启用 | **cordis 形态——不符** | ✅ 已提醒 | 待定 |

dsh-std 化选项（供选择）：
- **A. 生成配套 dsh-plugin.json**（声明层，按官方 dsh-plugin-0.15.schema.json；cordis 运行时不变）
- **B. 桥层 adapter 声明**（本项目侧记录 cordis loader 能力映射，不写插件文件）
- **C. 保持 cordis 原生**（作者明确接受；后续集成仍逐次提醒）

## 2026-09-14 作者拍板 **选项 A**：4 款全部完成 dsh-std 化（配套 manifest）

**落地内容**
- 配套声明文件：`build/windows/installer/plugin-manifests/<plugin>/dsh-plugin.json`（4 份，随包分发）
- 分发链路：`prepare-plugin-offline.ps1` 把声明写进离线源 `node_modules/<pkg>/dsh-plugin.json`
  （`$plugins` 数组新增 `manifestDir` 字段）→ 安装后 seed 复制整树时自动就位；
  `app_plugin_seed.go` 的 `ensureCompanionManifests()` 对**已存在插件**补写（幂等，已存在不覆盖）
- 回归保护：`app_plugin_manifests_test.go`——①每份 manifest 必须 valid 且 `compatible=true`；
  ②seed 清单每加一款插件都必须有配套 manifest 且 `name` 与包名一致

**声明依据（可核实，非编造）**
| 声明字段 | 依据来源 |
|---|---|
| `id` / `name` / `version` / `license` | 插件 `package.json`（license 缺失则如实省略） |
| `facets.host.entry` | `package.json` 的 `main`（index.mjs / dist/index.js / lib/index.js） |
| `requires.contracts` | `peerDependencies` → 契约映射：dsh-tools→`tool.dsh/v1 Tool`、dsh-commands→`command.dsh/v1 CommandRuntime`、dsh-session(-projection)→`session.dsh/v1alpha1 Session`、dsh-llm→`model.dsh/v1 ModelCatalog`、dsh-client-*（client.inject）→`presentation.dsh/v1alpha1 Presentation`、skill-filesystem/better-sqlite3→`storage.dsh/v1alpha1 Storage`（**optional + fallback**：Host 未声明支持 → degraded 而非拒绝） |
| `permissions` | 插件真实行为（限 8 个注册权限名）：写 `.agent-teams/`、sqlite 落盘、记忆读写 → `storage.local.read/write`；spawn 子智能体 → `session.create`；观察会话消息 → `messages.observe.read` |
| `subscriptions` | 源码 `ctx.on("<topic>")` 实证：openviking 5 个（`agent/session-start`、`agent/pre-step`、`session/event`、`session/flush`、`tools/pre-execute`）；其余未静态证实 → 空数组 |
| `contributes.commands` | 仅声明源码中确证的命令（agent-teams 的 `agent-teams`） |

**准入结果（真实安装链实测，`go test -count=1` 无缓存）**
| 插件 | 之前 | 现在 |
|---|---|---|
| @nanmicoder/dsh-agent-teams | manifestFound=false / unknown | **manifestFound=true / compatible** |
| @vectorize-io/hindsight-coding-agents | unknown | **compatible** |
| @openviking/dsh-memory-plugin | unknown | **compatible_degraded**（storage 可选契约 Host 未支持） |
| @memtensor/memos-local-plugin | unknown | **compatible_degraded**（同上） |

**已知边界（如实记录）**
1. manifest 是**声明层**：cordis 运行时行为不变，声明只描述"该插件在 Host 上如何使用"。
2. `contributes` 只支持 commands（v0.15 schema 无 tools 字段）→ 插件工具（`hindsight_*` 8 个、
   `memos_*` 7 个、`agent_teams_*` 15 个、openviking 的 MCP 动态工具）**无法在 manifest 中声明**。
3. openviking 工具经 MCP 代理动态提供、无静态命令 → commands 为空数组。
4. `@nanmicoder/dsh-agent-teams` 在本机是 Junction 链接到开发目录，写入声明会落到插件仓库工作区
   （`DSH-deskop/plugins/dsh-agent-teams/dsh-plugin.json`）——**建议**把这份声明合进插件本体仓库，
   这样 registry 版本自带声明，Host 侧无需补写。
5. 网络能力（hindsight 经 MCP 联网）不在 Host 的 8 项权限注册表内 → 未声明，属协议覆盖范围外。
