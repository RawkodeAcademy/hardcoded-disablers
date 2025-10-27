#!/usr/bin/env bash
set -euo pipefail

DEFAULT_REPO="ttl.sh/normalizer"
SUFFIX="$(date +%s)-${RANDOM}"
DEFAULT_IMAGE="${DEFAULT_REPO}-${SUFFIX}:1h"
IMAGE_TAG="${1:-$DEFAULT_IMAGE}"

docker build -t "${IMAGE_TAG}" .
docker push "${IMAGE_TAG}"

printf 'Image pushed: %s\n' "${IMAGE_TAG}"
