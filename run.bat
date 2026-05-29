@echo off
setlocal enabledelayedexpansion
chcp 65001 >nul
title SmartNPC Launcher (Hermes-first)

rem ---- Locate repo root (this script's own directory) ----
rem Allows the repo to be cloned anywhere; no hardcoded D:\SmartNPC.
set "SMARTNPC_REPO=%~dp0"
if "%SMARTNPC_REPO:~-1%"=="\" set "SMARTNPC_REPO=%SMARTNPC_REPO:~0,-1%"
cd /d "%SMARTNPC_REPO%"

rem ---- Load .env (KEY=VALUE per line, # comments) ----
rem Taskfile auto-loads .env via dotenv:; run.bat doesn't. Mirror that here so
rem flags like SMARTNPC_RELAY_DEBUG_PAYLOAD propagate into the mcp child.
if exist .env (
    for /f "tokens=1,* delims==" %%A in ('findstr /b /v /c:"#" .env') do (
        if not "%%A"=="" set "%%A=%%B"
    )
    echo [env] loaded .env
    if defined SMARTNPC_RELAY_DEBUG_PAYLOAD echo [env] SMARTNPC_RELAY_DEBUG_PAYLOAD=%SMARTNPC_RELAY_DEBUG_PAYLOAD% (relay 会打 outbound/inbound 完整 body)
)

rem ---- Resolve config (env var > .env > built-in default) ----
if not defined TASK_EXE              set "TASK_EXE=%USERPROFILE%\go\bin\task.exe"
if not defined WSL_DISTRO            set "WSL_DISTRO=Ubuntu-22.04"
if not defined SMARTNPC_GAME_PATH    set "SMARTNPC_GAME_PATH=D:\Stardew Valley"
if not defined SMARTNPC_HTTP_PORT    set "SMARTNPC_HTTP_PORT=3000"
if not defined SMARTNPC_WS_URL       set "SMARTNPC_WS_URL=ws://127.0.0.1:18745/ws"
if not defined SMARTNPC_HERMES_KEY   set "SMARTNPC_HERMES_KEY=smartnpc-test-key"
if not defined SMARTNPC_ACTIVE_PROFILES set "SMARTNPC_ACTIVE_PROFILES=xiami,abigail,haley,harvey,penny,sebastian"
if not defined SMARTNPC_HERMES_MODE  set "SMARTNPC_HERMES_MODE=docker"
if not defined HERMES_BOOT_TIMEOUT   set "HERMES_BOOT_TIMEOUT=90"
if not defined HERMES_EXE            set "HERMES_EXE=hermes"

rem ---- Mode-dependent setup ----
if /i "%SMARTNPC_HERMES_MODE%"=="local" goto local_setup
goto docker_setup

:local_setup
rem Local mode: all components on Windows, no WSL needed.
set "WIN_HOST_IP=127.0.0.1"
set "WSL_IP=127.0.0.1"
set "SMARTNPC_HERMES_GATEWAY_HOST=127.0.0.1"
goto after_setup

:docker_setup
rem Docker mode: Hermes in WSL Docker, need cross-boundary IPs.
rem ---- WSL path of the repo (e.g. /mnt/d/SmartNPC) ----
for /f "usebackq tokens=*" %%P in (`wsl -d %WSL_DISTRO% wslpath -a "%SMARTNPC_REPO%"`) do set "REPO_WSL=%%P"
if not defined REPO_WSL (
    echo [ERROR] failed to convert %SMARTNPC_REPO% to a WSL path.
    pause
    exit /b 1
)

rem ---- Auto-detect cross-boundary IPs ----
if not defined WIN_HOST_IP (
    for /f "usebackq delims=" %%L in (`wsl -d %WSL_DISTRO% bash %REPO_WSL%/scripts/detect_wsl_ips.sh`) do set "%%L"
)
if not defined WIN_HOST_IP (
    echo [WARN] could not auto-detect WIN_HOST_IP from WSL; falling back to 127.0.0.1
    set "WIN_HOST_IP=127.0.0.1"
)
if not defined WSL_IP (
    echo [WARN] could not auto-detect WSL_IP via hostname -I; falling back to 127.0.0.1
    set "WSL_IP=127.0.0.1"
)
if not defined SMARTNPC_HERMES_GATEWAY_HOST set "SMARTNPC_HERMES_GATEWAY_HOST=%WSL_IP%"
goto after_setup

:after_setup

echo [cfg] SMARTNPC_REPO       = %SMARTNPC_REPO%
echo [cfg] HERMES_MODE         = %SMARTNPC_HERMES_MODE%
echo [cfg] TASK_EXE            = %TASK_EXE%
echo [cfg] WIN_HOST_IP         = %WIN_HOST_IP%
echo [cfg] HERMES_GATEWAY_HOST = %SMARTNPC_HERMES_GATEWAY_HOST%
echo [cfg] SMARTNPC_GAME_PATH  = %SMARTNPC_GAME_PATH%

echo [cfg] SMARTNPC_HTTP_PORT  = %SMARTNPC_HTTP_PORT%
echo [cfg] SMARTNPC_WS_URL     = %SMARTNPC_WS_URL%
echo [cfg] ACTIVE_PROFILES     = %SMARTNPC_ACTIVE_PROFILES%
echo [cfg] HERMES_BOOT_TIMEOUT = %HERMES_BOOT_TIMEOUT%s
echo.

rem ---- Per-run log file: <repo>\logs\mcp_YYYYMMDD_HHMMSS.log. ----
rem Use PowerShell for the timestamp — wmic is removed on recent Win11 builds
rem and silently produces a garbage filename when called via for /f.
if not exist "%SMARTNPC_REPO%\logs" mkdir "%SMARTNPC_REPO%\logs"
set "SMARTNPC_LOG_DIR=%SMARTNPC_REPO%\logs"
for /f %%I in ('powershell -NoProfile -Command "Get-Date -Format yyyyMMdd_HHmmss"') do set MCP_TS=%%I
set "MCP_LOG=%SMARTNPC_REPO%\logs\mcp_%MCP_TS%.log"
rem Per-run payload trace (full request/response body, JSON lines). Routed
rem there only when SMARTNPC_RELAY_DEBUG_PAYLOAD=1 is set in .env.
set "SMARTNPC_RELAY_PAYLOAD_LOG=%SMARTNPC_REPO%\logs\payload_%MCP_TS%.log"

echo ============================================
echo   SmartNPC - Hermes-first One-Click Launcher
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
echo [2/6] Killing existing game / mcp processes (if any)...
powershell -NoProfile -Command "Get-Process -Name 'Stardew Valley','StardewModdingAPI' -ErrorAction SilentlyContinue | Stop-Process -Force"
powershell -NoProfile -Command "Get-Process -Name 'smartnpc-mcp' -ErrorAction SilentlyContinue | Stop-Process -Force"
timeout /t 1 /nobreak >nul
echo [OK] Old processes cleared.
echo.

rem ---- Step 3: Install mod ----
echo [3/6] Installing mod...
call "%TASK_EXE%" mod:install
if errorlevel 1 goto install_fail
echo [OK] Mod installed.
echo.

rem ---- Step 4: Start mcp in --http mode with multi-profile fan-out ----
rem mcp MUST start BEFORE Hermes gateways: at gateway startup Hermes
rem registers MCP tools by querying http://<HOST>:3000/mcp. If that
rem endpoint is down, Hermes caches "0 tools" and won't recover even
rem after mcp comes up. Result: NPC receives the chat event but has
rem no chat_say tool to reply with.
echo [4/6] Starting smartnpc-mcp (--http :%SMARTNPC_HTTP_PORT%, --hermes-config multi-profile)...
echo      log -^> %MCP_LOG%
rem SMARTNPC_HERMES_KEY must match API_SERVER_KEY in each profile's config-overlay.yaml.
rem Default 'smartnpc-test-key' matches the shipped overlays.
rem
rem Launch strategy:
rem   1. Try compiled exe (fastest startup).
rem   2. If blocked by WDAC/Device Guard, fall back to `go run` (go.exe is signed
rem      and trusted; the temp binary it produces lands in %TEMP% which is allowed).
rem
rem mcp logs to stderr (slog JSON). PowerShell -NoExit keeps the spawned window
rem open after exit; Tee-Object writes to BOTH the window and the log file.
rem `2>&1` merges stderr into stdout. Then `ForEach-Object { $_.ToString() }`
rem flattens Windows PowerShell 5.1's ErrorRecord wrapping.
set "MCP_EXE=%SMARTNPC_REPO%\smartnpc-mcp\bin\smartnpc-mcp.exe"

rem Quick probe: can we execute the binary at all?
"%MCP_EXE%" --help >nul 2>&1
if errorlevel 1 (
    echo      [WDAC] %MCP_EXE% blocked by policy, using go run fallback...
    start "smartnpc-mcp" powershell -NoProfile -NoExit -Command ^
        "Set-Location '%SMARTNPC_REPO%\smartnpc-mcp'; go run ./cmd/smartnpc-mcp --http ':%SMARTNPC_HTTP_PORT%' --ws-url '%SMARTNPC_WS_URL%' --hermes-config '%SMARTNPC_REPO%\hermes\runtime-config.yaml' --hermes-api-key '%SMARTNPC_HERMES_KEY%' --log-level debug 2>&1 | ForEach-Object { $_.ToString() } | Tee-Object -FilePath '%MCP_LOG%'"
) else (
    start "smartnpc-mcp" powershell -NoProfile -NoExit -Command ^
        "& '%MCP_EXE%' --http ':%SMARTNPC_HTTP_PORT%' --ws-url '%SMARTNPC_WS_URL%' --hermes-config '%SMARTNPC_REPO%\hermes\runtime-config.yaml' --hermes-api-key '%SMARTNPC_HERMES_KEY%' --log-level debug 2>&1 | ForEach-Object { $_.ToString() } | Tee-Object -FilePath '%MCP_LOG%'"
)
echo      Waiting for mcp HTTP endpoint to become reachable...
:wait_mcp
timeout /t 2 /nobreak >nul
if /i "%SMARTNPC_HERMES_MODE%"=="local" (
    curl -sS -o nul http://127.0.0.1:%SMARTNPC_HTTP_PORT%/healthz >nul 2>&1
) else (
    wsl -d %WSL_DISTRO% bash -c "curl -sS -o /dev/null http://%WIN_HOST_IP%:%SMARTNPC_HTTP_PORT%/mcp" >nul 2>&1
)
if errorlevel 1 (
echo      ... waiting for mcp
goto wait_mcp
)
echo [OK] mcp HTTP up at :%SMARTNPC_HTTP_PORT%/mcp.
echo.

rem ---- Step 4.5: Render Hermes profiles from _master/ ----
echo [4.5/6] Rendering Hermes profiles from _master/ ...
powershell -NoProfile -ExecutionPolicy Bypass -File "%SMARTNPC_REPO%\scripts\render_profiles.ps1" -RepoRoot "%SMARTNPC_REPO%"
if errorlevel 1 (
    echo [ERROR] Profile rendering failed.
    pause
    exit /b 1
)
echo [OK] Profiles rendered.
echo.

rem ---- Step 5: Start Hermes Gateways ----
if /i "%SMARTNPC_HERMES_MODE%"=="local" goto step5_local
goto step5_docker

:step5_local
echo [5/6] Starting Hermes Gateways (local mode, %HERMES_EXE%)...
for %%P in (%SMARTNPC_ACTIVE_PROFILES:,= %) do call :start_local_profile %%P
echo      Waiting for selected gateways to become healthy (each up to ~%HERMES_BOOT_TIMEOUT%s)...
for %%P in (%SMARTNPC_ACTIVE_PROFILES:,= %) do (
    call :wait_one_profile %%P
    if errorlevel 1 goto hermes_fail
)
echo [OK] All selected gateways healthy.
echo.
goto step6

:start_local_profile
set "_p=%~1"
start "hermes-%_p%" "%HERMES_EXE%" -p %_p% gateway run --accept-hooks
echo      started %_p%
goto :eof

:step5_docker
rem ---- Start Hermes Gateways via Docker Compose ----
rem Generates .env for Docker, syncs profile data, then builds/starts from the local Dockerfile.
echo [5/6] Starting Hermes Gateways via Docker Compose ...

rem Generate deploy/hermes/.env from root config (single source of truth).
>"%SMARTNPC_REPO%\deploy\hermes\.env" echo SMARTNPC_MCP_URL=http://%WIN_HOST_IP%:%SMARTNPC_HTTP_PORT%/mcp
>>"%SMARTNPC_REPO%\deploy\hermes\.env" echo SMARTNPC_HERMES_KEY=%SMARTNPC_HERMES_KEY%
if defined HERMES_AGENT_URL >>"%SMARTNPC_REPO%\deploy\hermes\.env" echo HERMES_AGENT_URL=%HERMES_AGENT_URL%
if defined HERMES_AGENT_API_KEY >>"%SMARTNPC_REPO%\deploy\hermes\.env" echo HERMES_AGENT_API_KEY=%HERMES_AGENT_API_KEY%
if defined HERMES_AGENT_MODEL >>"%SMARTNPC_REPO%\deploy\hermes\.env" echo HERMES_AGENT_MODEL=%HERMES_AGENT_MODEL%
if defined HERMES_LANGFUSE_PUBLIC_KEY >>"%SMARTNPC_REPO%\deploy\hermes\.env" echo HERMES_LANGFUSE_PUBLIC_KEY=%HERMES_LANGFUSE_PUBLIC_KEY%
if defined HERMES_LANGFUSE_SECRET_KEY >>"%SMARTNPC_REPO%\deploy\hermes\.env" echo HERMES_LANGFUSE_SECRET_KEY=%HERMES_LANGFUSE_SECRET_KEY%
if defined HERMES_LANGFUSE_BASE_URL >>"%SMARTNPC_REPO%\deploy\hermes\.env" echo HERMES_LANGFUSE_BASE_URL=%HERMES_LANGFUSE_BASE_URL%
echo      Generated deploy/hermes/.env (MCP_URL=http://%WIN_HOST_IP%:%SMARTNPC_HTTP_PORT%/mcp)

rem Sync profile data and start containers in WSL Docker from the local Dockerfile.
wsl -d %WSL_DISTRO% bash -lc "cd %REPO_WSL%/deploy/hermes && bash scripts/sync-profiles.sh && docker compose -f docker-compose.yml -f docker-compose.langfuse.yml build --pull && docker compose -f docker-compose.yml -f docker-compose.langfuse.yml up -d"
if errorlevel 1 goto hermes_fail

echo      Waiting for selected gateways to become healthy (each up to ~%HERMES_BOOT_TIMEOUT%s)...
for %%P in (%SMARTNPC_ACTIVE_PROFILES:,= %) do (
    call :wait_one_profile %%P
    if errorlevel 1 goto hermes_fail
)
echo [OK] All selected gateways healthy.
echo.
goto step6

:wait_one_profile
rem %1 = profile name. Look up port from the canonical mapping below.
set "_p=%~1"
set "_port="
if /i "%_p%"=="xiami"     set "_port=8642"
if /i "%_p%"=="abigail"   set "_port=8643"
if /i "%_p%"=="haley"     set "_port=8644"
if /i "%_p%"=="harvey"    set "_port=8645"
if /i "%_p%"=="penny"     set "_port=8646"
if /i "%_p%"=="sebastian" set "_port=8647"
if not defined _port (
    echo      [WARN] unknown profile '%_p%' — skip health probe
    goto :eof
)
set /a "_tries=(%HERMES_BOOT_TIMEOUT% + 4) / 5"
if %_tries% LSS 1 set "_tries=1"
:_wait_one_retry
if /i "%SMARTNPC_HERMES_MODE%"=="local" (
    curl -sS http://127.0.0.1:%_port%/health >nul 2>&1
) else (
    curl -sS http://%WSL_IP%:%_port%/health >nul 2>&1
)
if errorlevel 1 (
    set /a "_tries-=1"
    if !_tries! LEQ 0 (
        echo      [ERROR] %_p% failed to become healthy on :%_port% after %HERMES_BOOT_TIMEOUT%s
        if /i not "%SMARTNPC_HERMES_MODE%"=="local" echo      WSL diagnostics: cd %REPO_WSL%/deploy/hermes ^&^& docker compose logs --tail=160 hermes-%_p%
        exit /b 1
    )
    timeout /t 5 /nobreak >nul
    echo      ... waiting for %_p% on :%_port%
    goto _wait_one_retry
)
echo      [OK] %_p% on :%_port%
exit /b 0

:step6
rem ---- Step 6: Launch the game ----
echo [6/6] Launching Stardew Valley via SMAPI...
start "" "%SMARTNPC_GAME_PATH%\StardewModdingAPI.exe"
echo [OK] Game launching. Load a save file.
echo.
echo ===========================
echo   Active NPCs: %SMARTNPC_ACTIVE_PROFILES%
echo   Group chat: M6 (not orchestrated yet — UI works but no NPC replies).
echo.
echo   mcp log:     %MCP_LOG%
echo   payload log: %SMARTNPC_RELAY_PAYLOAD_LOG% (only filled if SMARTNPC_RELAY_DEBUG_PAYLOAD=1)
echo   live: 直接看那个 'smartnpc-mcp' PowerShell 窗口
echo   filter past: powershell -NoProfile -Command "Select-String -Path '%MCP_LOG%' -Pattern 'hermesrelay'"
echo ===========================
goto :eof

:hermes_fail
echo [ERROR] Hermes gateway startup or health check failed.
if /i "%SMARTNPC_HERMES_MODE%"=="docker" echo         Run in WSL: cd %REPO_WSL%/deploy/hermes ^&^& docker compose ps ^&^& docker compose logs --tail=160 hermes-xiami
pause
exit /b 1

:build_fail
echo [ERROR] Build failed.
pause
exit /b 1

:install_fail
echo [ERROR] Mod or Hermes install failed.
pause
exit /b 1
