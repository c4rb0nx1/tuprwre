package sandbox

import (
	"context"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
)

// TestExecBenchmark measures the latency of ExecWithExitCode against a warm
// container. This is the Phase A hard gate: if p50 > 80ms, the warm pool
// approach needs reevaluation.
//
// Run with: go test -v -run TestExecBenchmark -timeout 5m ./internal/sandbox/
func TestExecBenchmark(t *testing.T) {
	rt := requireDockerRuntime(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	if err := rt.initClient(); err != nil {
		t.Fatalf("init client: %v", err)
	}

	const imageName = "alpine:3.19"
	if err := rt.PullImage(ctx, imageName); err != nil {
		t.Fatalf("pull image: %v", err)
	}

	resp, err := rt.client.ContainerCreate(ctx, &container.Config{
		Image: imageName,
		Cmd:   []string{"sleep", "infinity"},
	}, &container.HostConfig{
		ReadonlyRootfs: true,
		Tmpfs:          map[string]string{"/tmp": "size=64m,noexec"},
	}, nil, nil, "")
	if err != nil {
		t.Fatalf("create warm container: %v", err)
	}
	t.Cleanup(func() {
		_ = rt.client.ContainerRemove(context.Background(), resp.ID, container.RemoveOptions{Force: true})
	})

	if err := rt.client.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		t.Fatalf("start warm container: %v", err)
	}

	const iterations = 30

	// Warm up: run a few execs to prime any caches
	for i := 0; i < 3; i++ {
		_, _ = rt.ExecWithExitCode(ctx, ExecOptions{
			ContainerID: resp.ID,
			Cmd:         []string{"true"},
		})
	}

	// Benchmark: exec "true" (no-op)
	trueTimes := make([]time.Duration, 0, iterations)
	for i := 0; i < iterations; i++ {
		start := time.Now()
		exitCode, err := rt.ExecWithExitCode(ctx, ExecOptions{
			ContainerID: resp.ID,
			Cmd:         []string{"true"},
		})
		elapsed := time.Since(start)
		if err != nil {
			t.Fatalf("exec true iteration %d: %v", i, err)
		}
		if exitCode != 0 {
			t.Fatalf("exec true iteration %d: exit code %d", i, exitCode)
		}
		trueTimes = append(trueTimes, elapsed)
	}

	// Benchmark: exec "echo hello" (with output)
	echoTimes := make([]time.Duration, 0, iterations)
	for i := 0; i < iterations; i++ {
		start := time.Now()
		exitCode, err := rt.ExecWithExitCode(ctx, ExecOptions{
			ContainerID: resp.ID,
			Cmd:         []string{"echo", "hello"},
		})
		elapsed := time.Since(start)
		if err != nil {
			t.Fatalf("exec echo iteration %d: %v", i, err)
		}
		if exitCode != 0 {
			t.Fatalf("exec echo iteration %d: exit code %d", i, exitCode)
		}
		echoTimes = append(echoTimes, elapsed)
	}

	trueStats := computeStats(trueTimes)
	echoStats := computeStats(echoTimes)

	t.Logf("========================================")
	t.Logf("  Go SDK ExecWithExitCode Benchmark")
	t.Logf("  Container: %s (%s)", imageName, resp.ID[:12])
	t.Logf("  Iterations: %d (+ 3 warmup)", iterations)
	t.Logf("========================================")
	t.Logf("  exec 'true':       p50=%3dms  p95=%3dms  mean=%3dms  min=%3dms  max=%3dms",
		trueStats.p50, trueStats.p95, trueStats.mean, trueStats.min, trueStats.max)
	t.Logf("  exec 'echo hello': p50=%3dms  p95=%3dms  mean=%3dms  min=%3dms  max=%3dms",
		echoStats.p50, echoStats.p95, echoStats.mean, echoStats.min, echoStats.max)
	t.Logf("========================================")

	// Hard gate: p50 must be < 80ms for exec "true"
	if trueStats.p50 > 80 {
		t.Fatalf("HARD GATE FAILED: exec 'true' p50=%dms > 80ms target. "+
			"Warm pool approach may not achieve <100ms with overhead. "+
			"Evaluate socket daemon or containerd approach.", trueStats.p50)
	}

	t.Logf("✅ HARD GATE PASSED: exec 'true' p50=%dms < 80ms", trueStats.p50)
}

