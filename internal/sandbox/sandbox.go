// Package sandbox provides Docker/container runtime abstraction for tuprwre.
package sandbox

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/user"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/google/uuid"
	"github.com/yourusername/tuprwre/internal/config"
)

// DockerRuntime provides container lifecycle management using Docker SDK.
type DockerRuntime struct {
	config *config.Config
	client *client.Client
}

// RunOptions contains parameters for running a sandboxed command.
type RunOptions struct {
	Image       string
	ContainerID string
	Binary      string
	Args        []string
	WorkDir     string
	Env         []string
	Volumes     []string
	Runtime     string
	Stdin       io.Reader
	Stdout      io.Writer
	Stderr      io.Writer
}

// New creates a new DockerRuntime instance.
func New(cfg *config.Config) *DockerRuntime {
	return &DockerRuntime{
		config: cfg,
	}
}

// initClient initializes the Docker client (lazy initialization).
func (d *DockerRuntime) initClient() error {
	if d.client != nil {
		return nil
	}

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return fmt.Errorf("failed to create Docker client: %w", err)
	}

	d.client = cli
	return nil
}

// PullImage ensures the base image exists locally, pulling if necessary.
func (d *DockerRuntime) PullImage(ctx context.Context, imageName string) error {
	if err := d.initClient(); err != nil {
		return err
	}

	// Check if image exists locally
	_, _, err := d.client.ImageInspectWithRaw(ctx, imageName)
	if err == nil {
		// Image exists locally
		return nil
	}

	// Image doesn't exist, pull it
	fmt.Printf("Pulling image %s...\n", imageName)
	reader, err := d.client.ImagePull(ctx, imageName, image.PullOptions{})
	if err != nil {
		return fmt.Errorf("failed to pull image %s: %w", imageName, err)
	}
	defer reader.Close()

	// Stream pull output to stdout
	if _, err := io.Copy(os.Stdout, reader); err != nil {
		return fmt.Errorf("failed to stream pull output: %w", err)
	}

	return nil
}

// CreateAndRunContainer creates a container, runs the command, and returns the container ID.
// It streams stdout/stderr to the terminal in real-time.
func (d *DockerRuntime) CreateAndRunContainer(ctx context.Context, baseImage, command string) (string, error) {
	if err := d.initClient(); err != nil {
		return "", err
	}

	// Ensure image is available
	if err := d.PullImage(ctx, baseImage); err != nil {
		return "", err
	}

	// Generate unique container name
	containerName := fmt.Sprintf("tuprwre-%s", uuid.New().String()[:8])

	// Create container configuration
	config := &container.Config{
		Image:        baseImage,
		Cmd:          []string{"sh", "-c", command},
		Tty:          false,
		AttachStdout: true,
		AttachStderr: true,
		OpenStdin:    false,
	}

	// Create the container
	resp, err := d.client.ContainerCreate(
		ctx,
		config,
		&container.HostConfig{},
		nil,
		nil,
		containerName,
	)
	if err != nil {
		return "", fmt.Errorf("failed to create container: %w", err)
	}

	containerID := resp.ID

	// Start the container
	if err := d.client.ContainerStart(ctx, containerID, container.StartOptions{}); err != nil {
		return "", fmt.Errorf("failed to start container: %w", err)
	}

	// Wait for container to finish and stream output
	statusCh, errCh := d.client.ContainerWait(ctx, containerID, container.WaitConditionNotRunning)

	// Attach to container to stream output
	attachOptions := container.AttachOptions{
		Stream: true,
		Stdout: true,
		Stderr: true,
	}

	attachResp, err := d.client.ContainerAttach(ctx, containerID, attachOptions)
	if err != nil {
		return "", fmt.Errorf("failed to attach to container: %w", err)
	}
	defer attachResp.Close()

	// Stream output in a goroutine
	go func() {
		// stdcopy.StdCopy demultiplexes stdout and stderr
		stdcopy.StdCopy(os.Stdout, os.Stderr, attachResp.Reader)
	}()

	// Wait for container to complete
	select {
	case err := <-errCh:
		if err != nil {
			return containerID, fmt.Errorf("container wait error: %w", err)
		}
	case status := <-statusCh:
		if status.StatusCode != 0 {
			return containerID, fmt.Errorf("container exited with code %d", status.StatusCode)
		}
	}

	return containerID, nil
}

