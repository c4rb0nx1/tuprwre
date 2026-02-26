package sandbox

import (
	"bytes"
	"context"
	"io"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/docker/docker/client"
	"github.com/yourusername/tuprwre/internal/config"
)

func requireDockerRuntime(t *testing.T) *DockerRuntime {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Skipf("docker client unavailable: %v", err)
	}
	defer cli.Close()

	if _, err := cli.Ping(ctx); err != nil {
		t.Skipf("docker daemon unavailable: %v", err)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	rt := New(cfg)
	t.Cleanup(func() {
		_ = rt.Close()
	})

	return rt
}

func runWithTimeout(t *testing.T, rt *DockerRuntime, opts RunOptions) (int, error) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	return rt.runWithContext(ctx, opts)
}

func TestDockerRuntimeRun_FastExitImmediateStdout(t *testing.T) {
	rt := requireDockerRuntime(t)

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode, err := runWithTimeout(t, rt, RunOptions{
		Image:  "ubuntu:22.04",
		Binary: "sh",
		Args:   []string{"-c", "printf READY"},
		Stdout: &stdout,
		Stderr: &stderr,
	})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if exitCode != 0 {
		t.Fatalf("unexpected exit code: %d", exitCode)
	}
	if got := stdout.String(); got != "READY" {
		t.Fatalf("unexpected stdout: %q", got)
	}
	if got := stderr.String(); got != "" {
		t.Fatalf("unexpected stderr: %q", got)
	}
}

func TestDockerRuntimeRun_NoOutput(t *testing.T) {
	rt := requireDockerRuntime(t)

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode, err := runWithTimeout(t, rt, RunOptions{
		Image:  "ubuntu:22.04",
		Binary: "sh",
		Args:   []string{"-c", "true"},
		Stdout: &stdout,
		Stderr: &stderr,
	})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if exitCode != 0 {
		t.Fatalf("unexpected exit code: %d", exitCode)
	}
	if got := stdout.String(); got != "" {
		t.Fatalf("unexpected stdout: %q", got)
	}
	if got := stderr.String(); got != "" {
		t.Fatalf("unexpected stderr: %q", got)
	}
}

func TestDockerRuntimeRun_StderrOnly(t *testing.T) {
	rt := requireDockerRuntime(t)

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode, err := runWithTimeout(t, rt, RunOptions{
		Image:  "ubuntu:22.04",
		Binary: "sh",
		Args:   []string{"-c", "printf ERR_ONLY >&2"},
		Stdout: &stdout,
		Stderr: &stderr,
	})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if exitCode != 0 {
		t.Fatalf("unexpected exit code: %d", exitCode)
	}
	if got := stdout.String(); got != "" {
		t.Fatalf("unexpected stdout: %q", got)
	}
	if got := stderr.String(); got != "ERR_ONLY" {
		t.Fatalf("unexpected stderr: %q", got)
	}
}

func TestDockerRuntimeRun_StdinFedTerminates(t *testing.T) {
	rt := requireDockerRuntime(t)

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode, err := runWithTimeout(t, rt, RunOptions{
		Image:  "ubuntu:22.04",
		Binary: "cat",
		Stdin:  strings.NewReader("hello-from-stdin\n"),
		Stdout: &stdout,
		Stderr: &stderr,
	})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if exitCode != 0 {
		t.Fatalf("unexpected exit code: %d", exitCode)
	}
	if got := stdout.String(); got != "hello-from-stdin\n" {
		t.Fatalf("unexpected stdout: %q", got)
	}
	if got := stderr.String(); got != "" {
		t.Fatalf("unexpected stderr: %q", got)
	}
}

func TestDockerRuntimeRun_StressFastExitOutput(t *testing.T) {
	rt := requireDockerRuntime(t)

	for i := 0; i < 50; i++ {
		var stdout bytes.Buffer
		var stderr bytes.Buffer

		exitCode, err := runWithTimeout(t, rt, RunOptions{
			Image:  "ubuntu:22.04",
			Binary: "sh",
			Args:   []string{"-c", "printf READY"},
			Stdout: &stdout,
			Stderr: &stderr,
		})
		if err != nil {
			t.Fatalf("iteration %d failed: %v", i, err)
		}
		if exitCode != 0 {
			t.Fatalf("iteration %d unexpected exit code: %d", i, exitCode)
		}
		if got := stdout.String(); got != "READY" {
			t.Fatalf("iteration %d unexpected stdout: %q", i, got)
		}
		if got := stderr.String(); got != "" {
			t.Fatalf("iteration %d unexpected stderr: %q", i, got)
		}
	}
}

type cancelOnFirstWrite struct {
	mu     sync.Mutex
	buf    bytes.Buffer
	once   sync.Once
	cancel context.CancelFunc
}

func (w *cancelOnFirstWrite) Write(p []byte) (int, error) {
	w.once.Do(w.cancel)
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(p)
}

func (w *cancelOnFirstWrite) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}

func TestDockerRuntimeRun_ContextCancelNoDeadlock(t *testing.T) {
	rt := requireDockerRuntime(t)

	before := runtime.NumGoroutine()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stdout := &cancelOnFirstWrite{cancel: cancel}
	var stderr bytes.Buffer

	done := make(chan struct{})
	var runErr error

	go func() {
		_, runErr = rt.runWithContext(ctx, RunOptions{
			Image:  "ubuntu:22.04",
			Binary: "sh",
			Args:   []string{"-c", "printf READY; tail -f /dev/null"},
			Stdout: stdout,
			Stderr: &stderr,
		})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Minute):
		t.Fatal("run did not return after cancellation")
	}

	if runErr == nil {
		t.Fatal("expected cancellation error, got nil")
	}
	if !strings.Contains(stdout.String(), "READY") {
		t.Fatalf("expected startup output before cancel, got: %q", stdout.String())
	}

	runtime.GC()
	runtime.GC()
	runtime.Gosched()
	after := runtime.NumGoroutine()
	if after > before+8 {
		t.Fatalf("possible goroutine leak: before=%d after=%d", before, after)
	}
}

func TestDockerRuntimeRun_StressHelper_NoEmptyOutput(t *testing.T) {
	rt := requireDockerRuntime(t)

	for i := 0; i < 30; i++ {
		var stdout bytes.Buffer
		var stderr bytes.Buffer

		exitCode, err := runWithTimeout(t, rt, RunOptions{
			Image:  "ubuntu:22.04",
			Binary: "sh",
			Args:   []string{"-c", "printf race-check"},
			Stdout: &stdout,
			Stderr: &stderr,
		})
		if err != nil {
			t.Fatalf("iteration %d failed: %v", i, err)
		}
		if exitCode != 0 {
			t.Fatalf("iteration %d exit code: %d", i, exitCode)
		}
		if strings.TrimSpace(stdout.String()) == "" {
			t.Fatalf("iteration %d had empty stdout", i)
		}
		if got := stderr.String(); got != "" {
			t.Fatalf("iteration %d unexpected stderr: %q", i, got)
		}
	}
}

var _ io.Writer = (*cancelOnFirstWrite)(nil)
