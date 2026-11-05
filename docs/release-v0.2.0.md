# DSH-ReasonixUI v0.2.0

DSH-Reasonix 桌面端：**Wails 外壳 + Reasonix 前端 + DSH 后端桥接**（后端 127.0.0.1:3080）。

> 上一发布 `v0.1.0`（2026-08-16）是 **Electron 架构**的历史版本，与本版本所在历史线没有共同祖先。
> `v0.2.0` 是 **Wails 架构线的首次正式发布**。

## 主要变化

### 架构
- Electron → **Wails**：原生 WebView2 + 原生拖拽（`--wails-draggable`），根除布局热切换时的侧栏 logo 叠影。
- 前端 dist 保持零改动，所有适配走 Go 桥（通用 RPC 透传，不设白名单）。

### 前端底座
- Reasonix 前端 **v1.29.0 → v1.38.2**（2026-09-17 完成升级，记录见 `docs/upgrade-v1382-log.md`）。
- 获得「设置-模型服务」独立页签、模型偏好、会话体验（standard/deep）等 v1.38.2 能力。
- v1.38.8+ 的宿主契约时代（`window.reasonixDesktop`）仍在评估中，未升级；评估见 `docs/upgrade-assessment-v1388.md`。

### 桥与实时链路
- 520+ 桥方法面，修复 **57 处桥方法参数错位**（Wails 参数个数不匹配会在 UI 角落静默报错）。
- DSH 实时事件流（`/api/events.mux` WebSocket）→ 会话实时滚动；live 会话实测 text 500+/reasoning 450+ 帧转发。
- 远程主机：远程文件读写、远程页签、端口转发、远程服务启停与日志。
- 附件链路：粘贴图片、拖拽附件、数据 URL 回填。

### 功能
- 项目树：事件驱动刷新、重命名/颜色/排序/移除、归档、在资源管理器中显示。
- 插件市场 v3：传统页码翻页 + 分页缓存、详情页 README 与预览图；插件准入（dsh-std 五态评估）。
- dsh-std Host 合规：`dsh-plugin.json` Community v0.15（`version: 0.2.0`）。
- 子代理面板、Agent 预设四模式（标准/PTC/极简/创造）、内存插件。
- 就地代码编辑器（预览内直接改文件 + 关键字/标识符自动补全 + Ctrl+S 写回）、文件预览、内置终端。
- 内置 skill 随包分发，首启播种到 `~/.dsh/skills`。
- DSH 版本检测/更新横幅、设置-更新页的 DSH 核心版本管理。
- 连接横幅自动收起、问题导航导轨修复、项目树早启动可见性修复。

### 工程与可复现
- `.gitattributes` 覆盖全部构建输入——闭合 131 个重放假差异。
- 构建门禁：dist 资产完整性（防「干净 checkout 卡加载中」）、桥 arity 校验、注入脚本语法门禁。
- 前端品牌改造 + 8 处注入由 `scripts/apply-*.js` 幂等重放（升级 dist 不丢功能）。
- 安装包双模式脚本（经典联网包 / 懒人离线包）与插件离线源播种脚本。
- 双端同步工具 `scripts/sync-remotes.js`：fetch → ls-remote → API 三级取证。

## 发布物

| 资产 | 说明 |
|---|---|
| `DSH-ReasonixUI-0.2.0-win64.exe` | 便携主程序（单文件，WebView2 运行时需系统已有或由 Windows 更新提供） |
| `DSH-ReasonixUI-0.2.0-win64-portable.zip` | 便携包：exe + `start-dsh.cmd` + README |
| `SHA256SUMS.txt` | 校验和 |

**使用前提**：需要一个可用的 DSH 后端（`@deepseek-ai/dsh`，`dsh web` 监听 127.0.0.1:3080）。
便携包内 `start-dsh.cmd` 会按「内置 runtime → 系统 `dsh` 命令」的顺序拉起后端；也可让应用自行拉起。

**已知限制**：本版**未重建 NSIS 安装包**（构建机未安装 makensis / NSIS），因此不提供 `Setup.exe`；
需要安装包的用户请等下一版或自行用 `build-installer.ps1` 构建。

## 校验

```powershell
# 校验（在资产所在目录）
Get-FileHash .\DSH-ReasonixUI-0.2.0-win64.exe -Algorithm SHA256
# 与 SHA256SUMS.txt 中对应行比对
```

## 相关文档

- 前端升级执行记录：`docs/upgrade-v1382-log.md`
- 前端升级评估（v1.38.8 存档）：`docs/upgrade-assessment-v1388.md`
- 上游 UI 漂移史：`docs/upstream-ui-drift.md`
- 项目原则（构建/发布/同步硬规则）：`PRINCIPLES.md`
