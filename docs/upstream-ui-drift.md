# 官方 Reasonix 桌面端 UI / 契约漂移对照（自动生成）

> 由 `scripts/ui-drift-report.js` 生成，**每次官方发版后跑一次**：
> `node scripts/ui-drift-report.js --upstream-root <目录> --history` 看版本史；
> `node scripts/ui-drift-report.js --upstream-root <目录> --baseline-version <我们挂载的版本> --version <新版本> --doc docs/upstream-ui-drift.md` 出本报告。

---

## 一、跨版本特征史

```
# 官方前端特征版本史（本地 16 个版本：1.31.4 … 1.38.10）

◆ [锚点] 右栏 tab 容器 .workbench-dock__tabs
      1.31.4..1.37.0     12
      1.38.0..1.38.3     14
      1.38.4..1.38.10    22
◆ [锚点] 右栏 tab 按钮 .workbench-dock__tab
      1.31.4..1.37.0     56
      1.38.0..1.38.1     60
      1.38.2..1.38.3     51
      1.38.4..1.38.10    96
  [锚点] 右栏容器 .workbench-dock__body
      1.31.4..1.38.10    6
◆ [锚点] 设置导航项 .settings-center__navitem
      1.31.4..1.38.1     52
      1.38.2..1.38.10    59
◆ [锚点] 模型服务左列表 .provider-connections__item
      1.31.4..1.38.1     0
      1.38.2..1.38.10    12
◆ [锚点] 模型服务详情标题 .connection-title
      1.31.4..1.38.1     0
      1.38.2..1.38.10    10
  [锚点] 模型服务地址输入 .provider-url-input
      1.31.4..1.38.10    1
◆ [锚点] Composer 附件菜单 .composer-access-menu__section
      1.31.4..1.38.2     6
      1.38.3..1.38.7     11
      1.38.8..1.38.10    10
  [锚点] 文件树行 .workspace-tree__row（内联编辑器锚点）
      1.31.4..1.38.10    53
  [锚点] 启动壳 boot-shell / boot-shell__mark（品牌锚点，index.html）
      1.31.4..1.38.10    有（mark=3, name=2）
◆ [锚点] Wails 开发遮罩锚点 wails-spinner（index.html）
      1.31.4             无
      1.32.0..1.38.3     有
      1.38.4..1.38.10    无
◆ [契约] lib/bridge.ts（React-to-Go 契约）
      1.31.4             290KB / 436 方法 / window.go=是
      1.32.0             291KB / 438 方法 / window.go=是
      1.34.0             294KB / 443 方法 / window.go=是
      1.36.0             295KB / 451 方法 / window.go=是
      1.37.0             296KB / 453 方法 / window.go=是
      1.38.0             298KB / 455 方法 / window.go=是
      1.38.1             301KB / 461 方法 / window.go=是
      1.38.2..1.38.3     301KB / 468 方法 / window.go=是
      1.38.4..1.38.7     294KB / 462 方法 / window.go=否
      1.38.8             302KB / 489 方法 / window.go=否
      1.38.9             304KB / 506 方法 / window.go=否
      1.38.10            314KB / 526 方法 / window.go=否
◆ [契约] 宿主契约层 lib/desktopHost.ts
      1.31.4..1.38.3     无
      1.38.4..1.38.10    ★出现（window.reasonixDesktop 契约层）
◆ [契约] generated/desktopContract.generated.ts
      1.31.4..1.38.3     无
      1.38.4..1.38.10    ★出现（命令契约文件）
◆ [契约] scripts/check-desktop-host-boundary.mjs（禁 window.go）
      1.31.4..1.38.3     无
      1.38.4..1.38.10    ★出现（边界校验：会禁我们的调用）
◆ [契约] components/TabContainer/（可增删 tab 容器）
      1.31.4..1.38.3     无
      1.38.4..1.38.10    ★出现（2 个相关文件）
◆ [契约] 事件通道 agent:event
      1.31.4..1.38.3     4
      1.38.4..1.38.10    5
◆ [契约] bridge.ts 接口方法数
      1.31.4             436
      1.32.0             438
      1.34.0             443
      1.36.0             451
      1.37.0             453
      1.38.0             455
      1.38.1             461
      1.38.2..1.38.3     468
      1.38.4..1.38.7     462
      1.38.8             489
      1.38.9             506
      1.38.10            526
◆ [UI] Composer 验收控件（标准|交付）
      1.31.4..1.38.2     两个并排按钮(标准|交付)
      1.38.3..1.38.7     单个勾选项(交付)
      1.38.8..1.38.10    无
◆ [UI] Composer 模型切换栏（分类搜索栏）
      1.31.4..1.37.0     248 行 / 分类筛选=无
      1.38.0..1.38.1     254 行 / 分类筛选=无
      1.38.2             247 行 / 分类筛选=无
      1.38.3..1.38.7     258 行 / 分类筛选=无
      1.38.8             356 行 / 分类筛选=★有
      1.38.9..1.38.10    365 行 / 分类筛选=★有
◆ [UI] 设置页签结构（SettingsTab 成员 / providers 是否独立）
      1.31.4..1.38.1     19 个页签 / providers=独立 / ⚠旧重定向
      1.38.2..1.38.6     20 个页签 / providers=独立
      1.38.7..1.38.10    21 个页签 / providers=独立
◆ [UI] 主题契约键名（settings 快照侧）
      1.31.4..1.38.1     desktopTheme=39 / themeMode=18 / sessionExperience=0
      1.38.2..1.38.3     desktopTheme=37 / themeMode=18 / sessionExperience=66
      1.38.4..1.38.7     desktopTheme=41 / themeMode=19 / sessionExperience=68
      1.38.8..1.38.9     desktopTheme=41 / themeMode=19 / sessionExperience=40
      1.38.10            desktopTheme=41 / themeMode=19 / sessionExperience=43
◆ [UI] 模型服务：供应商预设（providerPresets）
      1.31.4..1.38.1     13
      1.38.2..1.38.3     26
      1.38.4..1.38.10    27
◆ [UI] 模型服务：自定义供应商类型（providerKinds）
      1.31.4..1.38.3     7
      1.38.4..1.38.10    8
◆ [UI] 模型服务：模型目录方法（FetchProviderModelCatalog 等）
      1.31.4..1.36.0     1
      1.37.0             2
      1.38.0..1.38.1     4
      1.38.2..1.38.10    8
◆ [UI] 右栏 tabs 渲染者
      1.31.4..1.38.1     App.tsx（内联）
      1.38.2..1.38.3     app-shell/WorkspaceDockRegion.tsx
      1.38.4..1.38.10    TabContainer/TabBar（官方 tab 体系）

◆ = 该特征在本地版本区间内发生过变化（要跟的重点）；无 ◆ 的表示全程一致。
```

