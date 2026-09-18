# version-archaeology2.ps1 — 精确定位两个 UI 变化点
#   Q1: Composer 里「标准 | 交付」两个并排按钮 → 何时变成单个"交付"勾选项 / 何时彻底移除
#   Q2: ModelSwitcher（对话框模型切换栏）何时变成"带分类筛选的小型搜索栏"
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
$v = Resolve-Src "$b\src1314"; if ($v) { $versions["1.31.4"] = $v }
foreach ($n in @("1.32.0", "1.34.0", "1.36.0", "1.37.0", "1.38.0", "1.38.1", "1.38.2", "1.38.3", "1.38.4", "1.38.5", "1.38.6", "1.38.7")) {
  $v = Resolve-Src "$b\probe\src$n"; if ($v) { $versions[$n] = $v }
}
$v = Resolve-Src "$b\src"; if ($v) { $versions["1.38.8"] = $v }

Write-Host ("{0,-8} {1,-26} {2,-12} {3,-10} {4,-10} {5,-8}" -f "版本", "Composer 验收控件形态", "标准键引用", "标准按钮", "交付按钮", "筛选chips")
Write-Host ("-" * 92)

foreach ($kv in $versions.GetEnumerator()) {
  $name = $kv.Key; $src = $kv.Value
  $composer = Join-Path $src "components\Composer.tsx"
  $text = if (Test-Path $composer) { Get-Content $composer -Raw } else { "" }

  $stdRefs = ([regex]::Matches($text, 'qualityFloorStandard')).Count
  $delRefs = ([regex]::Matches($text, 'qualityFloorDelivery')).Count
  $choiceRefs = ([regex]::Matches($text, 'chooseQualityFloor')).Count

  # 形态判定：两个并排按钮 vs 单个菜单勾选项 vs 无
  $form = "无"
  if ($stdRefs -gt 0 -and $delRefs -gt 0) { $form = "两个并排按钮(标准|交付)" }
  elseif ($delRefs -gt 0 -and $choiceRefs -gt 0) { $form = "单个勾选项(交付)" }
  elseif ($choiceRefs -gt 0) { $form = "有逻辑无标签?" }

  $sw = Join-Path $src "components\ModelSwitcher.tsx"
  $swText = if (Test-Path $sw) { Get-Content $sw -Raw } else { "" }
  $chips = ([regex]::Matches($swText, 'activeFilter')).Count
  $fav = ([regex]::Matches($swText, 'favorite', 'IgnoreCase')).Count

  Write-Host ("{0,-8} {1,-26} {2,-12} {3,-10} {4,-10} {5,-8}" -f `
      $name, $form, $stdRefs, $stdRefs, $delRefs, $(if ($chips -gt 0) { "★有($chips)" } else { "无" }))
}
