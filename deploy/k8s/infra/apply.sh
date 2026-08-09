#!/usr/bin/env bash
# Applies the stateful-infra manifests (Day 13, docs/03-hld.md §5). Not
# part of the Helm chart we own (deploy/k8s/helm/nimbus/) — this is the
# "minimal manifests for infra we don't own" half of that split.
#
# The Grafana/Prometheus ConfigMaps are generated from the *same* files
# Compose already mounts (deploy/observability/...) rather than copied, so
# there's one source of truth for scrape config and dashboards regardless
# of which environment reads them.
set -euo pipefail
# Two path forms are needed on Git Bash + Windows: bash's own `cd` needs
# the POSIX form (/c/Users/...), but kubectl.exe is a native Windows
# binary and needs a Windows form (C:/Users/...) — and specifically one
# with any ".." already resolved, which kubectl.exe handles unreliably.
DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
OBS_POSIX="$(cd "$DIR/../../observability" && pwd)"
to_win() { if (cd "$1" && pwd -W) >/dev/null 2>&1; then (cd "$1" && pwd -W); else echo "$1"; fi; }
DIR_WIN="$(to_win "$DIR")"
OBS="$(to_win "$OBS_POSIX")"
NS=nimbus

kubectl apply -f "$DIR_WIN/namespace.yaml"

# metrics-server: cluster-wide (kube-system), applied before the namespaced
# infra below since nothing here depends on it — but the HPA (part of the
# Helm chart, installed after this script) does, to report real numbers
# instead of <unknown>.
kubectl apply -f "$DIR_WIN/metrics-server.yaml"

kubectl -n "$NS" create configmap prometheus-config \
  --from-file=prometheus.yml="$OBS/prometheus.yml" \
  --dry-run=client -o yaml | kubectl apply -f -

kubectl -n "$NS" create configmap grafana-datasources \
  --from-file="$OBS/grafana/provisioning/datasources" \
  --dry-run=client -o yaml | kubectl apply -f -

kubectl -n "$NS" create configmap grafana-dashboard-provider \
  --from-file="$OBS/grafana/provisioning/dashboards/dashboards.yml" \
  --dry-run=client -o yaml | kubectl apply -f -

kubectl -n "$NS" create configmap grafana-dashboards \
  --from-file="$OBS/grafana/dashboards" \
  --dry-run=client -o yaml | kubectl apply -f -

kubectl -n "$NS" create configmap tempo-config \
  --from-file=tempo.yaml="$OBS/tempo.yaml" \
  --dry-run=client -o yaml | kubectl apply -f -

kubectl apply -n "$NS" -f "$DIR_WIN/postgres.yaml" -f "$DIR_WIN/redis.yaml" -f "$DIR_WIN/nats.yaml" -f "$DIR_WIN/minio.yaml" -f "$DIR_WIN/prometheus.yaml" -f "$DIR_WIN/grafana.yaml" -f "$DIR_WIN/tempo.yaml"

echo "Waiting for stateful infra to be ready..."
kubectl -n "$NS" rollout status statefulset/postgres --timeout=120s
kubectl -n "$NS" rollout status statefulset/minio-node-1 --timeout=120s
kubectl -n "$NS" rollout status statefulset/minio-node-2 --timeout=120s
kubectl -n "$NS" rollout status statefulset/minio-node-3 --timeout=120s
kubectl -n "$NS" rollout status deployment/redis --timeout=60s
kubectl -n "$NS" rollout status deployment/nats --timeout=60s
kubectl -n "$NS" rollout status deployment/prometheus --timeout=60s
# Tempo before Grafana — Grafana's Tempo datasource doesn't block
# provisioning if Tempo isn't up yet, but waiting here keeps this script's
# own ordering honest about the real dependency.
kubectl -n "$NS" rollout status deployment/tempo --timeout=60s
kubectl -n "$NS" rollout status deployment/grafana --timeout=60s
kubectl -n kube-system rollout status deployment/metrics-server --timeout=90s
echo "Infra ready. Now: helm install nimbus deploy/k8s/helm/nimbus -n nimbus"
