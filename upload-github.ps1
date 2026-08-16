# GitHub API 上传脚本（用于 github.com git push 被墙时的替代方案）
# 用法: .\upload-github.ps1 -Token "ghp_xxx" -Repo "dsh-reasonix" -User "你的用户名"
param(
  [Parameter(Mandatory=$true)][string]$Token,
  [string]$Repo = "dsh-reasonix",
  [string]$User = ""
)

$ErrorActionPreference = 'Stop'
$headers = @{ Authorization = "Bearer $Token"; "User-Agent" = "dsh-reasonix-upload" }

# 1) 确认用户名
if (-not $User) {
  $me = Invoke-RestMethod -Uri "https://api.github.com/user" -Headers $headers -Method Get
  $User = $me.login
  Write-Host "登录用户: $User" -ForegroundColor Green
}

# 2) 建仓库（已存在则忽略）
$body = @{ name = $Repo; description = "DSH-Reasonix 桌面端 - Reasonix 前端 + DeepSeek Harness 后端"; public = $false } | ConvertTo-Json
try {
  $repo = Invoke-RestMethod -Uri "https://api.github.com/user/repos" -Headers $headers -Method Post -Body $body
  Write-Host "仓库已创建: $($repo.html_url)" -ForegroundColor Green
} catch {
  if ($_.Exception.Response.StatusCode.value__ -eq 422) { Write-Host "仓库已存在" -ForegroundColor Yellow }
  else { throw }
}

# 3) 创建 release 并上传三个安装包
$releaseBody = @{ tag_name = "v0.1.0"; name = "v0.1.0"; body = "DSH-Reasonix 首个发布版本" } | ConvertTo-Json
try {
  $release = Invoke-RestMethod -Uri "https://api.github.com/repos/$User/$Repo/releases" -Headers $headers -Method Post -Body $releaseBody
} catch {
  if ($_.Exception.Response.StatusCode.value__ -eq 422) {
    $release = Invoke-RestMethod -Uri "https://api.github.com/repos/$User/$Repo/releases/tags/v0.1.0" -Headers $headers -Method Get
  } else { throw }
}

$releaseDir = "C:\Users\ROG Zephyrus G16\Desktop\DSH\dsh-reasonix\release"
$assets = @(
  "DSH-Reasonix Setup 0.1.0.exe",
  "DSH-Reasonix-0.1.0-win.zip",
  "DSH-Reasonix-Setup.exe"
)
foreach ($a in $assets) {
  $p = Join-Path $releaseDir $a
  if (Test-Path $p) {
    Write-Host "上传 $a ({0:N1} MB)..." -f ((Get-Item $p).Length/1MB)
    $upHeaders = @{ Authorization = "Bearer $Token"; "User-Agent" = "dsh-reasonix-upload"; "Content-Type" = "application/octet-stream" }
    $url = "https://uploads.github.com/repos/$User/$Repo/releases/$($release.id)/assets?name=$([uri]::EscapeDataString($a))"
    Invoke-RestMethod -Uri $url -Headers $upHeaders -Method Post -InFile $p | Out-Null
    Write-Host "  完成" -ForegroundColor Green
  }
}
Write-Host "全部上传完成: https://github.com/$User/$Repo/releases" -ForegroundColor Cyan
