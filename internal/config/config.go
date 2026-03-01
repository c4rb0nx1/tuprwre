// Package config provides configuration management for tuprwre.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

	// InterceptCommands lists commands that should be intercepted
	InterceptCommands []string

	// AllowCommands lists commands that should be allowed without interception
	AllowCommands []string

	// WorkspaceRoot is the discovered workspace root path
	WorkspaceRoot string
}

type fileConfig struct {
	Intercept []string `json:"intercept,omitempty"`
	Allow     []string `json:"allow,omitempty"`
	BaseImage string   `json:"base_image,omitempty"`
	Runtime   string   `json:"runtime,omitempty"`
}

var defaultBaseImage = "ubuntu:22.04"
var defaultRuntime = "docker"
var defaultInterceptCommands = []string{"apt", "apt-get", "npm", "pip", "pip3", "curl", "wget"}

func copySlice(values []string) []string {
	if len(values) == 0 {
		return nil
	}

	copied := make([]string, len(values))
	copy(copied, values)
	return copied
}

func expandTildePath(path string) string {
	if !strings.HasPrefix(path, "~/") {
		return path
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return path
	}

	return filepath.Join(homeDir, path[2:])
}

func loadConfigFile(path string) (*fileConfig, error) {
	configPath := expandTildePath(path)
	data, err := os.ReadFile(configPath)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var cfg fileConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func findWorkspaceRoot(startDir string) string {
	currentDir := filepath.Clean(startDir)
	for {
		if _, err := os.Stat(filepath.Join(currentDir, ".tuprwre", "config.json")); err == nil {
			return currentDir
		} else if !os.IsNotExist(err) {
			return ""
		}

		parentDir := filepath.Dir(currentDir)
		if parentDir == currentDir {
			break
		}
		currentDir = parentDir
	}

	return ""
}

func getEnvSlice(key string) []string {
	value, ok := os.LookupEnv(key)
	if !ok {
		return nil
	}
	if strings.TrimSpace(value) == "" {
		return nil
	}

	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
	}

	return out
}

func applyAllowExceptions(intercept, allow []string) []string {
	if len(intercept) == 0 {
		return intercept
	}

	allowed := make(map[string]struct{}, len(allow))
	for _, command := range allow {
		allowed[command] = struct{}{}
	}

	filtered := make([]string, 0, len(intercept))
	for _, command := range intercept {
		if _, ok := allowed[command]; ok {
			continue
		}
		filtered = append(filtered, command)
	}

	return filtered
}

// Load loads the configuration from layered sources.
func Load() (*Config, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("failed to get working directory: %w", err)
	}

	baseDir := os.Getenv("TUPRWRE_DIR")
	if baseDir == "" {
		baseDir = filepath.Join(homeDir, ".tuprwre")
	}

	globalConfig, err := loadConfigFile("~/.tuprwre/config.json")
	if err != nil {
		return nil, fmt.Errorf("failed to load global config: %w", err)
	}

	workspaceRoot := findWorkspaceRoot(cwd)
	workspaceConfig := (*fileConfig)(nil)
	if workspaceRoot != "" {
		workspaceConfig, err = loadConfigFile(filepath.Join(workspaceRoot, ".tuprwre", "config.json"))
		if err != nil {
			return nil, fmt.Errorf("failed to load workspace config: %w", err)
		}
	}

	cfg := &Config{
		BaseDir:           baseDir,
		ShimDir:           filepath.Join(baseDir, "bin"),
		ContainerDir:      filepath.Join(baseDir, "containers"),
		WorkspaceRoot:     workspaceRoot,
		DefaultBaseImage:  defaultBaseImage,
		ContainerRuntime:  defaultRuntime,
		InterceptCommands: copySlice(defaultInterceptCommands),
	}

	if globalConfig != nil {
		if globalConfig.BaseImage != "" {
			cfg.DefaultBaseImage = globalConfig.BaseImage
		}
		if globalConfig.Runtime != "" {
			cfg.ContainerRuntime = globalConfig.Runtime
		}
		if len(globalConfig.Intercept) > 0 {
			cfg.InterceptCommands = copySlice(globalConfig.Intercept)
		}
		if len(globalConfig.Allow) > 0 {
			cfg.AllowCommands = copySlice(globalConfig.Allow)
		}
	}

	if workspaceConfig != nil {
		if workspaceConfig.BaseImage != "" {
			cfg.DefaultBaseImage = workspaceConfig.BaseImage
		}
		if workspaceConfig.Runtime != "" {
			cfg.ContainerRuntime = workspaceConfig.Runtime
		}
		if len(workspaceConfig.Intercept) > 0 {
			cfg.InterceptCommands = copySlice(workspaceConfig.Intercept)
		}
		if len(workspaceConfig.Allow) > 0 {
			cfg.AllowCommands = copySlice(workspaceConfig.Allow)
		}
	}

	cfg.DefaultBaseImage = getEnv("TUPRWRE_BASE_IMAGE", cfg.DefaultBaseImage)
	cfg.ContainerRuntime = getEnv("TUPRWRE_RUNTIME", cfg.ContainerRuntime)

	envIntercept := getEnvSlice("TUPRWRE_INTERCEPT")
	if envIntercept != nil {
		cfg.InterceptCommands = envIntercept
	}

	cfg.InterceptCommands = applyAllowExceptions(cfg.InterceptCommands, cfg.AllowCommands)

	// Ensure required directories exist
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
