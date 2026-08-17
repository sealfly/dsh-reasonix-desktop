# 创建 GitHub Release v0.1.0
# 用法：先 npm run dist，再运行本脚本（已登录 gh）
$gh = (Get-Command gh -ErrorAction SilentlyContinue).Source
if (-not $gh) { Write-Error "未找到 gh CLI，请先安装并登录: gh auth login"; exit 1 }
Set-Location $PSScriptRoot

$notes = @"
Reasonix 前端 + DeepSeek Harness 后端桥接桌面端。

三个安装包：
- dsh-(reasonix)UI-desktop Setup 0.1.0.exe — NSIS 标准安装向导（可选目录、建快捷方式）
- dsh-(reasonix)UI-desktop-0.1.0-win.zip — 便携版（解压即用）
- dsh-(reasonix)UI-desktop 0.1.0.exe — 单文件便携版

安装包内置 Node.js，无需预装；应用会自动拉起 DeepSeek Harness 后端。
"@

& $gh release create v0.1.0 --title "DSH-Reasonix v0.1.0" --notes $notes --repo sealfly/dsh-reasonix-desktop 2>&1 | Out-String
