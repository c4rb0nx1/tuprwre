// Package sandbox provides Docker/container runtime abstraction for tuprwre.
package sandbox

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/user"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/c4rb0nx1/tuprwre/internal/config"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/google/uuid"
)

// DockerRuntime provides container lifecycle management using Docker SDK.
type DockerRuntime struct {
	config *config.Config
	client *client.Client
}

type TuprwreImage struct {
	ID         string
	Repository string
	Tag        string
	Size       int64
	Created    int64
}

type TuprwreContainer struct {
	ID    string
	Name  string
	Image string
	State string
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
	DebugIO     bool
	DebugIOJSON bool
	CaptureFile string
	ReadOnlyCwd bool
	NoNetwork   bool
	MemoryLimit int64   // bytes; 0 means no limit
	CPULimit    float64 // number of CPUs; 0 means no limit
}

type runIODiagnostics struct {
	textEnabled bool
	jsonEnabled bool
	start       time.Time
	writer      io.Writer
	runID       string
	containerID string
}

type runIODiagnosticEvent struct {
	Timestamp   string         `json:"timestamp"`
	RunID       string         `json:"run_id"`
	Event       string         `json:"event"`
	ElapsedMs   int64          `json:"elapsed_ms"`
	ContainerID string         `json:"container_id,omitempty"`
	Details     map[string]any `json:"details,omitempty"`
}

func (d runIODiagnostics) event(name string) {
	d.eventWithDetails(name, nil)
}

func (d runIODiagnostics) eventWithDetails(name string, details map[string]any) {
	if (!d.textEnabled && !d.jsonEnabled) || d.writer == nil {
		return
	}

	elapsedMs := time.Since(d.start).Milliseconds()

	if d.textEnabled {
		_, _ = fmt.Fprintf(d.writer, "[tuprwre][debug-io] +%dms %s\n", elapsedMs, name)
	}

	if d.jsonEnabled {
		event := runIODiagnosticEvent{
			Timestamp:   time.Now().UTC().Format(time.RFC3339Nano),
			RunID:       d.runID,
			Event:       name,
			ElapsedMs:   elapsedMs,
			ContainerID: d.containerID,
			Details:     details,
		}

		payload, err := json.Marshal(event)
		if err == nil {
			_, _ = fmt.Fprintf(d.writer, "%s\n", payload)
		}
	}
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

	pingCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if _, err := cli.Ping(pingCtx); err != nil {
		_ = cli.Close()
		if client.IsErrConnectionFailed(err) {
			return fmt.Errorf("Docker daemon is not running or unreachable. %s", dockerStartHint(runtime.GOOS))
		}
		return fmt.Errorf("Docker daemon health check failed: %w", err)
	}

	d.client = cli
	return nil
}

