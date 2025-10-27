#!/usr/bin/env bash

# Applies every Kubernetes manifest found under each service's kubernetes/
# directory. Stops on the first failure (kubectl apply non-zero exit status).

set -euo pipefail

if ! command -v kubectl >/dev/null 2>&1; then
  printf 'kubectl executable not found in PATH\n' >&2
  exit 1
fi

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

for kube_dir in "$ROOT_DIR"/*/kubernetes; do
  [[ -d "$kube_dir" ]] || continue

  printf 'Applying manifests in %s\n' "$kube_dir"
  kubectl apply -f "$kube_dir"
done
