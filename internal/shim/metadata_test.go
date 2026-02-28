package shim

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/c4rb0nx1/tuprwre/internal/config"
)

func TestMetadataRoundTripsScriptInstallFields(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("TUPRWRE_DIR", tempHome)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	gen := NewGenerator(cfg)

	path := filepath.Join(tempHome, "scripts", "install.sh")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("prepare script dir: %v", err)
	}
	if err := os.WriteFile(path, []byte("echo hi"), 0o644); err != nil {
		t.Fatalf("write script: %v", err)
	}

	original := Metadata{
		BinaryName:        "tool",
		InstallMode:       "script",
		InstallScriptPath: path,
		InstallScriptArgs: []string{"--flag", "value"},
		BaseImage:         "ubuntu:22.04",
		OutputImage:       "tool-image",
		InstalledAt:       "2026-01-01T00:00:00Z",
		InstallForceUsed:  true,
	}
	if err := gen.SaveMetadata(original); err != nil {
		t.Fatalf("save metadata: %v", err)
	}

	loaded, err := gen.LoadMetadata("tool")
	if err != nil {
		t.Fatalf("load metadata: %v", err)
	}

	if loaded.InstallMode != original.InstallMode {
		t.Fatalf("install mode mismatch: got=%q want=%q", loaded.InstallMode, original.InstallMode)
	}
	if got, want := loaded.InstallScriptPath, original.InstallScriptPath; got != want {
		t.Fatalf("script path mismatch: got=%q want=%q", got, want)
	}
	if len(loaded.InstallScriptArgs) != len(original.InstallScriptArgs) {
		t.Fatalf("script arg count mismatch: got=%d want=%d", len(loaded.InstallScriptArgs), len(original.InstallScriptArgs))
	}
	for i := range original.InstallScriptArgs {
		if loaded.InstallScriptArgs[i] != original.InstallScriptArgs[i] {
			t.Fatalf("script arg mismatch[%d]: got=%q want=%q", i, loaded.InstallScriptArgs[i], original.InstallScriptArgs[i])
		}
	}
}

func TestListAllMetadata_ReturnsAll(t *testing.T) {
	gen, _ := setupTestGenerator(t)

	first := Metadata{
		BinaryName:     "alpha",
		InstallMode:    "command",
		InstallCommand: "apt-get install -y alpha",
		BaseImage:      "ubuntu:22.04",
		OutputImage:    "alpha-image:latest",
		InstalledAt:    "2026-01-01T00:00:00Z",
	}
	second := Metadata{
		BinaryName:     "beta",
		InstallMode:    "command",
		InstallCommand: "apt-get install -y beta",
		BaseImage:      "ubuntu:22.04",
		OutputImage:    "beta-image:latest",
		InstalledAt:    "2026-01-01T00:00:00Z",
	}

	if err := gen.SaveMetadata(first); err != nil {
		t.Fatalf("save first metadata: %v", err)
	}
	if err := gen.SaveMetadata(second); err != nil {
		t.Fatalf("save second metadata: %v", err)
	}

	metadataList, err := gen.ListAllMetadata()
	if err != nil {
		t.Fatalf("ListAllMetadata() failed: %v", err)
	}
	if len(metadataList) != 2 {
		t.Fatalf("expected 2 metadata entries, got %d", len(metadataList))
	}

	byName := map[string]Metadata{}
	for _, metadata := range metadataList {
		byName[metadata.BinaryName] = metadata
	}

	if byName["alpha"].OutputImage != "alpha-image:latest" {
		t.Fatalf("unexpected alpha output image: %q", byName["alpha"].OutputImage)
	}
	if byName["beta"].InstallCommand != "apt-get install -y beta" {
		t.Fatalf("unexpected beta install command: %q", byName["beta"].InstallCommand)
	}
}

func TestRemoveAllMetadata_CleansUp(t *testing.T) {
	gen, _ := setupTestGenerator(t)

	for _, metadata := range []Metadata{
		{
			BinaryName:     "alpha",
			InstallMode:    "command",
			InstallCommand: "apt-get install -y alpha",
			BaseImage:      "ubuntu:22.04",
			OutputImage:    "alpha-image:latest",
			InstalledAt:    "2026-01-01T00:00:00Z",
		},
		{
			BinaryName:     "beta",
			InstallMode:    "command",
			InstallCommand: "apt-get install -y beta",
			BaseImage:      "ubuntu:22.04",
			OutputImage:    "beta-image:latest",
			InstalledAt:    "2026-01-01T00:00:00Z",
		},
	} {
		if err := gen.SaveMetadata(metadata); err != nil {
			t.Fatalf("save metadata %q: %v", metadata.BinaryName, err)
		}
	}

	removed, err := gen.RemoveAllMetadata()
	if err != nil {
		t.Fatalf("RemoveAllMetadata() failed: %v", err)
	}
	if len(removed) != 2 {
		t.Fatalf("expected 2 removed metadata entries, got %d", len(removed))
	}

	if _, err := gen.LoadMetadata("alpha"); !os.IsNotExist(err) {
		t.Fatalf("expected alpha metadata removed, got err=%v", err)
	}
	if _, err := gen.LoadMetadata("beta"); !os.IsNotExist(err) {
		t.Fatalf("expected beta metadata removed, got err=%v", err)
	}
}
