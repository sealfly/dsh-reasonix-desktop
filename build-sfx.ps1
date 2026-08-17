# 构建自解压单文件包（解压后直接运行主程序）
# 用法：先运行 npm run dist 生成 release\win-unpacked，再运行本脚本
$7z = "C:\Program Files\7-Zip\7z.exe"
$sfx = "C:\Program Files\7-Zip\7z.sfx"
if (-not (Test-Path $7z)) { $7z = (Get-Command 7z -ErrorAction SilentlyContinue).Source }
if (-not (Test-Path $sfx) -and $7z) { $sfx = Join-Path (Split-Path $7z) "7z.sfx" }
$base = Join-Path $PSScriptRoot "release"
$unpacked = Join-Path $base "win-unpacked"
$work = Join-Path $base "sfx-build"
$out = Join-Path $base "DSH-Reasonix-Setup.exe"
# 实际打包出的主程序名（package.json productName 决定）
$mainExe = Get-ChildItem $unpacked -Filter "*.exe" -ErrorAction SilentlyContinue | Where-Object { $_.Name -notmatch 'uninstall' } | Select-Object -First 1 -ExpandProperty Name
if (-not $mainExe) { Write-Output "未找到 win-unpacked 主程序，请先 npm run dist"; exit 1 }

New-Item -ItemType Directory -Force -Path $work | Out-Null

# 1) 压缩 win-unpacked 内容
& $7z a -t7z (Join-Path $work "app.7z") (Join-Path $unpacked "*") -mx=5 -mmt=on | Out-Null

# 2) 自解压配置：解压后运行主程序
$config = @"
;!@Install@!UTF-8!
RunProgram="$mainExe"
GUIMode="2"
;!@InstallEnd@!
"@
Set-Content -Path (Join-Path $work "config.txt") -Value $config -Encoding UTF8

# 3) 拼接 sfx + config + 7z
$cmd = 'copy /b "' + $sfx + '" + "' + (Join-Path $work 'config.txt') + '" + "' + (Join-Path $work 'app.7z') + '" "' + $out + '"'
cmd /c $cmd | Out-Null

if (Test-Path $out) {
  Write-Output ("SFX 产物: {0:N1} MB -> {1}" -f ((Get-Item $out).Length/1MB), $out)
} else {
  Write-Output "SFX 生成失败"
}
