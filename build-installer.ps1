# build-installer.ps1 - Build NSIS installer with DSH backend integration.
# Two flavors:
#   default      -> classic installer (no bundled DSH; DSH component installs online via npm)
#   -Bundle      -> LAZY installer (embeds DSH runtime + node.exe; offline one-click DSH)
# Standard pipeline so EVERY installer carries the "DSH 后端" component option:
#   wails build -nsis  -> main exe + wails_tools.nsh + initial installer
#   go-winres patch    -> embed version resources / icon / manifest into main exe
#   sign main exe
#   [-Bundle] prepare-dsh-runtime.ps1 -> build\windows\installer\dsh-runtime\
#   makensis (custom project.nsi, -DBUNDLE_DSH when lazy) -> repackage with the patched exe
#   sign installer
#   copy to Desktop as DSH-ReasonixUI-安装包.exe / DSH-ReasonixUI-懒人包.exe
# Usage: .\build-installer.ps1 [-WailsBin <path>] [-CertThumbprint <thumb>] [-GoProxy <proxy>] [-SkipSign] [-Bundle]

param(
  [string]$WailsBin = "",
  [string]$CertThumbprint = "96A3EB4C926AFEAAA71A72119C3D34B5C7465335",
  [string]$GoProxy = "https://goproxy.cn,direct",
  [switch]$SkipSign,
  [switch]$CopyDesktop = $true,
  [switch]$Bundle
)

$ErrorActionPreference = "Stop"
$root = $PSScriptRoot
if (-not $WailsBin) { $WailsBin = Join-Path $env:USERPROFILE "go\bin\wails.exe" }
if (-not (Test-Path $WailsBin)) { Write-Error "wails not found: $WailsBin"; exit 1 }

# Locate NSIS (makensis) - required
$makensis = "makensis"
if (-not (Get-Command makensis -ErrorAction SilentlyContinue)) {
  foreach ($cand in @("C:\Program Files (x86)\NSIS\makensis.exe", "C:\Program Files\NSIS\makensis.exe")) {
    if (Test-Path $cand) { $makensis = $cand; break }
  }
}
if (-not (Get-Command $makensis -ErrorAction SilentlyContinue) -and -not (Test-Path $makensis)) {
  Write-Error "makensis not found (install NSIS: winget install NSIS.NSIS)"; exit 1
}

# Ensure go on PATH
if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
  foreach ($cand in @((Join-Path $env:USERPROFILE "go\bin\go.exe"), "C:\Go\bin\go.exe")) {
    if (Test-Path $cand) { $env:Path = "$(Split-Path $cand);$env:Path"; break }
  }
}
$env:GOPROXY = $GoProxy

$exe = Join-Path $root "build\bin\DSH-ReasonixUI.exe"
$installerOut = Join-Path $root "build\bin\dsh-reasonix-wails-amd64-installer.exe"
$installerDir = Join-Path $root "build\windows\installer"
$winresTool = Join-Path $root "tools\go-winres.exe"
$winresJson = Join-Path $root "build\windows\winres.json"
if ($Bundle) {
  $installerOut = Join-Path $root "build\bin\dsh-reasonix-wails-amd64-installer-lazy.exe"
}

# 1) wails build -nsis: main exe + wails_tools.nsh + initial installer
Write-Host "== Build (wails -nsis) =="
Push-Location $root
try {
  & $WailsBin build -tags native_webview2loader -nsis
  if ($LASTEXITCODE -ne 0) { throw "wails build -nsis failed (exit $LASTEXITCODE)" }
} finally { Pop-Location }
if (-not (Test-Path $exe)) { throw "build output missing: $exe" }
Write-Host "Built: $exe"

# 2) go-winres patch: version resources + icon + manifest (before signing)
Write-Host "== Resources =="
if (-not (Test-Path $winresTool)) { throw "go-winres missing: $winresTool" }
if (-not (Test-Path $winresJson)) { throw "winres.json missing: $winresJson" }
& $winresTool patch --in $winresJson --delete --no-backup --authenticode remove $exe
if ($LASTEXITCODE -ne 0) { throw "go-winres patch failed (exit $LASTEXITCODE)" }
Write-Host "Resources embedded: $exe"

