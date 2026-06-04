@echo off
setlocal enabledelayedexpansion
chcp 65001 >nul
title SmartNPC Launcher

rem ---- Locate repo root (this script's own directory) ----
set "SMARTNPC_REPO=%~dp0"
if "%SMARTNPC_REPO:~-1%"=="\" set "SMARTNPC_REPO=%SMARTNPC_REPO:~0,-1%"
cd /d "%SMARTNPC_REPO%"

rem ---- Load .env (KEY=VALUE per line, # comments) ----
if not exist .env (
    echo [ERROR] .env not found. Copy .env.example to .env and fill required values.
    pause
    exit /b 1
)
for /f "tokens=1,* delims==" %%A in ('findstr /b /v /c:"#" .env') do (
    if not "%%A"=="" set "%%A=%%B"
)
echo [env] loaded .env
if defined SMARTNPC_RELAY_DEBUG_PAYLOAD echo [env] SMARTNPC_RELAY_DEBUG_PAYLOAD=%SMARTNPC_RELAY_DEBUG_PAYLOAD%

rem ---- Defaults ----
if not defined TASK_EXE              set "TASK_EXE=%USERPROFILE%\go\bin\task.exe"
if not defined WSL_DISTRO            set "WSL_DISTRO=Ubuntu-22.04"
if not defined SMARTNPC_HTTP_PORT    set "SMARTNPC_HTTP_PORT=3000"
if not defined SMARTNPC_WS_URL       set "SMARTNPC_WS_URL=ws://127.0.0.1:18745/ws"
if not defined SMARTNPC_HERMES_KEY   set "SMARTNPC_HERMES_KEY=smartnpc-test-key"
if not defined SMARTNPC_ACTIVE_PROFILES set "SMARTNPC_ACTIVE_PROFILES=xiami,abigail,haley,harvey,penny,sebastian"
if not defined HERMES_BOOT_TIMEOUT   set "HERMES_BOOT_TIMEOUT=90"

rem ---- Prerequisites ----
if not defined SMARTNPC_GAME_PATH (
    echo [ERROR] SMARTNPC_GAME_PATH not set in .env.
    pause
    exit /b 1
)
if not exist "%SMARTNPC_GAME_PATH%\StardewModdingAPI.exe" (
    echo [ERROR] StardewModdingAPI.exe not found in %SMARTNPC_GAME_PATH%
    pause
    exit /b 1
)
if not exist "%SMARTNPC_REPO%\deploy\hermes\docker-compose.yml" (
    echo [ERROR] deploy/hermes/docker-compose.yml not found.
    echo         Run setup.bat first to build the Hermes Docker environment.
    pause
    exit /b 1
)

rem ---- Parse NPC ports from npcs.yaml ----
powershell -NoProfile -Command ^
  "switch -Regex -File '%SMARTNPC_REPO%\hermes\npcs.yaml' { '^\s*- id:\s*(\S+)' { $script:id=$Matches[1] }; '^\s*gateway_port:\s*(\d+)' { \"$script:id=$($Matches[1])\" } }" ^
  > "%TEMP%\smartnpc_ports.txt"

rem ---- Detect WSL IPs ----
for /f "usebackq tokens=*" %%P in (`wsl -d %WSL_DISTRO% wslpath -a "%SMARTNPC_REPO%"`) do set "REPO_WSL=%%P"
if not defined WIN_HOST_IP (
    for /f "usebackq delims=" %%L in (`wsl -d %WSL_DISTRO% bash %REPO_WSL%/scripts/detect_wsl_ips.sh`) do set "%%L"
)
if not defined WIN_HOST_IP set "WIN_HOST_IP=127.0.0.1"
if not defined WSL_IP set "WSL_IP=127.0.0.1"
if not defined SMARTNPC_HERMES_GATEWAY_HOST set "SMARTNPC_HERMES_GATEWAY_HOST=%WSL_IP%"

echo [cfg] SMARTNPC_REPO       = %SMARTNPC_REPO%
echo [cfg] TASK_EXE            = %TASK_EXE%
echo [cfg] WIN_HOST_IP         = %WIN_HOST_IP%
echo [cfg] HERMES_GATEWAY_HOST = %SMARTNPC_HERMES_GATEWAY_HOST%
echo [cfg] SMARTNPC_GAME_PATH  = %SMARTNPC_GAME_PATH%
echo [cfg] SMARTNPC_HTTP_PORT  = %SMARTNPC_HTTP_PORT%
echo [cfg] SMARTNPC_WS_URL     = %SMARTNPC_WS_URL%
echo [cfg] ACTIVE_PROFILES     = %SMARTNPC_ACTIVE_PROFILES%
echo [cfg] HERMES_BOOT_TIMEOUT = %HERMES_BOOT_TIMEOUT%s
echo.

rem ---- Per-run log file ----
if not exist "%SMARTNPC_REPO%\logs" mkdir "%SMARTNPC_REPO%\logs"
set "SMARTNPC_LOG_DIR=%SMARTNPC_REPO%\logs"
for /f %%I in ('powershell -NoProfile -Command "Get-Date -Format yyyyMMdd_HHmmss"') do set MCP_TS=%%I
set "MCP_LOG=%SMARTNPC_REPO%\logs\mcp_%MCP_TS%.log"
set "SMARTNPC_RELAY_PAYLOAD_LOG=%SMARTNPC_REPO%\logs\payload_%MCP_TS%.log"

echo ============================================
echo   SmartNPC - One-Click Launcher
echo ============================================
echo.

rem ---- Step 1: Build ----
echo [1/5] Building mod + mcp ...
call "%TASK_EXE%" mod:build
if errorlevel 1 goto build_fail
call "%TASK_EXE%" mcp:build
if errorlevel 1 goto build_fail
echo [OK] Build complete.
echo.

rem ---- Step 2: Kill old processes ----
echo [2/5] Killing existing game / mcp processes (if any)...
powershell -NoProfile -Command "Get-Process -Name 'Stardew Valley','StardewModdingAPI' -ErrorAction SilentlyContinue | Stop-Process -Force"
powershell -NoProfile -Command "Get-Process -Name 'smartnpc-mcp' -ErrorAction SilentlyContinue | Stop-Process -Force"
timeout /t 1 /nobreak >nul
echo [OK] Old processes cleared.
echo.

rem ---- Step 3: Install mod + Start mcp ----
echo [3/5] Installing mod and starting mcp...
call "%TASK_EXE%" mod:install
if errorlevel 1 goto install_fail

echo      Starting smartnpc-mcp (--http :%SMARTNPC_HTTP_PORT%)...
echo      log -> %MCP_LOG%
set "MCP_EXE=%SMARTNPC_REPO%\smartnpc-mcp\bin\smartnpc-mcp.exe"
"%MCP_EXE%" --help >nul 2>&1
if errorlevel 1 (
    echo      [WDAC] %MCP_EXE% blocked, using go run fallback...
    start "smartnpc-mcp" powershell -NoProfile -NoExit -Command ^
        "Set-Location '%SMARTNPC_REPO%\smartnpc-mcp'; go run ./cmd/smartnpc-mcp --http ':%SMARTNPC_HTTP_PORT%' --ws-url '%SMARTNPC_WS_URL%' --hermes-config '%SMARTNPC_REPO%\hermes\runtime-config.yaml' --hermes-api-key '%SMARTNPC_HERMES_KEY%' --log-level debug 2>&1 | ForEach-Object { $_.ToString() } | Tee-Object -FilePath '%MCP_LOG%'"
) else (
    start "smartnpc-mcp" powershell -NoProfile -NoExit -Command ^
        "& '%MCP_EXE%' --http ':%SMARTNPC_HTTP_PORT%' --ws-url '%SMARTNPC_WS_URL%' --hermes-config '%SMARTNPC_REPO%\hermes\runtime-config.yaml' --hermes-api-key '%SMARTNPC_HERMES_KEY%' --log-level debug 2>&1 | ForEach-Object { $_.ToString() } | Tee-Object -FilePath '%MCP_LOG%'"
)
echo      Waiting for mcp HTTP endpoint...
:wait_mcp
timeout /t 2 /nobreak >nul
wsl -d %WSL_DISTRO% bash -c "curl -sS -o /dev/null http://%WIN_HOST_IP%:%SMARTNPC_HTTP_PORT%/mcp" >nul 2>&1
if errorlevel 1 (
    echo      ... waiting for mcp
    goto wait_mcp
)
echo [OK] mcp HTTP up at :%SMARTNPC_HTTP_PORT%/mcp
echo.

rem ---- Step 4: Start Hermes Docker containers ----
echo [4/5] Starting Hermes gateways (docker compose up)...
wsl -d %WSL_DISTRO% bash -lc "cd %REPO_WSL%/deploy/hermes && docker compose up -d"
if errorlevel 1 goto hermes_fail

echo      Waiting for gateways to become healthy (up to %HERMES_BOOT_TIMEOUT%s each)...
for %%P in (%SMARTNPC_ACTIVE_PROFILES:,= %) do (
    call :wait_one_profile %%P
    if errorlevel 1 goto hermes_fail
)
echo [OK] All gateways healthy.
echo.

rem ---- Step 5: Launch game ----
echo [5/5] Launching Stardew Valley via SMAPI...
start "" "%SMARTNPC_GAME_PATH%\StardewModdingAPI.exe"
echo [OK] Game launching. Load a save file.
echo.
echo ===========================
echo   Active NPCs: %SMARTNPC_ACTIVE_PROFILES%
echo   mcp log:     %MCP_LOG%
echo   payload log: %SMARTNPC_RELAY_PAYLOAD_LOG%
echo   live: see 'smartnpc-mcp' PowerShell window
echo ===========================
goto :eof

rem ---- Subroutine: wait for one profile's health endpoint ----
:wait_one_profile
set "_p=%~1"
set "_port="
for /f "tokens=1,2 delims==" %%A in ('findstr /i "^%_p%=" "%TEMP%\smartnpc_ports.txt"') do set "_port=%%B"
if not defined _port (
    echo      [WARN] unknown profile '%_p%' — skip health probe
    goto :eof
)
set /a "_tries=(%HERMES_BOOT_TIMEOUT% + 4) / 5"
if %_tries% LSS 1 set "_tries=1"
:_wait_one_retry
curl -sS http://%WSL_IP%:%_port%/health >nul 2>&1
if errorlevel 1 (
    set /a "_tries-=1"
    if !_tries! LEQ 0 (
        echo      [ERROR] %_p% failed to become healthy on :%_port% after %HERMES_BOOT_TIMEOUT%s
        echo      Diagnostics: wsl -d %WSL_DISTRO% bash -c "cd %REPO_WSL%/deploy/hermes && docker compose logs --tail=80 hermes-%_p%"
        exit /b 1
    )
    timeout /t 5 /nobreak >nul
    echo      ... waiting for %_p% on :%_port%
    goto _wait_one_retry
)
echo      [OK] %_p% on :%_port%
exit /b 0

:hermes_fail
echo [ERROR] Hermes gateway startup failed.
echo         Run: wsl -d %WSL_DISTRO% bash -c "cd %REPO_WSL%/deploy/hermes && docker compose ps && docker compose logs --tail=80"
pause
exit /b 1

:build_fail
echo [ERROR] Build failed.
pause
exit /b 1

:install_fail
echo [ERROR] Mod install failed.
pause
exit /b 1
