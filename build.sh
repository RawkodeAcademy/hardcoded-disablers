#!/usr/bin/env bash

# Builds and pushes Docker images for every service directory that contains a
# Dockerfile, then rewrites that service's Kubernetes deployment manifest with
# the freshly pushed tag.

set -euo pipefail

if ! command -v docker >/dev/null 2>&1; then
  printf 'docker executable not found in PATH\n' >&2
  exit 1
fi

PYTHON_BIN="${PYTHON_BIN:-python3}"
if ! command -v "$PYTHON_BIN" >/dev/null 2>&1; then
  if command -v python >/dev/null 2>&1; then
    PYTHON_BIN=python
  else
    printf 'python3 (or python) executable not found in PATH\n' >&2
    exit 1
  fi
fi

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TAG_SUFFIX="${1:-$(date +%s)-${RANDOM}}"
REGISTRY_PREFIX="${REGISTRY_PREFIX:-ttl.sh}"
TTL_EXPIRY="${TTL_EXPIRY:-1h}"

declare -a pushed_images=()

update_manifest() {
  local service_dir="$1"
  local image_tag="$2"

  local deployment_file=""
  for candidate in "$service_dir"/kubernetes/deployment.yaml "$service_dir"/kubernetes/deployment.yml; do
    if [[ -f "$candidate" ]]; then
      deployment_file="$candidate"
      break
    fi
  done

  if [[ -z "$deployment_file" ]]; then
    printf 'No deployment manifest found in %s; skipping image tag update\n' "$service_dir" >&2
    return
  fi

  "$PYTHON_BIN" - "$deployment_file" "$image_tag" <<'PYTHON'
import pathlib
import sys

deployment_file = pathlib.Path(sys.argv[1])
image = sys.argv[2]

lines = deployment_file.read_text().splitlines()
updated = False

for idx, line in enumerate(lines):
    stripped = line.lstrip()
    if stripped.startswith("image:"):
        indent_len = len(line) - len(stripped)
        indent = " " * indent_len
        lines[idx] = f"{indent}image: {image}"
        updated = True
        break

if not updated:
    sys.stderr.write(f"No image key found in {deployment_file}; skipping update\n")
else:
    deployment_file.write_text("\n".join(lines) + "\n")
PYTHON
}

ensure_service_manifest() {
  local service_dir="$1"
  local service_name="$2"

  local service_file=""
  for candidate in "$service_dir"/kubernetes/service.yaml "$service_dir"/kubernetes/service.yml; do
    if [[ -f "$candidate" ]]; then
      service_file="$candidate"
      break
    fi
  done

  if [[ -n "$service_file" ]]; then
    return
  fi

  service_file="$service_dir/kubernetes/service.yaml"
  mkdir -p "$(dirname "$service_file")"
  cat >"$service_file" <<EOF
apiVersion: v1
kind: Service
metadata:
  name: ${service_name}
  labels:
    app: ${service_name}
spec:
  selector:
    app: ${service_name}
  ports:
    - name: http
      port: 80
      targetPort: 8080
EOF
}

build_and_push() {
  local service_dir="$1"
  local service_name="$2"
  local image_tag="$3"

  printf 'Building %s -> %s\n' "$service_name" "$image_tag"
  docker build -t "$image_tag" "$service_dir"
  docker push "$image_tag"
  pushed_images+=("$service_name=$image_tag")
}

services_found=0

for service_dir in "$ROOT_DIR"/*/; do
  [[ -d "$service_dir" ]] || continue
  [[ -f "$service_dir/Dockerfile" ]] || continue

  service_name="$(basename "$service_dir")"
  image_tag="${REGISTRY_PREFIX}/${service_name}-${TAG_SUFFIX}:${TTL_EXPIRY}"

  build_and_push "$service_dir" "$service_name" "$image_tag"
  update_manifest "$service_dir" "$image_tag"
  ensure_service_manifest "$service_dir" "$service_name"

  services_found=$((services_found + 1))
done

if (( services_found == 0 )); then
  printf 'No service directories with Dockerfiles found under %s\n' "$ROOT_DIR" >&2
  exit 1
fi

printf '\nImages pushed:\n'
for entry in "${pushed_images[@]}"; do
  printf ' - %s\n' "$entry"
done
