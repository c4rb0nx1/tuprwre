package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/c4rb0nx1/tuprwre/internal/config"
	"github.com/c4rb0nx1/tuprwre/internal/discovery"
	"github.com/c4rb0nx1/tuprwre/internal/sandbox"
	"github.com/c4rb0nx1/tuprwre/internal/shim"
	"github.com/spf13/cobra"
)

var (
	installBaseImage   string
	installContainerID string
	installImageName   string
	installForce       bool
	installScriptPath  string
	installArgsReader  = func() []string { return os.Args }
)

type installRequest struct {
	installCommand       string
	baseImage            string
	containerID          string
	imageName            string
	force                bool
	installScriptPath    string
	installScriptContent []byte
	installScriptArgs    []string
}

var installFlow = runInstallFlow

var installCmd = &cobra.Command{
	Use:   "install [flags] -- <command>",
	Short: "Run an installation script in a sandbox and generate shims",
	Long: `Executes the provided command inside an isolated Docker container,
commits the container state, discovers newly installed binaries, and
generates shim scripts on the host for transparent execution.

Workflow:
- Ephemeral container spin-up with the provided base image
- Execute the installation command
- Commit the container state to a new image
- Discover new binaries by diffing PATH or filesystem
- Generate shim scripts in ~/.tuprwre/bin/`,
	Example: `  # Install from a curl script
	  tuprwre install --base-image ubuntu:22.04 -- \
	    "curl -fsSL https://example.com/install-tool.sh | bash"

	  # Install with specific output image name
	  tuprwre install --base-image alpine:latest --image toolset:latest -- \
	    "wget -qO- https://example.com/install-tool.sh | sh"`,
	RunE: runInstall,
}

func init() {
	installCmd.Flags().StringVarP(&installBaseImage, "base-image", "i", "ubuntu:22.04", "Base Docker image to use for the sandbox")
	installCmd.Flags().StringVarP(&installContainerID, "container", "c", "", "Resume from an already-prepared container ID (advanced recovery/debugging flow).")
	installCmd.Flags().StringVarP(&installImageName, "image", "n", "", "Name for the committed image (auto-generated if not provided)")
	installCmd.Flags().StringVarP(&installScriptPath, "script", "s", "", "Path to a local shell script")
	installCmd.Flags().BoolVarP(&installForce, "force", "f", false, "Overwrite existing shims")
}

func runInstall(cmd *cobra.Command, args []string) error {
	req, err := resolveInstallRequest(args, installArgsReader())
	if err != nil {
		return err
	}

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	return installFlow(cmd, cfg, installRequest{
		installCommand:       req.installCommand,
		baseImage:            installBaseImage,
		containerID:          installContainerID,
		imageName:            installImageName,
		force:                installForce,
		installScriptPath:    req.installScriptPath,
		installScriptContent: req.installScriptContent,
		installScriptArgs:    req.installScriptArgs,
	})
}

func resolveInstallRequest(argsFromCobra []string, argv []string) (*installRequest, error) {
	if installScriptPath != "" {
		absScriptPath, err := filepath.Abs(installScriptPath)
		if err != nil {
			return nil, fmt.Errorf("script file not found: %s", installScriptPath)
		}

		content, err := os.ReadFile(absScriptPath)
		if err != nil {
			if os.IsNotExist(err) {
				return nil, fmt.Errorf("script file not found: %s", absScriptPath)
			}
			return nil, fmt.Errorf("failed to read script %s: %w", absScriptPath, err)
		}

		return &installRequest{
			installScriptPath:    absScriptPath,
			installScriptContent: content,
			installScriptArgs:    argsFromCobra,
		}, nil
	}

	installCommand, err := resolveInstallCommand(argsFromCobra, argv)
	if err != nil {
		return nil, err
	}

	return &installRequest{
		installCommand: installCommand,
	}, nil
}

func buildScriptInstallCommand(scriptContent []byte, args []string) string {
	encoded := base64.StdEncoding.EncodeToString(scriptContent)
	if len(args) == 0 {
		return fmt.Sprintf("printf '%s' | base64 -d | sh -s --", encoded)
	}

	quotedArgs := make([]string, 0, len(args))
	for _, arg := range args {
		quotedArgs = append(quotedArgs, strconv.Quote(arg))
	}
	return fmt.Sprintf("printf '%s' | base64 -d | sh -s -- %s", encoded, strings.Join(quotedArgs, " "))
}

