#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN="${ROOT_DIR}/tuprwre"
IMAGE="${1:-ubuntu:22.04}"
COUNT="${2:-50}"

if ! [[ "${COUNT}" =~ ^[0-9]+$ ]]; then
  printf 'COUNT must be numeric, got: %s\n' "${COUNT}" >&2
  exit 2
fi

if (( COUNT < 30 || COUNT > 100 )); then
  printf 'COUNT must be between 30 and 100, got: %s\n' "${COUNT}" >&2
  exit 2
fi

if ! command -v docker >/dev/null 2>&1; then
  printf 'docker CLI not found; skipping stress check\n' >&2
  exit 0
fi

if ! docker info >/dev/null 2>&1; then
  printf 'docker daemon unavailable; skipping stress check\n' >&2
  exit 0
fi

if [[ ! -x "${BIN}" ]]; then
  (cd "${ROOT_DIR}" && go build -o tuprwre ./cmd/tuprwre)
fi

for ((i=1; i<=COUNT; i++)); do
  out="$(${BIN} run --image "${IMAGE}" -- sh -c 'printf READY' 2>&1)"
  if [[ "${out}" != "READY" ]]; then
    printf 'iteration %d failed: expected READY, got <%s>\n' "${i}" "${out}" >&2
    exit 1
  fi
done

printf 'stress-run-output-race: %d/%d iterations passed\n' "${COUNT}" "${COUNT}"
