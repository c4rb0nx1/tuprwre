package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/c4rb0nx1/tuprwre/internal/config"
	"github.com/c4rb0nx1/tuprwre/internal/shim"
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

	if meta.InstallCommand == "" {
		return fmt.Errorf("metadata for shim %q is missing install command", shimName)
	}

	req := installRequest{
		installCommand: meta.InstallCommand,
		baseImage:      meta.BaseImage,
		imageName:      meta.OutputImage,
		force:          true,
	}
	out := cmd.OutOrStdout()
	_, _ = fmt.Fprintf(out, "Updating shim %q...\n", shimName)

	return installFlow(cmd, cfg, req)
}

