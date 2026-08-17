# 创建 GitHub Release v0.1.0
# 用法：先 npm run dist，再运行本脚本（已登录 gh）。
# 目标仓库自动从 git remote 读取——clone 后推到自己的 GitHub 仓库即可用，无需改脚本。
# 注意：Get-Command 在脚本文件内（后台任务上下文）可能返回空，先用常见安装路径探测。
$gh = $null
foreach ($c in @('C:\Program Files\GitHub CLI\gh.exe', "$env:LOCALAPPDATA\Programs\GitHub CLI\gh.exe", "$env:ProgramFiles\GitHub CLI\gh.exe")) {
  if (Test-Path $c) { $gh = $c; break }
}
if (-not $gh) { $gh = (Get-Command gh -ErrorAction SilentlyContinue).Source }
if (-not $gh) { Write-Error "未找到 gh CLI，请先安装并登录: gh auth login"; exit 1 }
$remote = (git remote get-url origin 2>$null)
$repo = ($remote -replace '^https?://[^/]+/','' -replace '^git@[^:]+:','' -replace '\.git$','').Trim()
if (-not $repo) { Write-Error "无法从 git remote 获取仓库名（origin），请确认仓库已设置 remote"; exit 1 }
Write-Host "目标仓库: $repo" -ForegroundColor Cyan
Set-Location $PSScriptRoot

$notes = @"
Reasonix 前端 + DeepSeek Harness 后端桥接桌面端。

三个安装包：
- dsh-(reasonix)UI-desktop Setup 0.1.0.exe — NSIS 标准安装向导（可选目录、建快捷方式）
- dsh-(reasonix)UI-desktop-0.1.0-win.zip — 便携版（解压即用）
- dsh-(reasonix)UI-desktop 0.1.0.exe — 单文件便携版

安装包内置 Node.js，无需预装；应用会自动拉起 DeepSeek Harness 后端。
"@

& $gh release create v0.1.0 --title "DSH-Reasonix v0.1.0" --notes $notes --repo $repo 2>&1 | Out-String
