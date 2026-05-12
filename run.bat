@echo off
setlocal
title SmartNPC Launcher
cd /d D:\SmartNPC

echo ============================================
echo   SmartNPC - One-Click Launcher
echo ============================================
echo.

rem ---- Step 1: Build ----
echo [1/5] Building mod + agent + mcp ...
call C:\Users\synchen\go\bin\task.exe mod:build
if errorlevel 1 goto build_fail
call C:\Users\synchen\go\bin\task.exe agent:build
if errorlevel 1 goto build_fail
call C:\Users\synchen\go\bin\task.exe mcp:build
if errorlevel 1 goto build_fail
echo [OK] Build complete.
echo.

rem ---- Step 2: Kill existing game process ----
echo [2/5] Killing existing game process (if any)...
powershell -NoProfile -Command "Get-Process -Name 'Stardew Valley','StardewModdingAPI' -ErrorAction SilentlyContinue | Stop-Process -Force"
powershell -NoProfile -Command "Get-Process -Name 'smartnpc-agent','smartnpc-mcp' -ErrorAction SilentlyContinue | Stop-Process -Force"
timeout /t 1 /nobreak >nul
echo [OK] Old processes cleared.
echo.

rem ---- Step 3: Install mod ----
echo [3/5] Installing mod to game directory...
call C:\Users\synchen\go\bin\task.exe mod:install
if errorlevel 1 goto install_fail
echo [OK] Mod installed.
echo.

rem ---- Step 4: Start Hermes gateway in WSL ----
echo [4/5] Starting Hermes gateway in WSL...
start "Hermes Gateway" wsl -d Ubuntu-22.04 bash -ic "hermes gateway run --accept-hooks"
echo [OK] Hermes gateway starting in background window.
echo      Waiting for Hermes to become healthy...

:wait_hermes
timeout /t 5 /nobreak >nul
curl -sS http://192.168.59.118:8642/health >nul 2>&1
if errorlevel 1 (
echo      ... still waiting for Hermes gateway
goto wait_hermes
)
echo [OK] Hermes gateway is healthy.
echo.

rem ---- Step 5: Start game via SMAPI ----
echo [5/5] Launching Stardew Valley via SMAPI...
start "" "D:\Stardew Valley\StardewModdingAPI.exe"
echo [OK] Game launching. Load a save file.
echo.
echo Waiting 15s for game to initialize...
timeout /t 15 /nobreak >nul

rem ---- Step 6: Start Agent ----
echo.
echo Starting SmartNPC multi-NPC Agent (Ctrl+C to stop)...
echo.
cd /d D:\SmartNPC\smartnpc-agent
rem Dual-LLM mode:
rem   --decision-url : tool-reliable decision model (GPT-5.5 via internal proxy)
rem   --persona-url  : local Hermes gateway (role-play layer, same key)
rem   --personas-dir : load every *.json under personas/ and route events by
rem                    the event's `npc` field (multi-NPC mode).
rem Override DECISION_URL / DECISION_MODEL env to swap endpoints without editing this script.
if not defined DECISION_URL   set DECISION_URL=http://v2.open.venus.oa.com/llmproxy
if not defined DECISION_MODEL set DECISION_MODEL=gpt-5.5
bin\smartnpc-agent.exe --mcp-bin ..\smartnpc-mcp\bin\smartnpc-mcp.exe --mcp-args "--ws-url ws://127.0.0.1:18745/ws" --log-level debug run --personas-dir personas --persona-url http://192.168.59.118:8642/v1 --api-key smartnpc-test-key --decision-url %DECISION_URL% --decision-model %DECISION_MODEL%
goto :eof

:build_fail
echo [ERROR] Build failed.
pause
exit /b 1

:install_fail
echo [ERROR] mod:install failed.
pause
exit /b 1
