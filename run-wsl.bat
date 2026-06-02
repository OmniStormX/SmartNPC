@echo off
setlocal enabledelayedexpansion
chcp 65001 >nul
title SmartNPC Launcher (WSL Hermes native)

rem ============================================================
rem  WSL-native Hermes mode: profiles run directly in WSL (not Docker).
rem  Langfuse tracing goes to cloud.langfuse.com via profile .env.
rem ============================================================

rem ---- Locate repo root ----
set "SMARTNPC_REPO=%~dp0"
if "%SMARTNPC_REPO:~-1%"=="\" set "SMARTNPC_REPO=%SMARTNPC_REPO:~0,-1%"
cd /d "%SMARTNPC_REPO%"

rem ---- Load .env ----
if exist .env (
    for /f "tokens=1,* delims==" %%A in ('findstr /b /v /c:"#" .env') do (
        if not "%%A"=="" set "%%A=%%B"
    )
    echo [env] loaded .env
)

rem ---- Config ----
if not defined TASK_EXE              set "TASK_EXE=%USERPROFILE%\go\bin\task.exe"
if not defined WSL_DISTRO            set "WSL_DISTRO=Ubuntu-22.04"
if not defined SMARTNPC_GAME_PATH    set "SMARTNPC_GAME_PATH=D:\Stardew Valley"
if not defined SMARTNPC_HTTP_PORT    set "SMARTNPC_HTTP_PORT=3000"
if not defined SMARTNPC_WS_URL       set "SMARTNPC_WS_URL=ws://127.0.0.1:18745/ws"
if not defined SMARTNPC_HERMES_KEY   set "SMARTNPC_HERMES_KEY=smartnpc-test-key"
if not defined SMARTNPC_ACTIVE_PROFILES set "SMARTNPC_ACTIVE_PROFILES=xiami,abigail,haley,harvey,penny,sebastian"
if not defined HERMES_BOOT_TIMEOUT   set "HERMES_BOOT_TIMEOUT=90"

rem ---- WSL path of the repo ----
for /f "usebackq tokens=*" %%P in (`wsl -d %WSL_DISTRO% wslpath -a "%SMARTNPC_REPO%"`) do set "REPO_WSL=%%P"
if not defined REPO_WSL (
    echo [ERROR] failed to convert %SMARTNPC_REPO% to a WSL path.
    pause & exit /b 1
)

rem ---- Detect cross-boundary IPs ----
if not defined WIN_HOST_IP (
    for /f "usebackq delims=" %%L in (`wsl -d %WSL_DISTRO% bash %REPO_WSL%/scripts/detect_wsl_ips.sh`) do set "%%L"
)
if not defined WIN_HOST_IP set "WIN_HOST_IP=127.0.0.1"
if not defined WSL_IP set "WSL_IP=127.0.0.1"

echo [cfg] REPO           = %SMARTNPC_REPO%
echo [cfg] REPO_WSL       = %REPO_WSL%
echo [cfg] WIN_HOST_IP    = %WIN_HOST_IP%
echo [cfg] WSL_IP         = %WSL_IP%
echo [cfg] ACTIVE_PROFILES= %SMARTNPC_ACTIVE_PROFILES%
echo [cfg] GAME_PATH      = %SMARTNPC_GAME_PATH%
echo.

rem ---- Log file ----
if not exist "%SMARTNPC_REPO%\logs" mkdir "%SMARTNPC_REPO%\logs"
set "SMARTNPC_LOG_DIR=%SMARTNPC_REPO%\logs"
for /f %%I in ('powershell -NoProfile -Command "Get-Date -Format yyyyMMdd_HHmmss"') do set MCP_TS=%%I
set "MCP_LOG=%SMARTNPC_REPO%\logs\mcp_%MCP_TS%.log"

echo ============================================
echo   SmartNPC - WSL Hermes Native Launcher
echo   (Langfuse ^> cloud.langfuse.com)
echo ============================================
echo.

rem ---- Step 1: Build ----
echo [1/6] Building mod + mcp ...
call "%TASK_EXE%" mod:build
if errorlevel 1 goto build_fail
call "%TASK_EXE%" mcp:build
if errorlevel 1 goto build_fail
echo [OK] Build complete.
echo.

rem ---- Step 2: Kill old processes ----
echo [2/6] Killing existing game / mcp / hermes processes...
powershell -NoProfile -Command "Get-Process -Name 'Stardew Valley','StardewModdingAPI' -ErrorAction SilentlyContinue | Stop-Process -Force"
powershell -NoProfile -Command "Get-Process -Name 'smartnpc-mcp' -ErrorAction SilentlyContinue | Stop-Process -Force"
wsl -d %WSL_DISTRO% bash -lc "pkill -f 'hermes.*gateway run' 2>/dev/null; true"
timeout /t 1 /nobreak >nul
echo [OK] Old processes cleared.
echo.

rem ---- Step 3: Install mod ----
echo [3/6] Installing mod...
call "%TASK_EXE%" mod:install
if errorlevel 1 goto install_fail
echo [OK] Mod installed.
echo.

