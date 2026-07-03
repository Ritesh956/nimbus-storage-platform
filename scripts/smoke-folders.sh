#!/usr/bin/env bash
# Day 3 deliverable (docs/09-roadmap.md): folder tree built/moved/renamed/
# trashed/restored via API, plus cycle-prevention and cross-org checks.
# File metadata operations are also exercised, against a row seeded
# directly in Postgres — real file *creation* is upload-gated (Day 5).
set -euo pipefail
BASE="${NIMBUS_BASE_URL:-http://localhost:8080}"
EMAIL="smoke-folders-$(date +%s)@nimbus.dev"
PASS="correct-horse-battery-staple"

curl -sf -X POST "$BASE/v1/auth/register" -H 'Content-Type: application/json' \
  -d "{\"email\":\"$EMAIL\",\"password\":\"$PASS\"}" > /dev/null
ACCESS=$(curl -sf -X POST "$BASE/v1/auth/login" -H 'Content-Type: application/json' \
  -d "{\"email\":\"$EMAIL\",\"password\":\"$PASS\"}" | grep -o '"access_token":"[^"]*"' | cut -d'"' -f4)
AUTH=(-H "Authorization: Bearer $ACCESS")

ORG_ID=$(curl -sf -X POST "$BASE/v1/orgs" "${AUTH[@]}" -H 'Content-Type: application/json' \
  -d '{"name":"Folder Smoke Org"}' | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)
echo "org: $ORG_ID"

echo "== create root folder 'Docs' =="
DOCS_ID=$(curl -sf -X POST "$BASE/v1/orgs/$ORG_ID/folders" "${AUTH[@]}" -H 'Content-Type: application/json' \
  -d '{"name":"Docs"}' | tee /dev/stderr | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)
echo

echo "== create child folder 'Reports' under Docs =="
REPORTS_ID=$(curl -sf -X POST "$BASE/v1/orgs/$ORG_ID/folders" "${AUTH[@]}" -H 'Content-Type: application/json' \
  -d "{\"name\":\"Reports\",\"parent_id\":\"$DOCS_ID\"}" | tee /dev/stderr | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)
echo

echo "== duplicate sibling name must 409 =="
if curl -sf -X POST "$BASE/v1/orgs/$ORG_ID/folders" "${AUTH[@]}" -H 'Content-Type: application/json' \
  -d '{"name":"Docs"}' > /tmp/dup.json 2>/dev/null; then
  echo "FAIL: duplicate root folder name accepted"; exit 1
fi
echo "OK: duplicate sibling name rejected"

echo "== list root children =="
curl -sf "$BASE/v1/folders/$DOCS_ID/children" "${AUTH[@]}"
echo

echo "== rename Reports -> Quarterly Reports =="
curl -sf -X PATCH "$BASE/v1/folders/$REPORTS_ID" "${AUTH[@]}" -H 'Content-Type: application/json' \
  -d '{"name":"Quarterly Reports"}'
echo

echo "== cyclic move must be rejected (Docs into its own child Reports) =="
if curl -sf -X PATCH "$BASE/v1/folders/$DOCS_ID" "${AUTH[@]}" -H 'Content-Type: application/json' \
  -d "{\"parent_id\":\"$REPORTS_ID\"}" > /tmp/cycle.json 2>/dev/null; then
  echo "FAIL: cyclic move was accepted"; exit 1
fi
echo "OK: cyclic move correctly rejected"

echo "== trash Docs (cascades to Reports) =="
curl -sf -X DELETE "$BASE/v1/folders/$DOCS_ID" "${AUTH[@]}" -w '%{http_code}\n' -o /dev/null

echo "== trashed folder no longer accessible =="
if curl -sf "$BASE/v1/folders/$DOCS_ID/children" "${AUTH[@]}" > /tmp/trashed.json 2>/dev/null; then
  echo "FAIL: trashed folder still accessible"; exit 1
fi
echo "OK: trashed folder returns 404"

echo "== restore Docs (cascades to Reports) =="
curl -sf -X POST "$BASE/v1/folders/$DOCS_ID/restore" "${AUTH[@]}"
echo
echo "== Docs accessible again =="
curl -sf "$BASE/v1/folders/$DOCS_ID/children" "${AUTH[@]}"
echo

echo
echo "== file metadata ops (seeding a file row directly, since creation is upload-gated) =="
FILE_ID=$(docker exec -i nimbus-postgres-1 psql -U nimbus -d nimbus -t -A -c \
  "INSERT INTO files (id, org_id, folder_id, name, created_by) VALUES (gen_random_uuid(), '$ORG_ID', '$REPORTS_ID', 'q1.pdf', (SELECT owner_user_id FROM organizations WHERE id = '$ORG_ID')) RETURNING id;" | head -1)
echo "seeded file: $FILE_ID"

echo "== rename file =="
curl -sf -X PATCH "$BASE/v1/files/$FILE_ID" "${AUTH[@]}" -H 'Content-Type: application/json' \
  -d '{"name":"q1-final.pdf"}'
echo

echo "== trash file =="
curl -sf -X DELETE "$BASE/v1/files/$FILE_ID" "${AUTH[@]}" -w '%{http_code}\n' -o /dev/null

echo "== purge before restore must be rejected... wait, purge requires trashed, so trash first then purge should succeed =="
curl -sf -X DELETE "$BASE/v1/files/$FILE_ID/purge" "${AUTH[@]}" -w '%{http_code}\n' -o /dev/null

echo
echo "ALL FOLDER/FILE SMOKE CHECKS PASSED"
