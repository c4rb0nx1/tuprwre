package main

import (
	"context"
	"fmt"
	"os"

	"github.com/c4rb0nx1/tuprwre/internal/config"
	"github.com/c4rb0nx1/tuprwre/internal/sandbox"
	"github.com/c4rb0nx1/tuprwre/internal/shim"
	"github.com/spf13/cobra"
)

var (
	removeAll    bool
	removeImages bool
)

var removeCmd = &cobra.Command{
	Use:   "remove [shim]",
	Short: "Remove a generated shim",
	RunE:  runRemove,
}

func init() {
	removeCmd.Flags().BoolVar(&removeAll, "all", false, "Remove all shims and metadata")
	removeCmd.Flags().BoolVar(&removeImages, "images", false, "Remove Docker images referenced by shim metadata")
}

func runRemove(cmd *cobra.Command, args []string) error {
	if removeAll {
		if len(args) > 0 {
			return fmt.Errorf("too many arguments; --all does not accept shim names")
		}
	} else {
		if len(args) == 0 {
			return fmt.Errorf("missing shim name")
		}
		if len(args) > 1 {
			return fmt.Errorf("too many arguments; expected one shim name")
		}
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	shimGen := shim.NewGenerator(cfg)
	out := cmd.OutOrStdout()

	if removeAll {
		imageSet := make(map[string]struct{})
		if removeImages {
			metadataList, err := shimGen.ListAllMetadata()
			if err != nil {
				return fmt.Errorf("failed to list metadata: %w", err)
			}
			for _, metadata := range metadataList {
				if metadata.OutputImage != "" {
					imageSet[metadata.OutputImage] = struct{}{}
				}
			}
		}

		shims, err := shimGen.List()
		if err != nil {
			return fmt.Errorf("failed to list shims: %w", err)
		}
		for _, shimName := range shims {
			if removeImages {
				metadata, err := shimGen.LoadMetadata(shimName)
				if err == nil && metadata.OutputImage != "" {
					imageSet[metadata.OutputImage] = struct{}{}
				}
			}

			if err := shimGen.Remove(shimName); err != nil {
				return fmt.Errorf("failed to remove shim %q: %w", shimName, err)
			}
			_ = shimGen.RemoveMetadata(shimName)
		}

		if _, err := shimGen.RemoveAllMetadata(); err != nil {
			return fmt.Errorf("failed to remove metadata: %w", err)
		}

		if len(shims) == 0 {
			_, _ = fmt.Fprintln(out, "No shims installed")
			return nil
		}

		_, _ = fmt.Fprintf(out, "Removed %d shims\n", len(shims))
		if removeImages {
			removedCount := 0
			failedCount := 0
			docker := sandbox.New(cfg)
			defer docker.Close()
			for imageName := range imageSet {
				if err := docker.RemoveImage(context.Background(), imageName); err != nil {
					failedCount++
					_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Warning: failed to remove image %q: %v\n", imageName, err)
					continue
				}
				removedCount++
			}
			_, _ = fmt.Fprintf(out, "Removed %d images (%d failed)\n", removedCount, failedCount)
		}
		return nil
	}

	shimName := args[0]
	imageName := ""
	if removeImages {
		metadata, err := shimGen.LoadMetadata(shimName)
		if err == nil {
			imageName = metadata.OutputImage
		}
	}

	if err := shimGen.Remove(shimName); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("shim %q not found", shimName)
		}
		return fmt.Errorf("failed to remove shim %q: %w", shimName, err)
	}

	_ = shimGen.RemoveMetadata(shimName)

	if removeImages && imageName != "" {
		docker := sandbox.New(cfg)
		defer docker.Close()
		if err := docker.RemoveImage(context.Background(), imageName); err != nil {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Warning: failed to remove image %q: %v\n", imageName, err)
		}
	}

	_, _ = fmt.Fprintln(out, "Removed shim:", shimName)
	return nil
}
