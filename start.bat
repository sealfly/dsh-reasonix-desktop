@echo off
setlocal
cd /d "%~dp0"
if not exist node_modules (
  echo [DSH x Reasonix] Installing dependencies (first run)...
  call npm install --no-audit --no-fund
  if errorlevel 1 ( echo install failed & pause & exit /b 1 )
)
call npm start
