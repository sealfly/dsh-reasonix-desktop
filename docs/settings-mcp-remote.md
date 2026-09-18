# 设置页完善记录：【MCP 与工具】与【远程 SSH】

> 2026-09-18 ｜ 背景：这两页此前是**空壳**——MCP 页把服务器写进一份 DSH 永远不会读的私有
> JSON（`~/.reasonix/mcp-servers.json`，所以加多少服务器 DSH 都不受影响、工具数恒为 0）；
> 远程 SSH 页的主机列表/扫描/状态方法全是 `return []any{}` / `return nil`，
> 页面能开但**永远是空的**、扫描无反应、状态恒空白。

---

## 1. 【MCP 与工具】——改成对 DSH 真实生效

### 1.1 机制（实测确认）

DSH 的 MCP 支持来自插件 `@deepseek-ai/dsh-mcp-client`，**每个 MCP 服务器 = profile 配置里的
一个插件实例**，且该插件支持 HMR 热替换（改配置即断线重连，无需重启）：

```yaml
# ~/.dsh/profiles/<profile>/cordis.patch.yml
- id: mcp-github
  name: '@deepseek-ai/dsh-mcp-client'
  disabled: true                    # ← 启停开关（HMR 即时生效）
  config:
    serverName: github              # 模型看到的工具名 mcp__<serverName>__<tool>
    transport: stdio                 # stdio | streamable-http
    command: npx
    args: ['-y', '@modelcontextprotocol/server-github']
    env: { GITHUB_TOKEN: xxx }
```

### 1.2 实现（`app_mcp_dsh.go`）

| 方法 | 行为 |
|---|---|
| `MCPServers` | 解析 profile patch 层的 mcp-client 实例 → `ServerView[]`；**并列出旧本地 JSON 里的历史配置**（标 `source=legacy-local`），避免用户以为配置丢了 |
| `AddMCPServer` / `UpdateMCPServer` / `RemoveMCPServer` | **外科式改写** profile 配置：只替换 MCP 块，其它行（注释、别的插件、用户手写内容）**逐字节保留**；写前备份、写后重解析校验、原子替换（tmp+rename）；改名被拒绝（name 是稳定身份，与官方一致） |
| `InstallMCPServer` | 写入 + 返回 `MCPInstallResult`（`state/action/message`），并**顺带迁移**旧本地 JSON 的同名条目 |
| `SetMCPServerEnabled` | 设/清 `disabled:`（真实启停；旧本地条目会先迁移再启用——否则"启用"毫无意义） |
| `ReconnectMCPServer` | 重写块 + touch 文件（触发 HMR 重连） |
| `MCPCapabilityMatrix` | 列出已配置服务器的连接方式与工具前缀 |
| `AuthenticateMCPServer` | **明确报"不支持"**（实测 dsh-mcp-client 无交互授权流程），不假装成功 |
| `ClearMCPServerAuthentication` | 无授权态可清 → 成功 |
| `AnswerMCPInteractionForTab` | DSH 未实现 elicitation 通道 → 明确报不支持 |

### 1.3 诚实边界（不伪造状态）

DSH **没有暴露任何 MCP 状态 RPC**（`mcp.*` / `plugin.*` 实测 404），`~/.dsh/logs` 也是空的，
因此页面上的状态是诚实映射：**禁用 → `disabled`；启用 → `deferred`（DSH 按需连接）**，
绝不谎报 `connected`；工具数返回 0 并说明"连接后才会注册 `mcp__<name>__<tool>`"。

### 1.4 实测（真机 + 真 profile）

```
MCPServers(before)        → []
InstallMCPServer          → state=ready，message="…模型可用的工具名为 mcp__dsh-e2e-probe__<工具>"
MCPServers(after)         → enabled=true status=deferred transport=stdio command=npx
                            args=[-y, @modelcontextprotocol/server-everything] envKeys=[PROBE_TOKEN] source=plugin
SetMCPServerEnabled(false)→ enabled=false status=disabled
SetMCPServerEnabled(true) → enabled=true  status=deferred
ReconnectMCPServer        → ok
MCPCapabilityMatrix       → configured=1 enabled=1 toolPrefix=mcp__dsh-e2e-probe__
AuthenticateMCPServer     → "DSH 的 MCP 客户端不支持交互式授权；如需鉴权请在 env/headers 里填令牌"
RemoveMCPServer           → 列表回到 []
profile 前后 SHA           → C4C526494F29 完全一致（无残留）
```

