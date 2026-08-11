package main

import (
	"context"
	"encoding/json"
	"fmt"
	"text/tabwriter"
	"time"

	"github.com/c4rb0nx1/tuprwre/internal/config"
	"github.com/c4rb0nx1/tuprwre/internal/sandbox"
	"github.com/spf13/cobra"
)

var (
	poolStatusJSON bool
	poolGCAll      bool
)

var poolCmd = &cobra.Command{
	Use:   "pool",
	Short: "Manage warm sandbox containers",
}

var poolStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "List warm pool containers and their lease state",
	Args:  cobra.NoArgs,
	RunE:  runPoolStatus,
}

var poolGCCmd = &cobra.Command{
	Use:   "gc",
	Short: "Remove dead and idle-expired warm pool containers",
	Args:  cobra.NoArgs,
	RunE:  runPoolGC,
}

func init() {
	poolStatusCmd.Flags().BoolVar(&poolStatusJSON, "json", false, "Output as JSON")
	poolGCCmd.Flags().BoolVar(&poolGCAll, "all", false, "Remove all unleased pool containers, not just expired ones")
	poolCmd.AddCommand(poolStatusCmd)
	poolCmd.AddCommand(poolGCCmd)
}

func runPoolStatus(cmd *cobra.Command, _ []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	docker := sandbox.New(cfg)
	defer docker.Close()

	entries, err := docker.PoolStatus(context.Background())
	if err != nil {
		return fmt.Errorf("failed to inspect warm pool: %w", err)
	}

	out := cmd.OutOrStdout()

	if poolStatusJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(entries)
	}

	if len(entries) == 0 {
		_, _ = fmt.Fprintln(out, "No warm pool containers")
		return nil
	}

	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "CONTAINER\tIMAGE\tSTATE\tLEASED\tIDLE\tWORKSPACE")
	for _, e := range entries {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
			shortContainerID(e.ContainerID), e.Image, e.State, yesNo(e.Locked), formatIdle(e.IdleFor), e.WorkspaceRoot)
	}
	_ = tw.Flush()
	_, _ = fmt.Fprintf(out, "%d warm pool containers\n", len(entries))
	return nil
}

func runPoolGC(cmd *cobra.Command, _ []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	docker := sandbox.New(cfg)
	defer docker.Close()

	ctx := context.Background()

	var removed int
	if poolGCAll {
		removed, err = docker.PoolDrain(ctx)
	} else {
		removed, err = docker.PoolGC(ctx)
	}
	if err != nil {
		return fmt.Errorf("failed to garbage-collect warm pool: %w", err)
	}

	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Removed %d pool containers\n", removed)
	return nil
}

func shortContainerID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func formatIdle(d time.Duration) string {
	if d <= 0 {
		return "-"
	}
	return d.Truncate(time.Second).String()
}
