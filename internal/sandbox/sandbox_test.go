package sandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/docker/docker/client"
	"github.com/c4rb0nx1/tuprwre/internal/config"
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

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (w *lockedBuffer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(p)
}

func (w *lockedBuffer) String() string {
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

func TestDockerRuntimeRun_CaptureFileCombinedStream(t *testing.T) {
	rt := requireDockerRuntime(t)

	tempDir := t.TempDir()
	capturePath := filepath.Join(tempDir, "capture.log")

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode, err := runWithTimeout(t, rt, RunOptions{
		Image:       "ubuntu:22.04",
		Binary:      "sh",
		Args:        []string{"-c", "printf OUT; printf ERR >&2"},
		Stdout:      &stdout,
		Stderr:      &stderr,
		CaptureFile: capturePath,
	})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if exitCode != 0 {
		t.Fatalf("unexpected exit code: %d", exitCode)
	}
	if got := stdout.String(); got != "OUT" {
		t.Fatalf("unexpected stdout: %q", got)
	}
	if got := stderr.String(); got != "ERR" {
		t.Fatalf("unexpected stderr: %q", got)
	}

	captureBytes, readErr := os.ReadFile(capturePath)
	if readErr != nil {
		t.Fatalf("read capture file: %v", readErr)
	}
	captured := string(captureBytes)
	if !strings.Contains(captured, "OUT") {
		t.Fatalf("capture missing stdout: %q", captured)
	}
	if !strings.Contains(captured, "ERR") {
		t.Fatalf("capture missing stderr: %q", captured)
	}
}

func TestDockerRuntimeRun_CaptureFileOnNonZeroExit(t *testing.T) {
	rt := requireDockerRuntime(t)

	tempDir := t.TempDir()
	capturePath := filepath.Join(tempDir, "capture-fail.log")

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode, err := runWithTimeout(t, rt, RunOptions{
		Image:       "ubuntu:22.04",
		Binary:      "sh",
		Args:        []string{"-c", "printf BEFORE_FAIL; exit 7"},
		Stdout:      &stdout,
		Stderr:      &stderr,
		CaptureFile: capturePath,
	})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if exitCode != 7 {
		t.Fatalf("unexpected exit code: %d", exitCode)
	}
	if got := stdout.String(); got != "BEFORE_FAIL" {
		t.Fatalf("unexpected stdout: %q", got)
	}
	if got := stderr.String(); got != "" {
		t.Fatalf("unexpected stderr: %q", got)
	}

	captureBytes, readErr := os.ReadFile(capturePath)
	if readErr != nil {
		t.Fatalf("read capture file: %v", readErr)
	}
	if got := string(captureBytes); !strings.Contains(got, "BEFORE_FAIL") {
		t.Fatalf("capture missing expected output: %q", got)
	}
}

func TestDockerRuntimeRun_CaptureFileOnCancel(t *testing.T) {
	rt := requireDockerRuntime(t)

	tempDir := t.TempDir()
	capturePath := filepath.Join(tempDir, "capture-cancel.log")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stdout := &cancelOnFirstWrite{cancel: cancel}
	var stderr bytes.Buffer

	runDone := make(chan struct{})
	var runErr error

	go func() {
		_, runErr = rt.runWithContext(ctx, RunOptions{
			Image:       "ubuntu:22.04",
			Binary:      "sh",
			Args:        []string{"-c", "printf READY; tail -f /dev/null"},
			Stdout:      stdout,
			Stderr:      &stderr,
			CaptureFile: capturePath,
		})
		close(runDone)
	}()

	select {
	case <-runDone:
	case <-time.After(2 * time.Minute):
		t.Fatal("run did not return after cancellation")
	}

	if runErr == nil {
		t.Fatal("expected cancellation error, got nil")
	}

	captureBytes, readErr := os.ReadFile(capturePath)
	if readErr != nil {
		t.Fatalf("read capture file: %v", readErr)
	}
	if got := string(captureBytes); !strings.Contains(got, "READY") {
		t.Fatalf("capture missing expected output: %q", got)
	}
}

func TestDockerRuntimeRun_DebugIOEvents(t *testing.T) {
	rt := requireDockerRuntime(t)

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode, err := runWithTimeout(t, rt, RunOptions{
		Image:   "ubuntu:22.04",
		Binary:  "sh",
		Args:    []string{"-c", "printf READY"},
		Stdout:  &stdout,
		Stderr:  &stderr,
		DebugIO: true,
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

	debugLog := stderr.String()
	for _, event := range []string{"create", "attach", "wait-registered", "start", "wait-exit", "stream-eof", "cleanup"} {
		if !strings.Contains(debugLog, event) {
			t.Fatalf("missing debug event %q in %q", event, debugLog)
		}
	}
}

var _ io.Writer = (*cancelOnFirstWrite)(nil)

type parsedDiagnosticEvent struct {
	Timestamp   string                 `json:"timestamp"`
	RunID       string                 `json:"run_id"`
	Event       string                 `json:"event"`
	ElapsedMs   int64                  `json:"elapsed_ms"`
	ContainerID string                 `json:"container_id"`
	Details     map[string]interface{} `json:"details"`
}

func parseDiagnosticEvents(t *testing.T, output string) []parsedDiagnosticEvent {
	t.Helper()

	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil
	}

	events := make([]parsedDiagnosticEvent, 0, len(lines))
	for i, line := range lines {
		var evt parsedDiagnosticEvent
		if err := json.Unmarshal([]byte(line), &evt); err != nil {
			t.Fatalf("line %d is not valid JSON: %v line=%q", i, err, line)
		}
		events = append(events, evt)
	}

	return events
}

func parseDiagnosticEventsFromMixed(t *testing.T, output string) []parsedDiagnosticEvent {
	t.Helper()

	lines := strings.Split(strings.TrimSpace(output), "\n")
	events := make([]parsedDiagnosticEvent, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		var evt parsedDiagnosticEvent
		if err := json.Unmarshal([]byte(line), &evt); err == nil && evt.Event != "" {
			events = append(events, evt)
		}
	}

	return events
}

func TestDockerRuntimeRun_DebugIOJSONEvents(t *testing.T) {
	rt := requireDockerRuntime(t)

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode, err := runWithTimeout(t, rt, RunOptions{
		Image:       "ubuntu:22.04",
		Binary:      "sh",
		Args:        []string{"-c", "printf READY"},
		Stdout:      &stdout,
		Stderr:      &stderr,
		DebugIOJSON: true,
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

	events := parseDiagnosticEvents(t, stderr.String())
	if len(events) == 0 {
		t.Fatal("expected json diagnostic events")
	}

	expectedOrder := []string{"create", "attach", "wait-registered", "start", "wait-exit", "stream-eof", "cleanup"}
	if len(events) != len(expectedOrder) {
		t.Fatalf("unexpected event count: got=%d want=%d", len(events), len(expectedOrder))
	}

	runID := events[0].RunID
	if runID == "" {
		t.Fatal("run_id must be set")
	}

	lastElapsed := int64(-1)
	for i, evt := range events {
		if evt.Event != expectedOrder[i] {
			t.Fatalf("event order mismatch at %d: got=%q want=%q", i, evt.Event, expectedOrder[i])
		}
		if evt.RunID != runID {
			t.Fatalf("run_id mismatch at %d: got=%q want=%q", i, evt.RunID, runID)
		}
		if evt.ContainerID == "" {
			t.Fatalf("container_id missing at %d event=%s", i, evt.Event)
		}
		if _, parseErr := time.Parse(time.RFC3339Nano, evt.Timestamp); parseErr != nil {
			t.Fatalf("invalid timestamp at %d: %v", i, parseErr)
		}
		if evt.ElapsedMs < lastElapsed {
			t.Fatalf("elapsed_ms regressed at %d: prev=%d curr=%d", i, lastElapsed, evt.ElapsedMs)
		}
		lastElapsed = evt.ElapsedMs
	}
}

func TestDockerRuntimeRun_DebugIOJSONEventOrderAcrossScenarios(t *testing.T) {
	rt := requireDockerRuntime(t)

	baseOrder := []string{"create", "attach", "wait-registered", "start", "wait-exit", "stream-eof", "cleanup"}

	tests := []struct {
		name   string
		binary string
		args   []string
		stdin  io.Reader
	}{
		{
			name:   "fast-exit",
			binary: "sh",
			args:   []string{"-c", "printf READY"},
		},
		{
			name:   "stderr-only",
			binary: "sh",
			args:   []string{"-c", "echo ERR_ONLY >&2"},
		},
		{
			name:   "stdin-present",
			binary: "cat",
			stdin:  strings.NewReader("hello-from-stdin\n"),
		},
		{
			name:   "no-output",
			binary: "sh",
			args:   []string{"-c", "true"},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			var stdout bytes.Buffer
			stderr := &lockedBuffer{}

			exitCode, err := runWithTimeout(t, rt, RunOptions{
				Image:       "ubuntu:22.04",
				Binary:      tc.binary,
				Args:        tc.args,
				Stdin:       tc.stdin,
				Stdout:      &stdout,
				Stderr:      stderr,
				DebugIOJSON: true,
			})
			if err != nil {
				t.Fatalf("run failed: %v", err)
			}
			if exitCode != 0 {
				t.Fatalf("unexpected exit code: %d", exitCode)
			}

			events := parseDiagnosticEventsFromMixed(t, stderr.String())
			if len(events) != len(baseOrder) {
				t.Fatalf("unexpected event count: got=%d want=%d", len(events), len(baseOrder))
			}

			for i, want := range baseOrder {
				if events[i].Event != want {
					t.Fatalf("event order mismatch at %d: got=%q want=%q", i, events[i].Event, want)
				}
			}
		})
	}
}

func TestDockerRuntimeRun_NoDiagnosticsByDefault(t *testing.T) {
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
		t.Fatalf("expected no diagnostics in default mode, got stderr=%q", got)
	}
}

func TestDockerRuntimeRun_DebugIOTextAndJSONTogether(t *testing.T) {
	rt := requireDockerRuntime(t)

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode, err := runWithTimeout(t, rt, RunOptions{
		Image:       "ubuntu:22.04",
		Binary:      "sh",
		Args:        []string{"-c", "printf READY"},
		Stdout:      &stdout,
		Stderr:      &stderr,
		DebugIO:     true,
		DebugIOJSON: true,
	})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if exitCode != 0 {
		t.Fatalf("unexpected exit code: %d", exitCode)
	}

	lines := strings.Split(strings.TrimSpace(stderr.String()), "\n")
	if len(lines) == 0 {
		t.Fatal("expected diagnostics output")
	}

	textSeen := false
	jsonSeen := false
	for _, line := range lines {
		if strings.Contains(line, "[tuprwre][debug-io]") {
			textSeen = true
			continue
		}

		var evt parsedDiagnosticEvent
		if err := json.Unmarshal([]byte(line), &evt); err == nil && evt.Event != "" {
			jsonSeen = true
		}
	}

	if !textSeen {
		t.Fatal("expected text debug diagnostics")
	}
	if !jsonSeen {
		t.Fatal("expected json debug diagnostics")
	}
}
