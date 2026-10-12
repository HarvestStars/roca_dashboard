# Expose the local demo (localhost:8080) at https://rocateq-demo.2-28-105-58.sslip.io
# through an SSH reverse tunnel to the German server, where Caddy terminates HTTPS.
#
#   powershell -ExecutionPolicy Bypass -File tools/tunnel.ps1
#
# Keep this window open during the demo (Ctrl+C to stop). It reconnects by itself
# if the network drops. The remote port is bound to the server's 127.0.0.1 only,
# so the demo is reachable exclusively through Caddy (HTTPS + password).
# Login: user and password are DEMO_BASIC_AUTH_USER / DEMO_BASIC_AUTH_PASSWORD in .env.
param(
    [string]$Server = "2.28.105.58",
    [int]$RemotePort = 18080,
    [int]$LocalPort = 8080
)

$url = "https://rocateq-demo.2-28-105-58.sslip.io"
Write-Host "Tunnel: $url  ->  localhost:$LocalPort   (Ctrl+C to stop)"
try {
    Invoke-WebRequest -UseBasicParsing -TimeoutSec 2 "http://localhost:$LocalPort/healthz" | Out-Null
} catch {
    Write-Warning "Nothing answers on localhost:$LocalPort yet - start the server: go run ./cmd/rocadash"
}

while ($true) {
    ssh -N `
        -o ServerAliveInterval=20 -o ServerAliveCountMax=3 `
        -o ExitOnForwardFailure=yes -o BatchMode=yes `
        -R "127.0.0.1:${RemotePort}:localhost:${LocalPort}" $Server
    Write-Host "$(Get-Date -Format HH:mm:ss) tunnel closed (exit $LASTEXITCODE), reconnecting in 3 s..."
    Start-Sleep -Seconds 3
}
