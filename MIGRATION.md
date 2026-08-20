# 迁移总结：Electron → Wails

DSH-ReasonixUI 从 Electron 迁移到 Wails v2 的完整记录。

## 迁移动机

Reasonix 前端（v1.29.0）**本来就是为 Wails 写的**，依赖三个 Wails 原生能力：

| 前端依赖 | Wails 原生 | Electron 手工模拟的问题 |
|---|---|---|
| `window.go.main.App` | 自动绑定 Go 方法 | preload.js 手工造假对象转 IPC，占位返回 `{}` 导致崩溃 |
| `window.runtime.EventsOn` | 原生事件 | 手工 eventsOn Map |
| `--wails-draggable: drag` | **原生窗口拖拽**（不碰渲染） | polyfill 成 `-webkit-app-region: drag` → **Chromium 合成层残留 → 侧栏 logo 叠影** |

核心结论：**Electron 版的所有问题（叠影/拖拽失效/桥崩溃/主题锁浅色），根因都是"把 Wails 前端塞进 Electron 后手工模拟 Wails 运行时，模拟得不完全"**。迁回 Wails 架构后，这些模拟层全部消失，问题根除。

## 迁移后的问题根除对照

| 问题 | Electron 根因 | Wails 解决 |
|---|---|---|
| 侧栏 logo 叠影 | `-webkit-app-region` 合成层残留 | 原生拖拽，**无 app-region**（前端 dist 0 次、Go 端 0 注入） |
| 拖拽失效 | 需 polyfill 且只覆盖顶栏 | 原生拖拽，`--wails-draggable` 全区域生效 |
| 设置面板关不掉 | `CapabilityDiagnostics` 返回 `{}` → 诊断页崩 | Go 返回完整结构（单元测试覆盖） |
| 主题锁浅色 | `GetThemeExperience` 返回 `{}` → auto | Go 返回 themeMode=dark |
| sync 崩溃 | `bot: {}` → 读 qq.enabled 崩 | mockBotSettings 完整结构 |

## 为什么 Wails 不会有这些问题（原理）

### 1. 叠影（最核心）

| | Electron（出问题） | Wails（没问题） |
|---|---|---|
| 前端标记 | `--wails-draggable: drag` | 同一个标记 |
| 谁处理 | 不认这个属性 → polyfill 成 `-webkit-app-region: drag` | Wails runtime（Go 层）**原生识别** |
| 底层机制 | `-webkit-app-region` 让 Chromium 把拖拽区放进**独立合成层** | 调 **OS 原生 API**（Windows `WM_NCHITTEST`）做窗口拖动 |
| 布局热切换 | 合成层没同步释放 → 旧帧（logo）**残留屏幕上** | sidebar 是普通 DOM，React 更新**不经过合成层** |

**一句话**：叠影的根子是「Electron 用渲染层的合成器去模拟本该由操作系统做的事」。Wails 直接交给操作系统，渲染层根本不知道"拖拽"，自然无残留。之前 Electron 版试过 body 抖动/hide-show/zoom/opacity/禁用 GPU 全无效——因为都在"清渲染残留"的错误方向打转；真正的解法是**别让拖拽经过渲染层**。

### 2. 拖拽失效

Electron 要手工 polyfill，旧 polyfill 只映射 `.topbar`/`.app-chrome`，**漏了 workbench 布局**的 `sidebar`/`topicbar`/`dock_tools`，换布局就拖不动。Wails 的前端 `--wails-draggable` 标记**天然全区域生效**，无需映射。

### 3. 桥占位崩溃

Electron 手工 JS 对象模拟 `window.go.main.App`，`CapabilityDiagnostics`/`bot` 返回 `{}` → 前端读 `report.summary.errors`/`bot.qq.enabled` → undefined 崩溃。Wails 的 `window.go.main.App` 由 **Wails 自动生成**，直接绑定 Go 强类型方法；缺方法或签名不匹配**编译期就报错**（`not a function` 就是这个过程），不会留到运行时才炸。

### 4. 主题锁浅色

同类问题：`GetThemeExperience` 返回 `{}` → 前端 normalize 成 auto → 移除 data-theme → 浅色。Wails 版 Go 返回精确的 `ThemeExperienceView` 结构（JSON tag 保证字段名），序列化后前端永远能读到字段。

### 结论

> **不是"Wails 比 Electron 好"，而是"这套前端代码是给 Wails 的机器写的，我们之前硬把它塞进 Electron 的机器，接口对不上，就用手工补丁糊接口，糊不全就漏"**。迁回 Wails，接口天然对得上，所有补丁（拖拽 polyfill、方法占位、结构兜底）全部删除，问题随根因一起消失。方向从一开始就反了——Wails 才是这套前端的原生宿主。

## 两个项目的关系

| 项目 | 路径 | 角色 |
|---|---|---|
| **dsh-reasonix-desktop**（Electron） | `C:\Users\chenz\Desktop\dsh-reasonix-desktop` | 旧版，fault-layout-ghost 分支保存了排查现场 |
| **dsh-reasonix-wails**（Wails） | `C:\Users\chenz\Desktop\dsh-reasonix-wails` | 新版，本迁移的成果 |

Electron 项目的 `renderer/dist`（v1.29.0 前端构建产物）被复制到 Wails 项目的 `frontend/dist`，**前端代码零改动**。

## 构建 & 运行

```powershell
$env:GOPROXY = "https://goproxy.cn,direct"

# 开发构建（关键：必须 -tags production）
go build -tags production -o DSH-ReasonixUI.exe .

# 正式打包（需 Wails CLI）
go install github.com/wailsapp/wails/v2/cmd/wails@v2.13.0
wails build          # 产出 build/bin/DSH-ReasonixUI.exe

# 测试（防崩溃结构）
go test ./...
```

## 项目原则（与 Electron 项目一致）

1. **前端 dist 零改动**：直接 go:embed frontend/dist。
2. **一切适配走 Go 桥**：App 方法转 DSH RPC。
3. **不限制 DSH 原生能力**：dsh.go 通用 RPC 透传，不设方法白名单。
4. **失败留痕兜底不崩溃**：关键方法返回完整结构（有单元测试防回归）。

## 关于「前端 UI 是否会限制 DSH 能力」的诚实边界

**桥层不限制**：`dsh.go` 的 `RPC(method, payload)` 是通用透传，任意 DSH 方法（含插件动态注册的）都能调，不设白名单。这是架构上的保证。

**但前端 UI 自身有功能边界**：Reasonix 前端的 bridge 只定义了约 340 个方法（会话/模型/项目树/设置/终端/历史等），对应 DSH 的核心能力。DSH 有而前端 UI 没有 GUI 入口的能力（如 `host.*` 底层文件操作、`credentials.*` 凭据管理、插件动态注册的方法），用户**无法通过这个 GUI 直接触发**。

这不是"阉割" DSH，而是"GUI 功能边界"——Reasonix 前端本来就是为 AI 编程助手设计的，DSH 也是同类后端，两者核心能力高度重合；重合之外的能力（尤其插件动态方法）仍可通过 DSH 的 API/CLI 访问，只是这个特定 UI 没提供按钮。若要扩展，需改前端 dist 加界面（会破坏"零改动"原则）。

## 剩余可选工作（不影响核心目标）

- MCP/远程/任务目录等低频方法的完整数据实现（当前返回空态，前端降级）
- closeBehavior 的 background 模式（当前简化为 quit）
- 质量地板/窗口缩放的持久化落盘（当前内存）
