# 上游 Issue 草稿（NanmiCoder/dsh-agent-teams）

> 状态：**已提交** → 见文件末尾「提交记录」。
> 用途：为上游补 `dsh-plugin.json`（dsh-std 声明清单）的建议书；正文可直接粘贴为 GitHub Issue / PR 描述。
> 单一来源：声明文件本体在 `build/windows/installer/plugin-manifests/dsh-agent-teams/dsh-plugin.json`。

---

## 标题

```
feat: 建议补一份 dsh-plugin.json（dsh-std 声明清单，Community Admission v0.15）
```

## 正文

### 背景

`@nanmicoder/dsh-agent-teams` 目前是 **cordis 加载器形态**（`package.json` 的 `dsh.bundle.patch` → `cordis.patch.yml`，另有 `dsh.client.inject` 注入 Web UI），仓库里没有 **`dsh-plugin.json`**。

对遵循 **dsh-std / Community Admission v0.15** 的宿主而言（例如桌面端项目 `dsh-reasonix-wails` 的插件准入器），缺少该清单会直接导致读不到声明：

```json
{ "manifestFound": false, "state": "unknown",
  "note": "no dsh-plugin.json found (plugin may be cordis-only or not installed yet)" }
```

也就是宿主**无法**对其做五态准入（compatible / compatible_degraded / waiting_authorization / rejected / unknown）与能力协商（requires / permissions / facets），也无法向用户如实陈列该插件用到了哪些能力与权限。

### 建议改动（2 个文件）

**1) 新增 `dsh-plugin.json`（放仓库根）** —— 内容见下方「完整清单」。

**2) `package.json` 的 `files` 数组加一项**（否则 `npm publish` 不会带上该文件，registry 版本仍缺声明）：

```diff
   "files": [
+    "dsh-plugin.json",
     "lib",
     "assets/agent-teams",
```

### 声明依据（均可从本仓库事实核对，非猜测）

| 字段 | 值 | 依据 |
|---|---|---|
| `id` | `com.nanmicoder.agent-teams` | 包名反域名化（schema 要求 namespaced、禁 `/`） |
| `version` / `license` | `0.1.15` / `MIT` | 本仓库 `package.json` |
| `facets.host.entry` | `lib/index.js` | 本仓库 `package.json` 的 `main` |
| `facets.host.apiVersion` | `v1alpha1` | schema 要求的 facet API 版本 |
| `requires.contracts` | `tool.dsh/v1 Tool`、`command.dsh/v1 CommandRuntime`、`session.dsh/v1alpha1 Session`、`model.dsh/v1 ModelCatalog`、`presentation.dsh/v1alpha1 Presentation` | `peerDependencies`：`dsh-tools`、`dsh-commands`、`dsh-session(-projection)`、`dsh-llm`、`dsh-client-ui-*` + `dsh-api-session-controller`（client.inject） |
| `permissions` | `storage.local.read/write`、`session.create`、`messages.observe.read` | 插件实际行为：`.agent-teams/` 状态落盘、成员即子智能体会话、队长/成员消息观察 |
| `contributes.commands` | `com.nanmicoder.agent-teams`（触发词 `agent-teams`） | 本仓库 `lib/command.js` |
| `subscriptions` | `[]` | 未在源码中确证 DSH 事件订阅（`lib/index.js` 里的 `on("data"/"end"/"error")` 是流事件，非 DSH 事件） |
| 网络权限 | **未声明** | 实测源码无外部域名（网络调用点为本地/DSH 自身 API），故不声明 `net.dsh.connect` |

### 实测效果（宿主侧，改动前后）

| 状态 | 结果 |
|---|---|
| 改动前 | `manifestFound=false`、`state=unknown` |
| 加声明后 | `manifestFound=true`、`state=compatible`、`compatible=true`（5 个契约全部被宿主支持） |

验证方式（宿主项目内，`go test -count=1` 无缓存）：

```
PluginDshStdAdmit("@nanmicoder/dsh-agent-teams")
→ {"compatible":true,"manifestFound":true,"state":"compatible"}
```

### 完整清单（可直接复制）

<details>
<summary>dsh-plugin.json</summary>

```json
{
  "$schema": "https://dsh.dev/schemas/dsh-plugin-0.15.schema.json",
  "manifestVersion": "0.15",
  "id": "com.nanmicoder.agent-teams",
  "name": "@nanmicoder/dsh-agent-teams",
  "version": "0.1.15",
  "license": "MIT",
  "source": "npm:@nanmicoder/dsh-agent-teams@0.1.15",
  "facets": {
    "host": {
      "entry": "lib/index.js",
      "apiVersion": "v1alpha1"
    }
  },
  "requires": {
    "contracts": [
      { "apiVersion": "tool.dsh/v1", "kind": "Tool" },
      { "apiVersion": "command.dsh/v1", "kind": "CommandRuntime" },
      { "apiVersion": "session.dsh/v1alpha1", "kind": "Session" },
      { "apiVersion": "model.dsh/v1", "kind": "ModelCatalog" },
      { "apiVersion": "presentation.dsh/v1alpha1", "kind": "Presentation" }
    ]
  },
  "permissions": [
    { "name": "storage.local.read", "scope": "workspace", "reason": "读取工作区内 .agent-teams/ 团队状态（team.json、任务与邮箱）" },
    { "name": "storage.local.write", "scope": "workspace", "reason": "持久化团队/成员/任务/消息状态" },
    { "name": "session.create", "scope": "session", "reason": "为每个成员派生子智能体会话（成员即子智能体）" },
    { "name": "messages.observe.read", "scope": "session", "reason": "观察队长与成员的消息以推进调度" }
  ],
  "contributes": {
    "commands": [
      {
        "id": "com.nanmicoder.agent-teams",
        "title": "AgentTeams",
        "description": "多智能体团队协作：队长/成员、带依赖的任务 DAG、邮箱消息与审查循环"
      }
    ]
  },
  "subscriptions": []
}
```

</details>

### 备注

1. **运行时行为零变化**：声明是静态清单层，不参与 cordis 加载；仅让宿主能读到能力/权限声明。
2. v0.15 schema 的 `contributes` **只有 `commands`**（无 tools 字段），因此插件的工具（`agent_teams_*`）无法在清单中声明；`provides` / `services` / `requires.services` / `contributes.panels` 在 v0.15 一律被拒。
3. 这是**建议**，若与项目规划不符可直接关闭；若方向可行，我可以提 PR（fork + 上述 2 处改动）。

## 提交记录

- 目标仓库：`NanmiCoder/dsh-agent-teams`
- 提交形式：GitHub Issue（附完整材料与 diff，供作者决策）
- 提交账号：`sealfly`
- **结果：已提交 → [#165](https://github.com/NanmiCoder/dsh-agent-teams/issues/165)（2026-09-14，state=open）**
- 后续：若作者认可方向，可再提 PR（fork + 上述 2 处改动）；若上游合入，本项目
  `ensureCompanionManifests()` 的补写会自然"无事可做"（插件自带声明），无需改本项目代码。
