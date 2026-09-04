# 本地模型接入指南（离线 / 内网 / 隐私场景）

> DSH（DeepSeek Harness）可以把推理**接到本地 OpenAI 兼容端点**（ollama / LM Studio /
> vLLM 等），配合"懒人包"安装器（内置 dsh + node，见 `build-installer.ps1 -Bundle`）
> 即可在**完全离线**的机器上跑通"UI → dsh → 本地模型"整条链路。
> 本指南内容已**实测验证**（见文末"验证记录"）。

## 1. 原理

dsh 的模型层基于 OpenAI 兼容网关（pi-ai）：任何 provider 都只是一个
`apiKeyEnv + api: openai-completions + baseURL + models 列表` 的声明。
`baseURL` 可以是任意端点——包括 `http://127.0.0.1:11434/v1`（ollama）、
`http://127.0.0.1:1234/v1`（LM Studio）、vLLM / llama.cpp 等。
默认安装只带 deepseek / pi-ai 两个 provider 包，但**不需要装新包**——
自定义 provider 直接写在用户配置文件里即可。

## 2. 配置文件

用户配置在 `~/.dsh/settings.yaml`（示例见下方），凭据在 `~/.dsh/.credentials.yaml`。

### ollama 示例（本地跑 `qwen2.5:7b`）

```yaml
llm-pi-ai:
  providers:
    ollama-local:                     # provider id（任意，UI 里会显示）
      apiKeyEnv: OLLAMA_DUMMY_KEY     # ollama 不校验 key；指向一个占位环境变量即可
      api: openai-completions
      baseURL: http://127.0.0.1:11434/v1
      models:
        - id: qwen2.5:7b
        # - id: llama3.1:8b            # 可列多个
agent-default-model:
  provider: ollama-local
  model: qwen2.5:7b
```

设置环境变量（ollama 不校验，任意值都行）：

```powershell
# 当前会话（或写进系统环境变量使其永久生效）
$env:OLLAMA_DUMMY_KEY = "local"
```

> 如果 provider 需要真实 key（如 vLLM 带鉴权），把 `apiKeyEnv` 指向你设置的实际
> key 环境变量，并在 `~/.dsh/.credentials.yaml` 的 `refs` 里登记。

### LM Studio 示例

```yaml
llm-pi-ai:
  providers:
    lmstudio-local:
      apiKeyEnv: LMSTUDIO_DUMMY_KEY
      api: openai-completions
      baseURL: http://127.0.0.1:1234/v1
      models:
        - id: local-model            # 用 LM Studio 里加载的模型 id
agent-default-model:
  provider: lmstudio-local
  model: local-model
```

## 3. 生效方式

改完 `settings.yaml` 后**重启 DSH 后端**（或重启 Harness Desktop）生效。
重启后 UI 的模型选择里会出现你的本地 provider 和模型。

## 4. 与懒人包配合（完全离线）

1. 用 `build-installer.ps1 -Bundle` 产出懒人包（内置 dsh 全树 + node.exe，约 96MB）。
2. 在离线机器上安装：勾选「DSH 后端」组件 → 无 Node 时直接用内置运行时（node.exe + dsh），
   无需联网。
3. 按上文配好 `settings.yaml` 指向本地模型服务。
4. 启动器 `start-dsh.cmd` 优先使用 `$INSTDIR\dsh-runtime`（捆绑的 node + dsh），离线可靠。

注意：即使模型本地化，DSH 的**部分**功能（web 搜索、telemetry 等）仍尝试联网——
离线部署如遇失败属预期，核心对话不受影响。

## 5. 验证记录（2026-09-03 实测）

- 环境：dsh 0.1.1-rc.2，隔离的临时 `$DSH_HOME`，`--profile headless` 单次问答通道
  （全程不触碰正在运行的 3080 实例与真实配置）。
- 步骤：本地起了 mock OpenAI 兼容服务（127.0.0.1:18999，支持 SSE 流式）→
  `settings.yaml` 声明 `localtest` provider 指向该端点 → headless 对话。
- 结果：dsh 真实向 mock 端点发起了 chat/completions 请求，流式响应被正常消费，
  输出「【本地模型验证成功】」，退出码 0。✅
- 结论：`api: openai-completions + baseURL: 本地端点` 的声明式配置**完整可用**；
  任何 OpenAI 兼容本地服务（ollama / LM Studio / vLLM）均可按此接入。