// Commit saves the container state to a new image.
func (d *DockerRuntime) Commit(ctx context.Context, containerID, imageName string) error {
	if err := d.initClient(); err != nil {
		return err
	}

	commitOptions := container.CommitOptions{
		Comment: "tuprwre installation commit",
		Author:  "tuprwre",
	}

	resp, err := d.client.ContainerCommit(ctx, containerID, commitOptions)
	if err != nil {
		return fmt.Errorf("failed to commit container: %w", err)
	}

	// Tag the image with the specified name
	if err := d.client.ImageTag(ctx, resp.ID, imageName); err != nil {
		return fmt.Errorf("failed to tag image: %w", err)
	}

	fmt.Printf("Successfully committed image: %s\n", imageName)
	return nil
}

// CleanupContainer removes an ephemeral container.
func (d *DockerRuntime) CleanupContainer(ctx context.Context, containerID string) error {
	if err := d.initClient(); err != nil {
		return err
	}

	removeOptions := container.RemoveOptions{
		Force:         true,
		RemoveVolumes: true,
	}

	if err := d.client.ContainerRemove(ctx, containerID, removeOptions); err != nil {
		return fmt.Errorf("failed to remove container: %w", err)
	}

	return nil
}

// GenerateImageName creates a unique image name for the committed container.
func (d *DockerRuntime) GenerateImageName() string {
	timestamp := time.Now().Format("20060102-150405")
	return fmt.Sprintf("tuprwre-%s-%s", timestamp, uuid.New().String()[:8])
}

// Execute runs a command inside an existing container (for Phase 2+ use).
func (d *DockerRuntime) Execute(ctx context.Context, containerID string, command string) error {
	if err := d.initClient(); err != nil {
		return err
	}

	execConfig := container.ExecOptions{
		Cmd:          []string{"sh", "-c", command},
		AttachStdout: true,
		AttachStderr: true,
		Tty:          false,
	}

	execResp, err := d.client.ContainerExecCreate(ctx, containerID, execConfig)
	if err != nil {
		return fmt.Errorf("failed to create exec: %w", err)
	}

	attachOptions := container.ExecAttachOptions{
		Tty: false,
	}

	resp, err := d.client.ContainerExecAttach(ctx, execResp.ID, attachOptions)
	if err != nil {
		return fmt.Errorf("failed to attach to exec: %w", err)
	}
	defer resp.Close()

	// Stream output
	stdcopy.StdCopy(os.Stdout, os.Stderr, resp.Reader)

	// Check exit code
	inspectResp, err := d.client.ContainerExecInspect(ctx, execResp.ID)
	if err != nil {
		return fmt.Errorf("failed to inspect exec: %w", err)
	}

	if inspectResp.ExitCode != 0 {
		return fmt.Errorf("command exited with code %d", inspectResp.ExitCode)
	}

	return nil
}

// CreateContainer creates an ephemeral container from a base image (legacy interface).
// For Phase 1, use CreateAndRunContainer instead.
func (d *DockerRuntime) CreateContainer(baseImage string) (string, error) {
	ctx := context.Background()
	if err := d.initClient(); err != nil {
		return "", err
	}

	// Ensure image is available
	if err := d.PullImage(ctx, baseImage); err != nil {
		return "", err
	}

	containerName := fmt.Sprintf("tuprwre-%s", uuid.New().String()[:8])

	config := &container.Config{
		Image: baseImage,
		Cmd:   []string{"sleep", "3600"}, // Keep container running
		Tty:   false,
	}

	resp, err := d.client.ContainerCreate(
		ctx,
		config,
		&container.HostConfig{},
		nil,
		nil,
		containerName,
	)
	if err != nil {
		return "", fmt.Errorf("failed to create container: %w", err)
	}

	if err := d.client.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return "", fmt.Errorf("failed to start container: %w", err)
	}

	return resp.ID, nil
}

