#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN="${ROOT_DIR}/tuprwre"

# Defaults
IMAGE="ubuntu:22.04"
COUNT=20
CMD="echo OK"
JSON_OUTPUT=false

# Portable millisecond timestamp (macOS date doesn't support %N)
timestamp_ms() {
  perl -MTime::HiRes -e 'printf("%.0f\n", Time::HiRes::time()*1000)'
}

usage() {
  cat <<EOF
Usage: $(basename "$0") [OPTIONS]

Benchmark tuprwre run invocation latency (cold start + consecutive runs).

Options:
  --image <name>    Docker image to benchmark (default: ubuntu:22.04)
  --count <N>       Number of consecutive iterations (default: 20)
  --cmd <command>   Command to run inside the container (default: "echo OK")
  --json            Output results as JSON to stdout
  --help            Show this help message

Examples:
  $(basename "$0")
  $(basename "$0") --image alpine:3.19 --count 50
  $(basename "$0") --json | jq .
  $(basename "$0") --json --count 10 --cmd "true" > results.json
EOF
}

# Parse arguments
while [[ $# -gt 0 ]]; do
  case "$1" in
    --image)
      IMAGE="$2"
      shift 2
      ;;
    --count)
      COUNT="$2"
      shift 2
      ;;
    --cmd)
      CMD="$2"
      shift 2
      ;;
    --json)
      JSON_OUTPUT=true
      shift
      ;;
    --help)
      usage
      exit 0
      ;;
    *)
      printf 'Unknown option: %s\n' "$1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

# Validate count
if ! [[ "${COUNT}" =~ ^[0-9]+$ ]] || (( COUNT < 1 || COUNT > 500 )); then
  printf 'COUNT must be a number between 1 and 500, got: %s\n' "${COUNT}" >&2
  exit 2
fi

# Helper: print to stderr (used for progress in JSON mode, and always for preflight)
log() {
  printf '%s\n' "$*" >&2
}

# Helper: print to stdout in human mode, stderr in JSON mode
info() {
  if [[ "${JSON_OUTPUT}" == true ]]; then
    printf '%s\n' "$*" >&2
  else
    printf '%s\n' "$*"
  fi
}

# Preflight checks
if ! command -v docker >/dev/null 2>&1; then
  log 'docker CLI not found; skipping benchmark'
  exit 0
fi

if ! docker info >/dev/null 2>&1; then
  log 'docker daemon unavailable; skipping benchmark'
  exit 0
fi

if ! command -v perl >/dev/null 2>&1; then
  log 'perl not found (needed for sub-ms timing); skipping benchmark'
  exit 0
fi

# Auto-build if binary is missing
if [[ ! -x "${BIN}" ]]; then
  info 'Binary not found, building...'
  (cd "${ROOT_DIR}" && go build -o tuprwre ./cmd/tuprwre)
fi

info "=== tuprwre shim latency benchmark ==="
info "Image:      ${IMAGE}"
info "Command:    ${CMD}"
info "Iterations: ${COUNT}"
info ""

# Pre-pull image so pull time doesn't pollute results
info 'Pre-pulling image...'
if ! docker image inspect "${IMAGE}" >/dev/null 2>&1; then
  docker pull "${IMAGE}" >/dev/null 2>&1
fi
info 'Image ready.'
info ""

# Storage for timings (milliseconds)
TIMINGS_FILE="$(mktemp)"
trap 'rm -f "${TIMINGS_FILE}"' EXIT

# --- Cold start ---
info '--- Cold start ---'
start_ms="$(timestamp_ms)"
${BIN} run --image "${IMAGE}" -- sh -c "${CMD}" >/dev/null
end_ms="$(timestamp_ms)"
cold_start_ms=$(( end_ms - start_ms ))
info "Cold start: ${cold_start_ms}ms"
info ""

# --- Consecutive runs ---
info "--- Consecutive runs (${COUNT} iterations) ---"
for ((i=1; i<=COUNT; i++)); do
  start_ms="$(timestamp_ms)"
  ${BIN} run --image "${IMAGE}" -- sh -c "${CMD}" >/dev/null
  end_ms="$(timestamp_ms)"
  elapsed_ms=$(( end_ms - start_ms ))
  printf '%d\n' "${elapsed_ms}" >> "${TIMINGS_FILE}"
  info "  [$i/${COUNT}] ${elapsed_ms}ms"
done
info ""

# --- Compute stats ---
read -r stat_min stat_max stat_avg stat_p50 stat_p95 <<< "$(
  sort -n "${TIMINGS_FILE}" | awk '
  {
    vals[NR] = $1
    sum += $1
  }
  END {
    n = NR
    if (n == 0) { print 0, 0, 0, 0, 0; exit }
    min_v = vals[1]
    max_v = vals[n]
    avg_v = sum / n

    # p50 (median)
    p50_idx = int(n * 0.5 + 0.5)
    if (p50_idx < 1) p50_idx = 1
    if (p50_idx > n) p50_idx = n
    p50_v = vals[p50_idx]

    # p95
    p95_idx = int(n * 0.95 + 0.5)
    if (p95_idx < 1) p95_idx = 1
    if (p95_idx > n) p95_idx = n
    p95_v = vals[p95_idx]

    printf "%d %d %.0f %d %d\n", min_v, max_v, avg_v, p50_v, p95_v
  }'
)"

# --- Output ---
if [[ "${JSON_OUTPUT}" == true ]]; then
  # Build JSON timings array
  timings_json="$(awk 'BEGIN { sep="" } { printf "%s%s", sep, $1; sep="," }' "${TIMINGS_FILE}")"

  # Escape command for JSON
  cmd_escaped="$(printf '%s' "${CMD}" | sed 's/\\/\\\\/g; s/"/\\"/g')"

  cat <<JSONEOF
{
  "image": "${IMAGE}",
  "command": "${cmd_escaped}",
  "iterations": ${COUNT},
  "cold_start_ms": ${cold_start_ms},
  "timings_ms": [${timings_json}],
  "stats": {
    "min_ms": ${stat_min},
    "max_ms": ${stat_max},
    "avg_ms": ${stat_avg},
    "p50_ms": ${stat_p50},
    "p95_ms": ${stat_p95}
  }
}
JSONEOF
else
  printf '%-12s %s\n' 'Cold start:' "${cold_start_ms}ms"
  printf '%-12s %s\n' 'Min:'        "${stat_min}ms"
  printf '%-12s %s\n' 'Max:'        "${stat_max}ms"
  printf '%-12s %s\n' 'Avg:'        "${stat_avg}ms"
  printf '%-12s %s\n' 'p50:'        "${stat_p50}ms"
  printf '%-12s %s\n' 'p95:'        "${stat_p95}ms"
  echo ""
  echo "Done. ${COUNT} iterations completed."
fi