func dockerStartHint(goos string) string {
	switch goos {
	case "darwin", "windows":
		return "Start Docker Desktop and retry."
	default:
		return "Start Docker and retry (for example: 'systemctl start docker')."
	}
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

func (d *DockerRuntime) RemoveImage(ctx context.Context, imageName string) error {
	if err := d.initClient(); err != nil {
		return err
	}

	if _, err := d.client.ImageRemove(ctx, imageName, image.RemoveOptions{PruneChildren: true}); err != nil {
		return fmt.Errorf("failed to remove image %s: %w", imageName, err)
	}

	return nil
}

func (d *DockerRuntime) ListStoppedTuprwreContainers(ctx context.Context) ([]TuprwreContainer, error) {
	if err := d.initClient(); err != nil {
		return nil, err
	}

	containerList, err := d.client.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	var containers []TuprwreContainer
	for _, containerSummary := range containerList {
		if containerSummary.State != "exited" && containerSummary.State != "dead" && containerSummary.State != "created" {
			continue
		}

		name := ""
		imageMatched := false
		for _, containerName := range containerSummary.Names {
			candidate := strings.TrimPrefix(containerName, "/")
			if strings.HasPrefix(candidate, "tuprwre-") {
				name = candidate
				break
			}
		}
		if name == "" && strings.HasPrefix(containerSummary.Image, "tuprwre-") {
			imageMatched = true
			if len(containerSummary.Names) > 0 {
				name = strings.TrimPrefix(containerSummary.Names[0], "/")
			}
		}

		if name == "" && imageMatched {
			if len(containerSummary.ID) >= 12 {
				name = containerSummary.ID[:12]
			} else {
				name = containerSummary.ID
			}
		}
		if name == "" {
			continue
		}

		containers = append(containers, TuprwreContainer{
			ID:    containerSummary.ID,
			Name:  name,
			Image: containerSummary.Image,
			State: containerSummary.State,
		})
	}

	return containers, nil
}

func (d *DockerRuntime) RemoveContainer(ctx context.Context, containerID string) error {
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

func (d *DockerRuntime) ListTuprwreImages(ctx context.Context) ([]TuprwreImage, error) {
	if err := d.initClient(); err != nil {
		return nil, err
	}

	imageSummaries, err := d.client.ImageList(ctx, image.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list images: %w", err)
	}

	images := make([]TuprwreImage, 0, len(imageSummaries))
	for _, imageSummary := range imageSummaries {
		repository := ""
		tag := ""
		for _, repoTag := range imageSummary.RepoTags {
			repoPart, tagPart, ok := splitRepoTag(repoTag)
			if !ok {
				continue
			}
			if strings.HasPrefix(repoPart, "tuprwre-") {
				repository = repoPart
				tag = tagPart
				break
			}
		}

		if repository == "" {
			continue
		}

		images = append(images, TuprwreImage{
			ID:         imageSummary.ID,
			Repository: repository,
			Tag:        tag,
			Size:       imageSummary.Size,
			Created:    imageSummary.Created,
		})
	}

	sort.Slice(images, func(i, j int) bool {
		return images[i].Created > images[j].Created
	})

	return images, nil
}

// CreateAndRunContainer creates a container, runs the command, and returns the container ID.
// It streams stdout/stderr to the terminal in real-time.
// Resource limits from the policy are applied to the container's HostConfig.
func (d *DockerRuntime) CreateAndRunContainer(ctx context.Context, baseImage, command string, resources ResourcePolicy) (string, error) {
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

	hostConfig := &container.HostConfig{}
	applyResourceLimits(hostConfig, resources)

	// Create the container
	resp, err := d.client.ContainerCreate(
		ctx,
		config,
		hostConfig,
		nil,
		nil,
		containerName,
	)
	if err != nil {
		return "", fmt.Errorf("failed to create container: %w", err)
	}

	containerID := resp.ID

	exitCode, err := d.runAttachedAndDrain(ctx, resp.ID, nil, os.Stdout, os.Stderr, runIODiagnostics{})
	if err != nil {
		return containerID, err
	}
	if exitCode != 0 {
		return containerID, fmt.Errorf("container exited with code %d", exitCode)
	}

	return containerID, nil
}

func (d *DockerRuntime) runAttachedAndDrain(ctx context.Context, containerID string, stdin io.Reader, stdout, stderr io.Writer, diag runIODiagnostics) (int, error) {
	diag.containerID = containerID

	attachOptions := container.AttachOptions{
		Stream: true,
		Stdout: true,
		Stderr: true,
		Stdin:  stdin != nil,
	}

	attachResp, err := d.client.ContainerAttach(ctx, containerID, attachOptions)
	if err != nil {
		return 1, fmt.Errorf("failed to attach to container: %w", err)
	}
	diag.event("attach")

	var closeAttachOnce sync.Once
	closeAttach := func() {
		closeAttachOnce.Do(func() {
			attachResp.Close()
		})
	}

	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}

	outputDone := make(chan struct{})
	go func() {
		_, _ = stdcopy.StdCopy(stdout, stderr, attachResp.Reader)
		close(outputDone)
	}()

	var drainOnce sync.Once
	waitForDrain := func() {
		drainOnce.Do(func() {
			<-outputDone
			diag.event("stream-eof")
		})
	}

	forceCloseAndDrain := func() {
		closeAttach()
		waitForDrain()
	}

	defer forceCloseAndDrain()

	statusCh, errCh := d.client.ContainerWait(ctx, containerID, container.WaitConditionNextExit)
	diag.event("wait-registered")

	if err := d.client.ContainerStart(ctx, containerID, container.StartOptions{}); err != nil {
		forceCloseAndDrain()
		return 1, fmt.Errorf("failed to start container: %w", err)
	}
	diag.event("start")

	if stdin != nil {
		go func() {
			_, _ = io.Copy(attachResp.Conn, stdin)
			_ = attachResp.CloseWrite()
		}()
	}

	// Invariants:
	// (1) Attach happens before start so no early output is lost.
	// (2) Wait registration happens before start so fast exits are observed.
	// (3) Return happens only after stream EOF, or after explicit cancellation cleanup closes attach.
	for {
		select {
		case waitErr, ok := <-errCh:
			if !ok {
				errCh = nil
				if statusCh == nil {
					forceCloseAndDrain()
					return 1, fmt.Errorf("container wait channels closed without status")
				}
				continue
			}
			if waitErr != nil {
				diag.event("wait-exit")
				forceCloseAndDrain()
				return 1, fmt.Errorf("container wait error: %w", waitErr)
			}
			continue
		case status, ok := <-statusCh:
			if !ok {
				statusCh = nil
				if errCh == nil {
					forceCloseAndDrain()
					return 1, fmt.Errorf("container wait channels closed without status")
				}
				continue
			}
			diag.event("wait-exit")
			waitForDrain()
			return int(status.StatusCode), nil
		}
	}
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
	return d.runWithContext(context.Background(), opts)
}

func (d *DockerRuntime) runWithContext(ctx context.Context, opts RunOptions) (int, error) {
	if err := d.initClient(); err != nil {
		return 1, err
	}

	diag := runIODiagnostics{
		textEnabled: opts.DebugIO,
		jsonEnabled: opts.DebugIOJSON,
		start:       time.Now(),
		writer:      opts.Stderr,
		runID:       uuid.NewString(),
	}
	if (diag.textEnabled || diag.jsonEnabled) && diag.writer == nil {
		diag.writer = os.Stderr
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

	containerConfig := &container.Config{
		Image:           opts.Image,
		Cmd:             cmd,
		Tty:             false,
		AttachStdin:     opts.Stdin != nil,
		AttachStdout:    true,
		AttachStderr:    true,
		OpenStdin:       opts.Stdin != nil,
		StdinOnce:       opts.Stdin != nil,
		Env:             opts.Env,
		WorkingDir:      opts.WorkDir,
		User:            fmt.Sprintf("%s:%s", currentUser.Uid, currentUser.Gid),
		NetworkDisabled: opts.NoNetwork,
	}

	// Prepare host config with volume mounts and resource limits
	hostConfig := &container.HostConfig{
		AutoRemove:     false,
		ReadonlyRootfs: true,
		Tmpfs: map[string]string{
			"/tmp": "size=64m,noexec",
		},
	}

	applyResourceLimits(hostConfig, ResourcePolicy{
		Memory: opts.MemoryLimit,
		CPUs:   opts.CPULimit,
	})

	if len(opts.Volumes) > 0 {
		hostConfig.Binds = opts.Volumes
	}

	// Create container (but don't start it yet)
	containerName := fmt.Sprintf("tuprwre-%s", uuid.NewString()[:8])
	resp, err := d.client.ContainerCreate(ctx, containerConfig, hostConfig, nil, nil, containerName)
	if err != nil {
		return 1, fmt.Errorf("failed to create container: %w", err)
	}
	diag.containerID = resp.ID
	diag.event("create")

	var captureFile *os.File
	defer func() {
		if captureFile != nil {
			_ = captureFile.Close()
		}
		diag.event("cleanup")
		removeOptions := container.RemoveOptions{
			Force:         true,
			RemoveVolumes: true,
		}
		_ = d.client.ContainerRemove(context.Background(), resp.ID, removeOptions)
	}()

	stdout := opts.Stdout
	stderr := opts.Stderr
	if opts.CaptureFile != "" {
		captureFile, err = os.Create(opts.CaptureFile)
		if err != nil {
			return 1, fmt.Errorf("failed to create capture file: %w", err)
		}

		if stdout == nil {
			stdout = io.Discard
		}
		if stderr == nil {
			stderr = io.Discard
		}

		stdout = io.MultiWriter(stdout, captureFile)
		stderr = io.MultiWriter(stderr, captureFile)
	}

	return d.runAttachedAndDrain(ctx, resp.ID, opts.Stdin, stdout, stderr, diag)
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

func splitRepoTag(repoTag string) (string, string, bool) {
	if repoTag == "" || repoTag == "<none>:<none>" {
		return "", "", false
	}

	idx := strings.LastIndex(repoTag, ":")
	if idx <= 0 || idx >= len(repoTag)-1 {
		return "", "", false
	}

	return repoTag[:idx], repoTag[idx+1:], true
}

// Close closes the Docker client connection.
func (d *DockerRuntime) Close() error {
	if d.client != nil {
		return d.client.Close()
	}
	return nil
}
