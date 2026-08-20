# DSH-ReasonixUI（Wails 版）

Reasonix v1.29.0 前端的 Wails 外壳，桥接到 DSH 后端（127.0.0.1:3080）。

## 为什么从 Electron 迁到 Wails

Reasonix 前端是为 **Wails** 写的（依赖 `window.go.main.App` + `window.runtime` + `--wails-draggable` 原生拖拽）。之前的 Electron 桥把 `--wails-draggable` 映射成 `-webkit-app-region: drag`，触发了 Chromium 的合成层行为，导致布局热切换时**侧栏 logo 叠影**（旧帧残留）。Wails 的拖拽是原生层的，不经过渲染合成，根除叠影。

## 项目结构

```
├── main.go           Wails 入口（embed 前端 dist + 原生拖拽）
├── app.go            App 结构体 + 窗口控制
├── app_settings.go   设置/主题/诊断/bot（防前端崩溃结构）
├── app_session.go    会话（DSH session.* 透传）
├── app_models.go     模型（session.models/selectModel）
├── app_tree.go       项目树（session.list 按 cwd 分组）
├── app_cost.go       上下文预算卡/质量地板/AI 重命名/外壳状态
├── app_history.go    历史（列表/搜索/上下文）
├── app_prices.go     费用计算（prices.json 定价表）
├── app_terminal.go   终端桥方法
├── dsh.go            DSH 通用 RPC 透传（不设白名单，不限制能力）
├── settings.go       JSON 设置持久化
├── terminal.go       本地终端（os/exec spawn）
├── prices.json       费用定价表（可编辑，元/百万 tokens）
└── frontend/dist/    Reasonix v1.29.0 前端（从 Electron 项目复制，零改动）
```

## 构建 & 运行

```powershell
# 前置：Go 1.26+（goproxy.cn 代理）
$env:GOPROXY = "https://goproxy.cn,direct"

# 构建（关键：Wails 必须加 -tags production，否则编译"提示错误"占位）
go build -tags production -o DSH-ReasonixUI.exe .

# 或正式打包（需 Wails CLI）
go install github.com/wailsapp/wails/v2/cmd/wails@v2.13.0
wails build

# 运行
.\DSH-ReasonixUI.exe
```

## 验证清单（迁移核心目标）

1. **暗色主题**：GetThemeExperience 返回 themeMode=dark
2. **原生拖拽**：`--wails-draggable` 由 Wails runtime 处理，无 polyfill
3. **无叠影**：切工作台↔经典↔创作，侧栏 logo 不重复打印（核心验证点）
4. **设置面板**：诊断页不崩（CapabilityDiagnostics 返回完整结构）
5. **终端**：cmd/pwsh spawn + terminal:output/exit 事件

## 关键约定（防回归）

- **项目原则**（见 Electron 项目 PRINCIPLES.md）：前端 dist 零改动、一切适配走 Go 桥、不限制 DSH 能力（dsh.go 通用 RPC 透传）。
- **防崩溃结构**：GetThemeExperience / CapabilityDiagnostics / RuntimeDoctor / bot / StorageSettings / SkillsSettings 必须返回完整结构，不能返回空（否则前端读字段崩溃——这是 Electron 版踩过的坑）。
- **DSH 后端是共享实例**（3080），绝不主动关闭它（杀它会杀本会话）。
