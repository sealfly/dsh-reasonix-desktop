# 故障分支归因审计（fault-layout-ghost）

对比参照点：
- **GitHub master** = `744c566`（升级官方 UI 前的项目基线）
- **官方旧版** = `reasonix-old-0814/DeepSeek-Reasonix-main-v2`
- **官方新版** = `reasonix-desktop`（v1.29.0）

## 一、故障分支改了什么（相对 master，742 行新增）

| 类别 | 内容 |
|---|---|
| 功能桥接（前序会话，同步 v1.29.0） | 本地终端方案A、质量地板、AI 重命名、历史目录/搜索、任务目录、上下文预算卡、窗口缩放、桌面外壳状态、思考内容 expanded |
| 拖拽 polyfill 重写 | 官方 11 个 `--wails-draggable: drag` 区域完整映射为 `-webkit-app-region: drag` |
| CapabilityDiagnostics / RuntimeDoctor 补全 | 从 `{}` 改为完整结构 |
| mockBotSettings | bot 从 `{}` 改为含 `qq/allowlist/connections` 的安全结构 |
| 叠影清理 | sidebar display 抖动 + 窗口级重绘 |
| modal X 热区 / controls no-drag | 事件委托扩热区 + 窗口控制按钮强制 no-drag |

## 二、关键结论：多数"官方新 bug"实为 master 桥占位被暴露

### 1. 拖拽失效 —— 不是版本切换引入

官方旧版 0814 与新版 v1.29.0 的 `--wails-draggable: drag` 选择器**完全相同**（11 个逐个一致）：

```
.sidebar / .topbar / .onboarding / .app-chrome / .app-chrome__identity /
.app--darwin .app-chrome--tabs .tabbar /
.app--windows-frameless:not(.app--workbench):not(.app--creation) .app-chrome--native-tabs .app-chrome__drag-rail /
.app--windows-frameless:not(.app--workbench):not(.app--creation) .app-chrome--native-tabs .tabbar /
.workspace-tabs-bar / .topicbar / .workbench-dock__tools
```

master 的旧 polyfill 只映射 `.topbar, .app-chrome`（并臆造了 `.topbar--drag`、`.app-chrome--tabs`、`.app-chrome--native-tabs` 三个官方不存在的类）。

**根因**：workbench 布局下 `.app-chrome` 被隐藏（appChromeHidden），真实拖拽区是 `.topicbar` / `.workbench-dock__tools` / `.workspace-tabs-bar`，旧 polyfill 全部未映射 → workbench 布局整窗拖不动。classic/creation 顶栏仍在所以能拖。

### 2. 设置面板关不掉 —— `CapabilityDiagnostics: () => ({})` 是 master 就有的

- 官方新旧版 DiagnosticsSettingsPage **完全一致**，都读 `report.summary.errors`（L127）。
- master 桥就是 `CapabilityDiagnostics: async () => ({})`（占位）。
- 升级后用户首次打开「诊断」页 → `undefined.errors` 崩溃 → React 崩溃 → modal 状态卡死 → "像钉子关不掉"。

### 3. `desktop preferences sync ... 'enabled'` —— `bot: {}` 是 master 就有的

官方新旧版 App.tsx 都有 `sidebarImQQAdded`（读 `bot.qq.enabled`），master 桥 `bot: {}` → 崩溃（catch 成 warn 兜底）。

## 三、真正由环境/官方设计引入的问题（仅两个）

| 问题 | 根因 | 责任 |
|---|---|---|
| 叠影（侧栏 logo 视觉残留） | 布局热切换（无 reload）+ Chromium 合成层在 Electron 下不释放 GPU 层；官方 Wails 环境无此问题 | Electron 兼容性 |
| X 太小 | 官方 ModalCloseButton 用 15px lucide X（新旧版一致） | 官方设计 |

## 四、我们自身引入的新 bug

**窗口控制按钮不可用**：日志证明点击未到达主进程（无 `[WIN-CTRL]`）。根因是重写后的 polyfill 把 `.app-chrome` 设为 drag，Electron 的 drag 命中吞掉了与之重叠的窗口控制按钮点击。已用 `.windows-window-controls { -webkit-app-region: no-drag }` 修复。

## 五、结论

"同步官方 v1.29.0 出现的新 bug"这一判断不准确：官方 0814→v1.29.0 的拖拽区、诊断页读法、bot 读法**完全没变**。真相是 GitHub master 的桥一直是占位实现，master 时代未暴露（用户未打开诊断页、主要用经典布局），v1.29.0 升级后暴露。真正由环境引入的只有叠影（Electron 合成层残留），真正由新代码引入的只有 controls 不可用（polyfill 副作用）。
