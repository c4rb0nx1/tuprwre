package main

import (
	"fmt"
	"os"

	"github.com/c4rb0nx1/tuprwre/internal/config"
	"github.com/c4rb0nx1/tuprwre/internal/shim"
	"github.com/spf13/cobra"
)

var updateCmd = &cobra.Command{
	Use:   "update <shim>",
	Short: "Re-run install for a shim using stored metadata",
	RunE:  runUpdate,
}

func runUpdate(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("missing shim name")
	}
	if len(args) > 1 {
		return fmt.Errorf("too many arguments; expected one shim name")
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	shimName := args[0]
	shimGen := shim.NewGenerator(cfg)
	meta, err := shimGen.LoadMetadata(shimName)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("shim %q is missing lifecycle metadata (legacy shim). Reinstall using `tuprwre install -- <command>` before updating", shimName)
		}
		return fmt.Errorf("failed to load metadata for shim %q: %w", shimName, err)
	}

	var req installRequest
	req.baseImage = meta.BaseImage
	req.imageName = meta.OutputImage
	req.force = true

	switch meta.InstallMode {
	case "script":
		if meta.InstallScriptPath == "" {
			return fmt.Errorf("metadata for shim %q is missing script path", shimName)
		}
		content, err := os.ReadFile(meta.InstallScriptPath)
		if err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("stored script for shim %q is unavailable: script file not found: %s", shimName, meta.InstallScriptPath)
			}
			return fmt.Errorf("stored script for shim %q is unavailable: failed to read script %s: %w", shimName, meta.InstallScriptPath, err)
		}
		req.installScriptPath = meta.InstallScriptPath
		req.installScriptContent = content
		req.installScriptArgs = meta.InstallScriptArgs
	case "", "command":
		if meta.InstallCommand == "" {
			return fmt.Errorf("metadata for shim %q is missing install command", shimName)
		}
		req.installCommand = meta.InstallCommand
	default:
		if meta.InstallCommand != "" {
			req.installCommand = meta.InstallCommand
			break
		}
		if meta.InstallScriptPath != "" {
			content, err := os.ReadFile(meta.InstallScriptPath)
			if err != nil {
				if os.IsNotExist(err) {
					return fmt.Errorf("stored script for shim %q is unavailable: script file not found: %s", shimName, meta.InstallScriptPath)
				}
				return fmt.Errorf("stored script for shim %q is unavailable: failed to read script %s: %w", shimName, meta.InstallScriptPath, err)
			}
			req.installScriptPath = meta.InstallScriptPath
			req.installScriptContent = content
			req.installScriptArgs = meta.InstallScriptArgs
			break
		}
		return fmt.Errorf("metadata for shim %q is missing install source", shimName)
	}
	out := cmd.OutOrStdout()
	_, _ = fmt.Fprintf(out, "Updating shim %q...\n", shimName)

	return installFlow(cmd, cfg, req)
}
