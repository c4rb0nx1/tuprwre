package main

import (
	"fmt"
	"sort"

	"github.com/spf13/cobra"
	"github.com/c4rb0nx1/tuprwre/internal/config"
	"github.com/c4rb0nx1/tuprwre/internal/shim"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List installed shims",
	RunE:  runList,
}

func runList(cmd *cobra.Command, _ []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	shimGen := shim.NewGenerator(cfg)
	shims, err := shimGen.List()
	if err != nil {
		return fmt.Errorf("failed to list shims: %w", err)
	}

	out := cmd.OutOrStdout()
	if len(shims) == 0 {
		_, _ = fmt.Fprintln(out, "No shims installed.")
		return nil
	}

	sort.Strings(shims)
	for _, item := range shims {
		meta, err := shimGen.LoadMetadata(item)
		if err == nil && meta.Workspace != "" {
			_, _ = fmt.Fprintf(out, "%s  (workspace: %s)\n", item, meta.Workspace)
		} else {
			_, _ = fmt.Fprintln(out, item)
		}
	}

	return nil
}