# 3) Sign main exe (warn only if cert missing)
Write-Host "== Sign exe =="
$cert = $null
if (-not $SkipSign) {
  $cert = Get-ChildItem Cert:\CurrentUser\My | Where-Object { $_.Thumbprint -eq $CertThumbprint } | Select-Object -First 1
}
if ($cert) {
  Set-AuthenticodeSignature -FilePath $exe -Certificate $cert -HashAlgorithm SHA256 | Out-Null
  Write-Host "Signed ($($cert.Thumbprint))"
} else {
  Write-Warning "Cert not found; skipped signing main exe."
}

# 3.5) Lazy flavor: prepare offline DSH runtime (node.exe + dsh tree) BEFORE makensis
if ($Bundle) {
  $runtimeDir = Join-Path $installerDir "dsh-runtime"
  $runtimeOk = (Test-Path (Join-Path $runtimeDir "node.exe")) -and
               (Test-Path (Join-Path $runtimeDir "dsh\node_modules\@deepseek-ai\dsh\lib\bin.js"))
  if ($runtimeOk) {
    Write-Host "== Bundled runtime already present (skip prepare): $runtimeDir =="
    Write-Host "   To rebuild it: remove dsh-runtime\ then rerun -Bundle"
  } else {
    Write-Host "== Prepare bundled DSH runtime (lazy installer) =="
    $prepare = Join-Path $installerDir "prepare-dsh-runtime.ps1"
    if (-not (Test-Path $prepare)) { throw "prepare-dsh-runtime.ps1 missing: $prepare" }
    & $prepare
    if ($LASTEXITCODE -ne 0) { throw "prepare-dsh-runtime failed (exit $LASTEXITCODE)" }
  }
  if (-not (Test-Path (Join-Path $runtimeDir "node.exe"))) { throw "runtime node.exe missing: $runtimeDir" }
  if (-not (Test-Path (Join-Path $runtimeDir "dsh\node_modules\@deepseek-ai\dsh\lib\bin.js"))) { throw "runtime dsh missing: $runtimeDir" }
  Write-Host "Runtime ready: $runtimeDir"
}

# 4) makensis repackage with the patched exe (custom project.nsi carries DSH component)
Write-Host "== NSIS (custom project.nsi with DSH option) =="
Push-Location $installerDir
try {
  if ($Bundle) {
    & $makensis "-DARG_WAILS_AMD64_BINARY=$exe" "-DBUNDLE_DSH" "project.nsi"
  } else {
    & $makensis "-DARG_WAILS_AMD64_BINARY=$exe" "project.nsi"
  }
  if ($LASTEXITCODE -ne 0) { throw "makensis failed (exit $LASTEXITCODE)" }
} finally { Pop-Location }
if (-not (Test-Path $installerOut)) { throw "installer missing: $installerOut" }
Write-Host "Installer: $installerOut ($([math]::Round((Get-Item $installerOut).Length/1MB,1)) MB)"

# 5) Sign installer
Write-Host "== Sign installer =="
if ($cert) {
  Set-AuthenticodeSignature -FilePath $installerOut -Certificate $cert -HashAlgorithm SHA256 | Out-Null
  Write-Host "Installer signed"
} else {
  Write-Warning "Installer not signed (no cert)."
}

# 6) Copy to Desktop
if ($CopyDesktop) {
  $desktop = Join-Path $env:USERPROFILE "Desktop"
  $name = if ($Bundle) { "DSH-ReasonixUI-懒人包.exe" } else { "DSH-ReasonixUI-安装包.exe" }
  $dst = Join-Path $desktop $name
  Copy-Item $installerOut $dst -Force
  Write-Host "Desktop copy: $dst"
}
Write-Host "Done"
