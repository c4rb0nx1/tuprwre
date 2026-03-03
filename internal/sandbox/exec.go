package sandbox

import (
	"context"
	"fmt"
	"io"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/pkg/stdcopy"
)

// ExecOptions contains parameters for executing a command inside an existing container.
type ExecOptions struct {
	ContainerID string
	Cmd         []string
	Env         []string
	WorkDir     string
	User        string
	Stdin       io.Reader
	Stdout      io.Writer
	Stderr      io.Writer
}

// ExecWithExitCode runs a command inside an existing running container and returns
// the process exit code. Unlike Execute, it supports configurable I/O streams,
// environment variables, working directory, user, and stdin piping.
func (d *DockerRuntime) ExecWithExitCode(ctx context.Context, opts ExecOptions) (int, error) {
	if err := d.initClient(); err != nil {
		return 1, err
	}

	execConfig := container.ExecOptions{
		Cmd:          opts.Cmd,
		Env:          opts.Env,
		WorkingDir:   opts.WorkDir,
		User:         opts.User,
		AttachStdout: true,
		AttachStderr: true,
		AttachStdin:  opts.Stdin != nil,
	}

	execResp, err := d.client.ContainerExecCreate(ctx, opts.ContainerID, execConfig)
	if err != nil {
		return 1, fmt.Errorf("failed to create exec: %w", err)
	}

	attachResp, err := d.client.ContainerExecAttach(ctx, execResp.ID, container.ExecAttachOptions{})
	if err != nil {
		return 1, fmt.Errorf("failed to attach to exec: %w", err)
	}
	defer attachResp.Close()

	stdout := opts.Stdout
	stderr := opts.Stderr
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}

	if opts.Stdin != nil {
		go func() {
			_, _ = io.Copy(attachResp.Conn, opts.Stdin)
			_ = attachResp.CloseWrite()
		}()
	}

	_, _ = stdcopy.StdCopy(stdout, stderr, attachResp.Reader)

	inspectResp, err := d.client.ContainerExecInspect(ctx, execResp.ID)
	if err != nil {
		return 1, fmt.Errorf("failed to inspect exec: %w", err)
	}

	return inspectResp.ExitCode, nil
}
