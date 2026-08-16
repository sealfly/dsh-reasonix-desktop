$7z = "C:\Program Files\7-Zip\7z.exe"
$sfx = "C:\Program Files\7-Zip\7z.sfx"
$base = "C:\Users\ROG Zephyrus G16\Desktop\DSH\dsh-reasonix\release"
$unpacked = Join-Path $base "win-unpacked"
$work = Join-Path $base "sfx-build"
$out = Join-Path $base "DSH-Reasonix-Setup.exe"

New-Item -ItemType Directory -Force -Path $work | Out-Null

# 1) 压缩 win-unpacked 内容
& $7z a -t7z (Join-Path $work "app.7z") (Join-Path $unpacked "*") -mx=5 -mmt=on | Out-Null

# 2) 自解压配置：解压后运行主程序
$config = @"
;!@Install@!UTF-8!
RunProgram="DSH-Reasonix.exe"
GUIMode="2"
;!@InstallEnd@!
"@
Set-Content -Path (Join-Path $work "config.txt") -Value $config -Encoding UTF8

# 3) 拼接 sfx + config + 7z
$cmd = 'copy /b "' + $sfx + '" + "' + (Join-Path $work 'config.txt') + '" + "' + (Join-Path $work 'app.7z') + '" "' + $out + '"'
cmd /c $cmd | Out-Null

if (Test-Path $out) {
  Write-Output ("SFX 产物: {0:N1} MB" -f ((Get-Item $out).Length/1MB))
} else {
  Write-Output "SFX 生成失败"
}
