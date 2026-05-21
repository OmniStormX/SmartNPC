#!/usr/bin/env bash
# Print KEY=VALUE lines for `for /f` consumption from a Windows .bat.
# Output (LF; cmd's `for /f` accepts both LF and CRLF):
#
#   WIN_HOST_IP=192.168.48.1
#   WSL_IP=192.168.59.118
#
# Used by run.bat to auto-detect cross-boundary IPs without embedding
# awk/grep inside cmd.exe's quoting hell.

set -e

WIN_HOST_IP="$(ip route 2>/dev/null | awk '/^default/ {print $3; exit}')"
WSL_IP="$(hostname -I 2>/dev/null | awk '{print $1}')"

printf 'WIN_HOST_IP=%s\nWSL_IP=%s\n' "${WIN_HOST_IP:-}" "${WSL_IP:-}"
