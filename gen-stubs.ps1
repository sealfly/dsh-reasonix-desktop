# 生成缺失的 Go stub 方法
$new = "C:\Users\chenz\Desktop\DSH-deskop\reasonix-desktop\desktop\frontend\src"
$preloadFile = "C:\Users\chenz\Desktop\dsh-reasonix-desktop\src\preload.js"
$outFile = "C:\Users\chenz\Desktop\dsh-reasonix-wails\app_stubs2.go"

# 前端调用的方法
$frontendMethods = Get-ChildItem $new -Recurse -Include *.ts,*.tsx |
  Where-Object { $_.FullName -notmatch '__tests__|bridge.ts' } |
  Select-String -Pattern 'app\.[A-Z][A-Za-z0-9]+\(' -AllMatches |
  ForEach-Object { $_.Matches } |
  ForEach-Object { $_.Value -replace 'app\.','' -replace '\(','' } |
  Sort-Object -Unique

# Go 已实现
$goMethods = Select-String -Path "C:\Users\chenz\Desktop\dsh-reasonix-wails\*.go" -Pattern 'func \(a \*App\) ([A-Z][A-Za-z0-9]+)\(' |
  ForEach-Object { $_.Matches[0].Groups[1].Value } | Sort-Object -Unique

# preload 方法定义（名 -> 参数串）
$preloadDef = @{}
Get-Content "$env:TEMP\preload-methods.txt" | ForEach-Object {
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

$lines | Out-File -FilePath $outFile -Encoding UTF8
Write-Host "已生成 $outFile"
Write-Host "共 $($lines.Count) 行"
