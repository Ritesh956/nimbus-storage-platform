#!/usr/bin/env bash
# Day 2 deliverable (docs/09-roadmap.md): register -> login -> create org ->
# refresh rotation, exercised against a running `make dev` stack.
set -euo pipefail
BASE="${NIMBUS_BASE_URL:-http://localhost:8080}"
EMAIL="smoke-$(date +%s)@nimbus.dev"
PASS="correct-horse-battery-staple"

echo "== register =="
curl -sf -X POST "$BASE/v1/auth/register" -H 'Content-Type: application/json' \
  -d "{\"email\":\"$EMAIL\",\"password\":\"$PASS\"}" | tee /tmp/register.json
echo

echo "== login =="
curl -sf -X POST "$BASE/v1/auth/login" -H 'Content-Type: application/json' \
  -d "{\"email\":\"$EMAIL\",\"password\":\"$PASS\"}" | tee /tmp/login.json
echo
ACCESS=$(grep -o '"access_token":"[^"]*"' /tmp/login.json | cut -d'"' -f4)
REFRESH=$(grep -o '"refresh_token":"[^"]*"' /tmp/login.json | cut -d'"' -f4)

echo "== create org (authenticated) =="
curl -sf -X POST "$BASE/v1/orgs" -H "Authorization: Bearer $ACCESS" -H 'Content-Type: application/json' \
  -d '{"name":"Smoke Test Org"}' | tee /tmp/org.json
echo
ORG_ID=$(grep -o '"id":"[^"]*"' /tmp/org.json | head -1 | cut -d'"' -f4)

echo "== list members (owner sees self) =="
curl -sf "$BASE/v1/orgs/$ORG_ID/members" -H "Authorization: Bearer $ACCESS"
echo

echo "== refresh rotation =="
curl -sf -X POST "$BASE/v1/auth/refresh" -H 'Content-Type: application/json' \
  -d "{\"refresh_token\":\"$REFRESH\"}" | tee /tmp/refresh.json
echo

echo "== reuse of rotated (old) refresh token must now fail =="
if curl -sf -X POST "$BASE/v1/auth/refresh" -H 'Content-Type: application/json' \
  -d "{\"refresh_token\":\"$REFRESH\"}" > /tmp/reuse.json 2>/dev/null; then
  echo "FAIL: reused old refresh token was accepted"
  exit 1
else
  echo "OK: old refresh token correctly rejected (reuse detection)"
fi

echo
echo "ALL SMOKE CHECKS PASSED"
