# build-deploy.ps1 - Standard build + sign + deploy pipeline.
# Portable: all paths derive from $PSScriptRoot / %USERPROFILE%; missing cert warns only.
# Usage: .\build-deploy.ps1 [-WailsBin <path>] [-CertThumbprint <thumb>] [-DeployName <name>] [-GoProxy <proxy>] [-Launch]

param(
  # wails executable (default %USERPROFILE%\go\bin\wails.exe)
  [string]$WailsBin = "",
  # Code-signing cert thumbprint (CurrentUser\My); warns if missing (re-import cert on new machine)
  [string]$CertThumbprint = "96A3EB4C926AFEAAA71A72119C3D34B5C7465335",
  # Deploy copy filename (repo root)
  [string]$DeployName = "DSH-ReasonixUI-new.exe",
  # Go module proxy (adjust for your network; goproxy.cn works behind GFW)
  [string]$GoProxy = "https://goproxy.cn,direct",
  # Launch the deployed copy after build
  [switch]$Launch
)

$ErrorActionPreference = "Stop"
$root = $PSScriptRoot
if (-not $WailsBin) { $WailsBin = Join-Path $env:USERPROFILE "go\bin\wails.exe" }
if (-not (Test-Path $WailsBin)) {
  Write-Error "wails not found: $WailsBin (pass -WailsBin to specify)"
  exit 1
}

# 1) Build
Write-Host "== Build =="
# Ensure go is on PATH (wails build needs it); try common install locations
if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
  foreach ($cand in @((Join-Path $env:USERPROFILE "go\bin\go.exe"), "C:\Go\bin\go.exe")) {
    if (Test-Path $cand) { $env:Path = "$(Split-Path $cand);$env:Path"; break }
  }
}
if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
  Write-Error "go not found (install Go and add to PATH, or confirm a common install location)"
  exit 1
}
$env:GOPROXY = $GoProxy
Push-Location $root
try {
  & $WailsBin build -tags native_webview2loader
  if ($LASTEXITCODE -ne 0) { throw "wails build failed (exit $LASTEXITCODE)" }
} finally { Pop-Location }
$exe = Join-Path $root "build\bin\DSH-ReasonixUI.exe"
if (-not (Test-Path $exe)) { throw "build output missing: $exe" }
Write-Host "Built: $exe"

# 1.5) Embed version resources + icon + manifest (wails v2.13 does not do this itself)
# go-winres patch replaces resources; must run BEFORE signing (it removes the signature).
Write-Host "== Resources =="
$winresTool = Join-Path $root "tools\go-winres.exe"
$winresJson = Join-Path $root "build\windows\winres.json"
if (-not (Test-Path $winresTool)) { throw "go-winres missing: $winresTool (run: go install github.com/tc-hib/go-winres@v0.3.1)" }
if (-not (Test-Path $winresJson)) { throw "winres.json missing: $winresJson" }
& $winresTool patch --in $winresJson --delete --no-backup --authenticode remove $exe
if ($LASTEXITCODE -ne 0) { throw "go-winres patch failed (exit $LASTEXITCODE)" }
Write-Host "Resources embedded: $exe"

# 2) Sign (warn only if cert missing)
Write-Host "== Sign =="
$cert = Get-ChildItem Cert:\CurrentUser\My | Where-Object { $_.Thumbprint -eq $CertThumbprint } | Select-Object -First 1
if ($cert) {
  Set-AuthenticodeSignature -FilePath $exe -Certificate $cert -HashAlgorithm SHA256 | Out-Null
  Write-Host "Signed ($($cert.Thumbprint))"
} else {
  Write-Warning "Cert $CertThumbprint not found in CurrentUser store; skipped signing. (Import the cert on a new machine, or pass -CertThumbprint)"
}

# 3) Deploy copy
Write-Host "== Deploy =="
$deploy = Join-Path $root $DeployName
Copy-Item $exe $deploy -Force
Write-Host "Deployed: $deploy"

# 4) Optional launch
if ($Launch) {
  Write-Host "== Launch =="
  $p = Start-Process -FilePath $deploy -PassThru
  Write-Host "PID: $($p.Id)"
}
Write-Host "Done"
