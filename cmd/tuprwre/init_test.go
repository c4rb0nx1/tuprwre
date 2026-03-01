package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
)

func TestRunInit_CreatesWorkspaceConfig(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}
	defer os.Chdir(origDir)

	initGlobal = false
	cmd := &cobra.Command{}
	cmd.SetOut(&bytes.Buffer{})

	if err := runInit(cmd, nil); err != nil {
		t.Fatalf("runInit failed: %v", err)
	}

	configPath := filepath.Join(tmpDir, ".tuprwre", "config.json")
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("config file not created: %v", err)
	}

	content, _ := os.ReadFile(configPath)
	if len(content) == 0 {
		t.Fatal("config file is empty")
	}
}

func TestRunInit_RefusesOverwrite(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}
	defer os.Chdir(origDir)

	// Create existing config
	os.MkdirAll(filepath.Join(tmpDir, ".tuprwre"), 0o755)
	os.WriteFile(filepath.Join(tmpDir, ".tuprwre", "config.json"), []byte("{}"), 0o644)

	initGlobal = false
	cmd := &cobra.Command{}
	cmd.SetOut(&bytes.Buffer{})

	err := runInit(cmd, nil)
	if err == nil {
		t.Fatal("expected error for existing config")
	}
}

func TestRunInit_GlobalFlag(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("TUPRWRE_DIR", tmpDir)

	initGlobal = true
	cmd := &cobra.Command{}
	cmd.SetOut(&bytes.Buffer{})

	if err := runInit(cmd, nil); err != nil {
		t.Fatalf("runInit --global failed: %v", err)
	}

	configPath := filepath.Join(tmpDir, "config.json")
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("global config not created: %v", err)
	}
}
