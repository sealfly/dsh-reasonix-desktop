# 生成缺失的 Go stub 方法（一次性工具：从 Electron 旧版前端/preload 对比生成）。
# 可移植：所有路径参数化，默认从 $PSScriptRoot 与 %USERPROFILE% 推导；
# 换电脑后路径变化时传参数即可：.\gen-stubs.ps1 -ReasonixFrontendSrc <...> -PreloadFile <...>

param(
  # 旧版前端 src 目录（含 app.xxx( 调用的 ts/tsx）
  [string]$ReasonixFrontendSrc = "",
  # 旧版 Electron preload.js
  [string]$PreloadFile = "",
  # 本仓库 Go 源码目录（默认脚本所在目录）
  [string]$GoSrcDir = "",
  # 输出文件
  [string]$OutFile = "",
  # preload 方法清单（名\t参数串，由 preload 分析脚本生成）
  [string]$PreloadMethodsTxt = ""
)

$ErrorActionPreference = "Stop"
if (-not $GoSrcDir) { $GoSrcDir = $PSScriptRoot }
if (-not $OutFile) { $OutFile = Join-Path $GoSrcDir "app_stubs2.go" }
if (-not $PreloadMethodsTxt) { $PreloadMethodsTxt = Join-Path $env:TEMP "preload-methods.txt" }
# 旧版仓库默认位置：本机旧存档（换电脑请用 -ReasonixFrontendSrc / -PreloadFile 指定）
if (-not $ReasonixFrontendSrc) { $ReasonixFrontendSrc = Join-Path $env:USERPROFILE "Desktop\DSH-deskop\reasonix-desktop\desktop\frontend\src" }
if (-not $PreloadFile) { $PreloadFile = Join-Path $env:USERPROFILE "Desktop\dsh-reasonix-desktop\src\preload.js" }

foreach ($p in @(
  @{ Name = "ReasonixFrontendSrc"; Path = $ReasonixFrontendSrc; Need = "前端 ts 源码" },
  @{ Name = "PreloadFile"; Path = $PreloadFile; Need = "旧版 preload.js" },
  @{ Name = "PreloadMethodsTxt"; Path = $PreloadMethodsTxt; Need = "preload 方法清单" }
)) {
  if (-not (Test-Path $p.Path)) {
    Write-Error "缺少 $($p.Name)（$($p.Need)）：$($p.Path)`n请用 -$($p.Name) 参数指定实际路径"
    exit 1
  }
}

# 前端调用的方法
$frontendMethods = Get-ChildItem $ReasonixFrontendSrc -Recurse -Include *.ts,*.tsx |
  Where-Object { $_.FullName -notmatch '__tests__|bridge.ts' } |
  Select-String -Pattern 'app\.[A-Z][A-Za-z0-9]+\(' -AllMatches |
  ForEach-Object { $_.Matches } |
  ForEach-Object { $_.Value -replace 'app\.','' -replace '\(','' } |
  Sort-Object -Unique

# Go 已实现
$goMethods = Select-String -Path (Join-Path $GoSrcDir "*.go") -Pattern 'func \(a \*App\) ([A-Z][A-Za-z0-9]+)\(' |
  ForEach-Object { $_.Matches[0].Groups[1].Value } | Sort-Object -Unique

# preload 方法定义（名 -> 参数串）
$preloadDef = @{}
Get-Content $PreloadMethodsTxt | ForEach-Object {
  $parts = $_ -split "`t", 2
  if ($parts.Count -eq 2) { $preloadDef[$parts[0]] = $parts[1] }
}

# 缺失 = 前端调用 - Go已有 - preload 内部函数
$internal = @('applyDefaultApproval','buildProjectTree','call','catalog','clearHistory','dshModelsFor','getConfig','history','onEvent','prompt','rpc','sessions','sessionToHistoryMeta','setConfig','setOfficialPrice','setRelayPrice','updateDsh','versions','cancel','deleteSession','createSession','EventsOff')
$missing = $frontendMethods | Where-Object { $_ -notin $goMethods -and $_ -notin $internal } | Sort-Object -Unique
Write-Host "前端调用但 Go 缺失: $($missing.Count) 个"

function Get-GoType($param) {
  $p = $param.Trim()
  if ($p -eq '' ) { return $null }
  if ($p -match '^(req|options|state|payload|config|settings|body|params|args|input)$') { return 'map[string]any' }
  if ($p -match '^(enabled|pinned|sandbox|force|checked|visible|running|open|active|explicit|auto|dryRun|allowAll)$') { return 'bool' }
  if ($p -match '^(factor|zoom|ratio|ms|depth|n|cols|rows|index|limit|cursor|count|size|revision|turns|port|timeout|height|width|score|generation)$') { return 'int' }
  return 'string'
}

