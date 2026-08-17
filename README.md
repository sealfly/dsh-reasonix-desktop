# DSH × Reasonix 桥接桌面端

**Reasonix 前端 + DeepSeek Harness 后端**——按你的思路：直接跑 Reasonix 桌面端的前端 UI，后端换成 DSH。

## 架构

```
┌─────────────────────────────────────────────────┐
│ Reasonix 前端（dist 构建产物，不动一行代码）        │
│  - 完整 UI：会话列表/对话流/步骤卡片/遥测/设置       │
│  - 通过 app Proxy 调 window.go.main.App           │
└──────────────┬──────────────────────────────────┘
               │ bridge 注入（preload）
┌──────────────▼──────────────────────────────────┐
│ 桥接层 src/preload.js                            │
│  - ListTabs → DSH session.list                   │
│  - SubmitToTab → DSH session.prompt              │
│  - 事件流 → DSH events.mux WebSocket             │
│  - DSH 会话 → Reasonix TabMeta 转换              │
│  - 未实现方法 → 安全兜底（不崩溃）                │
└──────────────┬──────────────────────────────────┘
               │ RPC / WS（127.0.0.1:3080）
┌──────────────▼──────────────────────────────────┐
│ DSH（DeepSeek Harness）dsh web 服务              │
│  - /api session.* RPC                           │
│  - /api/events.mux 事件流                        │
└─────────────────────────────────────────────────┘
```

## 使用

```bat
start.bat
```

## 安全模式三档 ↔ DSH 权限

Reasonix 的 ToolApprovalMode 三档与 DSH 的权限体系关联（从会话 projections 的 `permissions` 读取/同步）：

| Reasonix 模式 | DSH 权限 | 说明 |
|---|---|---|
| ask | read-only | 保守：写类工具会被 DSH 拒绝 |
| auto | workspace-write | 常用工具自动放行 |
| yolo | danger-full-access | 全自动执行 |

- 会话列表按 DSH 实际权限回显当前模式（`read-only→ask / workspace-write→auto / danger-full-access→yolo`）
- 切换模式通过 DSH `/permission` 命令在**运行中实时生效**（新建会话统一使用 `code` preset；权限独立于 preset 控制）
- `SetToolApprovalMode / SetToolApprovalModeForTab / ToolApprovalMode` 已映射
- 注意：DSH 权限是预设级，工具由 agent 按当前权限自动批准/拒绝，**不支持逐项人工审批**（`Approve/Reject` 明确返回不支持）

## 新建项目

- `PickBlankProjectParent` → Electron 目录选择对话框（可选父目录）
- `CreateBlankProject(parentDir, name)` → 真正创建项目文件夹（含名校验）
- 创建后自动 `OpenProjectTab` 打开并刷新会话列表
- 项目树「新建项目」入口完整可用

## 前置

- **无需预装 Node.js**：安装包内置便携版 Node（`resources/node`），应用启动时自动 `npx -y @deepseek-ai/dsh web` 拉起 DSH 后端（首次需联网下载，约 1-3 分钟）；若 3080 已有 DSH 实例则直接复用
- 开发模式：DSH web 服务运行在 3080（`start-dsh-web.bat`），且 `reasonix-reference/desktop/frontend/dist` 已构建（或复制到 `renderer/dist`）

## 桥接映射（当前已实现）

| Reasonix 方法 | DSH RPC |
|---|---|
| ListTabs / OpenProjectTab / OpenGlobalTab | session.list / session.create |
| Submit / SubmitToTab / Steer / SteerForTab | session.prompt |
| Cancel / CancelForTab | session.cancel |
| Meta / MetaForTab | session.list projections |
| onEvent（agent:event） | events.mux WebSocket → WireEvent |

未实现的 300+ 方法返回安全兜底（前端不崩溃），需要时逐个映射。

## 参考

- 前端源码/构建：`../reasonix-reference/desktop/frontend`（MIT）
- 桥接核心：`src/preload.js`（window.go.main.App 注入）