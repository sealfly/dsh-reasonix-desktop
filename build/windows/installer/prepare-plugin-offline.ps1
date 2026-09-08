# prepare-plugin-offline.ps1 - Prepare offline cordis plugin bundles for installers.
#
# Runs on a NETWORKED build machine. Installs the "default-attached" plugin set
# (memory plugins + dsh-agent-teams) into a self-contained offline tree that
# installers embed, so end users without network still get them.
#
# Output (default): build\windows\installer\plugins-offline\
#   plugins-offline\node_modules\          - flat npm tree of the plugin set + deps
#   plugins-offline\manifest.json          - { name, spec, installedVersion, defaultEnabled }
#
# The Go side (app_plugin_seed.go) reads manifest.json, merges the bundles into
# the DSH web profile (package.json dsh.profile.bundles + dependencies) and
# copies node_modules entries into ~/.dsh/profiles/web/node_modules.
#
# Usage: .\prepare-plugin-offline.ps1 [-Registry https://registry.npmjs.org]
#        -SkipInstall: reuse an existing plugins-offline dir (build loop)

param(
  [string]$Registry = "https://registry.npmjs.org",
  [switch]$SkipInstall
)

$ErrorActionPreference = "Stop"
# Output lands next to this script: build\windows\installer\plugins-offline\
$outDir = Join-Path $PSScriptRoot "plugins-offline"
if ($SkipInstall -and (Test-Path (Join-Path $outDir "manifest.json"))) {
  Write-Host "plugins-offline already present; skipping install (-SkipInstall)"
  $sz = (Get-ChildItem $outDir -Recurse -File | Measure-Object Length -Sum).Sum
  Write-Host "size: $([math]::Round($sz/1MB,1)) MB"
  exit 0
}

# Default-attached plugin set (name, npm spec pinned, defaultEnabled)
#   memory plugins: installed but disabled by default (save tokens; one-click in 设置-记忆)
#   dsh-agent-teams: enabled (pure capability, no token side effect)
$plugins = @(
  @{ name = "@openviking/dsh-memory-plugin";         spec = "@openviking/dsh-memory-plugin@0.3.0";         defaultEnabled = $false },
  @{ name = "@vectorize-io/hindsight-coding-agents"; spec = "@vectorize-io/hindsight-coding-agents@0.4.3"; defaultEnabled = $false },
  @{ name = "@memtensor/memos-local-plugin";         spec = "@memtensor/memos-local-plugin@2.0.18";       defaultEnabled = $false },
  @{ name = "@nanmicoder/dsh-agent-teams";           spec = "@nanmicoder/dsh-agent-teams@0.1.15";          defaultEnabled = $true  }
)

# locate node/npm
$nodeCandidates = @("C:\Users\chenz\nodejs\node-v24.19.0-win-x64\node.exe", "$env:ProgramFiles\nodejs\node.exe", "$env:APPDATA\npm\node.exe")
$node = $nodeCandidates | Where-Object { Test-Path $_ } | Select-Object -First 1
if (-not $node) { Write-Error "node.exe not found"; exit 1 }
$npm = Join-Path (Split-Path $node) "npm.cmd"

$stage = Join-Path $env:TEMP "plugins-offline-stage"
if (Test-Path $stage) { Remove-Item $stage -Recurse -Force }
New-Item -ItemType Directory -Force -Path $stage | Out-Null

