@echo off
setlocal enabledelayedexpansion
chcp 65001 >nul
title SmartNPC Setup

rem ---- Locate repo root ----
set "SMARTNPC_REPO=%~dp0"
if "%SMARTNPC_REPO:~-1%"=="\" set "SMARTNPC_REPO=%SMARTNPC_REPO:~0,-1%"
cd /d "%SMARTNPC_REPO%"

echo ============================================
echo   SmartNPC - First-Time Setup
echo ============================================
echo.

rem ---- Load .env ----
if not exist .env (
    echo [ERROR] .env not found. Copy .env.example to .env and fill required values.
    pause
    exit /b 1
)
for /f "tokens=1,* delims==" %%A in ('findstr /b /v /c:"#" .env') do (
    if not "%%A"=="" set "%%A=%%B"
)
echo [1/7] Loaded .env

rem ---- Defaults ----
if not defined TASK_EXE           set "TASK_EXE=%USERPROFILE%\go\bin\task.exe"
if not defined WSL_DISTRO         set "WSL_DISTRO=Ubuntu-22.04"
if not defined SMARTNPC_HTTP_PORT set "SMARTNPC_HTTP_PORT=3000"
if not defined SMARTNPC_HERMES_KEY set "SMARTNPC_HERMES_KEY=smartnpc-test-key"

rem ---- [2/7] Prerequisites ----
echo [2/7] Checking prerequisites...

if not defined SMARTNPC_GAME_PATH (
    echo [ERROR] SMARTNPC_GAME_PATH is not set in .env
    pause
    exit /b 1
)
if not exist "%SMARTNPC_GAME_PATH%\StardewModdingAPI.exe" (
    echo [ERROR] StardewModdingAPI.exe not found in %SMARTNPC_GAME_PATH%
    echo         Make sure SMARTNPC_GAME_PATH points to your Stardew Valley installation.
    pause
    exit /b 1
)

if not defined HERMES_AGENT_URL (
    echo [ERROR] HERMES_AGENT_URL is not set in .env
    pause
    exit /b 1
)
if not defined HERMES_AGENT_API_KEY (
    echo [ERROR] HERMES_AGENT_API_KEY is not set in .env
    pause
    exit /b 1
)

wsl --status >nul 2>&1
if errorlevel 1 (
    echo [ERROR] WSL is not available. Install WSL2 first.
    pause
    exit /b 1
)

wsl -d %WSL_DISTRO% docker info >nul 2>&1
if errorlevel 1 (
    echo [ERROR] Docker is not available in WSL distro '%WSL_DISTRO%'.
    echo         Install Docker Engine in WSL or set WSL_DISTRO in .env.
    pause
    exit /b 1
)
echo [OK] Prerequisites passed.
echo.

rem ---- [3/7] Render profiles ----
echo [3/7] Rendering Hermes profiles from _master/...
powershell -NoProfile -ExecutionPolicy Bypass -File "%SMARTNPC_REPO%\scripts\render_profiles.ps1" -RepoRoot "%SMARTNPC_REPO%"
if errorlevel 1 (
    echo [ERROR] Profile rendering failed.
    pause
    exit /b 1
)
echo [OK] Profiles rendered.
echo.

rem ---- [4/7] Detect WSL IPs ----
echo [4/7] Detecting WSL/Windows IPs...
for /f "usebackq tokens=*" %%P in (`wsl -d %WSL_DISTRO% wslpath -a "%SMARTNPC_REPO%"`) do set "REPO_WSL=%%P"
if not defined REPO_WSL (
    echo [ERROR] Failed to convert repo path to WSL path.
    pause
    exit /b 1
)

if not defined WIN_HOST_IP (
    for /f "usebackq delims=" %%L in (`wsl -d %WSL_DISTRO% bash %REPO_WSL%/scripts/detect_wsl_ips.sh`) do set "%%L"
)
if not defined WIN_HOST_IP set "WIN_HOST_IP=127.0.0.1"
if not defined WSL_IP set "WSL_IP=127.0.0.1"
echo [OK] WIN_HOST_IP=%WIN_HOST_IP%, WSL_IP=%WSL_IP%
echo.

rem ---- [5/7] Generate docker-compose.yml ----
echo [5/7] Generating deploy/hermes/docker-compose.yml from npcs.yaml...
powershell -NoProfile -ExecutionPolicy Bypass -File "%SMARTNPC_REPO%\scripts\generate_compose.ps1" -RepoRoot "%SMARTNPC_REPO%"
if errorlevel 1 (
    echo [ERROR] docker-compose.yml generation failed.
    pause
    exit /b 1
)
echo.

rem ---- [6/7] Generate deploy/hermes/.env ----
echo [6/7] Generating deploy/hermes/.env...
if not defined HERMES_AGENT_MODEL set "HERMES_AGENT_MODEL=deepseek-v4-pro"

>"%SMARTNPC_REPO%\deploy\hermes\.env" echo SMARTNPC_MCP_URL=http://%WIN_HOST_IP%:%SMARTNPC_HTTP_PORT%/mcp
>>"%SMARTNPC_REPO%\deploy\hermes\.env" echo SMARTNPC_HERMES_KEY=%SMARTNPC_HERMES_KEY%
>>"%SMARTNPC_REPO%\deploy\hermes\.env" echo HERMES_AGENT_URL=%HERMES_AGENT_URL%
>>"%SMARTNPC_REPO%\deploy\hermes\.env" echo HERMES_AGENT_API_KEY=%HERMES_AGENT_API_KEY%
>>"%SMARTNPC_REPO%\deploy\hermes\.env" echo HERMES_AGENT_MODEL=%HERMES_AGENT_MODEL%
if defined HERMES_LANGFUSE_PUBLIC_KEY >>"%SMARTNPC_REPO%\deploy\hermes\.env" echo HERMES_LANGFUSE_PUBLIC_KEY=%HERMES_LANGFUSE_PUBLIC_KEY%
if defined HERMES_LANGFUSE_SECRET_KEY >>"%SMARTNPC_REPO%\deploy\hermes\.env" echo HERMES_LANGFUSE_SECRET_KEY=%HERMES_LANGFUSE_SECRET_KEY%
if defined HERMES_LANGFUSE_BASE_URL >>"%SMARTNPC_REPO%\deploy\hermes\.env" echo HERMES_LANGFUSE_BASE_URL=%HERMES_LANGFUSE_BASE_URL%
echo [OK] deploy/hermes/.env (MCP_URL=http://%WIN_HOST_IP%:%SMARTNPC_HTTP_PORT%/mcp)
echo.

rem ---- [7/7] Sync profiles + Docker build ----
echo [7/7] Syncing profiles and building Docker images (this may take a few minutes)...
wsl -d %WSL_DISTRO% bash -lc "cd %REPO_WSL%/deploy/hermes && bash scripts/sync-profiles.sh && docker compose build"
if errorlevel 1 (
    echo [ERROR] Docker build failed.
    echo         Check WSL Docker logs: wsl -d %WSL_DISTRO% bash -c "cd %REPO_WSL%/deploy/hermes && docker compose logs"
    pause
    exit /b 1
)
echo.

echo ============================================
echo   Setup complete!
echo ============================================
echo.
echo   Next step: run.bat
echo.
echo   What was done:
echo     - Rendered NPC profiles from _master/ templates
echo     - Generated Docker Compose config from npcs.yaml
echo     - Built Hermes Docker images in WSL
echo.
echo   When to re-run setup.bat:
echo     - After editing hermes/ files (SOUL.md, skills, npcs.yaml)
echo     - After changing LLM credentials in .env
echo.
pause
