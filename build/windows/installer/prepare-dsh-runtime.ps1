# prepare-dsh-runtime.ps1 - Prepare offline DSH runtime for the lazy installer.
# Produces (default): build\windows\installer\dsh-runtime\
#   dsh-runtime\node.exe                      - node single exe (runs dsh)
#   dsh-runtime\dsh\node_modules\             - @deepseek-ai/dsh full dependency tree
# Run on a NETWORKED build machine. The installer then embeds this directory
# (makensis -DBUNDLE_DSH) so end users without network/node get a working DSH.
# Usage: .\prepare-dsh-runtime.ps1 [-NodeVersion v24.19.0] [-DshVersion latest] [-SkipDownload]

param(
  [string]$NodeVersion = "v24.19.0",
  [string]$DshVersion = "latest",
  [switch]$SkipNodeDownload
)

$ErrorActionPreference = "Stop"
# Output lands next to this script: build\windows\installer\dsh-runtime\
# (makensis runs from this dir with File /r dsh-runtime)
$outDir = Join-Path $PSScriptRoot "dsh-runtime"
$stage = Join-Path $env:TEMP "dsh-runtime-stage"
if (Test-Path $outDir) { Remove-Item $outDir -Recurse -Force }
New-Item -ItemType Directory -Force -Path $outDir | Out-Null

# ---------- 1. node.exe (single file from official zip) ----------
$nodeExe = Join-Path $outDir "node.exe"
if ($SkipNodeDownload -and (Test-Path $nodeExe)) {
  Write-Host "Skip node download, reuse existing: $nodeExe"
} else {
  $nodeZip = Join-Path $stage "node.zip"
  $nodeUnzip = Join-Path $stage "node-unzip"
  if (Test-Path $stage) { Remove-Item $stage -Recurse -Force }
  New-Item -ItemType Directory -Force -Path $nodeUnzip | Out-Null
  $url = "https://nodejs.org/dist/$NodeVersion/node-$NodeVersion-win-x64.zip"
  Write-Host "== Download node $NodeVersion =="
  Write-Host "  $url"
  Invoke-WebRequest -Uri $url -OutFile $nodeZip
  Write-Host "  zip: $([math]::Round((Get-Item $nodeZip).Length/1MB,1)) MB"
  Write-Host "== Extract node.exe =="
  Expand-Archive -Path $nodeZip -DestinationPath $nodeUnzip -Force
  $found = Get-ChildItem $nodeUnzip -Recurse -Filter "node.exe" -File | Select-Object -First 1
  if (-not $found) { throw "node.exe not found in archive" }
  Copy-Item $found.FullName $nodeExe -Force
  Write-Host "  node.exe: $([math]::Round((Get-Item $nodeExe).Length/1MB,1)) MB"
  # sanity: version
  & $nodeExe --version
}

# ---------- 2. dsh full dependency tree ----------
$dshDir = Join-Path $outDir "dsh"
$dshModules = Join-Path $dshDir "node_modules"
if (Test-Path $dshDir) { Remove-Item $dshDir -Recurse -Force }
New-Item -ItemType Directory -Force -Path $dshDir | Out-Null
Write-Host "== npm install @deepseek-ai/dsh@$DshVersion (networked) =="
$npmStage = Join-Path $stage "npm-stage"
if (Test-Path $npmStage) { Remove-Item $npmStage -Recurse -Force }
New-Item -ItemType Directory -Force -Path $npmStage | Out-Null
Push-Location $npmStage
try {
  npm install "@deepseek-ai/dsh@$DshVersion" --no-audit --no-fund --loglevel=error
  if ($LASTEXITCODE -ne 0) { throw "npm install failed (exit $LASTEXITCODE)" }
} finally { Pop-Location }
# move node_modules into dsh-runtime\dsh\
$stagedModules = Join-Path $npmStage "node_modules"
if (-not (Test-Path (Join-Path $stagedModules "@deepseek-ai\dsh\lib\bin.js"))) {
  throw "dsh bin.js missing after install"
}
Move-Item $stagedModules $dshModules -Force

# ---------- 3. verify + report ----------
$bin = Join-Path $dshModules "@deepseek-ai\dsh\lib\bin.js"
if (-not (Test-Path $bin)) { throw "dsh bin.js missing: $bin" }
if (-not (Test-Path $nodeExe)) { throw "node.exe missing: $nodeExe" }
$total = (Get-ChildItem $outDir -Recurse -File | Measure-Object -Property Length -Sum).Sum
$files = (Get-ChildItem $outDir -Recurse -File).Count
$pkgJson = Join-Path $dshModules "@deepseek-ai\dsh\package.json"
$dshVer = (Get-Content $pkgJson -Raw | ConvertFrom-Json).version
$nodeVer = & $nodeExe --version 2>&1
Write-Host ""
Write-Host "== Runtime ready =="
Write-Host "  dir:   $outDir"
Write-Host "  size:  $([math]::Round($total/1MB,1)) MB ($files files)"
Write-Host "  node:  $nodeVer"
Write-Host "  dsh:   $dshVer"
Write-Host "Next: makensis -DBUNDLE_DSH project.nsi  (or .\build-installer.ps1 -Bundle)"
