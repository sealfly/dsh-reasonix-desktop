@echo off
rem DSH backend launcher (DeepSeek Harness web mode, 127.0.0.1:3080).
rem Priority:
rem   1. Bundled runtime (lazy installer): "%APP_DIR%dsh-runtime\node.exe" + dsh  (offline-ready)
rem   2. System dsh command (npm global)
rem   3. Hint how to install.
setlocal

set "APP_DIR=%~dp0"

rem 1. Already running? Probe 3080
powershell -NoProfile -Command "if (Get-NetTCPConnection -LocalPort 3080 -State Listen -ErrorAction SilentlyContinue) { exit 0 } else { exit 1 }" >nul 2>&1
if %ERRORLEVEL%==0 (
  echo DSH already running (127.0.0.1:3080)
  echo Just open DSH-ReasonixUI.
  pause
  exit /b 0
)

rem 2. Bundled runtime (offline lazy installer)
if exist "%APP_DIR%dsh-runtime\node.exe" (
  if exist "%APP_DIR%dsh-runtime\dsh\node_modules\@deepseek-ai\dsh\lib\bin.js" (
    echo Starting bundled DSH backend (127.0.0.1:3080)...
    echo First start initializes; keep this window open.
    echo Closing this window stops the DSH backend.
    echo.
    "%APP_DIR%dsh-runtime\node.exe" "%APP_DIR%dsh-runtime\dsh\node_modules\@deepseek-ai\dsh\lib\bin.js" web
    endlocal
    exit /b 0
  )
)

rem 3. System dsh command (npm global install)
set "DSH_CMD="
where dsh >nul 2>&1 && set "DSH_CMD=dsh"
if not defined DSH_CMD (
  if exist "%APPDATA%\npm\dsh.cmd" set "DSH_CMD=%APPDATA%\npm\dsh.cmd"
)
if not defined DSH_CMD (
  if exist "%ProgramFiles%\nodejs\dsh.cmd" set "DSH_CMD=%ProgramFiles%\nodejs\dsh.cmd"
)
if defined DSH_CMD (
  echo Starting DSH backend (127.0.0.1:3080)...
  echo First start initializes; keep this window open.
  echo Closing this window stops the DSH backend.
  echo.
  "%DSH_CMD%" web
  endlocal
  exit /b 0
)

rem 4. Nothing found
echo DSH not found. Install it first:
echo   npm install -g @deepseek-ai/dsh
echo (offline? use the lazy installer which bundles DSH + node runtime)
pause
exit /b 1