func resolveInstallCommand(argsFromCobra []string, argv []string) (string, error) {
	if command, ok, err := parseInstallCommandFromArgv(argv); err != nil {
		return "", err
	} else if ok && command != "" {
		return command, nil
	}

	if len(argsFromCobra) == 0 {
		return "", fmt.Errorf("no installation command provided")
	}
	return strings.Join(argsFromCobra, " "), nil
}

func parseInstallCommandFromArgv(argv []string) (string, bool, error) {
	installIndex := -1
	for i, arg := range argv {
		if arg == "install" {
			installIndex = i
			break
		}
	}

	if installIndex == -1 {
		return "", false, nil
	}

	for i := installIndex + 1; i < len(argv); i++ {
		if argv[i] == "--" {
			if i+1 >= len(argv) {
				return "", false, fmt.Errorf("no installation command provided")
			}
			return strings.Join(argv[i+1:], " "), true, nil
		}
	}

	return "", false, nil
}

func runInstallFlow(cmd *cobra.Command, cfg *config.Config, req installRequest) error {
	installCommand := req.installCommand
	if req.installScriptPath != "" {
		scriptContent := req.installScriptContent
		if scriptContent == nil {
			content, err := os.ReadFile(req.installScriptPath)
			if err != nil {
				if os.IsNotExist(err) {
					return fmt.Errorf("script file not found: %s", req.installScriptPath)
				}
				return fmt.Errorf("failed to read script %s: %w", req.installScriptPath, err)
			}
			scriptContent = content
		}
		installCommand = buildScriptInstallCommand(scriptContent, req.installScriptArgs)
	}

	// Create Docker runtime
	docker := sandbox.New(cfg)
	defer docker.Close()

	ctx := context.Background()
	var containerID string
	var err error

	if req.containerID != "" {
		// Use existing container
		containerID = req.containerID
		fmt.Printf("Using existing container: %s\n", containerID)
	} else {
		// Create and run container with installation command
		fmt.Printf("Creating sandbox container from image: %s\n", req.baseImage)
		fmt.Printf("Running installation command...\n\n")

		containerID, err = docker.CreateAndRunContainer(ctx, req.baseImage, installCommand)

		// ALWAYS cleanup the container we just created, regardless of success/fail
		defer func() {
			if containerID != "" {
				docker.CleanupContainer(context.Background(), containerID)
			}
		}()
		if err != nil {
			return fmt.Errorf("installation failed: %w", err)
		}
		fmt.Printf("\nContainer finished successfully: %s\n", containerID)
	}

	// Commit container state
	imageName := req.imageName
	if imageName == "" {
		imageName = docker.GenerateImageName()
	}

	fmt.Printf("Committing container to image: %s\n", imageName)
	if err := docker.Commit(ctx, containerID, imageName); err != nil {
		return fmt.Errorf("failed to commit container: %w", err)
	}

	// Discover binaries
	fmt.Printf("Discovering installed binaries...\n")
	disc := discovery.New(cfg, docker)
	binaries, err := disc.DiscoverBinaries(req.baseImage, imageName)
	if err != nil {
		return fmt.Errorf("failed to discover binaries: %w", err)
	}

	cmd.Printf("Discovered %d new binaries\n", len(binaries))

	// Generate shims
	if len(binaries) > 0 {
		fmt.Printf("Generating shim scripts...\n")
		shimGen := shim.NewGenerator(cfg)
		for _, binary := range binaries {
			if err := shimGen.Create(binary, imageName, req.force); err != nil {
				cmd.Printf("Warning: failed to create shim for %s: %v\n", binary.Name, err)
				continue
			}

			metadata := shim.Metadata{
				BinaryName:        binary.Name,
				InstallMode:       metadataInstallMode(&req),
				InstallCommand:    req.installCommand,
				InstallScriptPath: req.installScriptPath,
				InstallScriptArgs: req.installScriptArgs,
				BaseImage:         req.baseImage,
				OutputImage:       imageName,
				InstalledAt:       time.Now().UTC().Format(time.RFC3339),
				InstallForceUsed:  req.force,
			}
			if err := shimGen.SaveMetadata(metadata); err != nil {
				cmd.Printf("Warning: failed to persist metadata for %s: %v\n", binary.Name, err)
			} else {
				cmd.Printf("Created shim: %s\n", binary.Name)
			}
		}
	}

	cmd.Printf("\nInstallation complete! Add %s to your PATH.\n", cfg.ShimDir)
	cmd.Printf("Run: export PATH=\"%s:$PATH\"\n", cfg.ShimDir)
	return nil
}

func metadataInstallMode(req *installRequest) string {
	if req == nil || req.installScriptPath == "" {
		return "command"
	}
	return "script"
}
