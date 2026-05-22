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

rem ---- WSL path of the repo (e.g. /mnt/d/SmartNPC) ----
rem Used by `bash` calls inside WSL. Computed once via wslpath.
for /f "usebackq tokens=*" %%P in (`wsl -d %WSL_DISTRO% wslpath -a "%SMARTNPC_REPO%"`) do set "REPO_WSL=%%P"
if not defined REPO_WSL (
    echo [ERROR] failed to convert %SMARTNPC_REPO% to a WSL path.
    pause
    exit /b 1
)

rem ---- Auto-detect cross-boundary IPs (delegated to a shell script to avoid
rem      awk/grep/redirect quoting hell inside cmd's `for /f` backticks). ----
rem WIN_HOST_IP: from WSL → reach Windows host (used by Hermes gateways to
rem              call mcp HTTP).
rem WSL_IP:      from Windows → reach WSL (used by run.bat to curl gateway
rem              /health endpoints).
rem Both can be preset in .env to override auto-detection.
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

echo [cfg] SMARTNPC_REPO       = %SMARTNPC_REPO%

echo [cfg] TASK_EXE            = %TASK_EXE%
echo [cfg] WSL_DISTRO          = %WSL_DISTRO%
echo [cfg] WIN_HOST_IP         = %WIN_HOST_IP%
echo [cfg] WSL_IP              = %WSL_IP%
echo [cfg] HERMES_GATEWAY_HOST = %SMARTNPC_HERMES_GATEWAY_HOST%
echo [cfg] SMARTNPC_GAME_PATH  = %SMARTNPC_GAME_PATH%

echo [cfg] SMARTNPC_HTTP_PORT  = %SMARTNPC_HTTP_PORT%
echo [cfg] SMARTNPC_WS_URL     = %SMARTNPC_WS_URL%
echo [cfg] ACTIVE_PROFILES     = %SMARTNPC_ACTIVE_PROFILES%
echo.

rem ---- Per-run log file: <repo>\logs\mcp_YYYYMMDD_HHMMSS.log. ----
rem Use PowerShell for the timestamp — wmic is removed on recent Win11 builds
rem and silently produces a garbage filename when called via for /f.
if not exist "%SMARTNPC_REPO%\logs" mkdir "%SMARTNPC_REPO%\logs"
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

rem ---- Step 3: Install mod + sync hermes profiles + ensure auxiliary model ----
echo [3/6] Installing mod, syncing Hermes profiles, ensuring auxiliary model...
call "%TASK_EXE%" mod:install
if errorlevel 1 goto install_fail
wsl -d %WSL_DISTRO% bash -lc "bash %REPO_WSL%/hermes/install.sh"
if errorlevel 1 goto install_fail
wsl -d %WSL_DISTRO% bash -lc "bash %REPO_WSL%/scripts/apply_hermes_tuning.sh"
if errorlevel 1 goto install_fail
wsl -d %WSL_DISTRO% bash -lc "bash %REPO_WSL%/scripts/ensure_hermes_aux.sh"
echo [OK] Mod installed, Hermes profiles synced, tuning applied, session_search routed to gpt-4o-mini.
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
rem mcp logs to stderr (slog JSON). PowerShell -NoExit keeps the spawned window
rem open after exit; Tee-Object writes to BOTH the window and the log file.
rem `2>&1` merges stderr into stdout. Then `ForEach-Object { $_.ToString() }`
rem flattens Windows PowerShell 5.1's ErrorRecord wrapping (which otherwise
rem decorates every stderr line with NativeCommandError + position info).
start "smartnpc-mcp" powershell -NoProfile -NoExit -Command ^
    "& '%SMARTNPC_REPO%\smartnpc-mcp\bin\smartnpc-mcp.exe' --http ':%SMARTNPC_HTTP_PORT%' --ws-url '%SMARTNPC_WS_URL%' --hermes-config '%SMARTNPC_REPO%\hermes\runtime-config.yaml' --hermes-api-key '%SMARTNPC_HERMES_KEY%' --log-level debug 2>&1 | ForEach-Object { $_.ToString() } | Tee-Object -FilePath '%MCP_LOG%'"
echo      Waiting for mcp HTTP endpoint to become reachable from WSL (http://%WIN_HOST_IP%:%SMARTNPC_HTTP_PORT%/mcp)...
:wait_mcp
timeout /t 2 /nobreak >nul
wsl -d %WSL_DISTRO% bash -c "curl -sS -o /dev/null http://%WIN_HOST_IP%:%SMARTNPC_HTTP_PORT%/mcp" >nul 2>&1
if errorlevel 1 (
echo      ... waiting for mcp
goto wait_mcp
)
echo [OK] mcp HTTP up at :%SMARTNPC_HTTP_PORT%/mcp.
echo.

rem ---- Step 5: Start Hermes Gateways for selected NPC profiles ----
rem Each gateway adds ~300-500MB RAM and 30-60s cold start. With 6 gateways
rem expect ~2-3GB RAM and ~3-6 min total before all 6 are healthy. To trim,
rem set SMARTNPC_ACTIVE_PROFILES in .env (comma-separated subset).
echo [5/6] Starting Hermes Gateways: %SMARTNPC_ACTIVE_PROFILES% ...
start "Hermes Gateways" wsl -d %WSL_DISTRO% bash -ic "bash %REPO_WSL%/scripts/start_hermes_profiles.sh %SMARTNPC_ACTIVE_PROFILES%"
echo      Waiting for selected gateways to become healthy (each up to ~90s)...

rem Iterate ACTIVE_PROFILES, pick port from the static profile→port table.
for %%P in (%SMARTNPC_ACTIVE_PROFILES:,= %) do call :wait_one_profile %%P
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
:_wait_one_retry
curl -sS http://%WSL_IP%:%_port%/health >nul 2>&1
if errorlevel 1 (
    timeout /t 5 /nobreak >nul
    echo      ... waiting for %_p% on :%_port%
    goto _wait_one_retry
)
echo      [OK] %_p% on :%_port%
goto :eof

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

:build_fail
echo [ERROR] Build failed.
pause
exit /b 1

:install_fail
echo [ERROR] Mod or Hermes install failed.
pause
exit /b 1