type benchStats struct {
	p50, p95, mean, min, max int64
}

func computeStats(durations []time.Duration) benchStats {
	if len(durations) == 0 {
		return benchStats{}
	}

	ms := make([]int64, len(durations))
	var total int64
	for i, d := range durations {
		ms[i] = d.Milliseconds()
		total += ms[i]
	}
	sort.Slice(ms, func(i, j int) bool { return ms[i] < ms[j] })

	n := len(ms)
	p50idx := n / 2
	p95idx := int(float64(n) * 0.95)
	if p95idx >= n {
		p95idx = n - 1
	}

	return benchStats{
		p50:  ms[p50idx],
		p95:  ms[p95idx],
		mean: total / int64(n),
		min:  ms[0],
		max:  ms[n-1],
	}
}

// TestExecBenchmark_VsColdPath compares warm exec latency against the cold
// docker-create-start-remove path. This is informational, not a gate.
func TestExecBenchmark_VsColdPath(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping cold path comparison in short mode")
	}

	rt := requireDockerRuntime(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	if err := rt.initClient(); err != nil {
		t.Fatalf("init client: %v", err)
	}

	const imageName = "alpine:3.19"
	if err := rt.PullImage(ctx, imageName); err != nil {
		t.Fatalf("pull image: %v", err)
	}

	// Create warm container for exec path
	resp, err := rt.client.ContainerCreate(ctx, &container.Config{
		Image: imageName,
		Cmd:   []string{"sleep", "infinity"},
	}, &container.HostConfig{
		ReadonlyRootfs: true,
		Tmpfs:          map[string]string{"/tmp": "size=64m,noexec"},
	}, nil, nil, "")
	if err != nil {
		t.Fatalf("create warm container: %v", err)
	}
	t.Cleanup(func() {
		_ = rt.client.ContainerRemove(context.Background(), resp.ID, container.RemoveOptions{Force: true})
	})
	if err := rt.client.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		t.Fatalf("start warm container: %v", err)
	}

	const iterations = 10

	// Warm path
	warmTimes := make([]time.Duration, 0, iterations)
	for i := 0; i < iterations; i++ {
		start := time.Now()
		_, err := rt.ExecWithExitCode(ctx, ExecOptions{
			ContainerID: resp.ID,
			Cmd:         []string{"true"},
		})
		elapsed := time.Since(start)
		if err != nil {
			t.Fatalf("warm iteration %d: %v", i, err)
		}
		warmTimes = append(warmTimes, elapsed)
	}

	// Cold path
	coldTimes := make([]time.Duration, 0, iterations)
	for i := 0; i < iterations; i++ {
		start := time.Now()
		exitCode, err := rt.runWithContext(ctx, RunOptions{
			Image:  imageName,
			Binary: "true",
		})
		elapsed := time.Since(start)
		if err != nil {
			t.Fatalf("cold iteration %d: %v", i, err)
		}
		if exitCode != 0 {
			t.Fatalf("cold iteration %d: exit code %d", i, exitCode)
		}
		coldTimes = append(coldTimes, elapsed)
	}

	warmStats := computeStats(warmTimes)
	coldStats := computeStats(coldTimes)

	speedup := "N/A"
	if warmStats.p50 > 0 {
		speedup = fmt.Sprintf("%.1fx", float64(coldStats.p50)/float64(warmStats.p50))
	}

	t.Logf("========================================")
	t.Logf("  Warm vs Cold Path Comparison")
	t.Logf("  Image: %s | Iterations: %d", imageName, iterations)
	t.Logf("========================================")
	t.Logf("  Warm (exec):  p50=%3dms  p95=%3dms  mean=%3dms", warmStats.p50, warmStats.p95, warmStats.mean)
	t.Logf("  Cold (run):   p50=%3dms  p95=%3dms  mean=%3dms", coldStats.p50, coldStats.p95, coldStats.mean)
	t.Logf("  Speedup:      %s", speedup)
	t.Logf("========================================")
}