Write-Host "== Install plugin set (networked, one-by-one) =="
Push-Location $stage
try {
  & $npm init -y | Out-Null
  foreach ($p in $plugins) {
    Write-Host "npm install $($p.spec) --registry $Registry"
    # --legacy-peer-deps: the plugin set's peer chains conflict on @deepseek-ai/dsh-agent
    # (host DSH profile already provides the whole @deepseek-ai tree, so peers are skipped)
    # npm writes deprecation warnings to stderr; under $ErrorActionPreference=Stop they
    # surface as NativeCommandError and abort the script. Relax it around the npm call.
    $savedEAP = $ErrorActionPreference
    $ErrorActionPreference = "Continue"
    $out = & $npm install $p.spec --registry $Registry --no-audit --no-fund --legacy-peer-deps 2>&1 | Out-String
    $npmExit = $LASTEXITCODE
    $ErrorActionPreference = $savedEAP
    if ($npmExit -ne 0) {
      Write-Host $out
      Write-Error "npm install failed for $($p.spec) (exit $LASTEXITCODE)"
      exit 1
    }
    if (-not (Test-Path (Join-Path $stage "node_modules\$($p.name.Replace('/','\'))"))) {
      Write-Host $out
      Write-Error "npm install reported success but $($p.name) missing from tree"
      exit 1
    }
    Write-Host "  ok: $($p.name)"
  }
} finally { Pop-Location }

Write-Host "== Trim windows-only runtime (host runs on Windows node) =="
# 1) onnxruntime-web: browser/webgpu backend only - transformers.node.cjs (memos entry) does
#    NOT require it (verified: externals are onnxruntime-node / onnxruntime-common / sharp).
$webDir = Join-Path $stage "node_modules\onnxruntime-web"
if (Test-Path $webDir) { Remove-Item $webDir -Recurse -Force; Write-Host "  removed onnxruntime-web (browser backend)" }
# 2) onnxruntime-node carries darwin/linux binaries inside bin/napi-v6 - keep win32 only.
$v6 = Join-Path $stage "node_modules\onnxruntime-node\bin\napi-v6"
foreach ($plat in @("darwin", "linux")) {
  $pd = Join-Path $v6 $plat
  if (Test-Path $pd) { Remove-Item $pd -Recurse -Force; Write-Host "  removed onnxruntime-node $plat" }
}

Write-Host "== Assemble offline tree =="
if (Test-Path $outDir) { Remove-Item $outDir -Recurse -Force }
New-Item -ItemType Directory -Force -Path $outDir | Out-Null
$srcNm = Join-Path $stage "node_modules"
$dstNm = Join-Path $outDir "node_modules"
New-Item -ItemType Directory -Force -Path $dstNm | Out-Null

# Copy every installed package (plugins + their transitive deps). npm flat tree:
# just copy the whole node_modules minus metadata noise.
$copied = 0
Get-ChildItem $srcNm -Force -ErrorAction SilentlyContinue | ForEach-Object {
  $src = $_.FullName
  if (-not (Test-Path $src)) { Write-Host "  skip vanished: $($_.Name)"; return }
  Copy-Item $src (Join-Path $dstNm $_.Name) -Recurse -Force
  $copied++
}
Write-Host "copied $copied top-level entries"

# Manifest with real installed versions
$rows = @()
foreach ($p in $plugins) {
  $pj = Join-Path $dstNm ($p.name.Replace("/", "\") + "\package.json")
  $ver = ""
  if (Test-Path $pj) { $j = Get-Content $pj -Raw | ConvertFrom-Json; $ver = $j.version }
  $rows += @{
    name            = $p.name
    spec            = $p.spec
    installedVersion = $ver
    defaultEnabled  = $p.defaultEnabled
    bundle          = $p.name
  }
}
$manifest = @{
  updated = (Get-Date).ToString("yyyy-MM-dd")
  source  = "npm:$Registry"
  plugins = $rows
}
$manifest | ConvertTo-Json -Depth 6 | Set-Content (Join-Path $outDir "manifest.json") -Encoding UTF8

$sz = (Get-ChildItem $outDir -Recurse -File | Measure-Object Length -Sum).Sum
Write-Host "== plugins-offline ready: $outDir =="
Write-Host "size: $([math]::Round($sz/1MB,1)) MB, files: $((Get-ChildItem $outDir -Recurse -File).Count)"
Write-Host "manifest:"
Get-Content (Join-Path $outDir "manifest.json") -Raw
Write-Host "Done"
