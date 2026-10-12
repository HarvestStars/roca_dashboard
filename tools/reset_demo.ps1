# Reset the demo to a clean state.
#
#   powershell -ExecutionPolicy Bypass -File tools\reset_demo.ps1            # full reset (both demos)
#   powershell -ExecutionPolicy Bypass -File tools\reset_demo.ps1 -ErpOnly   # only trips/receipts/e-mails
#
# Full reset : stops rocadash, deletes logs\ and uploads\, restores data\devices\ from git.
# -ErpOnly   : stops rocadash, deletes logs\erp\ and uploads\ only. Diagnostics tickets survive,
#              so the REWE ticket shows up again in the back-office dispatch queue.
#
# The server must be stopped first: it keeps state in memory and would write it back.
param([switch]$ErpOnly)

$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
Set-Location $root

$procs = Get-Process -Name rocadash -ErrorAction SilentlyContinue
if ($procs) {
    $procs | Stop-Process -Force
    Start-Sleep -Milliseconds 500
    Write-Host "stopped rocadash ($($procs.Count) process)"
}
# A force-killed server leaves its `-tunnel` ssh child behind; close only that one
# (matched by the tunnel's port mapping, never other ssh sessions).
Get-CimInstance Win32_Process -Filter "Name='ssh.exe'" |
    Where-Object { $_.CommandLine -like '*127.0.0.1:18080:localhost:*' } |
    ForEach-Object { Stop-Process -Id $_.ProcessId -Force; Write-Host "stopped demo tunnel (ssh pid $($_.ProcessId))" }

if ($ErpOnly) {
    if (Test-Path logs\erp) { Remove-Item -Recurse -Force logs\erp; Write-Host "removed logs\erp" }
} else {
    if (Test-Path logs) { Remove-Item -Recurse -Force logs; Write-Host "removed logs" }
    git checkout -- data/devices
    Write-Host "restored data\devices from git"
}
if (Test-Path uploads) { Remove-Item -Recurse -Force uploads; Write-Host "removed uploads" }

Write-Host "done. start again with: go run ./cmd/rocadash"
