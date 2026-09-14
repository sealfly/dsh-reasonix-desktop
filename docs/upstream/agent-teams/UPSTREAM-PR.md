# 上游贡献材料：为 dsh-agent-teams 补一份 dsh-std 声明（dsh-plugin.json）

> 目标仓库：`@nanmicoder/dsh-agent-teams`（GitHub: NanmiCoder/dsh-agent-teams）
> 文件来源（本项目的单一来源，勿另存副本以免漂移）：
> `build/windows/installer/plugin-manifests/dsh-agent-teams/dsh-plugin.json`

## 为什么

该插件是 **cordis 加载器形态**（`package.json` 的 `dsh.bundle.patch` → `cordis.patch.yml`，
外加 `dsh.client.inject` 做 Web UI 注入），仓库里没有 **`dsh-plugin.json`**。
后果：任何遵循 **dsh-std / Community Admission v0.15** 的宿主（例如本项目
dsh-reasonix-wails 的插件准入器 `PluginDshStdAdmit`）读不到清单，只能给出：

```
{ "manifestFound": false, "state": "unknown",
  "note": "no dsh-plugin.json found (plugin may be cordis-only or not installed yet)" }
```

补上声明后，宿主可做**五态准入**（compatible / compatible_degraded /
waiting_authorization / rejected / unknown）与能力协商（requires/permissions/facets），
插件本身**运行时行为零变化**（声明是静态清单层，不参与 cordis 加载）。

## 改什么（两处，共 2 个文件）

### 1）新增文件 `dsh-plugin.json`（放仓库根）

内容 = 本项目 `build/windows/installer/plugin-manifests/dsh-agent-teams/dsh-plugin.json`。
关键字段与**依据**（均可从本仓库事实核对，非猜测）：

| 字段 | 值 | 依据 |
|---|---|---|
| `id` | `com.nanmicoder.agent-teams` | 包名反域名化（schema 要求 namespaced、禁 `/`） |
| `version` | `0.1.15` | `package.json` version |
| `license` | `MIT` | `package.json` license |
| `facets.host.entry` | `lib/index.js` | `package.json` main |
| `facets.host.apiVersion` | `v1alpha1` | 官方 schema 要求的 facet API 版本 |
| `requires.contracts` | Tool / CommandRuntime / Session / ModelCatalog / Presentation | `peerDependencies`：`dsh-tools`、`dsh-commands`、`dsh-session(-projection)`、`dsh-llm`、`dsh-client-ui-*` + `dsh-api-session-controller`（client.inject） |
| `permissions` | `storage.local.read/write`（`.agent-teams/` 状态落盘）、`session.create`（成员=子智能体会话）、`messages.observe.read`（队长/成员消息观察） | 插件实际行为（`lib/state.js`、`lib/scheduler.js`、`lib/members.js`） |
| `contributes.commands` | `com.nanmicoder.agent-teams`（触发词 `agent-teams`） | `lib/command.js` |
| `subscriptions` | `[]` | 未在源码中确证 DSH 事件订阅（`lib/index.js` 的 `on("data"/"end"/"error")` 是流事件，非 DSH 事件） |

### 2）修改 `package.json`：把声明纳入发布清单

```diff
   "files": [
+    "dsh-plugin.json",
     "lib",
     "assets/agent-teams",
```

（否则 `npm publish` 不会带上该文件，registry 版本仍缺声明。）

## 建议的提交信息

```
feat: 补 dsh-plugin.json（dsh-std 声明清单，Community Admission v0.15）

- 供遵循 dsh-std 的宿主做五态准入与能力协商（requires/permissions/facets）
- 声明依据取自本仓库 package.json（peerDependencies/main/license）与源码实际行为
- 运行时行为不变：声明是静态清单层，不参与 cordis 加载
- package.json files 纳入该文件，保证 npm 发布带上
```

## 如何验证（本地即可）

```bash
# 1) 结构校验：宿主侧解析器对未知字段零容忍（additionalProperties=false）
#    本项目等价实现：ParseDshPluginManifest（app_dshstd.go）
# 2) 准入：本项目 PluginDshStdAdmit("@nanmicoder/dsh-agent-teams")
#    期望：{ manifestFound: true, state: "compatible", compatible: true }
```

本项目实测结果（`go test -count=1`，无缓存）：

```
@nanmicoder/dsh-agent-teams  {"compatible":true,"manifestFound":true,"state":"compatible"}
```

## 备注

- 本项目已把这份声明**随安装包分发**（离线源 → 首启注入时补写到插件目录），
  并在 `ensureCompanionManifests()` 里对已装插件幂等补写——因此**上游合入后**，
  宿主侧无需再补写（声明随包而来），这也是本材料的目的。
- 若上游希望声明反映更多能力（工具列表等），注意 v0.15 schema 的 `contributes`
  **只有 `commands`**（无 tools 字段），工具无法在清单中声明；`provides`/`services`/
  `requires.services`/`contributes.panels` 在 v0.15 一律被拒。
