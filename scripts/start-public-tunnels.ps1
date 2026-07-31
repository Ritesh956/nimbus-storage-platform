# Relaunches the 5 Cloudflare Quick Tunnels (web, api, minio-node-1/2/3) this
# project's free self-hosted deployment relies on (see docs/00-project-state.md
# "Known issues" - go-public plan). Quick Tunnels have no fixed hostname: every
# time cloudflared restarts (including after this script reruns, or a reboot),
# each service gets a brand new random *.trycloudflare.com URL. Run this after
# any restart, then follow the printed instructions to re-point the backend
# and rebuild the frontend.
#
# Usage: powershell -File scripts/start-public-tunnels.ps1

$ErrorActionPreference = "Stop"
$cloudflared = "C:\Program Files (x86)\cloudflared\cloudflared.exe"
$logDir = "$env:TEMP"

function Start-QuickTunnel($name, $port, $logFile) {
    $log = Join-Path $logDir $logFile
    if (Test-Path $log) { Remove-Item $log -Force }
    Start-Process -FilePath $cloudflared -ArgumentList "tunnel --url http://127.0.0.1:$port --logfile `"$log`"" -WindowStyle Hidden
    Write-Host "Starting tunnel for $name (port $port)..."
    $url = $null
    for ($i = 0; $i -lt 20; $i++) {
        Start-Sleep -Seconds 1
        if (Test-Path $log) {
            $match = Select-String -Path $log -Pattern "https://\S+\.trycloudflare\.com" -ErrorAction SilentlyContinue | Select-Object -First 1
            if ($match) {
                $url = $match.Matches[0].Value
                break
            }
        }
    }
    if (-not $url) { throw "Timed out waiting for $name tunnel URL - check $log" }
    Write-Host "  $name -> $url"
    return $url
}

$webUrl    = Start-QuickTunnel "web"          3010  "cloudflared-web.log"
$apiUrl    = Start-QuickTunnel "api"          8080  "cloudflared-api.log"
$minio1Url = Start-QuickTunnel "minio-node-1" 9000  "cloudflared-minio1.log"
$minio2Url = Start-QuickTunnel "minio-node-2" 9010  "cloudflared-minio2.log"
$minio3Url = Start-QuickTunnel "minio-node-3" 9020  "cloudflared-minio3.log"

Write-Host ""
Write-Host "=== All 5 tunnels are up. Next steps: ==="
Write-Host ""
Write-Host "1. Update deploy/.env's NIMBUS_STORAGE_NODES line to:"
Write-Host "   NIMBUS_STORAGE_NODES=node-1=http://minio-node-1:9000|$minio1Url,node-2=http://minio-node-2:9000|$minio2Url,node-3=http://minio-node-3:9000|$minio3Url"
Write-Host ""
Write-Host "2. Restart the backend so it picks up the new endpoints:"
Write-Host "   docker compose -f deploy/docker-compose.yml --env-file deploy/.env up -d --no-build nimbus-api nimbus-worker"
Write-Host ""
Write-Host "3. Rebuild the frontend with the new public API URL:"
Write-Host "   docker build -f deploy/Dockerfile.web -t nimbus-nimbus-web:latest --build-arg NEXT_PUBLIC_API_URL=$apiUrl ."
Write-Host "   docker compose -f deploy/docker-compose.yml up -d --no-build nimbus-web"
Write-Host ""
Write-Host "4. The site is now live at: $webUrl"
