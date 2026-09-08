# dsh-std / dsh-ecosystem-spec v0.15 官方 conformance evidence

> 记录日期：2026-09（Community v0.15 基线，TUI Admission v0.15）
> 本文件回答：**我们的 dsh-plugin.json 形态是否通过官方工具链的准入校验**。

## 工具链与运行方式

- 规范克隆：`dsh-ecosystem-spec`（T-Auto），submodule `vendor/dsh-std`（固定 revision `614dfa1`）
- 官方入口：`node scripts/conformance.mjs --standalone --manifest <file>`
  （standalone 模式自动初始化 vendor/dsh-std、安装并构建 `@dsh-std/*`，
  以 `registry/host-descriptor.tui.example.json` 为 Host、`registry/registry-0.15.json` 为契约注册表）
- 环境：Node v24

## 结果

| manifest | 结果 |
|---|---|
| 旧形态（description + supports + facets.plugin + id 含 `/`，缺 permissions/contributes/subscriptions） | ❌ `rejected` / `INVALID_MANIFEST`：`unknown field "description"` → `unknown field "supports"` |
| v3（去 description/supports，requires 仍声明 core.dsh#Negotiation） | ❌ `rejected`：`protocol definition is not admitted by this profile: core.dsh/v1alpha1#Negotiation` |
| **v4 self（空 requires 契约集 + 官方十字段全齐）** | ✅ **`valid: true, decision: "compatible"`** |
| **v4 生成示例（requires commands.dsh/v1alpha1 Command + permissions commands.invoke）** | ✅ **`valid: true, decision: "compatible"`** |

## 官方 v0.15 schema 关键事实（实测对照）

1. 顶层**无** `description` / `supports` / `services` / `provides` —— 出现即 rejected（unknown field）。
2. 顶层必填十字段：`$schema` `manifestVersion` `id` `name` `version` `facets` `requires`
   `permissions` `contributes` `subscriptions`（后三者可为空数组，requires 必须存在）。
3. `facets` 只有 `host`（required：entry + apiVersion）；**没有** `facets.plugin`。
4. `id` / 权限名 / 命令 id 必须 namespaced：`^[a-z][a-z0-9]*(?:[.-][a-z0-9][a-z0-9-]*)+$`
   —— **不允许 `/`**（`com.example/test` rejected；`com.example.test-plugin` OK）。
5. Registry 闭合（TUI-PKG-002）：requires.contracts 坐标必须被目标 profile 的
   `registry/registry-0.15.json` admit；`core.dsh/v1alpha1#Negotiation` 属于 dsh-std core
   协商层，**不在 tui profile registry**（引用即 rejected）。registry 内坐标示例：
   `commands.dsh/v1alpha1 Command`、`storage.dsh/v1alpha1 LocalStorage`、
   `messages.dsh/v1alpha1 MessageObserver`、`presentation.dsh/v1alpha1 *`、
   `tui.dsh/v1alpha1 *`、`workspace.dsh/v1alpha1 WorkspaceProvider`。
6. optional 契约必须带 fallback（缺 fallback → rejected / missing-fallback）。

## 对应代码侧落地

- `app_dshstd.go`：`dshPluginManifest` / `ParseDshPluginManifest` 按官方 schema 收紧——
  未知顶层字段拒绝、十字段必填、facets.host 必填(entry+apiVersion)、id/命令/权限 namespaced、
  optional 契约带 fallback、contributes.commands 校验；`DshStdSelfManifest` /
  Host Descriptor 的 id 改为 `com.dsh-reasonix.desktop`（namespaced 无 `/`），
  self manifest 用空 requires 契约集（registry 闭合可过）。
- `skills/dsh-std-plugin-gen/SKILL.md`：模板与规则按官方 schema + registry 闭合重写。

## 声明边界（诚实）

- 本 evidence 验证的是 **manifest 形态层**（schema + registry 闭合）在官方工具链下的通过性。
- **未覆盖**：官方 conformance 的其余要求（effect ledger / 依赖闭合 / 远端确定性 / registry hash
  漂移等）——本 Host 未实现，也不影响"manifest 准入判断"这一层。
- 规范本身为 Draft/Experimental：任何实现只能声明"实验适配"，不能自我认证。
