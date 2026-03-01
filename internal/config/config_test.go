package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestLoad_Defaults(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	tempDir := t.TempDir()
	t.Setenv("TUPRWRE_DIR", tempDir)
	// Clear optional env vars to ensure defaults
	t.Setenv("TUPRWRE_BASE_IMAGE", "")
	t.Setenv("TUPRWRE_RUNTIME", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if cfg.BaseDir != tempDir {
		t.Fatalf("BaseDir = %q, want %q", cfg.BaseDir, tempDir)
	}
	if want := filepath.Join(tempDir, "bin"); cfg.ShimDir != want {
		t.Fatalf("ShimDir = %q, want %q", cfg.ShimDir, want)
	}
	if want := filepath.Join(tempDir, "containers"); cfg.ContainerDir != want {
		t.Fatalf("ContainerDir = %q, want %q", cfg.ContainerDir, want)
	}
	if cfg.DefaultBaseImage != "ubuntu:22.04" {
		t.Fatalf("DefaultBaseImage = %q, want %q", cfg.DefaultBaseImage, "ubuntu:22.04")
	}
	if cfg.ContainerRuntime != "docker" {
		t.Fatalf("ContainerRuntime = %q, want %q", cfg.ContainerRuntime, "docker")
	}

	expectedIntercept := []string{"apt", "apt-get", "pip", "pip3", "curl", "wget"}
	if !reflect.DeepEqual(cfg.InterceptCommands, expectedIntercept) {
		t.Fatalf("InterceptCommands = %v, want %v", cfg.InterceptCommands, expectedIntercept)
	}
}

func TestLoad_EnvOverrides(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	tempDir := t.TempDir()
	t.Setenv("TUPRWRE_DIR", tempDir)
	t.Setenv("TUPRWRE_BASE_IMAGE", "alpine:3.19")
	t.Setenv("TUPRWRE_RUNTIME", "containerd")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if cfg.DefaultBaseImage != "alpine:3.19" {
		t.Fatalf("DefaultBaseImage = %q, want %q", cfg.DefaultBaseImage, "alpine:3.19")
	}
	if cfg.ContainerRuntime != "containerd" {
		t.Fatalf("ContainerRuntime = %q, want %q", cfg.ContainerRuntime, "containerd")
	}
}

func TestLoad_CreatesDirectories(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	tempDir := t.TempDir()
	baseDir := filepath.Join(tempDir, "nested", "tuprwre")
	t.Setenv("TUPRWRE_DIR", baseDir)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	for _, dir := range []string{cfg.BaseDir, cfg.ShimDir, cfg.ContainerDir} {
		info, statErr := os.Stat(dir)
		if statErr != nil {
			t.Fatalf("directory %q not created: %v", dir, statErr)
		}
		if !info.IsDir() {
			t.Fatalf("%q is not a directory", dir)
		}
	}
}

func TestLoad_DefaultBaseDir(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	// Unset TUPRWRE_DIR to test default behavior
	t.Setenv("TUPRWRE_DIR", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	home, _ := os.UserHomeDir()
	expected := filepath.Join(home, ".tuprwre")
	if cfg.BaseDir != expected {
		t.Fatalf("BaseDir = %q, want %q", cfg.BaseDir, expected)
	}
}

func TestGetEnv_DefaultValue(t *testing.T) {
	t.Setenv("TEST_NONEXISTENT_VAR", "")
	got := getEnv("TEST_NONEXISTENT_VAR", "fallback")
	if got != "fallback" {
		t.Fatalf("getEnv() = %q, want %q", got, "fallback")
	}
}

func TestGetEnv_OverrideValue(t *testing.T) {
	t.Setenv("TEST_EXISTING_VAR", "custom")
	got := getEnv("TEST_EXISTING_VAR", "fallback")
	if got != "custom" {
		t.Fatalf("getEnv() = %q, want %q", got, "custom")
	}
}

func TestLoadConfigFile_ValidJSON(t *testing.T) {
	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "config.json")

	configJSON := `{
  "intercept": ["apt", "npm"],
  "allow": ["curl"],
  "base_image": "alpine:3.19",
  "runtime": "containerd"
}`

	if err := os.WriteFile(path, []byte(configJSON), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	cfg, err := loadConfigFile(path)
	if err != nil {
		t.Fatalf("loadConfigFile() failed: %v", err)
	}
	if cfg == nil {
		t.Fatalf("expected config, got nil")
	}
	if !reflect.DeepEqual(cfg.Intercept, []string{"apt", "npm"}) {
		t.Fatalf("intercept = %v, want %v", cfg.Intercept, []string{"apt", "npm"})
	}
	if !reflect.DeepEqual(cfg.Allow, []string{"curl"}) {
		t.Fatalf("allow = %v, want %v", cfg.Allow, []string{"curl"})
	}
	if cfg.BaseImage != "alpine:3.19" {
		t.Fatalf("base_image = %q, want %q", cfg.BaseImage, "alpine:3.19")
	}
	if cfg.Runtime != "containerd" {
		t.Fatalf("runtime = %q, want %q", cfg.Runtime, "containerd")
	}
}

func TestLoadConfigFile_MissingFile(t *testing.T) {
	cfg, err := loadConfigFile("/path/does/not/exist/config.json")
	if err != nil {
		t.Fatalf("loadConfigFile() returned unexpected error: %v", err)
	}
	if cfg != nil {
		t.Fatalf("expected nil config, got %#v", cfg)
	}
}

func TestLoadConfigFile_MalformedJSON(t *testing.T) {
	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "bad.json")

	if err := os.WriteFile(path, []byte(`{"intercept": ["apt", ]}`), 0644); err != nil {
		t.Fatalf("failed to write bad json: %v", err)
	}

	cfg, err := loadConfigFile(path)
	if err == nil {
		t.Fatalf("expected parse error, got nil with config %#v", cfg)
	}
}

func TestFindWorkspaceRoot_Found(t *testing.T) {
	tempDir := t.TempDir()
	projectRoot := filepath.Join(tempDir, "x", "project")
	deepPath := filepath.Join(projectRoot, "src", "deep")
	if err := os.MkdirAll(filepath.Join(projectRoot, ".tuprwre"), 0755); err != nil {
		t.Fatalf("failed to create workspace marker directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectRoot, ".tuprwre", "config.json"), []byte(`{}`), 0644); err != nil {
		t.Fatalf("failed to create config file: %v", err)
	}

	root := findWorkspaceRoot(deepPath)
	if root != filepath.Clean(projectRoot) {
		t.Fatalf("findWorkspaceRoot(%q) = %q, want %q", deepPath, root, filepath.Clean(projectRoot))
	}
}

func TestFindWorkspaceRoot_NotFound(t *testing.T) {
	tempDir := t.TempDir()
	if root := findWorkspaceRoot(tempDir); root != "" {
		t.Fatalf("findWorkspaceRoot(%q) = %q, want empty string", tempDir, root)
	}
}

func TestLoadMerge_WorkspaceOverridesGlobal(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	globalDir := filepath.Join(tempHome, ".tuprwre")
	if err := os.MkdirAll(globalDir, 0755); err != nil {
		t.Fatalf("failed to create global dir: %v", err)
	}
	globalCfg := filepath.Join(globalDir, "config.json")
	if err := os.WriteFile(globalCfg, []byte(`{"intercept": ["apt"]}`), 0644); err != nil {
		t.Fatalf("failed to write global config: %v", err)
	}

	workspaceRoot := t.TempDir()
	projectRoot := filepath.Join(workspaceRoot, "project")
	workspacePath := filepath.Join(projectRoot, "src")
	if err := os.MkdirAll(workspacePath, 0755); err != nil {
		t.Fatalf("failed to create workspace path: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(projectRoot, ".tuprwre"), 0755); err != nil {
		t.Fatalf("failed to create workspace dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectRoot, ".tuprwre", "config.json"), []byte(`{"intercept": ["apt", "brew"]}`), 0644); err != nil {
		t.Fatalf("failed to write workspace config: %v", err)
	}

	t.Setenv("TUPRWRE_DIR", filepath.Join(tempHome, "runtime"))
	t.Chdir(workspacePath)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if cfg.WorkspaceRoot != filepath.Clean(projectRoot) {
		t.Fatalf("WorkspaceRoot = %q, want %q", cfg.WorkspaceRoot, filepath.Clean(projectRoot))
	}
	if !reflect.DeepEqual(cfg.InterceptCommands, []string{"apt", "brew"}) {
		t.Fatalf("InterceptCommands = %v, want %v", cfg.InterceptCommands, []string{"apt", "brew"})
	}
}

func TestLoadMerge_EnvOverridesAll(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	if err := os.MkdirAll(filepath.Join(tempHome, ".tuprwre"), 0755); err != nil {
		t.Fatalf("failed to create global config dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tempHome, ".tuprwre", "config.json"), []byte(`{"intercept": ["apt"]}`), 0644); err != nil {
		t.Fatalf("failed to write global config: %v", err)
	}

	workspaceRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workspaceRoot, ".tuprwre"), 0755); err != nil {
		t.Fatalf("failed to create workspace dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspaceRoot, ".tuprwre", "config.json"), []byte(`{"intercept": ["curl"]}`), 0644); err != nil {
		t.Fatalf("failed to write workspace config: %v", err)
	}

	t.Setenv("TUPRWRE_DIR", filepath.Join(tempHome, "runtime"))
	t.Setenv("TUPRWRE_INTERCEPT", "npm,pip")
	t.Chdir(workspaceRoot)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if !reflect.DeepEqual(cfg.InterceptCommands, []string{"npm", "pip"}) {
		t.Fatalf("InterceptCommands = %v, want %v", cfg.InterceptCommands, []string{"npm", "pip"})
	}
}

func TestLoadMerge_AllowRemovesFromIntercept(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	workspaceRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workspaceRoot, ".tuprwre"), 0755); err != nil {
		t.Fatalf("failed to create workspace dir: %v", err)
	}
	config := map[string]any{
		"intercept": []string{"apt", "curl", "wget"},
		"allow":     []string{"curl"},
	}
	configData, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("failed to marshal config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspaceRoot, ".tuprwre", "config.json"), configData, 0644); err != nil {
		t.Fatalf("failed to write workspace config: %v", err)
	}

	t.Setenv("TUPRWRE_DIR", filepath.Join(tempHome, "runtime"))
	t.Chdir(workspaceRoot)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if !reflect.DeepEqual(cfg.InterceptCommands, []string{"apt", "wget"}) {
		t.Fatalf("InterceptCommands = %v, want %v", cfg.InterceptCommands, []string{"apt", "wget"})
	}
	if !reflect.DeepEqual(cfg.AllowCommands, []string{"curl"}) {
		t.Fatalf("AllowCommands = %v, want %v", cfg.AllowCommands, []string{"curl"})
	}
}

func TestLoadMerge_DefaultInterceptList(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("TUPRWRE_DIR", filepath.Join(tempHome, "runtime"))

	root := t.TempDir()
	t.Chdir(root)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	expected := []string{"apt", "apt-get", "pip", "pip3", "curl", "wget"}
	if !reflect.DeepEqual(cfg.InterceptCommands, expected) {
		t.Fatalf("InterceptCommands = %v, want %v", cfg.InterceptCommands, expected)
	}
}
