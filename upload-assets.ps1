# 上传 release 资产到 GitHub Release（v0.1.0）
# 用法：先 npm run dist，再运行本脚本（已登录 gh）
$gh = (Get-Command gh -ErrorAction SilentlyContinue).Source
if (-not $gh) { Write-Error "未找到 gh CLI，请先安装并登录: gh auth login"; exit 1 }
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
    & $gh release upload v0.1.0 $p --repo sealfly/dsh-reasonix-desktop --clobber 2>&1 | Out-String
  } else {
    Write-Host ("跳过（不存在）: {0}" -f $a) -ForegroundColor Yellow
  }
}
Write-Host "全部上传完成"