---

## 二、本次对照（基线 → 目标）

# 官方前端 UI / 契约漂移报告

- 基线: C:\Users\chenz\Desktop\dsh-upstream-v1388\probe\src1.38.2\esengine-DeepSeek-Reasonix-f5745ba\desktop\frontend\src
- 目标: C:\Users\chenz\Desktop\dsh-upstream-v1388\probe\src1.38.10\esengine-DeepSeek-Reasonix-366e0eb\desktop\frontend\src
- 结论: 26 项观察点，8 项相同、13 项变化、1 项破坏性（锚点消失）、4 项需评估（契约/架构）

## 一、本项目注入/品牌依赖的锚点（缺一即碎）

| 项 | 基线 | 目标 | 结论 |
|---|---|---|---|
| 右栏 tab 容器 .workbench-dock__tabs | 14 | 22 | 变化 |
| 右栏 tab 按钮 .workbench-dock__tab | 51 | 96 | 变化 |
| 右栏容器 .workbench-dock__body | 6 | 6 | 相同 |
| 设置导航项 .settings-center__navitem | 59 | 59 | 相同 |
| 模型服务左列表 .provider-connections__item | 12 | 12 | 相同 |
| 模型服务详情标题 .connection-title | 10 | 10 | 相同 |
| 模型服务地址输入 .provider-url-input | 1 | 1 | 相同 |
| Composer 附件菜单 .composer-access-menu__section | 6 | 10 | 变化 |
| 文件树行 .workspace-tree__row（内联编辑器锚点） | 53 | 53 | 相同 |
| 启动壳 boot-shell / boot-shell__mark（品牌锚点，index.html） | 有（mark=3, name=2） | 有（mark=3, name=2） | 相同 |
| Wails 开发遮罩锚点 wails-spinner（index.html） | 有 | 无 | ★破坏 |

