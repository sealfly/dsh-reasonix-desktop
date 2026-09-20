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

# 0) Frontend brand + injections: frontend/dist is a build artifact (an upstream upgrade
#    overwrites it), so BOTH the DSH brand patch and every DSH UI adaptation that patches
#    index.html must be re-applied here, and they are idempotent (PRINCIPLES P6).
#    apply-all-injections.js is manifest-driven (scripts/injections.json) and gates each
#    source on a syntax check, so a broken payload fails the build instead of silently
#    never executing. Missing node is fatal on purpose: without these steps the injected
#    UI features and the DSH-Reasonix branding silently disappear from the build.
if (-not (Get-Command node -ErrorAction SilentlyContinue)) {
  foreach ($cand in @((Join-Path $env:LOCALAPPDATA "Programs\nodejs\node.exe"),
                      (Join-Path $env:ProgramFiles "nodejs\node.exe"),
                      (Join-Path $env:APPDATA "npm\node.exe"))) {
    if (Test-Path $cand) { $env:Path = "$(Split-Path $cand);$env:Path"; break }
  }
}
if (-not (Get-Command node -ErrorAction SilentlyContinue)) {
  Write-Error "node not found (frontend injections require node; install Node.js or add it to PATH)"
  exit 1
}
Write-Host "== Frontend brand + injections =="
foreach ($ij in @("apply-branding.js", "apply-monaco-vendor.js", "apply-all-injections.js")) {
  & node (Join-Path $root "scripts\$ij")
  if ($LASTEXITCODE -ne 0) { throw "frontend patch failed: $ij (exit $LASTEXITCODE)" }
}

# 0.5) dist asset integrity: everything index.html references — plus the chunk closure those
#      entries pull in — must exist on disk AND be tracked by git. 2026-09-18 incident: an
#      upstream frontend upgrade committed only index.html and left 291 assets (including the
#      main entry) out of git (frontend/dist is gitignored), so every clean checkout hung on
#      "loading" forever. This gate turns that failure into a build-time error with the exact
#      file list instead of a runtime white screen.
Write-Host "== Verify dist assets =="
& node (Join-Path $root "scripts\verify-dist-assets.js")
if ($LASTEXITCODE -ne 0) { throw "dist asset verification failed (missing or untracked assets; see output above)" }

# 0.6) Bridge arity: every method the real UI calls must accept exactly the argument count it
#      passes. Wails validates arity and rejects otherwise with
#      "error parsing arguments: received N arguments to method 'main.App.X', expected M" —
#      a one-line message in a corner of the UI, so these break silently.
#      2026-09-20: this gate's absence let the right-dock "remote" tab ship broken ("loading
#      failed" forever) and 56 more methods carried the same defect. Only REAL UI call sites
#      fail the build; the dev mock bridge and the reviewed-exception table do not.
Write-Host "== Verify bridge arity =="
& node (Join-Path $root "scripts\bridge-arity-check.js")
if ($LASTEXITCODE -ne 0) { throw "bridge arity verification failed (Go signatures do not match frontend call sites; see output above)" }

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
