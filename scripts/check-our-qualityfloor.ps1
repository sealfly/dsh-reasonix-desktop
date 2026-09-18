# check-our-qualityfloor.ps1 — 确认我们当前 dist（1.38.2 基线）里的「验收」控件是哪一版形态
$ErrorActionPreference = "Stop"
$dist = Join-Path $PSScriptRoot "..\frontend\dist\assets"
$hitsStd = @()
$hitsDel = @()
Get-ChildItem $dist -Filter "*.js" | ForEach-Object {
  $t = Get-Content $_.FullName -Raw -Encoding UTF8
  if ($t -match 'qualityFloorStandard') { $hitsStd += $_.Name }
  if ($t -match 'qualityFloorDelivery') { $hitsDel += $_.Name }
}
Write-Host ("  含 qualityFloorStandard(标准档) 的 chunk: {0}" -f $(if ($hitsStd) { $hitsStd -join ", " } else { "（无 -> 单勾选版）" }))
Write-Host ("  含 qualityFloorDelivery(交付档) 的 chunk: {0}" -f $(if ($hitsDel) { $hitsDel -join ", " } else { "（无）" }))

$zh = Get-ChildItem $dist -Filter "zh-*.js" | Select-Object -First 1
$t = Get-Content $zh.FullName -Raw -Encoding UTF8
foreach ($k in @("qualityFloor", "qualityFloorStandard", "qualityFloorDelivery")) {
  $m = [regex]::Match($t, '"composer\.' + $k + '":"([^"]*)"')
  Write-Host ("    composer.{0} = {1}" -f $k, $(if ($m.Success) { $m.Groups[1].Value } else { "（无）" }))
}
$sw = Get-ChildItem $dist -Filter "*.js" | Where-Object { (Get-Content $_.FullName -Raw -Encoding UTF8) -match 'modelSwitcher\.favorites' }
Write-Host ("  模型切换栏分类键 modelSwitcher.favorites: {0}" -f $(if ($sw) { "存在（$($sw.Name -join ', ')）" } else { "不存在 -> 旧形态（仅搜索+分组）" }))
