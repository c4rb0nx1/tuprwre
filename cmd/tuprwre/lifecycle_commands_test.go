package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/c4rb0nx1/tuprwre/internal/config"
	"github.com/c4rb0nx1/tuprwre/internal/shim"
	"github.com/spf13/cobra"
)

func TestRunListCommandWithShims(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("TUPRWRE_DIR", tempHome)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cfg.ShimDir, "delta"), []byte("#!bin\n"), 0o755); err != nil {
		t.Fatalf("seed shim: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cfg.ShimDir, "alpha"), []byte("#!bin\n"), 0o755); err != nil {
		t.Fatalf("seed shim: %v", err)
	}

	out := &bytes.Buffer{}
	cmd := &cobra.Command{}
	cmd.SetOut(out)

	if err := runList(cmd, nil); err != nil {
		t.Fatalf("runList failed: %v", err)
	}

	lines := strings.Fields(strings.TrimSpace(out.String()))
	expected := []string{"alpha", "delta"}
	if len(lines) != len(expected) {
		t.Fatalf("unexpected output count: got=%d want=%d output=%q", len(lines), len(expected), out.String())
	}
	for i, line := range lines {
		if line != expected[i] {
			t.Fatalf("unexpected item at %d: got=%q want=%q output=%q", i, line, expected[i], out.String())
		}
	}
}

func TestRunListCommandNoShims(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("TUPRWRE_DIR", tempHome)

	out := &bytes.Buffer{}
	cmd := &cobra.Command{}
	cmd.SetOut(out)

	if err := runList(cmd, nil); err != nil {
		t.Fatalf("runList failed: %v", err)
	}

	if strings.TrimSpace(out.String()) != "No shims installed." {
		t.Fatalf("unexpected output: %q", out.String())
	}
}

func TestRunRemoveCommandExistingAndMissingShim(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("TUPRWRE_DIR", tempHome)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	gen := shim.NewGenerator(cfg)
	shimPath := gen.GetPath("tool")
	if err := os.WriteFile(shimPath, []byte("#!bin\n"), 0o755); err != nil {
		t.Fatalf("seed shim: %v", err)
	}
	if err := gen.SaveMetadata(shim.Metadata{
		BinaryName:     "tool",
		InstallCommand: "echo ok",
		BaseImage:      "ubuntu:22.04",
		OutputImage:    "example:latest",
		InstalledAt:    time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		t.Fatalf("seed metadata: %v", err)
	}

	cmd := &cobra.Command{}
	out := &bytes.Buffer{}
	cmd.SetOut(out)

	if err := runRemove(cmd, []string{"tool"}); err != nil {
		t.Fatalf("runRemove existing shim failed: %v", err)
	}
	if _, err := os.Stat(shimPath); !os.IsNotExist(err) {
		t.Fatalf("expected shim removed, got err=%v", err)
	}
	if _, err := os.Stat(gen.MetadataPath("tool")); !os.IsNotExist(err) {
		t.Fatalf("expected metadata removed, got err=%v", err)
	}

	if err := runRemove(cmd, []string{"does-not-exist"}); err == nil {
		t.Fatalf("expected missing shim removal to fail")
	} else if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("unexpected missing-shim error: %v", err)
	}
}