// Run executes a binary inside a container with proper I/O handling (for shim use).
// Returns the exit code of the command.
func (d *DockerRuntime) Run(opts RunOptions) (int, error) {
	ctx := context.Background()
	if err := d.initClient(); err != nil {
		return 1, err
	}

	// Pull image if needed
	if err := d.PullImage(ctx, opts.Image); err != nil {
		return 1, err
	}

	// Get current user info for UID/GID mapping
	currentUser, err := user.Current()
	if err != nil {
		return 1, fmt.Errorf("failed to get current user: %w", err)
	}

	// Prepare command
	cmd := append([]string{opts.Binary}, opts.Args...)

	config := &container.Config{
		Image:        opts.Image,
		Cmd:          cmd,
		Tty:          false,
		AttachStdout: true,
		AttachStderr: true,
		OpenStdin:    opts.Stdin != nil,
		Env:          opts.Env,
		WorkingDir:   opts.WorkDir,
		User:         fmt.Sprintf("%s:%s", currentUser.Uid, currentUser.Gid),
	}

	// Prepare host config with volume mounts
	hostConfig := &container.HostConfig{
		AutoRemove: true,
	}

	if len(opts.Volumes) > 0 {
		hostConfig.Binds = opts.Volumes
	}

	// Create and start container
	resp, err := d.client.ContainerCreate(ctx, config, hostConfig, nil, nil, "")
	if err != nil {
		return 1, fmt.Errorf("failed to create container: %w", err)
	}

	if err := d.client.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return 1, fmt.Errorf("failed to start container: %w", err)
	}

	// Attach to container
	attachOptions := container.AttachOptions{
		Stream: true,
		Stdout: true,
		Stderr: true,
		Stdin:  opts.Stdin != nil,
	}

	attachResp, err := d.client.ContainerAttach(ctx, resp.ID, attachOptions)
	if err != nil {
		return 1, fmt.Errorf("failed to attach to container: %w", err)
	}
	defer attachResp.Close()

	// Stream stdin if provided
	if opts.Stdin != nil {
		go func() {
			io.Copy(attachResp.Conn, opts.Stdin)
			attachResp.CloseWrite()
		}()
	}

	// Stream stdout/stderr
	stdcopy.StdCopy(opts.Stdout, opts.Stderr, attachResp.Reader)

	// Wait for container
	statusCh, errCh := d.client.ContainerWait(ctx, resp.ID, container.WaitConditionNotRunning)

	select {
	case err := <-errCh:
		if err != nil {
			return 1, fmt.Errorf("container wait error: %w", err)
		}
	case status := <-statusCh:
		return int(status.StatusCode), nil
	}

	return 0, nil
}

// ListExecutables returns all executable files in the container's PATH.
func (d *DockerRuntime) ListExecutables(containerID string) ([]string, error) {
	ctx := context.Background()
	if err := d.initClient(); err != nil {
		return nil, err
	}

	// Get PATH environment variable
	inspect, err := d.client.ContainerInspect(ctx, containerID)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect container: %w", err)
	}

	pathEnv := "/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin"
	for _, env := range inspect.Config.Env {
		if len(env) > 5 && env[:5] == "PATH=" {
			pathEnv = env[5:]
			break
		}
	}

	// Find executables in PATH directories
	cmd := fmt.Sprintf("find $(echo %s | tr ':' ' ') -maxdepth 1 -type f -executable 2>/dev/null | sort -u", pathEnv)
	execConfig := container.ExecOptions{
		Cmd:          []string{"sh", "-c", cmd},
		AttachStdout: true,
		AttachStderr: false,
	}

	execResp, err := d.client.ContainerExecCreate(ctx, containerID, execConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create exec: %w", err)
	}

	attachResp, err := d.client.ContainerExecAttach(ctx, execResp.ID, container.ExecAttachOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to attach to exec: %w", err)
	}
	defer attachResp.Close()

	// Read output
	output, err := io.ReadAll(attachResp.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to read exec output: %w", err)
	}

	// Parse output into list
	var executables []string
	lines := string(output)
	for _, line := range splitLines(lines) {
		line = strings.TrimSpace(line)
		if line != "" {
			executables = append(executables, line)
		}
	}

	return executables, nil
}