rem ---- Step 4: Start mcp ----
echo [4/6] Starting smartnpc-mcp (--http :%SMARTNPC_HTTP_PORT%)...
echo      log -> %MCP_LOG%
set "MCP_EXE=%SMARTNPC_REPO%\smartnpc-mcp\bin\smartnpc-mcp.exe"
"%MCP_EXE%" --help >nul 2>&1
if errorlevel 1 (
    echo      [WDAC] exe blocked, using go run fallback...
    start "smartnpc-mcp" powershell -NoProfile -NoExit -Command ^
        "Set-Location '%SMARTNPC_REPO%\smartnpc-mcp'; go run ./cmd/smartnpc-mcp --http ':%SMARTNPC_HTTP_PORT%' --ws-url '%SMARTNPC_WS_URL%' --hermes-config '%SMARTNPC_REPO%\hermes\runtime-config.yaml' --hermes-api-key '%SMARTNPC_HERMES_KEY%' --log-level debug 2>&1 | ForEach-Object { $_.ToString() } | Tee-Object -FilePath '%MCP_LOG%'"
) else (
    start "smartnpc-mcp" powershell -NoProfile -NoExit -Command ^
        "& '%MCP_EXE%' --http ':%SMARTNPC_HTTP_PORT%' --ws-url '%SMARTNPC_WS_URL%' --hermes-config '%SMARTNPC_REPO%\hermes\runtime-config.yaml' --hermes-api-key '%SMARTNPC_HERMES_KEY%' --log-level debug 2>&1 | ForEach-Object { $_.ToString() } | Tee-Object -FilePath '%MCP_LOG%'"
)
echo      Waiting for mcp HTTP endpoint...
:wait_mcp
timeout /t 2 /nobreak >nul
curl -sS -o nul http://127.0.0.1:%SMARTNPC_HTTP_PORT%/healthz >nul 2>&1
if errorlevel 1 (
    echo      ... waiting for mcp
    goto wait_mcp
)
echo [OK] mcp HTTP up at :%SMARTNPC_HTTP_PORT%
echo.

rem ---- Step 4.5: Sync Hermes profiles in WSL ----
echo [4.5/6] Syncing Hermes profiles to WSL...
echo      Updating SMARTNPC_MCP_URL in profile .env files (IP=%WIN_HOST_IP%)...
wsl -d %WSL_DISTRO% bash -lc "cd %REPO_WSL% && for d in hermes/profiles/*/; do p=$(basename \"$d\"); [ \"$p\" = '_master' ] && continue; env_file=\"$d/.env\"; if [ -f \"$env_file\" ]; then sed -i 's|^SMARTNPC_MCP_URL=.*|SMARTNPC_MCP_URL=http://%WIN_HOST_IP%:%SMARTNPC_HTTP_PORT%/mcp|' \"$env_file\"; fi; done && bash hermes/install.sh"
if errorlevel 1 (
    echo [WARN] install.sh failed, profiles may be stale
)
echo [OK] Profiles synced (MCP_URL=http://%WIN_HOST_IP%:%SMARTNPC_HTTP_PORT%/mcp).
echo.

rem ---- Step 5: Start Hermes Gateways in WSL (native) ----
echo [5/6] Starting Hermes Gateways in WSL (native hermes)...
echo      MCP_URL for Hermes: http://%WIN_HOST_IP%:%SMARTNPC_HTTP_PORT%/mcp
echo      Profiles: %SMARTNPC_ACTIVE_PROFILES%

rem Kill any leftover gateway processes, then launch all profiles via helper script.
rem setsid ensures processes survive after the WSL bash session exits.
wsl.exe -d %WSL_DISTRO% -u synchen -- bash -c "bash %REPO_WSL%/scripts/launch_hermes_wsl.sh %SMARTNPC_ACTIVE_PROFILES:,= %"

echo      Waiting for gateways to become healthy (timeout %HERMES_BOOT_TIMEOUT%s each)...
for %%P in (%SMARTNPC_ACTIVE_PROFILES:,= %) do (
    call :wait_one_profile %%P
    if errorlevel 1 goto hermes_fail
)
echo [OK] All gateways healthy.
echo.

rem ---- Step 6: Launch the game ----
echo [6/6] Launching Stardew Valley via SMAPI...
start "" "%SMARTNPC_GAME_PATH%\StardewModdingAPI.exe"
echo [OK] Game launching. Load a save file.
echo.
echo ===========================
echo   Active NPCs: %SMARTNPC_ACTIVE_PROFILES%
echo   Hermes mode: WSL native (Langfuse Cloud)
echo   mcp log: %MCP_LOG%
echo   hermes logs: wsl -d %WSL_DISTRO% cat ~/.hermes/profiles/xiami/logs/gateway.log
echo   stop hermes: wsl -d %WSL_DISTRO% bash -lc "pkill -f 'hermes.*gateway run'"
echo ===========================
goto :eof

:wait_one_profile
set "_p=%~1"
set "_port="
if /i "%_p%"=="xiami"     set "_port=8642"
if /i "%_p%"=="abigail"   set "_port=8643"
if /i "%_p%"=="haley"     set "_port=8644"
if /i "%_p%"=="harvey"    set "_port=8645"
if /i "%_p%"=="penny"     set "_port=8646"
if /i "%_p%"=="sebastian" set "_port=8647"
if not defined _port (
    echo      [WARN] unknown profile '%_p%' - skip
    goto :eof
)
set /a "_tries=(%HERMES_BOOT_TIMEOUT% + 2) / 3"
if %_tries% LSS 1 set "_tries=1"
:_wait_retry
curl -sS http://127.0.0.1:%_port%/health >nul 2>&1
if errorlevel 1 (
    set /a "_tries-=1"
    if !_tries! LEQ 0 (
        echo      [ERROR] %_p% failed to become healthy on :%_port%
        echo      Check: wsl -d %WSL_DISTRO% tail -30 ~/.hermes/profiles/%_p%/logs/gateway.log
        exit /b 1
    )
    timeout /t 3 /nobreak >nul
    goto _wait_retry
)
echo      [OK] %_p% on :%_port%
exit /b 0

:hermes_fail
echo [ERROR] Hermes gateway startup failed.
pause & exit /b 1

:build_fail
echo [ERROR] Build failed.
pause & exit /b 1

:install_fail
echo [ERROR] Mod install failed.
pause & exit /b 1
