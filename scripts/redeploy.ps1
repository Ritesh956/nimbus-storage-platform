# Full cold-start for the free self-hosted public deployment (see
# docs/00-project-state.md "Known issues" - go-public plan). Run this after
# any PC reboot, Docker Desktop restart, or whenever the public site has gone
# dark. Brings up the whole Compose stack, relaunches all 5 Cloudflare Quick
# Tunnels, rewires the new (random) tunnel hostnames into the backend and
# frontend, and prints the final live URL.
#
# Usage: powershell -File scripts/redeploy.ps1
# Must be run with the repo root as the working directory.

$ErrorActionPreference = "Stop"
$cloudflared = "C:\Program Files (x86)\cloudflared\cloudflared.exe"
$logDir = $env:TEMP
$repoRoot = (Get-Location).Path
$envFile = Join-Path $repoRoot "deploy\.env"
$composeFile = Join-Path $repoRoot "deploy\docker-compose.yml"

if (-not (Test-Path $cloudflared)) {
    throw "cloudflared not found at $cloudflared - is it still installed? (winget install --id Cloudflare.cloudflared)"
}
if (-not (Test-Path $envFile)) {
    throw "deploy\.env not found - run this from the repo root."
}

Write-Host "=== 1. Checking Docker is running ==="
$dockerOk = $false
try { docker info 2>&1 | Out-Null; $dockerOk = $true } catch {}
if (-not $dockerOk) {
    Write-Host "Docker doesn't seem to be running. Starting Docker Desktop..."
    Start-Process "C:\Program Files\Docker\Docker\Docker Desktop.exe"
    Write-Host "Waiting for Docker to come up (this can take a minute)..."
    $ready = $false
    for ($i = 0; $i -lt 60; $i++) {
        Start-Sleep -Seconds 2
        try { docker info 2>&1 | Out-Null; $ready = $true; break } catch {}
    }
    if (-not $ready) { throw "Docker Desktop didn't come up in time - start it manually and rerun this script." }
}
Write-Host "Docker is up."

Write-Host ""
Write-Host "=== 2. Bringing up the full Compose stack ==="
docker compose -f $composeFile --env-file $envFile up -d
if ($LASTEXITCODE -ne 0) { throw "docker compose up failed - check the output above." }

Write-Host ""
Write-Host "=== 3. Waiting for nimbus-api and nimbus-web to answer on localhost ==="
function Wait-ForPort($port, $label) {
    for ($i = 0; $i -lt 30; $i++) {
        try {
            $resp = Invoke-WebRequest -Uri "http://127.0.0.1:$port" -UseBasicParsing -TimeoutSec 3 -ErrorAction Stop
            Write-Host "$label (port $port) is responding."
            return
        } catch {
            if ($_.Exception.Response) { Write-Host "$label (port $port) is responding."; return }
        }
        Start-Sleep -Seconds 2
    }
    Write-Host "WARNING: $label (port $port) did not respond in time - continuing anyway, check 'docker ps' if the site doesn't work."
}
Wait-ForPort 8080 "nimbus-api"
Wait-ForPort 3010 "nimbus-web"

Write-Host ""
Write-Host "=== 4. Stopping any old cloudflared tunnels, then launching 5 fresh ones ==="
Get-Process cloudflared -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue
Start-Sleep -Seconds 2

function Start-QuickTunnel($name, $port, $logFile) {
    $log = Join-Path $logDir $logFile
    if (Test-Path $log) { Remove-Item $log -Force -ErrorAction SilentlyContinue }
    Start-Process -FilePath $cloudflared -ArgumentList "tunnel --url http://127.0.0.1:$port --logfile `"$log`"" -WindowStyle Hidden
    Write-Host "Starting tunnel for $name (port $port)..."
    $url = $null
    for ($i = 0; $i -lt 20; $i++) {
        Start-Sleep -Seconds 1
        if (Test-Path $log) {
            $match = Select-String -Path $log -Pattern "https://\S+\.trycloudflare\.com" -ErrorAction SilentlyContinue | Select-Object -First 1
            if ($match) { $url = $match.Matches[0].Value; break }
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
Write-Host "=== 5. Updating deploy\.env with the new MinIO public endpoints ==="
$newStorageNodes = "NIMBUS_STORAGE_NODES=node-1=http://minio-node-1:9000|$minio1Url,node-2=http://minio-node-2:9000|$minio2Url,node-3=http://minio-node-3:9000|$minio3Url"
$envContent = Get-Content $envFile
if ($envContent -match "^NIMBUS_STORAGE_NODES=") {
    $envContent = $envContent -replace "^NIMBUS_STORAGE_NODES=.*", $newStorageNodes
} else {
    $envContent += $newStorageNodes
}
Set-Content -Path $envFile -Value $envContent
Write-Host "deploy\.env updated."

Write-Host ""
Write-Host "=== 6. Restarting nimbus-api and nimbus-worker with the new endpoints ==="
docker compose -f $composeFile --env-file $envFile up -d --no-build nimbus-api nimbus-worker
if ($LASTEXITCODE -ne 0) { throw "restarting nimbus-api/nimbus-worker failed." }

Write-Host ""
Write-Host "=== 7. Rebuilding nimbus-web with the new public API URL ==="
docker build -f (Join-Path $repoRoot "deploy\Dockerfile.web") -t nimbus-nimbus-web:latest --build-arg "NEXT_PUBLIC_API_URL=$apiUrl" $repoRoot
if ($LASTEXITCODE -ne 0) { throw "rebuilding nimbus-web failed." }
docker compose -f $composeFile --env-file $envFile up -d --no-build nimbus-web
if ($LASTEXITCODE -ne 0) { throw "restarting nimbus-web failed." }

Write-Host ""
Write-Host "=================================================="
Write-Host " Nimbus is live at: $webUrl"
Write-Host "=================================================="