func TestRunUpdateCommandWithMetadata(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("TUPRWRE_DIR", tempHome)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	gen := shim.NewGenerator(cfg)
	shimName := "toolx"
	shimPath := gen.GetPath(shimName)
	if err := os.WriteFile(shimPath, []byte("#!bin\n"), 0o755); err != nil {
		t.Fatalf("seed shim: %v", err)
	}
	if err := gen.SaveMetadata(shim.Metadata{
		BinaryName:     shimName,
		InstallCommand: "echo install",
		BaseImage:      "ubuntu:22.04",
		OutputImage:    "toolx-image",
		InstalledAt:    time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		t.Fatalf("seed metadata: %v", err)
	}

	origFlow := installFlow
	var captured installRequest
	var invoked bool
	installFlow = func(cmd *cobra.Command, c *config.Config, req installRequest) error {
		invoked = true
		captured = req
		return nil
	}
	t.Cleanup(func() {
		installFlow = origFlow
	})

	cmd := &cobra.Command{}
	cmd.SetOut(&bytes.Buffer{})
	if err := runUpdate(cmd, []string{shimName}); err != nil {
		t.Fatalf("runUpdate with metadata failed: %v", err)
	}

	if !invoked {
		t.Fatal("expected install flow to be invoked")
	}
	if captured.installCommand != "echo install" {
		t.Fatalf("unexpected install command: %q", captured.installCommand)
	}
	if !captured.force {
		t.Fatal("expected update to force overwrite")
	}
	if captured.baseImage != "ubuntu:22.04" {
		t.Fatalf("unexpected base image: %q", captured.baseImage)
	}
	if captured.imageName != "toolx-image" {
		t.Fatalf("unexpected output image: %q", captured.imageName)
	}
}

func TestRunUpdateCommandWithScriptMetadata(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("TUPRWRE_DIR", tempHome)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	gen := shim.NewGenerator(cfg)
	shimName := "script-tool"
	shimPath := gen.GetPath(shimName)
	if err := os.WriteFile(shimPath, []byte("#!bin\n"), 0o755); err != nil {
		t.Fatalf("seed shim: %v", err)
	}

	scriptPath := filepath.Join(tempHome, "install.sh")
	if err := os.WriteFile(scriptPath, []byte("echo ok"), 0o644); err != nil {
		t.Fatalf("seed script: %v", err)
	}
	if err := gen.SaveMetadata(shim.Metadata{
		BinaryName:        shimName,
		InstallMode:       "script",
		InstallScriptPath: scriptPath,
		InstallScriptArgs: []string{"--flag", "value"},
		BaseImage:         "ubuntu:22.04",
		OutputImage:       "script-tool-image",
		InstalledAt:       time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		t.Fatalf("seed metadata: %v", err)
	}

	origFlow := installFlow
	var captured installRequest
	var invoked bool
	installFlow = func(cmd *cobra.Command, c *config.Config, req installRequest) error {
		invoked = true
		captured = req
		return nil
	}
	t.Cleanup(func() {
		installFlow = origFlow
	})

	cmd := &cobra.Command{}
	cmd.SetOut(&bytes.Buffer{})
	if err := runUpdate(cmd, []string{shimName}); err != nil {
		t.Fatalf("runUpdate with script metadata failed: %v", err)
	}
	if !invoked {
		t.Fatal("expected install flow to be invoked for script metadata")
	}
	if got, want := captured.installScriptPath, scriptPath; got != want {
		t.Fatalf("script path mismatch: got=%q want=%q", got, want)
	}
	if got, want := captured.imageName, "script-tool-image"; got != want {
		t.Fatalf("output image mismatch: got=%q want=%q", got, want)
	}
	if len(captured.installScriptArgs) != 2 {
		t.Fatalf("unexpected script args: %#v", captured.installScriptArgs)
	}
	if len(captured.installScriptContent) == 0 {
		t.Fatal("expected script content to be captured for update")
	}
	if captured.baseImage != "ubuntu:22.04" {
		t.Fatalf("unexpected base image: %q", captured.baseImage)
	}
}

func TestRunUpdateCommandWithScriptMetadataAndMissingScriptFile(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("TUPRWRE_DIR", tempHome)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	gen := shim.NewGenerator(cfg)
	shimName := "script-tool-missing"
	shimPath := gen.GetPath(shimName)
	if err := os.WriteFile(shimPath, []byte("#!bin\n"), 0o755); err != nil {
		t.Fatalf("seed shim: %v", err)
	}
	missingScriptPath := filepath.Join(tempHome, "missing.sh")
	if err := gen.SaveMetadata(shim.Metadata{
		BinaryName:        shimName,
		InstallMode:       "script",
		InstallScriptPath: missingScriptPath,
		BaseImage:         "ubuntu:22.04",
		OutputImage:       "script-tool-image",
		InstalledAt:       time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		t.Fatalf("seed metadata: %v", err)
	}

	origFlow := installFlow
	var invoked bool
	installFlow = func(cmd *cobra.Command, c *config.Config, req installRequest) error {
		invoked = true
		return nil
	}
	t.Cleanup(func() {
		installFlow = origFlow
	})

	cmd := &cobra.Command{}
	cmd.SetOut(&bytes.Buffer{})
	err = runUpdate(cmd, []string{shimName})
	if err == nil {
		t.Fatal("expected runUpdate to fail when update script path missing")
	}
	if !strings.Contains(err.Error(), "stored script for shim") {
		t.Fatalf("unexpected error: %v", err)
	}
	if invoked {
		t.Fatal("expected install flow not to run when script source is missing")
	}
}

func TestRunUpdateCommandWithoutMetadata(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("TUPRWRE_DIR", tempHome)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	gen := shim.NewGenerator(cfg)
	shimName := "legacy-tool"
	shimPath := gen.GetPath(shimName)
	if err := os.WriteFile(shimPath, []byte("#!bin\n"), 0o755); err != nil {
		t.Fatalf("seed shim: %v", err)
	}

	origFlow := installFlow
	var invoked bool
	installFlow = func(cmd *cobra.Command, c *config.Config, req installRequest) error {
		invoked = true
		return nil
	}
	t.Cleanup(func() {
		installFlow = origFlow
	})

	cmd := &cobra.Command{}
	updateErr := runUpdate(cmd, []string{shimName})
	if updateErr == nil {
		t.Fatal("expected runUpdate to fail for missing metadata")
	}
	if !strings.Contains(updateErr.Error(), "legacy shim") {
		t.Fatalf("expected friendly legacy metadata error, got: %v", updateErr)
	}
	if invoked {
		t.Fatal("expected install flow to not run when metadata is missing")
	}

	if err := os.Remove(shimPath); err != nil {
		t.Fatalf("cleanup shim: %v", err)
	}
}
