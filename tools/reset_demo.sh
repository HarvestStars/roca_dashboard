#!/usr/bin/env sh
# Reset the demo to a clean state (see tools/reset_demo.ps1 for details).
#   sh tools/reset_demo.sh            # full reset (both demos)
#   sh tools/reset_demo.sh --erp-only # only trips/receipts/e-mails
set -e
cd "$(dirname "$0")/.."

# stop the server first: it would write its in-memory state back
if command -v taskkill >/dev/null 2>&1; then
  taskkill //F //IM rocadash.exe >/dev/null 2>&1 && echo "stopped rocadash" || true
else
  pkill -x rocadash 2>/dev/null && echo "stopped rocadash" || true
fi
# close the demo tunnel left behind by a killed server (only that ssh, matched by its port mapping)
if command -v powershell >/dev/null 2>&1; then
  powershell -NoProfile -Command "Get-CimInstance Win32_Process -Filter \"Name='ssh.exe'\" | Where-Object { \$_.CommandLine -like '*127.0.0.1:18080:localhost:*' } | ForEach-Object { Stop-Process -Id \$_.ProcessId -Force; 'stopped demo tunnel' }"
else
  pkill -f '127.0.0.1:18080:localhost:' 2>/dev/null && echo "stopped demo tunnel" || true
fi
sleep 1

if [ "$1" = "--erp-only" ]; then
  rm -rf logs/erp && echo "removed logs/erp"
else
  rm -rf logs && echo "removed logs"
  git checkout -- data/devices && echo "restored data/devices from git"
fi
rm -rf uploads && echo "removed uploads"
echo "done. start again with: go run ./cmd/rocadash"
