@echo off
rem One-click: strip smartnpc's injected overlay block from each Hermes
rem profile's config.yaml so the next run.bat re-injects a fresh copy
rem with the current hermes/profiles/_master/config-overlay.yaml content
rem (tool include list, model.context_length, compression.*, ...).
rem
rem SOUL.md and skills/ are re-synced on every run.bat anyway. This script
rem only exists to bust the config.yaml overlay, which install.sh refuses
rem to re-append once mcp_servers: is already present.
rem
rem Usage:
rem   scripts\reset-hermes.bat                  one-shot reset, all 6 NPCs
rem   scripts\reset-hermes.bat --wipe-state     also clear conversations / state.db
rem   scripts\reset-hermes.bat xiami abigail    only listed NPCs
rem
rem After running, re-launch run.bat.

setlocal
cd /d D:\SmartNPC

echo ============================================
echo   SmartNPC - Reset Hermes Overlay
echo ============================================

wsl -d Ubuntu-22.04 bash -lc "bash /mnt/d/SmartNPC/scripts/reset_hermes_overlay.sh %*"
if errorlevel 1 (
  echo [ERROR] reset_hermes_overlay.sh failed.
  pause
  exit /b 1
)

echo.
echo Done. Now run:  run.bat
echo.