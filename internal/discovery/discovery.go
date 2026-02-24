// Package discovery provides binary discovery and diffing capabilities.
package discovery

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/yourusername/tuprwre/internal/config"
	"github.com/yourusername/tuprwre/internal/sandbox"
)

// Binary represents a discovered executable binary.
type Binary struct {
	// Name is the binary name (e.g., "kimi")
	Name string

	// Path is the full path inside the container (e.g., "/usr/local/bin/kimi")
	Path string

	// Version is the detected version (if available)
	Version string
}

// Discoverer handles binary discovery in containers.
type Discoverer struct {
	config  *config.Config
	sandbox *sandbox.DockerRuntime
}

// New creates a new Discoverer instance.
func New(cfg *config.Config, sb *sandbox.DockerRuntime) *Discoverer {
	return &Discoverer{
		config:  cfg,
		sandbox: sb,
	}
}

// DiscoverBinaries finds new binaries installed in a container.
// It compares the current state against the base image to identify
// newly added executables.
func (d *Discoverer) DiscoverBinaries(baseImage, newImage string) ([]Binary, error) {
	ctx := context.Background()

	// Phase 1: Get baseline from base image
	baselinePaths, err := d.sandbox.ListImageExecutables(ctx, baseImage)
	if err != nil {
		return nil, fmt.Errorf("failed to list baseline executables: %w", err)
	}

	// Phase 2: Get current executables from new image
	currentPaths, err := d.sandbox.ListImageExecutables(ctx, newImage)
	if err != nil {
		return nil, fmt.Errorf("failed to list current executables: %w", err)
	}

	// Phase 3: Diff to find new binaries (Current - Baseline)
	newPaths := difference(currentPaths, baselinePaths)
	// Convert to Binary structs
	var binaries []Binary
	for _, path := range newPaths {
		binary := Binary{
			Name: extractNameFromPath(path),
			Path: path,
		}
		binaries = append(binaries, binary)
	}

	// Phase 4: Filter out system binaries
	binaries = d.FilterSystemBinaries(binaries)

	return binaries, nil
}

// DiscoverFromFilesystemDiff compares filesystem states to find new binaries.
// This is an alternative approach that diffs the entire filesystem.
func (d *Discoverer) DiscoverFromFilesystemDiff(containerID, baseImage string) ([]Binary, error) {
	// TODO: Implement filesystem-level diffing
	// 1. Get base image filesystem
	// 2. Get container filesystem
	// 3. Find new executable files in common bin directories
	// 4. Filter out system packages
	return nil, fmt.Errorf("filesystem diff not implemented")
}

// GetBinaryVersion attempts to detect the version of a binary.
func (d *Discoverer) GetBinaryVersion(binaryPath, containerID string) (string, error) {
	// TODO: Try common version flags: --version, -v, -V, version
	return "", nil
}

// FilterSystemBinaries removes common system binaries from the list.
func (d *Discoverer) FilterSystemBinaries(binaries []Binary) []Binary {
	systemBins := map[string]bool{
		"sh": true, "bash": true, "zsh": true,
		"ls": true, "cat": true, "grep": true,
		"awk": true, "sed": true, "curl": true,
		"wget": true, "tar": true, "gzip": true,
	}

	var filtered []Binary
	for _, b := range binaries {
		if !systemBins[b.Name] {
			filtered = append(filtered, b)
		}
	}
	return filtered
}

func extractNameFromPath(path string) string {
	return filepath.Base(path)
}

// difference returns elements in current that are not in baseline (set difference: current - baseline)
func difference(current, baseline []string) []string {
	// Create lookup map for baseline
	baselineMap := make(map[string]struct{}, len(baseline))
	for _, b := range baseline {
		baselineMap[b] = struct{}{}
	}

	// Filter current to find new elements
	var diff []string
	for _, c := range current {
		if _, exists := baselineMap[c]; !exists {
			diff = append(diff, c)
		}
	}
	return diff
}
