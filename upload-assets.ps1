# 上传 release 资产到 GitHub Release（v0.1.0）
# 用法：先 npm run dist，再运行本脚本（已登录 gh）。
# 目标仓库自动从 git remote 读取——clone 后推到自己的 GitHub 仓库即可用，无需改脚本。
$gh = (Get-Command gh -ErrorAction SilentlyContinue).Source
if (-not $gh) { Write-Error "未找到 gh CLI，请先安装并登录: gh auth login"; exit 1 }
$remote = (git remote get-url origin 2>$null)
$repo = ($remote -replace '^https?://[^/]+/','' -replace '^git@[^:]+:','' -replace '\.git$','').Trim()
if (-not $repo) { Write-Error "无法从 git remote 获取仓库名（origin），请确认仓库已设置 remote"; exit 1 }
Write-Host "目标仓库: $repo" -ForegroundColor Cyan
$r = Join-Path $PSScriptRoot "release"
Set-Location $PSScriptRoot

$assets = @(
  "dsh-(reasonix)UI-desktop Setup 0.1.0.exe",
  "dsh-(reasonix)UI-desktop-0.1.0-win.zip",
  "dsh-(reasonix)UI-desktop 0.1.0.exe"
)
foreach ($a in $assets) {
  $p = Join-Path $r $a
  if (Test-Path $p) {
    Write-Host ("上传 {0} ({1:N1} MB)..." -f $a, ((Get-Item $p).Length/1MB))
    & $gh release upload v0.1.0 $p --repo $repo --clobber 2>&1 | Out-String
  } else {
    Write-Host ("跳过（不存在）: {0}" -f $a) -ForegroundColor Yellow
  }
}
Write-Host "全部上传完成"