---

## 2. 【远程 SSH】——接真实数据与真实动作

| 方法 | 行为 |
|---|---|
| `ScanSSHConfig` | **真实解析** `~/.ssh/config`（Host/HostName/User/Port/IdentityFile/ProxyJump），跳过 `Host *` 这类模板条目；已在库里的不重复给出 |
| `RemoteHosts` / `Add` / `Update` / `RemoveRemoteHost` | 主机库持久化到 `~/.reasonix/remote-hosts.json`（原子写），字段对齐 `RemoteHostView` |
| `RemoteConnectionStatuses` | **真的跑一次 ssh 探测**（`BatchMode=yes` + `ConnectTimeout=6`，隐藏窗口，不用密码交互）→ `connected` / `degraded`（主机可达但认证失败）/ `stopped` + **原始错误文本**；结果 TTL 20s 缓存，避免页面重渲染反复拉起 ssh |
| `ConnectRemoteHost` / `DisconnectRemoteHost` | 主动探测并记录；失败**返回错误**（前端据此提示），断开即状态归零 |
| `ScanRemoteLegacyWorkbenchData` / `CleanRemoteLegacyWorkbenchData` | 扫描/清理旧版远程工作台遗留物（`~/.reasonix/remote-mirrors`、`remote-trust.json`），镜像字节数**递归统计** |

### 明确不做（诚实边界，不返回假数据）

远端 DSH 服务托管（`RemoteServerStatus/Logs/StopRemoteServer` → 如实报 `stopped`/空）、
端口转发隧道（`RemoteForwards` → 空表；`AddRemoteForward` → **明确报未实现**）、
远端会话/标签。这些属于"远程开发子系统"，设置页并不使用。

### 实测（真机）

```
RemoteHosts               → 0（你这台机器没有 ~/.ssh/config，扫描为空 = 正确）
ScanSSHConfig             → 0
ScanRemoteLegacyWorkbenchData → {mirrorCount:0, mirrorBytes:0, trustFile:false}
AddRemoteHost             → id=host-127-0-0-1-nobody-65000，列表 1
RemoteConnectionStatuses  → state=stopped
ConnectRemoteHost         → 报错 "ssh: connect to host 127.0.0.1 port 9: Connection refused"
                            ← 这条真实 ssh 报错证明探测确实在跑，而不是伪造状态
RemoveRemoteHost          → 列表 0
```

---

## 3. 事故与加固：测试污染了真实 DSH profile

**事故**：MCP 的旧测试断言的是"写本地 JSON"的旧契约、又**没有隔离 `DSH_HOME`**，
在新实现下它们把 4 条测试数据（`mcp-gh` / `mcp-filesystem` / `mcp-srv` / `mcp-b`）
写进了**用户真实的** `~/.dsh/profiles/web/cordis.patch.yml`。
靠写入器留下的 `.bak-dsh-mcp-*` 备份恢复到原状（551 字节，SHA `C4C526494F29`，逐字节一致）。

**加固（三层）**：
1. `app_testmain_test.go` 的 `TestMain` **全局守卫**：测试进程若未设 `DSH_HOME`，自动指向临时目录
   （拿不到临时目录就拒绝运行）——任何测试都再也碰不到真实 `~/.dsh`。
2. 旧契约测试删除，只保留纯函数测试；新契约测试全部 `t.Setenv("DSH_HOME", t.TempDir())` 隔离
   （见 `app_mcp_remote_test.go`，13 项）。
3. 写入器本身保留"写前备份 + 写后重解析校验"，并已在 PRINCIPLES 原则 7（磁盘零残留）中登记
   "测试不得污染用户配置目录"。
