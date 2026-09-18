# version-archaeology.ps1 — 逐版本对照官方前端的两个功能点，用于回答"哪个版本改的"。
#   Q1: 「标准-交付」按钮（quality floor / 交付底线）在哪个版本消失
#   Q2: Composer 模型切换栏在哪个版本变成"带分类筛选的小型搜索栏"
$ErrorActionPreference = "Stop"
$b = "$env:USERPROFILE\Desktop\dsh-upstream-v1388"

function Resolve-Src([string]$root) {
  $inner = Get-ChildItem $root -Directory -ErrorAction SilentlyContinue | Select-Object -First 1
  if (-not $inner) { return $null }
  $p = Join-Path $inner.FullName "desktop\frontend\src"
  if (Test-Path $p) { return $p }
  return $null
}

$versions = [ordered]@{}
$v1314 = Resolve-Src "$b\src1314"; if ($v1314) { $versions["1.31.4"] = $v1314 }
foreach ($v in @("1.32.0", "1.33.0", "1.34.0", "1.35.0", "1.36.0", "1.37.0", "1.38.0", "1.38.1", "1.38.2", "1.38.3", "1.38.4", "1.38.5", "1.38.6", "1.38.7")) {
  $r = Resolve-Src "$b\probe\src$v"
  if ($r) { $versions[$v] = $r }
}
$v1388 = Resolve-Src "$b\src"; if ($v1388) { $versions["1.38.8"] = $v1388 }

Write-Host ("{0,-8} {1,-6} {2,-9} {3,-9} {4,-6} {5,-9} {6,-9} {7}" -f "版本", "切换栏", "行数", "activeFilter", "favorites", "qualityFloor键", "Composer引用", "src/qualityFloor 命中数")
Write-Host ("-" * 96)

foreach ($kv in $versions.GetEnumerator()) {
  $name = $kv.Key
  $src = $kv.Value
  $sw = Join-Path $src "components\ModelSwitcher.tsx"
  $swExists = Test-Path $sw
  $swLines = if ($swExists) { (Get-Content $sw).Count } else { 0 }
  $swText = if ($swExists) { Get-Content $sw -Raw } else { "" }
  $hasFilter = $swText -match 'activeFilter'
  $hasFav = $swText -match 'favorite'

  $zhFiles = Get-ChildItem $src -Recurse -Include zh.ts -ErrorAction SilentlyContinue
  $zhText = ($zhFiles | ForEach-Object { Get-Content $_.FullName -Raw }) -join "`n"
  $qfKey = $zhText -match '"composer\.qualityFloor"'
  $qfStd = $zhText -match '"composer\.qualityFloorStandard"'

  $composer = Join-Path $src "components\Composer.tsx"
  $composerHits = if (Test-Path $composer) { (Select-String -Path $composer -Pattern 'qualityFloor' -AllMatches | Measure-Object).Count } else { 0 }
  $anyQf = (Get-ChildItem $src -Recurse -Include *.ts,*.tsx -ErrorAction SilentlyContinue | Select-String -Pattern 'qualityFloor' -List | Measure-Object).Count

  Write-Host ("{0,-8} {1,-6} {2,-9} {3,-9} {4,-6} {5,-9} {6,-9} {7}" -f `
      $name, $(if ($swExists) { "有" } else { "无" }), $swLines, `
      $(if ($hasFilter) { "★有" } else { "无" }), $(if ($hasFav) { "★有" } else { "无" }), `
      $(if ($qfKey) { "有(std=$qfStd)" } else { "★无" }), $composerHits, $anyQf)
}
