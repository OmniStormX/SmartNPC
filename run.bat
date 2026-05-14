@echo off
setlocal
title SmartNPC Launcher (Hermes-first)
cd /d D:\SmartNPC

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

rem Per-run log file: D:\SmartNPC\logs\mcp_YYYYMMDD_HHMMSS.log.
rem Use PowerShell for the timestamp — wmic is removed on recent Win11 builds
rem and silently produces a garbage filename when called via for /f.
if not exist D:\SmartNPC\logs mkdir D:\SmartNPC\logs
for /f %%I in ('powershell -NoProfile -Command "Get-Date -Format yyyyMMdd_HHmmss"') do set MCP_TS=%%I
set MCP_LOG=D:\SmartNPC\logs\mcp_%MCP_TS%.log
rem Per-run payload trace (full request/response body, JSON lines). Routed
rem there only when SMARTNPC_RELAY_DEBUG_PAYLOAD=1 is set in .env.
set SMARTNPC_RELAY_PAYLOAD_LOG=D:\SmartNPC\logs\payload_%MCP_TS%.log

echo ============================================
echo   SmartNPC - Hermes-first One-Click Launcher
echo ============================================
echo.

rem ---- Step 1: Build ----
echo [1/6] Building mod + mcp ...
call C:\Users\synchen\go\bin\task.exe mod:build
if errorlevel 1 goto build_fail
call C:\Users\synchen\go\bin\task.exe mcp:build
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
call C:\Users\synchen\go\bin\task.exe mod:install
if errorlevel 1 goto install_fail
wsl -d Ubuntu-22.04 bash -lc "bash /mnt/d/SmartNPC/hermes/install.sh"
if errorlevel 1 goto install_fail
wsl -d Ubuntu-22.04 bash -lc "bash /mnt/d/SmartNPC/scripts/apply_hermes_tuning.sh"
if errorlevel 1 goto install_fail
wsl -d Ubuntu-22.04 bash -lc "bash /mnt/d/SmartNPC/scripts/ensure_hermes_aux.sh"
echo [OK] Mod installed, Hermes profiles synced, tuning applied, session_search routed to gpt-4o-mini.
echo.

rem ---- Step 4: Start mcp in --http mode with multi-profile fan-out ----
rem mcp MUST start BEFORE Hermes gateways: at gateway startup Hermes
rem registers MCP tools by querying http://<HOST>:3000/mcp. If that
rem endpoint is down, Hermes caches "0 tools" and won't recover even
rem after mcp comes up. Result: NPC receives the chat event but has
rem no chat_say tool to reply with.
echo [4/6] Starting smartnpc-mcp (--http :3000, --hermes-config multi-profile)...
echo      log -^> %MCP_LOG%
rem SMARTNPC_HERMES_KEY must match API_SERVER_KEY in each profile's config-overlay.yaml.
rem Default 'smartnpc-test-key' matches the shipped overlays.
if not defined SMARTNPC_HERMES_KEY set SMARTNPC_HERMES_KEY=smartnpc-test-key
rem mcp logs to stderr (slog JSON). PowerShell -NoExit keeps the spawned window
rem open after exit; Tee-Object writes to BOTH the window and the log file.
rem `2>&1` merges stderr into stdout so PowerShell's pipeline can tee it.
start "smartnpc-mcp" powershell -NoProfile -NoExit -Command ^
    "& 'D:\SmartNPC\smartnpc-mcp\bin\smartnpc-mcp.exe' --http ':3000' --ws-url 'ws://127.0.0.1:18745/ws' --hermes-config 'D:\SmartNPC\hermes\runtime-config.yaml' --hermes-api-key '%SMARTNPC_HERMES_KEY%' --log-level debug 2>&1 | Tee-Object -FilePath '%MCP_LOG%'"
echo      Waiting for mcp HTTP endpoint to become reachable...
:wait_mcp
timeout /t 2 /nobreak >nul
wsl -d Ubuntu-22.04 bash -c "curl -sS -o /dev/null http://192.168.48.1:3000/mcp" >nul 2>&1
if errorlevel 1 (
echo      ... waiting for mcp
goto wait_mcp
)
echo [OK] mcp HTTP up at :3000/mcp.
echo.

rem ---- Step 5: Start Hermes Gateways for the active profiles ----
rem Default active set: xiami + abigail. To add others (haley/harvey/penny/sebastian),
rem append to ACTIVE_PROFILES below. Each additional gateway adds ~300-500MB RAM
rem and 30-60s startup; balance accordingly.
echo [5/6] Starting Hermes Gateways (xiami + abigail)...
set ACTIVE_PROFILES=xiami,abigail
start "Hermes Gateways" wsl -d Ubuntu-22.04 bash -ic "bash /mnt/d/SmartNPC/scripts/start_hermes_profiles.sh %ACTIVE_PROFILES%"
echo      Waiting for both gateways to become healthy (up to 90s each)...

:wait_xiami
timeout /t 5 /nobreak >nul
curl -sS http://192.168.59.118:8642/health >nul 2>&1
if errorlevel 1 (
echo      ... waiting for xiami
goto wait_xiami
)
:wait_abigail
curl -sS http://192.168.59.118:8643/health >nul 2>&1
if errorlevel 1 (
timeout /t 5 /nobreak >nul
echo      ... waiting for abigail
goto wait_abigail
)
echo [OK] Both gateways healthy.
echo.

rem ---- Step 6: Launch the game ----
echo [6/6] Launching Stardew Valley via SMAPI...
start "" "D:\Stardew Valley\StardewModdingAPI.exe"
echo [OK] Game launching. Load a save file.
echo.
echo ===========================
echo   Active NPCs: %ACTIVE_PROFILES%
echo   To enable haley/harvey/penny/sebastian, edit ACTIVE_PROFILES above.
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
