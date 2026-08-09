#!/usr/bin/env bash
# Backs up Postgres (pg_dump) and all three MinIO nodes' buckets (mc mirror)
# to ./backups/<timestamp>/ — the "no documented backup/restore story"
# gap the audit named for §16. Works against either environment: pass
# `compose` (default) or `kind` as $1 to pick how Postgres is reached; MinIO
# is identical either way since both environments expose the same S3 API
# ports on localhost by design (deploy/k8s/kind-config.yaml's comment).
#
# Usage: scripts/backup.sh [compose|kind]
set -euo pipefail
ENV="${1:-compose}"
DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TS="$(date +%Y%m%d-%H%M%S)"
OUT="$DIR/backups/$TS"
mkdir -p "$OUT"

echo "== Postgres ($ENV) =="
case "$ENV" in
  compose)
    docker compose -f "$DIR/deploy/docker-compose.yml" exec -T postgres \
      pg_dump -U nimbus nimbus | gzip > "$OUT/postgres.sql.gz"
    ;;
  kind)
    kubectl -n nimbus exec statefulset/postgres -- \
      pg_dump -U nimbus nimbus | gzip > "$OUT/postgres.sql.gz"
    ;;
  *)
    echo "unknown environment '$ENV', expected compose or kind" >&2
    exit 1
    ;;
esac
echo "  -> $OUT/postgres.sql.gz ($(du -h "$OUT/postgres.sql.gz" | cut -f1))"

# MinIO: mc mirror each node's buckets into a throwaway minio/mc container
# on host networking (same pattern scripts/load-upload.js already uses for
# reaching localhost-published ports from inside a container), then `docker
# cp` the result out. Deliberately *not* a bind-mount (`-v`) — this repo's
# path contains spaces ("Ritesh Gupta", "FINAL PROJECTS"), and Docker
# Desktop on Windows silently fails to translate a `-v` source path with
# spaces (confirmed: the container sees a "nonexistent directory", no
# error surfaced) — the same class of footgun CLAUDE.md already documents
# for Vitest's pool spawning. `docker cp` isn't a bind mount and isn't
# affected, so it's the one that actually works here.
echo "== MinIO (3 nodes) =="
CONTAINER="nimbus-backup-mc-$TS"
docker run -d --name "$CONTAINER" --network host --entrypoint sh minio/mc:latest -c 'sleep 300' >/dev/null
docker exec "$CONTAINER" sh -c '
    set -e
    for i in 1 2 3; do
      case $i in
        1) port=9000 ;;
        2) port=9010 ;;
        3) port=9020 ;;
      esac
      mc alias set "n$i" "http://localhost:$port" nimbus nimbus-secret >/dev/null
      mkdir -p "/backup/minio$i"
      mc mirror --quiet "n$i" "/backup/minio$i"
    done
  '
docker cp "$CONTAINER:/backup" "$OUT/minio-tmp" >/dev/null
mv "$OUT/minio-tmp"/* "$OUT/" 2>/dev/null || true
rmdir "$OUT/minio-tmp" 2>/dev/null || true
docker rm -f "$CONTAINER" >/dev/null
echo "  -> $OUT/minio{1,2,3}/"
echo
echo "Backup complete: $OUT"
