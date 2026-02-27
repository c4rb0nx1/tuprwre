package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_Defaults(t *testing.T) {
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
}

func TestLoad_EnvOverrides(t *testing.T) {
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
