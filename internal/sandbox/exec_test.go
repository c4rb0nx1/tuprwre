package sandbox

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
)

// requireWarmContainer creates a running container with "sleep infinity"
// and returns its ID. The container is automatically removed on cleanup.
func requireWarmContainer(t *testing.T, rt *DockerRuntime, imageName string) string {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	if err := rt.initClient(); err != nil {
		t.Fatalf("init client: %v", err)
	}

	if err := rt.PullImage(ctx, imageName); err != nil {
		t.Fatalf("pull image: %v", err)
	}

	resp, err := rt.client.ContainerCreate(ctx, &container.Config{
		Image: imageName,
		Cmd:   []string{"sleep", "infinity"},
	}, &container.HostConfig{}, nil, nil, "")
	if err != nil {
		t.Fatalf("create warm container: %v", err)
	}

	if err := rt.client.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		t.Fatalf("start warm container: %v", err)
	}

	t.Cleanup(func() {
		_ = rt.client.ContainerRemove(context.Background(), resp.ID, container.RemoveOptions{Force: true})
	})

	return resp.ID
}

func TestExecWithExitCode_TrueReturnsZero(t *testing.T) {
	rt := requireDockerRuntime(t)
	cid := requireWarmContainer(t, rt, "ubuntu:22.04")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	exitCode, err := rt.ExecWithExitCode(ctx, ExecOptions{
		ContainerID: cid,
		Cmd:         []string{"true"},
	})
	if err != nil {
		t.Fatalf("exec failed: %v", err)
	}
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}
}

func TestExecWithExitCode_FalseReturnsNonZero(t *testing.T) {
	rt := requireDockerRuntime(t)
	cid := requireWarmContainer(t, rt, "ubuntu:22.04")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	exitCode, err := rt.ExecWithExitCode(ctx, ExecOptions{
		ContainerID: cid,
		Cmd:         []string{"false"},
	})
	if err != nil {
		t.Fatalf("exec failed: %v", err)
	}
	if exitCode == 0 {
		t.Fatal("expected non-zero exit code from 'false'")
	}
}

func TestExecWithExitCode_CapturesStdout(t *testing.T) {
	rt := requireDockerRuntime(t)
	cid := requireWarmContainer(t, rt, "ubuntu:22.04")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var stdout bytes.Buffer
	exitCode, err := rt.ExecWithExitCode(ctx, ExecOptions{
		ContainerID: cid,
		Cmd:         []string{"printf", "HELLO"},
		Stdout:      &stdout,
	})
	if err != nil {
		t.Fatalf("exec failed: %v", err)
	}
	if exitCode != 0 {
		t.Fatalf("unexpected exit code: %d", exitCode)
	}
	if got := stdout.String(); got != "HELLO" {
		t.Fatalf("unexpected stdout: %q", got)
	}
}

func TestExecWithExitCode_CapturesStderr(t *testing.T) {
	rt := requireDockerRuntime(t)
	cid := requireWarmContainer(t, rt, "ubuntu:22.04")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var stdout, stderr bytes.Buffer
	exitCode, err := rt.ExecWithExitCode(ctx, ExecOptions{
		ContainerID: cid,
		Cmd:         []string{"sh", "-c", "printf ERR >&2"},
		Stdout:      &stdout,
		Stderr:      &stderr,
	})
	if err != nil {
		t.Fatalf("exec failed: %v", err)
	}
	if exitCode != 0 {
		t.Fatalf("unexpected exit code: %d", exitCode)
	}
	if got := stdout.String(); got != "" {
		t.Fatalf("unexpected stdout: %q", got)
	}
	if got := stderr.String(); got != "ERR" {
		t.Fatalf("unexpected stderr: %q", got)
	}
}

func TestExecWithExitCode_PassesEnvAndWorkDir(t *testing.T) {
	rt := requireDockerRuntime(t)
	cid := requireWarmContainer(t, rt, "ubuntu:22.04")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var stdout bytes.Buffer
	exitCode, err := rt.ExecWithExitCode(ctx, ExecOptions{
		ContainerID: cid,
		Cmd:         []string{"sh", "-c", "echo $MY_VAR && pwd"},
		Env:         []string{"MY_VAR=warm-pool-test"},
		WorkDir:     "/tmp",
		Stdout:      &stdout,
	})
	if err != nil {
		t.Fatalf("exec failed: %v", err)
	}
	if exitCode != 0 {
		t.Fatalf("unexpected exit code: %d", exitCode)
	}
	output := stdout.String()
	if !strings.Contains(output, "warm-pool-test") {
		t.Fatalf("expected env var in output, got: %q", output)
	}
	if !strings.Contains(output, "/tmp") {
		t.Fatalf("expected /tmp working dir in output, got: %q", output)
	}
}

func TestExecWithExitCode_StdinPiped(t *testing.T) {
	rt := requireDockerRuntime(t)
	cid := requireWarmContainer(t, rt, "ubuntu:22.04")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var stdout bytes.Buffer
	exitCode, err := rt.ExecWithExitCode(ctx, ExecOptions{
		ContainerID: cid,
		Cmd:         []string{"cat"},
		Stdin:       strings.NewReader("hello-from-stdin\n"),
		Stdout:      &stdout,
	})
	if err != nil {
		t.Fatalf("exec failed: %v", err)
	}
	if exitCode != 0 {
		t.Fatalf("unexpected exit code: %d", exitCode)
	}
	if got := stdout.String(); got != "hello-from-stdin\n" {
		t.Fatalf("unexpected stdout: %q", got)
	}
}

func TestRunViaExec_ContainerIDShortcut(t *testing.T) {
	rt := requireDockerRuntime(t)
	cid := requireWarmContainer(t, rt, "ubuntu:22.04")

	var stdout, stderr bytes.Buffer
	exitCode, err := runWithTimeout(t, rt, RunOptions{
		Image:       "ubuntu:22.04",
		ContainerID: cid,
		Binary:      "sh",
		Args:        []string{"-c", "printf EXEC_PATH"},
		Stdout:      &stdout,
		Stderr:      &stderr,
	})
	if err != nil {
		t.Fatalf("run via exec failed: %v", err)
	}
	if exitCode != 0 {
		t.Fatalf("unexpected exit code: %d", exitCode)
	}
	if got := stdout.String(); got != "EXEC_PATH" {
		t.Fatalf("unexpected stdout: %q", got)
	}
}

