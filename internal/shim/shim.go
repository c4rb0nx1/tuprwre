// Package shim provides shim script generation for transparent execution.
package shim

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/yourusername/tuprwre/internal/config"
	"github.com/yourusername/tuprwre/internal/discovery"
)

// Generator creates shim scripts for sandboxed binaries.
type Generator struct {
	config *config.Config
}

// shimTemplate is the bash script template for proxying to Docker.
const shimTemplate = `#!/bin/bash
# Generated shim for {{.BinaryName}}
# Proxies execution to sandboxed container: {{.ImageName}}

set -e

# tuprwre run configuration
IMAGE_NAME="{{.ImageName}}"
BINARY_NAME="{{.BinaryName}}"

# Forward all arguments to the sandboxed binary
exec tuprwre run --image "${IMAGE_NAME}" -- "${BINARY_NAME}" "$@"
`

// containerdShimTemplate is the future template for containerd runtime.
const containerdShimTemplate = `#!/bin/bash
# Generated shim for {{.BinaryName}}
# Proxies execution to sandboxed container via containerd: {{.ImageName}}

set -e

# tuprwre run configuration with containerd
IMAGE_NAME="{{.ImageName}}"
BINARY_NAME="{{.BinaryName}}"

# Forward all arguments to the sandboxed binary
exec tuprwre run --runtime containerd --image "${IMAGE_NAME}" -- "${BINARY_NAME}" "$@"
`

type shimData struct {
	BinaryName string
	ImageName  string
}

// NewGenerator creates a new shim Generator.
func NewGenerator(cfg *config.Config) *Generator {
	return &Generator{
		config: cfg,
	}
}

// Create generates a shim script for the given binary.
func (g *Generator) Create(binary discovery.Binary, imageName string, force bool) error {
	shimPath := filepath.Join(g.config.ShimDir, binary.Name)

	// Check if shim already exists
	if _, err := os.Stat(shimPath); err == nil && !force {
		return fmt.Errorf("shim already exists: %s (use --force to overwrite)", shimPath)
	}

	// Determine which template to use
	tmplStr := shimTemplate
	if g.config.ContainerRuntime == "containerd" {
		tmplStr = containerdShimTemplate
	}

	// Parse template
	tmpl, err := template.New("shim").Parse(tmplStr)
	if err != nil {
		return fmt.Errorf("failed to parse shim template: %w", err)
	}

	// Create shim file
	file, err := os.OpenFile(shimPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return fmt.Errorf("failed to create shim file: %w", err)
	}
	defer file.Close()

	// Execute template
	data := shimData{
		BinaryName: binary.Name,
		ImageName:  imageName,
	}
	if err := tmpl.Execute(file, data); err != nil {
		return fmt.Errorf("failed to write shim: %w", err)
	}

	return nil
}

// Remove deletes a shim script.
func (g *Generator) Remove(binaryName string) error {
	shimPath := filepath.Join(g.config.ShimDir, binaryName)
	return os.Remove(shimPath)
}

// List returns all existing shim scripts.
func (g *Generator) List() ([]string, error) {
	entries, err := os.ReadDir(g.config.ShimDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read shim directory: %w", err)
	}

	var shims []string
	for _, entry := range entries {
		if !entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") {
			shims = append(shims, entry.Name())
		}
	}
	return shims, nil
}

// GetPath returns the full path to a shim.
func (g *Generator) GetPath(binaryName string) string {
	return filepath.Join(g.config.ShimDir, binaryName)
}

// ValidateShimDir ensures the shim directory is in the user's PATH.
func (g *Generator) ValidateShimDir() error {
	path := os.Getenv("PATH")
	if !strings.Contains(path, g.config.ShimDir) {
		return fmt.Errorf("shim directory %s is not in PATH. Add:\n  export PATH=\"$HOME/.tuprwre/bin:$PATH\"", g.config.ShimDir)
	}
	return nil
}
