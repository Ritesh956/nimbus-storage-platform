#!/usr/bin/env bash
# Day 4 deliverable (docs/09-roadmap.md): stop a MinIO container, watch
# /v1/admin/nodes flip to "down" within 10s (NFR-3), restart it, watch it
# recover. Requires `make dev` running and NIMBUS_STORAGE_NODES set to the
# default 3-node compose config (node-1/2/3).
set -euo pipefail
BASE="${NIMBUS_BASE_URL:-http://localhost:8080}"
COMPOSE_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../deploy" && pwd)"
TARGET_NODE="${1:-node-2}"
TARGET_CONTAINER="minio-${TARGET_NODE}"

EMAIL="storage-smoke-$(date +%s)@nimbus.dev"
PASS="correct-horse-battery-staple"
curl -sf -X POST "$BASE/v1/auth/register" -H 'Content-Type: application/json' \
  -d "{\"email\":\"$EMAIL\",\"password\":\"$PASS\"}" > /dev/null
ACCESS=$(curl -sf -X POST "$BASE/v1/auth/login" -H 'Content-Type: application/json' \
  -d "{\"email\":\"$EMAIL\",\"password\":\"$PASS\"}" | node -e \
  "let d='';process.stdin.on('data',c=>d+=c);process.stdin.on('end',()=>console.log(JSON.parse(d).access_token))")

status_of() {
  curl -sf "$BASE/v1/admin/nodes" -H "Authorization: Bearer $ACCESS" | node -e \
    "let d='';process.stdin.on('data',c=>d+=c);process.stdin.on('end',()=>{const n=JSON.parse(d).find(x=>x.id==='$TARGET_NODE');console.log(n?n.status:'missing')})"
}

wait_for_status() {
  local want="$1" label="$2" start elapsed status
  start=$(date +%s)
  for _ in $(seq 1 15); do
    status=$(status_of)
    elapsed=$(( $(date +%s) - start ))
    if [ "$status" = "$want" ]; then
      echo "OK: $label after ${elapsed}s"
      return 0
    fi
    sleep 1
  done
  echo "FAIL: $label did not happen within 15s (last status: $status)"
  return 1
}

echo "== baseline: $TARGET_NODE should be healthy =="
[ "$(status_of)" = "healthy" ] || { echo "FAIL: $TARGET_NODE not healthy at baseline"; exit 1; }
echo "OK"

echo "== stopping $TARGET_CONTAINER =="
(cd "$COMPOSE_DIR" && docker compose stop "$TARGET_CONTAINER" > /dev/null)
wait_for_status down "down-detection (must be <=10s per NFR-3)"

echo "== restarting $TARGET_CONTAINER =="
(cd "$COMPOSE_DIR" && docker compose start "$TARGET_CONTAINER" > /dev/null)
wait_for_status healthy "recovery"

echo
echo "ALL STORAGE FAILOVER CHECKS PASSED"
