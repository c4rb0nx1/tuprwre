package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/c4rb0nx1/tuprwre/internal/config"
	"github.com/spf13/cobra"
)

var initGlobal bool

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize a tuprwre config file",
	Long: `Creates a .tuprwre/config.json in the current directory (workspace mode)
or ~/.tuprwre/config.json (global mode with --global).

The config file controls which commands are intercepted in tuprwre shell,
the default base image, and other settings.`,
	RunE: runInit,
}

type initConfig struct {
	Intercept []string `json:"intercept"`
	Allow     []string `json:"allow"`
	BaseImage string   `json:"base_image"`
	Runtime   string   `json:"runtime"`
}

func init() {
	initCmd.Flags().BoolVar(&initGlobal, "global", false, "Create config in ~/.tuprwre/ instead of current directory")
}

func runInit(cmd *cobra.Command, args []string) error {
	var configDir string

	if initGlobal {
		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}
		configDir = cfg.BaseDir
	} else {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get working directory: %w", err)
		}
		configDir = filepath.Join(cwd, ".tuprwre")
	}

	configPath := filepath.Join(configDir, "config.json")

	// Check if config already exists
	if _, err := os.Stat(configPath); err == nil {
		return fmt.Errorf("config already exists: %s", configPath)
	}

	// Create directory
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// Write default config
	defaultCfg := initConfig{
		Intercept: []string{"apt", "apt-get", "npm", "pip", "pip3", "curl", "wget"},
		Allow:     []string{},
		BaseImage: "ubuntu:22.04",
		Runtime:   "docker",
	}

	payload, err := json.MarshalIndent(defaultCfg, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(configPath, append(payload, '\n'), 0o644); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	cmd.Printf("Created config: %s\n", configPath)
	return nil
}
