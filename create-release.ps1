$gh = "C:\Users\ROG Zephyrus G16\Desktop\DSH\dsh-reasonix\.tools\bin\gh.exe"
$r = "C:\Users\ROG Zephyrus G16\Desktop\DSH\dsh-reasonix\release"
Set-Location "C:\Users\ROG Zephyrus G16\Desktop\DSH\dsh-reasonix"

$notes = @"
Reasonix 前端 + DeepSeek Harness 后端桥接桌面端。

三个安装包：
- DSH-Reasonix Setup 0.1.0.exe — NSIS 标准安装向导（可选目录、建快捷方式）
- DSH-Reasonix-0.1.0-win.zip — 便携版（解压即用）
- DSH-Reasonix-Setup.exe — 自解压单文件（双击即用，适合发微信）

前置：需已安装 Node.js（应用会自动 npx 拉起 DeepSeek Harness）。
"@

& $gh release create v0.1.0 --title "DSH-Reasonix v0.1.0" --notes $notes 2>&1 | Out-String