$lines = New-Object System.Collections.ArrayList
[void]$lines.Add("package main")
[void]$lines.Add("")
[void]$lines.Add("// 批量生成的空实现（gen-stubs.ps1 从 preload.js + 前端调用清单对比生成）。")
[void]$lines.Add("// 覆盖前端调用但 Go 端缺失的方法，返回安全空态/降级，防 not-a-function 崩溃。")
[void]$lines.Add("")

foreach ($m in $missing) {
  $params = $preloadDef[$m]
  $goParams = @()
  $goArgs = @()
  if ($params -and $params.Trim() -ne '') {
    foreach ($p in ($params -split ',')) {
      $pname = $p.Trim()
      if ($pname -eq '') { continue }
      $type = Get-GoType $pname
      if ($null -eq $type) { continue }
      $goParams += ('_' + $pname + ' ' + $type)
      $goArgs += ('_' + $pname)
    }
  }
  $paramStr = $goParams -join ', '
  $argStr = ''
  if ($goArgs.Count -gt 0) { $argStr = ', ' + ($goArgs -join ', ') }

  $ret = 'err'
  if ($m -match '^(List|Query|Scan|Jobs|Checkpoints|Models|Plugins|Skills|Commands|ThemePacks|Extensions|RemoteHosts|RemoteForwards|RemoteConnectionStatuses|BackgroundRuntimes|InboxSnapshot|AvailableSubagentTools|SubagentsForTab|SubagentProgressForTab|ListTask|ListTasks|ListHistory|ListDir|ListRemoteDir|ListWorkspaces|ListProject|ListTrashed|ListSessions|MCPServers|MCPMarketplace|TodoSnapshot|WorkspaceChanges|WorkspaceGitHistory|HistorySlice|Checkpoint|MemoryRevisions|ProviderPresets|FetchAllProviderModels|ScanSSHConfig|ScanPromptHistory|ExtensionCatalog|ExtensionStatus|HeartbeatListTasks)$') {
    $ret = 'list'
  } elseif ($m -match '^(Get|Meta|Balance|Capabilities|Capability|Status|Catalog|Snapshot|Summary|Info|State|Goal|Mode|Plan|Usage|Context|Shell|Diagnostic|Version|Zoom|SlashArgs|Hooks|ExternalOpeners|WorkspaceConflict|WorkspaceRevision|WorkspaceGitCommitDetail|CheckpointSummary|Recovery|SessionCatalog|TaskCatalog|HistoryCatalog|BlankProject|CurrentTask|ProviderModels|PreviewSession|PreviewWorkspace|DeliveryWorktree|IsolatedWorktree|GetRecoveryLineage|GetUserConfigPath|GetTopicSummary|GetSessionCatalogStatus|GetTaskCatalogStatus|BotSettings|BotRuntimeStatus|BotConnectionDiagnostic|ToolResult|ToolApprovalMode|InboxHasItems|RemoteLastWorkspace|RemoteServerStatus|RemoteServerLogs|ShellForTerminal|ThemePackForTab)$') {
    $ret = 'map'
  } elseif ($m -match '^(Is|Has|Needs|Available|Enabled|Running|Supported)$') {
    $ret = 'bool'
  } elseif ($m -match '^(Current|Pick|Choose|Resolve|Read|ConnectKey|SaveClipboard|SavePasted|AttachmentDataURL|ExportThemePack|BrowserOpenURL|GetUserConfigPath|ShellForTerminal|ZoomFactor)$') {
    $ret = 'str'
  }

  switch ($ret) {
    'list' { [void]$lines.Add("func (a *App) $m($paramStr) []any { return []any{} }") }
    'map'  { [void]$lines.Add("func (a *App) $m($paramStr) map[string]any { return nil }") }
    'bool' { [void]$lines.Add("func (a *App) $m($paramStr) bool { return false }") }
    'str'  { [void]$lines.Add('func (a *App) ' + $m + '(' + $paramStr + ') string { return "" }') }
    default { [void]$lines.Add("func (a *App) $m($paramStr) error { return nil }") }
  }
}

$lines | Out-File -FilePath $OutFile -Encoding UTF8
Write-Host "已生成 $OutFile"
Write-Host "共 $($lines.Count) 行"