## 二、宿主契约与调用模式（决定升级工作量）

| 项 | 基线 | 目标 | 结论 |
|---|---|---|---|
| lib/bridge.ts（React-to-Go 契约） | 301KB / 468 方法 / window.go=是 | 314KB / 526 方法 / window.go=否 | 变化 |
| 宿主契约层 lib/desktopHost.ts | 无 | ★出现（window.reasonixDesktop 契约层） | ★需评估 |
| generated/desktopContract.generated.ts | 无 | ★出现（命令契约文件） | ★需评估 |
| scripts/check-desktop-host-boundary.mjs（禁 window.go） | 无 | ★出现（边界校验：会禁我们的调用） | ★需评估 |
| components/TabContainer/（可增删 tab 容器） | 无 | ★出现（2 个相关文件） | ★需评估 |
| 事件通道 agent:event | 4 | 5 | 变化 |
| bridge.ts 接口方法数 | 468 | 526 | 变化 |

## 三、用户可见 UI 形态

| 项 | 基线 | 目标 | 结论 |
|---|---|---|---|
| Composer 验收控件（标准|交付） | 两个并排按钮(标准\|交付) | 无 | 变化 |
| Composer 模型切换栏（分类搜索栏） | 247 行 / 分类筛选=无 | 365 行 / 分类筛选=★有 | 变化 |
| 设置页签结构（SettingsTab 成员 / providers 是否独立） | 20 个页签 / providers=独立 | 21 个页签 / providers=独立 | 变化 |
| 主题契约键名（settings 快照侧） | desktopTheme=37 / themeMode=18 / sessionExperience=66 | desktopTheme=41 / themeMode=19 / sessionExperience=43 | 变化 |
| 模型服务：供应商预设（providerPresets） | 26 | 27 | 变化 |
| 模型服务：自定义供应商类型（providerKinds） | 7 | 8 | 变化 |
| 模型服务：模型目录方法（FetchProviderModelCatalog 等） | 8 | 8 | 相同 |
| 右栏 tabs 渲染者 | app-shell/WorkspaceDockRegion.tsx | TabContainer/TabBar（官方 tab 体系） | 变化 |

## 需要人工评估的项

- **Wails 开发遮罩锚点 wails-spinner（index.html）**：有 → 无
- **宿主契约层 lib/desktopHost.ts**：无 → ★出现（window.reasonixDesktop 契约层）
- **generated/desktopContract.generated.ts**：无 → ★出现（命令契约文件）
- **scripts/check-desktop-host-boundary.mjs（禁 window.go）**：无 → ★出现（边界校验：会禁我们的调用）
- **components/TabContainer/（可增删 tab 容器）**：无 → ★出现（2 个相关文件）

## 用法

```powershell
# 列出本地已解压的官方版本
node scripts/ui-drift-report.js --upstream-root <目录> --list-versions
# 对照我们挂载的版本与新版本
node scripts/ui-drift-report.js --upstream-root <目录> --baseline-version 1.38.2 --version 1.38.10 --md docs/drift-1.38.10.md
```