// ListImageExecutables returns all executable files in an image's PATH.
// It briefly starts a container from the image, runs find on PATH, and returns the list.
func (d *DockerRuntime) ListImageExecutables(ctx context.Context, imageName string) ([]string, error) {
	if err := d.initClient(); err != nil {
		return nil, err
	}

	// Ensure image is available
	if err := d.PullImage(ctx, imageName); err != nil {
		return nil, err
	}

	// Create a temporary container to inspect the image
	containerName := fmt.Sprintf("tuprwre-inspect-%s", uuid.New().String()[:8])
	config := &container.Config{
		Image: imageName,
		Cmd:   []string{"sleep", "3600"}, // Keep container running
		Tty:   false,
	}

	resp, err := d.client.ContainerCreate(
		ctx,
		config,
		&container.HostConfig{},
		nil,
		nil,
		containerName,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create inspection container: %w", err)
	}
	containerID := resp.ID

	// Clean up container when done
	defer func() {
		removeOptions := container.RemoveOptions{
			Force:         true,
			RemoveVolumes: true,
		}
		d.client.ContainerRemove(ctx, containerID, removeOptions)
	}()

	// Start the container so we can exec into it
	if err := d.client.ContainerStart(ctx, containerID, container.StartOptions{}); err != nil {
		return nil, fmt.Errorf("failed to start inspection container: %w", err)
	}

	// Get PATH environment variable
	inspect, err := d.client.ContainerInspect(ctx, containerID)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect container: %w", err)
	}

	pathEnv := "/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin"
	for _, env := range inspect.Config.Env {
		if len(env) > 5 && env[:5] == "PATH=" {
			pathEnv = env[5:]
			break
		}
	}

	// Find executables in PATH directories
	cmd := fmt.Sprintf("find $(echo %s | tr ':' ' ') -maxdepth 1 -type f -executable 2>/dev/null | sort -u", pathEnv)
	execConfig := container.ExecOptions{
		Cmd:          []string{"sh", "-c", cmd},
		AttachStdout: true,
		AttachStderr: false,
	}

	execResp, err := d.client.ContainerExecCreate(ctx, containerID, execConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create exec: %w", err)
	}

	attachResp, err := d.client.ContainerExecAttach(ctx, execResp.ID, container.ExecAttachOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to attach to exec: %w", err)
	}
	defer attachResp.Close()

	// Read output
	output, err := io.ReadAll(attachResp.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to read exec output: %w", err)
	}

	// Parse output into list
	var executables []string
	lines := string(output)
	for _, line := range splitLines(lines) {
		line = strings.TrimSpace(line)
		if line != "" {
			executables = append(executables, line)
		}
	}

	return executables, nil
}

// GetContainerFilesystem returns the container's filesystem root for analysis.
func (d *DockerRuntime) GetContainerFilesystem(containerID string) (string, error) {
	return "", fmt.Errorf("GetContainerFilesystem not implemented")
}

// splitLines splits a string by newlines.
func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

// Close closes the Docker client connection.
func (d *DockerRuntime) Close() error {
	if d.client != nil {
		return d.client.Close()
	}
	return nil
}
