@echo off
rem DSH ????? (DeepSeek Harness web ??)
rem ?? 127.0.0.1:3080 ??; ????????
setlocal

rem 1. ????? ?? 3080
powershell -NoProfile -Command "if (Get-NetTCPConnection -LocalPort 3080 -State Listen -ErrorAction SilentlyContinue) { exit 0 } else { exit 1 }" >nul 2>&1
if %ERRORLEVEL%==0 (
  echo DSH ???? (127.0.0.1:3080)
  echo ???? DSH-ReasonixUI ??
  pause
  exit /b 0
)

rem 2. ? dsh ??
set "DSH_CMD="
where dsh >nul 2>&1 && set "DSH_CMD=dsh"
if not defined DSH_CMD (
  if exist "%APPDATA%\npm\dsh.cmd" set "DSH_CMD=%APPDATA%\npm\dsh.cmd"
)
if not defined DSH_CMD (
  if exist "%ProgramFiles%\nodejs\dsh.cmd" set "DSH_CMD=%ProgramFiles%\nodejs\dsh.cmd"
)
if not defined DSH_CMD (
  echo ??? DSH?????: npm install -g @deepseek-ai/dsh
  pause
  exit /b 1
)

rem 3. ?? dsh web
echo ?? DSH ?? (127.0.0.1:3080)...
echo ????????, ????????
echo ????? = ?? DSH ??
echo.
"%DSH_CMD%" web

endlocal
