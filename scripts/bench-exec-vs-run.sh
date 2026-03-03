#!/usr/bin/env bash
# bench-exec-vs-run.sh — Compare docker exec (warm) vs docker run (cold) latency
# Usage: bash scripts/bench-exec-vs-run.sh [iterations]
set -euo pipefail

ITERATIONS="${1:-20}"
IMAGE="alpine:3.19"
CONTAINER_NAME="tuprwre-bench-warm-$$"

echo "=== Docker Exec vs Run Latency Benchmark ==="
echo "Image:      $IMAGE"
echo "Iterations: $ITERATIONS"
echo ""

# Ensure image is pulled (don't count pull time)
echo "Pre-pulling $IMAGE..."
docker pull -q "$IMAGE" >/dev/null 2>&1 || true

# --- Warm container setup ---
echo "Starting warm container ($CONTAINER_NAME)..."
docker run -d --name "$CONTAINER_NAME" "$IMAGE" sh -c 'while true; do sleep 3600; done' >/dev/null 2>&1
trap 'docker rm -f "$CONTAINER_NAME" >/dev/null 2>&1' EXIT

# Let it stabilize
sleep 1

# --- Benchmark: docker exec (warm path) ---
echo ""
echo "--- docker exec (warm container) ---"
EXEC_TIMES=()
for i in $(seq 1 "$ITERATIONS"); do
    START=$(python3 -c 'import time; print(time.time())')
    docker exec "$CONTAINER_NAME" true
    END=$(python3 -c 'import time; print(time.time())')
    ELAPSED=$(python3 -c "print(int(($END - $START) * 1000))")
    EXEC_TIMES+=("$ELAPSED")
    printf "  run %2d: %dms\n" "$i" "$ELAPSED"
done

# --- Benchmark: docker run (cold path) ---
echo ""
echo "--- docker run --rm (cold container) ---"
RUN_TIMES=()
for i in $(seq 1 "$ITERATIONS"); do
    START=$(python3 -c 'import time; print(time.time())')
    docker run --rm "$IMAGE" true
    END=$(python3 -c 'import time; print(time.time())')
    ELAPSED=$(python3 -c "print(int(($END - $START) * 1000))")
    RUN_TIMES+=("$ELAPSED")
    printf "  run %2d: %dms\n" "$i" "$ELAPSED"
done

# --- Benchmark: docker exec with actual command output ---
echo ""
echo "--- docker exec with output (echo hello) ---"
EXEC_OUT_TIMES=()
for i in $(seq 1 "$ITERATIONS"); do
    START=$(python3 -c 'import time; print(time.time())')
    docker exec "$CONTAINER_NAME" echo hello >/dev/null
    END=$(python3 -c 'import time; print(time.time())')
    ELAPSED=$(python3 -c "print(int(($END - $START) * 1000))")
    EXEC_OUT_TIMES+=("$ELAPSED")
    printf "  run %2d: %dms\n" "$i" "$ELAPSED"
done

# --- Benchmark: docker run with actual command output ---
echo ""
echo "--- docker run --rm with output (echo hello) ---"
RUN_OUT_TIMES=()
for i in $(seq 1 "$ITERATIONS"); do
    START=$(python3 -c 'import time; print(time.time())')
    docker run --rm "$IMAGE" echo hello >/dev/null
    END=$(python3 -c 'import time; print(time.time())')
    ELAPSED=$(python3 -c "print(int(($END - $START) * 1000))")
    RUN_OUT_TIMES+=("$ELAPSED")
    printf "  run %2d: %dms\n" "$i" "$ELAPSED"
done

# --- Summary ---
echo ""
echo "========================================="
echo "           SUMMARY (all times in ms)"
echo "========================================="

calc_stats() {
    local name="$1"
    shift
    local times=("$@")
    python3 -c "
import statistics
times = [${times[*]}]
times.sort()
n = len(times)
p50 = times[n//2]
p95 = times[int(n*0.95)]
mean = int(statistics.mean(times))
mn = min(times)
mx = max(times)
print(f'  {\"$name\":<35s}  p50={p50:>4d}  p95={p95:>4d}  mean={mean:>4d}  min={mn:>4d}  max={mx:>4d}')
"
}

calc_stats "docker exec (true)" "${EXEC_TIMES[@]}"
calc_stats "docker run --rm (true)" "${RUN_TIMES[@]}"
calc_stats "docker exec (echo hello)" "${EXEC_OUT_TIMES[@]}"
calc_stats "docker run --rm (echo hello)" "${RUN_OUT_TIMES[@]}"

echo ""
python3 -c "
exec_times = [${EXEC_TIMES[*]}]
run_times = [${RUN_TIMES[*]}]
exec_times.sort()
run_times.sort()
n = len(exec_times)
exec_p50 = exec_times[n//2]
run_p50 = run_times[n//2]
speedup = run_p50 / exec_p50 if exec_p50 > 0 else float('inf')
savings = run_p50 - exec_p50
print(f'Speedup: docker exec is {speedup:.1f}x faster than docker run (saves {savings}ms at p50)')
print()
if exec_p50 < 100:
    print('✅ docker exec p50 < 100ms — warm pool approach CAN hit the target')
else:
    print('⚠️  docker exec p50 >= 100ms — warm pool alone may not be enough')
    print('   Consider: containerd shim, socket daemon, or Docker Desktop tuning')
"
