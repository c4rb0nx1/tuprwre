package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/yourusername/tuprwre/internal/config"
	"github.com/yourusername/tuprwre/internal/sandbox"
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
  tuprwre run --image kimi:latest -- kimi --version

  # With volume mounts
  tuprwre run --image kimi:latest -v $(pwd):/workspace -- kimi /workspace`,
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

	_ = runCmd.MarkFlagRequired("image")
}

func runSandboxed(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("no binary specified")
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
	volumes = append(volumes, fmt.Sprintf("%s:%s", cwd, cwd))

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
	}

	// Execute in sandbox
	exitCode, err := sb.Run(opts)
	if err != nil {
		return fmt.Errorf("sandbox execution failed: %w", err)
	}

	os.Exit(exitCode)
	return nil
}
