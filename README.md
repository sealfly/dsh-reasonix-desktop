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
| ask（每次询问） | workspace-write（保守） | 关键操作需确认 |
| auto（自动批准常用） | danger-full-access | 常用工具自动放行 |
| yolo（全自动） | danger-full-access | 全自动执行 |

- 会话列表按 DSH 实际权限回显当前模式（`read-only→ask / workspace-write→auto / danger-full-access→yolo`）
- 切换模式存到桥状态，**新建会话时按当前模式选择 agentPreset**（DSH 权限由 preset 决定，创建后不可运行中切换）
- `SetToolApprovalMode / SetToolApprovalModeForTab / ToolApprovalMode` 已映射

## 新建项目

- `PickBlankProjectParent` → Electron 目录选择对话框（可选父目录）
- `CreateBlankProject(parentDir, name)` → 真正创建项目文件夹（含名校验）
- 创建后自动 `OpenProjectTab` 打开并刷新会话列表
- 项目树「新建项目」入口完整可用

前置：DSH web 服务运行在 3080（`start-dsh-web.bat`），且 `reasonix-reference/desktop/frontend/dist` 已构建。

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