# DSH-Reasonix 一键安装脚本
# 自动安装：Node.js（DSH 必需）、Python（视觉插件可选）、DeepSeek Harness、启动桌面端
$ErrorActionPreference = 'Continue'
$ProgressPreference = 'SilentlyContinue'
Write-Host "==============================================" -ForegroundColor Cyan
Write-Host "  DSH-Reasonix 一键安装" -ForegroundColor Cyan
Write-Host "==============================================" -ForegroundColor Cyan

function Test-Cmd($cmd) {
  $null = Get-Command $cmd -ErrorAction SilentlyContinue
  return $?
}

# ---------- 1. Node.js ----------
Write-Host ""
Write-Host "[1/4] 检查 Node.js..." -ForegroundColor Yellow
if (Test-Cmd "node") {
  Write-Host "  Node.js 已安装: $(node --version)" -ForegroundColor Green
} else {
  Write-Host "  未检测到 Node.js，正在下载安装（LTS）..." -ForegroundColor Yellow
  $nodeUrl = "https://nodejs.org/dist/v22.17.0/node-v22.17.0-x64.msi"
  $installer = "$env:TEMP\node-installer.msi"
  try {
    Invoke-WebRequest -Uri $nodeUrl -OutFile $installer -UseBasicParsing -TimeoutSec 300
    Start-Process msiexec.exe -ArgumentList "/i","$installer","/qn","/norestart" -Wait
    Write-Host "  Node.js 安装完成（需重启终端后生效）" -ForegroundColor Green
    $env:Path = "C:\Program Files\nodejs;" + $env:Path
  } catch {
    Write-Host "  Node.js 下载失败: $($_.Exception.Message)" -ForegroundColor Red
    Write-Host "  请手动从 https://nodejs.org 安装后重试" -ForegroundColor Red
  }
}

# ---------- 2. DeepSeek Harness ----------
Write-Host ""
Write-Host "[2/4] 检查 DeepSeek Harness (dsh)..." -ForegroundColor Yellow
$portOpen = Test-NetConnection -ComputerName 127.0.0.1 -Port 3080 -WarningAction SilentlyContinue -ErrorAction SilentlyContinue
if ($portOpen -and $portOpen.TcpTestSucceeded) {
  Write-Host "  DSH 服务已在运行 (127.0.0.1:3080)" -ForegroundColor Green
} else {
  Write-Host "  正在通过 npx 启动 DSH（首次需下载，约 1-3 分钟）..." -ForegroundColor Yellow
  Write-Host "  （DSH 服务会在后台运行，桌面端会自动连接）" -ForegroundColor Yellow
  # 后台启动 dsh web
  Start-Process -FilePath "cmd.exe" -ArgumentList "/c","npx.cmd -y @deepseek-ai/dsh web" -WindowStyle Hidden
}

# ---------- 3. Python（视觉插件可选） ----------
Write-Host ""
Write-Host "[3/4] 检查 Python（视觉插件可选）..." -ForegroundColor Yellow
if (Test-Cmd "python" -or Test-Cmd "py") {
  Write-Host "  Python 已安装" -ForegroundColor Green
} else {
  Write-Host "  未检测到 Python。视觉插件（vision-toolkit）需要 Python 3.11+，跳过不影响核心功能。" -ForegroundColor DarkYellow
  Write-Host "  如需视觉能力，请手动安装: winget install Python.Python.3.12" -ForegroundColor DarkYellow
}

# ---------- 4. 启动桌面端 ----------
Write-Host ""
Write-Host "[4/4] 启动 DSH-Reasonix 桌面端..." -ForegroundColor Yellow
# 实际打包的主程序名由 package.json productName 决定，动态查找
$exe = Get-ChildItem $PSScriptRoot -Filter "*.exe" -ErrorAction SilentlyContinue | Where-Object { $_.Name -match 'UI-desktop' -and $_.Name -notmatch 'uninstall' } | Select-Object -First 1 -ExpandProperty FullName
if ($exe -and (Test-Path $exe)) {
  Start-Process $exe
  Write-Host "  已启动 DSH-Reasonix" -ForegroundColor Green
} else {
  # 开发模式：electron
  $electron = Join-Path $PSScriptRoot "node_modules\electron\dist\electron.exe"
  if (Test-Path $electron) {
    Start-Process $electron -ArgumentList "." -WorkingDirectory $PSScriptRoot
    Write-Host "  已启动（开发模式）" -ForegroundColor Green
  } else {
    Write-Host "  未找到可执行文件，请先运行 npm install" -ForegroundColor Red
  }
}

Write-Host ""
Write-Host "==============================================" -ForegroundColor Cyan
Write-Host "  安装完成！DSH 服务将在后台运行，桌面端自动连接。" -ForegroundColor Cyan
Write-Host "  桌面端窗口已弹出（或稍后自动弹出）。" -ForegroundColor Cyan
Write-Host "==============================================" -ForegroundColor Cyan
