package main

import (
	"fmt"
	"sort"

	"github.com/c4rb0nx1/tuprwre/internal/config"
	"github.com/c4rb0nx1/tuprwre/internal/shim"
	"github.com/spf13/cobra"
)

var (
	listWorkspace bool
	listGlobal    bool
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List installed shims",
	RunE:  runList,
}

func init() {
	listCmd.Flags().BoolVar(&listWorkspace, "workspace", false, "Show only shims installed in the current workspace")
	listCmd.Flags().BoolVar(&listGlobal, "global", false, "Show only globally installed shims")
	listCmd.MarkFlagsMutuallyExclusive("workspace", "global")
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

	sort.Strings(shims)

	var filtered []string
	type shimInfo struct {
		workspace string
	}
	info := map[string]shimInfo{}

	for _, item := range shims {
		meta, metaErr := shimGen.LoadMetadata(item)
		ws := ""
		if metaErr == nil {
			ws = meta.Workspace
		}

		if listWorkspace {
			if ws == "" || ws != cfg.WorkspaceRoot {
				continue
			}
		} else if listGlobal {
			if ws != "" {
				continue
			}
		}

		filtered = append(filtered, item)
		info[item] = shimInfo{workspace: ws}
	}

	if len(filtered) == 0 {
		_, _ = fmt.Fprintln(out, "No shims installed.")
		return nil
	}

	for _, item := range filtered {
		si := info[item]
		if si.workspace != "" {
			_, _ = fmt.Fprintf(out, "%s  (workspace: %s)\n", item, si.workspace)
		} else {
			_, _ = fmt.Fprintln(out, item)
		}
	}

	return nil
}

