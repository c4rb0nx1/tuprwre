// Package config provides configuration management for tuprwre.
package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// Config holds the application configuration.
type Config struct {
	// BaseDir is the root directory for tuprwre data
	BaseDir string

	// ShimDir is where shim scripts are generated
	ShimDir string

	// ContainerDir stores container state information
	ContainerDir string

	// DefaultBaseImage is the default Docker image for new containers
	DefaultBaseImage string

	// ContainerRuntime specifies the runtime to use (docker, containerd)
	ContainerRuntime string
}

// Load loads the configuration from environment and defaults.
func Load() (*Config, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	baseDir := os.Getenv("TUPRWRE_DIR")
	if baseDir == "" {
		baseDir = filepath.Join(homeDir, ".tuprwre")
	}

	cfg := &Config{
		BaseDir:          baseDir,
		ShimDir:          filepath.Join(baseDir, "bin"),
		ContainerDir:     filepath.Join(baseDir, "containers"),
		DefaultBaseImage: getEnv("TUPRWRE_BASE_IMAGE", "ubuntu:22.04"),
		ContainerRuntime: getEnv("TUPRWRE_RUNTIME", "docker"),
	}

	// Ensure directories exist
	for _, dir := range []string{cfg.BaseDir, cfg.ShimDir, cfg.ContainerDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	return cfg, nil
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
