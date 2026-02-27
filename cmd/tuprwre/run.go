package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/docker/go-units"
	"github.com/spf13/cobra"
	"github.com/c4rb0nx1/tuprwre/internal/config"
	"github.com/c4rb0nx1/tuprwre/internal/sandbox"
)

var (
	runContainerImage string
	runContainerID    string
	runWorkDir        string
	runEnv            []string
	runVolumes        []string
	runDebugIO        bool
	runDebugIOJSON    bool
	runCaptureFile    string
	runReadOnlyCwd    bool
	runNoNetwork      bool
	runMemoryLimit    string
	runCPULimit       float64
	// For Containerd migration (future)
	runRuntime string
)

var runCmd = &cobra.Command{
	Use:   "run [flags] -- <binary> [args...]",
	Short: "Execute a command inside a sandboxed container (used by shims)",
	Long: `Executes a binary inside a sandboxed Docker container.
This command is typically invoked by generated shim scripts and should
not be called directly by users in most cases.

The shim passes through:
  - stdin/stdout/stderr streams
  - All command-line arguments ($@)
  - Current working directory context
  - Selected environment variables`,
	Example: `  # Internal usage by shims:
	  tuprwre run --image toolset:latest -- tool --version

	  # With volume mounts
	  tuprwre run --image toolset:latest -v $(pwd):/workspace -- tool /workspace`,
	RunE: runSandboxed,
}

func init() {
	runCmd.Flags().StringVarP(&runContainerImage, "image", "i", "", "Docker image to run (required)")
	runCmd.Flags().StringVarP(&runContainerID, "container", "c", "", "Existing container to use instead of image")
	runCmd.Flags().StringVarP(&runWorkDir, "workdir", "w", "", "Working directory inside container (default: current directory)")
	runCmd.Flags().StringArrayVarP(&runEnv, "env", "e", []string{}, "Environment variables to pass (KEY=VALUE)")
	runCmd.Flags().StringArrayVarP(&runVolumes, "volume", "v", []string{}, "Volume mounts (host:container)")
	runCmd.Flags().StringVarP(&runRuntime, "runtime", "r", "docker", "Container runtime (docker|containerd)")
	runCmd.Flags().BoolVar(&runDebugIO, "debug-io", false, "Print human-readable container I/O lifecycle diagnostics")
	runCmd.Flags().BoolVar(&runDebugIOJSON, "debug-io-json", false, "Emit container I/O diagnostics as NDJSON (optional JSON mode)")
	runCmd.Flags().StringVar(&runCaptureFile, "capture-file", "", "Write combined stdout/stderr stream to a file")
	runCmd.Flags().BoolVar(&runReadOnlyCwd, "read-only-cwd", false, "Mount working directory as read-only inside the container")
	runCmd.Flags().BoolVar(&runNoNetwork, "no-network", false, "Disable network access inside the container")
	runCmd.Flags().StringVar(&runMemoryLimit, "memory", "", "Memory limit for the container (e.g. 512m, 1g)")
	runCmd.Flags().Float64Var(&runCPULimit, "cpus", 0, "CPU limit for the container (e.g. 0.5, 1.0, 2.0)")

	_ = runCmd.MarkFlagRequired("image")
}

func runSandboxed(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("no binary specified")
	}
	if err := validateRunRuntime(runRuntime); err != nil {
		return err
	}

	binaryName := args[0]
	binaryArgs := args[1:]

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Setup sandbox
	sb := sandbox.New(cfg)

	// Get current working directory for host mount
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current working directory: %w", err)
	}

	// Determine working directory (use provided or default to cwd)
	workDir := runWorkDir
	if workDir == "" {
		workDir = cwd
	}

	// Build volume mounts: always mount current directory for host file access
	volumes := append([]string{}, runVolumes...)
	cwdMount := fmt.Sprintf("%s:%s", cwd, cwd)
	if runReadOnlyCwd {
		cwdMount += ":ro"
	}
	volumes = append(volumes, cwdMount)

	// Parse memory limit if provided
	var memoryLimit int64
	if runMemoryLimit != "" {
		parsed, err := units.RAMInBytes(runMemoryLimit)
		if err != nil {
			return fmt.Errorf("invalid memory limit %q: %w", runMemoryLimit, err)
		}
		memoryLimit = parsed
	}

	// Build run options
	opts := sandbox.RunOptions{
		Image:       runContainerImage,
		ContainerID: runContainerID,
		Binary:      binaryName,
		Args:        binaryArgs,
		WorkDir:     workDir,
		Env:         runEnv,
		Volumes:     volumes,
		Runtime:     runRuntime,
		Stdin:       os.Stdin,
		Stdout:      os.Stdout,
		Stderr:      os.Stderr,
		DebugIO:     runDebugIO,
		DebugIOJSON: runDebugIOJSON,
		CaptureFile: runCaptureFile,
		ReadOnlyCwd: runReadOnlyCwd,
		NoNetwork:   runNoNetwork,
		MemoryLimit: memoryLimit,
		CPULimit:    runCPULimit,
	}

	// Execute in sandbox
	exitCode, err := sb.Run(opts)
	if err != nil {
		return fmt.Errorf("sandbox execution failed: %w", err)
	}

	os.Exit(exitCode)
	return nil
}

func validateRunRuntime(runtime string) error {
	switch strings.ToLower(strings.TrimSpace(runtime)) {
	case "docker":
		return nil
	case "containerd":
		return fmt.Errorf("runtime %q is not implemented yet in run path", runtime)
	default:
		return fmt.Errorf("runtime %q is not supported (supported: docker, containerd)", runtime)
	}
}
