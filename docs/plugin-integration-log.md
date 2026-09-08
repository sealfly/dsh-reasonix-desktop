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
