package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/yourusername/tuprwre/internal/config"
	"github.com/yourusername/tuprwre/internal/shim"
)

var removeCmd = &cobra.Command{
	Use:   "remove <shim>",
	Short: "Remove a generated shim",
	RunE:  runRemove,
}

func runRemove(cmd *cobra.Command, args []string) error {
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

	if err := shimGen.Remove(shimName); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("shim %q not found", shimName)
		}
		return fmt.Errorf("failed to remove shim %q: %w", shimName, err)
	}

	_ = shimGen.RemoveMetadata(shimName)

	out := cmd.OutOrStdout()
	_, _ = fmt.Fprintln(out, "Removed shim:", shimName)
	return nil
}

