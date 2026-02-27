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
