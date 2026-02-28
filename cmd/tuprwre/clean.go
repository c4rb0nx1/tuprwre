package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/c4rb0nx1/tuprwre/internal/config"
	"github.com/c4rb0nx1/tuprwre/internal/sandbox"
	"github.com/docker/go-units"
	"github.com/spf13/cobra"
)

var (
	cleanForce  bool
	cleanDryRun bool
)

var cleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "Remove orphaned Docker images created by tuprwre",
	Args:  cobra.NoArgs,
	RunE:  runClean,
}

func init() {
	cleanCmd.Flags().BoolVar(&cleanForce, "force", false, "Skip confirmation prompt")
	cleanCmd.Flags().BoolVar(&cleanDryRun, "dry-run", false, "List images without removing")
}

func runClean(cmd *cobra.Command, _ []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	docker := sandbox.New(cfg)
	defer docker.Close()

	ctx := context.Background()
	stoppedContainers, err := docker.ListStoppedTuprwreContainers(ctx)
	if err != nil {
		return fmt.Errorf("failed to list stopped containers: %w", err)
	}

	if len(stoppedContainers) > 0 {
		stderr := cmd.ErrOrStderr()
		_, _ = fmt.Fprintf(stderr, "Found %d stopped tuprwre containers\n", len(stoppedContainers))

		removedContainers := 0
		failedContainers := 0
		for _, container := range stoppedContainers {
			if err := docker.RemoveContainer(ctx, container.ID); err != nil {
				failedContainers++
				_, _ = fmt.Fprintf(stderr, "Warning: failed to remove container %s: %v\n", container.Name, err)
				continue
			}
			removedContainers++
			_, _ = fmt.Fprintf(stderr, "Removed container %s\n", container.Name)
		}
		_, _ = fmt.Fprintf(stderr, "Cleaned up %d containers (%d failed)\n", removedContainers, failedContainers)
	}

	images, err := docker.ListTuprwreImages(ctx)
	if err != nil {
		return fmt.Errorf("failed to list tuprwre images: %w", err)
	}

	out := cmd.OutOrStdout()
	if len(images) == 0 {
		_, _ = fmt.Fprintln(out, "No tuprwre images found")
		return nil
	}

	var totalSize int64
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	for _, image := range images {
		totalSize += image.Size
		_, _ = fmt.Fprintf(tw, "%s:%s\t%s\t%s\n", image.Repository, image.Tag, units.HumanSize(float64(image.Size)), formatRelativeAge(image.Created))
	}
	_ = tw.Flush()
	_, _ = fmt.Fprintf(out, "Found %d tuprwre images (%s total)\n", len(images), units.HumanSizeWithPrecision(float64(totalSize), 1))

	if cleanDryRun {
		_, _ = fmt.Fprintln(out, "Dry run — no images removed")
		return nil
	}

	if !cleanForce {
		stderr := cmd.ErrOrStderr()
		_, _ = fmt.Fprint(stderr, "Remove these images? [y/N]: ")
		reader := bufio.NewReader(os.Stdin)
		confirmation, err := reader.ReadString('\n')
		if err != nil && err != io.EOF {
			return fmt.Errorf("failed to read confirmation: %w", err)
		}

		confirmation = strings.TrimSpace(confirmation)
		if confirmation != "y" && confirmation != "Y" {
			_, _ = fmt.Fprintln(stderr, "Aborted")
			return nil
		}
	}

	removedCount := 0
	failedCount := 0
	for _, image := range images {
		if err := docker.RemoveImage(ctx, image.ID); err != nil {
			failedCount++
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Warning: failed to remove image %s:%s: %v\n", image.Repository, image.Tag, err)
			continue
		}
		removedCount++
		_, _ = fmt.Fprintf(out, "Removed %s:%s\n", image.Repository, image.Tag)
	}

	_, _ = fmt.Fprintf(out, "Removed %d images (%d failed)\n", removedCount, failedCount)
	return nil
}

func formatRelativeAge(createdUnix int64) string {
	if createdUnix <= 0 {
		return "unknown"
	}

	elapsed := time.Since(time.Unix(createdUnix, 0))
	if elapsed < 0 {
		elapsed = 0
	}
	if elapsed < time.Minute {
		return "just now"
	}
	if elapsed < time.Hour {
		minutes := int(elapsed.Minutes())
		if minutes == 1 {
			return "1 minute ago"
		}
		return fmt.Sprintf("%d minutes ago", minutes)
	}
	if elapsed < 24*time.Hour {
		hours := int(elapsed.Hours())
		if hours == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", hours)
	}

	days := int(elapsed.Hours() / 24)
	if days == 1 {
		return "1 day ago"
	}
	return fmt.Sprintf("%d days ago", days)
}
