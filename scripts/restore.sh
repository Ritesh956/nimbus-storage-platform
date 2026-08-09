#!/usr/bin/env bash
# Restores a backup made by scripts/backup.sh. Destructive: overwrites the
# target Postgres database and re-mirrors the MinIO buckets on top of
# whatever's there now. Requires an explicit CONFIRM=yes to actually run,
# same guard style as this project's other irreversible-by-design scripts.
#
# Usage: CONFIRM=yes scripts/restore.sh <backup-dir> [compose|kind]
# Example: CONFIRM=yes scripts/restore.sh backups/20260809-014200 kind
set -euo pipefail
BACKUP_DIR="${1:?usage: scripts/restore.sh <backup-dir> [compose|kind]}"
ENV="${2:-compose}"
DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

if [ "${CONFIRM:-}" != "yes" ]; then
  echo "This overwrites the live Postgres database and MinIO buckets from $BACKUP_DIR." >&2
  echo "Re-run with CONFIRM=yes to proceed." >&2
  exit 1
fi
if [ ! -f "$BACKUP_DIR/postgres.sql.gz" ]; then
  echo "no postgres.sql.gz found under $BACKUP_DIR" >&2
  exit 1
fi

echo "== Postgres ($ENV) =="
case "$ENV" in
  compose)
    gunzip -c "$BACKUP_DIR/postgres.sql.gz" | \
      docker compose -f "$DIR/deploy/docker-compose.yml" exec -T postgres \
      psql -U nimbus -d nimbus
    ;;
  kind)
    gunzip -c "$BACKUP_DIR/postgres.sql.gz" | \
      kubectl -n nimbus exec -i statefulset/postgres -- \
      psql -U nimbus -d nimbus
    ;;
  *)
    echo "unknown environment '$ENV', expected compose or kind" >&2
    exit 1
    ;;
esac

# Same docker-cp-not-bind-mount approach as scripts/backup.sh — a `-v`
# bind mount silently fails to translate a host path with spaces
# ("Ritesh Gupta", "FINAL PROJECTS") under Docker Desktop on Windows.
echo "== MinIO (3 nodes) =="
CONTAINER="nimbus-restore-mc-$(date +%s)"
docker run -d --name "$CONTAINER" --network host --entrypoint sh minio/mc:latest -c 'sleep 300' >/dev/null
docker exec "$CONTAINER" mkdir -p /backup
for i in 1 2 3; do
  [ -d "$BACKUP_DIR/minio$i" ] && docker cp "$BACKUP_DIR/minio$i" "$CONTAINER:/backup/minio$i"
done
docker exec "$CONTAINER" sh -c '
    set -e
    for i in 1 2 3; do
      case $i in
        1) port=9000 ;;
        2) port=9010 ;;
        3) port=9020 ;;
      esac
      [ -d "/backup/minio$i" ] || continue
      mc alias set "n$i" "http://localhost:$port" nimbus nimbus-secret >/dev/null
      mc mirror --quiet "/backup/minio$i" "n$i"
    done
  '
docker rm -f "$CONTAINER" >/dev/null

echo
echo "Restore complete from $BACKUP_DIR"
