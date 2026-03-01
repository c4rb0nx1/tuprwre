package shim

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/c4rb0nx1/tuprwre/internal/config"
	"github.com/c4rb0nx1/tuprwre/internal/discovery"
)

func setupTestGenerator(t *testing.T) (*Generator, string) {
	t.Helper()
	tempDir := t.TempDir()
	t.Setenv("TUPRWRE_DIR", tempDir)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	gen := NewGenerator(cfg)
	return gen, tempDir
}

func TestCreate_GeneratesShimFile(t *testing.T) {
	gen, tempDir := setupTestGenerator(t)

	binary := discovery.Binary{Name: "mytool", Path: "/usr/local/bin/mytool"}
	if err := gen.Create(binary, "toolset:latest", false); err != nil {
		t.Fatalf("Create() failed: %v", err)
	}

	shimPath := filepath.Join(tempDir, "bin", "mytool")
	content, err := os.ReadFile(shimPath)
	if err != nil {
		t.Fatalf("failed to read shim: %v", err)
	}

	shimStr := string(content)
	if !strings.Contains(shimStr, "#!/bin/bash") {
		t.Fatal("shim missing shebang")
	}
	if !strings.Contains(shimStr, "toolset:latest") {
		t.Fatal("shim missing image name")
	}
	if !strings.Contains(shimStr, "mytool") {
		t.Fatal("shim missing binary name")
	}
	if !strings.Contains(shimStr, "tuprwre run") {
		t.Fatal("shim missing tuprwre run command")
	}

	info, err := os.Stat(shimPath)
	if err != nil {
		t.Fatalf("stat shim: %v", err)
	}
	if info.Mode().Perm() != 0755 {
		t.Fatalf("shim permissions = %o, want 0755", info.Mode().Perm())
	}
}

func TestCreate_BlocksOverwrite(t *testing.T) {
	gen, _ := setupTestGenerator(t)

	binary := discovery.Binary{Name: "mytool", Path: "/usr/local/bin/mytool"}
	if err := gen.Create(binary, "toolset:latest", false); err != nil {
		t.Fatalf("first Create() failed: %v", err)
	}

	err := gen.Create(binary, "toolset:v2", false)
	if err == nil {
		t.Fatal("expected error on duplicate create without force")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCreate_ForceOverwrite(t *testing.T) {
	gen, tempDir := setupTestGenerator(t)

	binary := discovery.Binary{Name: "mytool", Path: "/usr/local/bin/mytool"}
	if err := gen.Create(binary, "toolset:v1", false); err != nil {
		t.Fatalf("first Create() failed: %v", err)
	}

	if err := gen.Create(binary, "toolset:v2", true); err != nil {
		t.Fatalf("force Create() failed: %v", err)
	}

	shimPath := filepath.Join(tempDir, "bin", "mytool")
	content, err := os.ReadFile(shimPath)
	if err != nil {
		t.Fatalf("read shim: %v", err)
	}
	if !strings.Contains(string(content), "toolset:v2") {
		t.Fatal("shim not updated with new image")
	}
}

func TestCreate_ContainerdTemplate(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("TUPRWRE_DIR", tempDir)
	t.Setenv("TUPRWRE_RUNTIME", "containerd")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	gen := NewGenerator(cfg)

	binary := discovery.Binary{Name: "mytool", Path: "/usr/local/bin/mytool"}
	if err := gen.Create(binary, "toolset:latest", false); err != nil {
		t.Fatalf("Create() failed: %v", err)
	}

	shimPath := filepath.Join(tempDir, "bin", "mytool")
	content, err := os.ReadFile(shimPath)
	if err != nil {
		t.Fatalf("read shim: %v", err)
	}
	if !strings.Contains(string(content), "--runtime containerd") {
		t.Fatal("containerd shim missing --runtime containerd")
	}
}

func TestRemove_DeletesShim(t *testing.T) {
	gen, tempDir := setupTestGenerator(t)

	binary := discovery.Binary{Name: "mytool", Path: "/usr/local/bin/mytool"}
	if err := gen.Create(binary, "toolset:latest", false); err != nil {
		t.Fatalf("Create() failed: %v", err)
	}

	if err := gen.Remove("mytool"); err != nil {
		t.Fatalf("Remove() failed: %v", err)
	}

	shimPath := filepath.Join(tempDir, "bin", "mytool")
	if _, err := os.Stat(shimPath); !os.IsNotExist(err) {
		t.Fatal("shim should have been removed")
	}
}

func TestRemove_NonexistentShim(t *testing.T) {
	gen, _ := setupTestGenerator(t)

	err := gen.Remove("nonexistent")
	if err == nil {
		t.Fatal("expected error removing nonexistent shim")
	}
}

func TestRemoveAll_RemovesAllShims(t *testing.T) {
	gen, _ := setupTestGenerator(t)

	for _, name := range []string{"tool-a", "tool-b", "tool-c"} {
		binary := discovery.Binary{Name: name, Path: "/usr/local/bin/" + name}
		if err := gen.Create(binary, name+":latest", false); err != nil {
			t.Fatalf("Create(%s) failed: %v", name, err)
		}
	}

	removed, err := gen.RemoveAll()
	if err != nil {
		t.Fatalf("RemoveAll() failed: %v", err)
	}
	if len(removed) != 3 {
		t.Fatalf("expected 3 removed shims, got %d", len(removed))
	}

	shims, err := gen.List()
	if err != nil {
		t.Fatalf("List() failed: %v", err)
	}
	if len(shims) != 0 {
		t.Fatalf("expected 0 shims after RemoveAll(), got %d: %v", len(shims), shims)
	}
}

func TestRemoveAll_EmptyDir(t *testing.T) {
	gen, _ := setupTestGenerator(t)

	removed, err := gen.RemoveAll()
	if err != nil {
		t.Fatalf("RemoveAll() failed on empty dir: %v", err)
	}
	if len(removed) != 0 {
		t.Fatalf("expected 0 removed shims, got %d", len(removed))
	}
}

func TestList_ReturnsShims(t *testing.T) {
	gen, _ := setupTestGenerator(t)

	for _, name := range []string{"tool-a", "tool-b", "tool-c"} {
		binary := discovery.Binary{Name: name, Path: "/usr/local/bin/" + name}
		if err := gen.Create(binary, name+":latest", false); err != nil {
			t.Fatalf("Create(%s) failed: %v", name, err)
		}
	}

	shims, err := gen.List()
	if err != nil {
		t.Fatalf("List() failed: %v", err)
	}
	if len(shims) != 3 {
		t.Fatalf("expected 3 shims, got %d: %v", len(shims), shims)
	}
}

func TestList_EmptyDir(t *testing.T) {
	gen, _ := setupTestGenerator(t)

	shims, err := gen.List()
	if err != nil {
		t.Fatalf("List() failed: %v", err)
	}
	if len(shims) != 0 {
		t.Fatalf("expected 0 shims, got %d", len(shims))
	}
}

func TestList_SkipsHiddenFiles(t *testing.T) {
	gen, tempDir := setupTestGenerator(t)

	// Create a hidden file
	hiddenPath := filepath.Join(tempDir, "bin", ".hidden")
	if err := os.WriteFile(hiddenPath, []byte("hidden"), 0644); err != nil {
		t.Fatalf("write hidden file: %v", err)
	}

	binary := discovery.Binary{Name: "visible", Path: "/usr/local/bin/visible"}
	if err := gen.Create(binary, "img:latest", false); err != nil {
		t.Fatalf("Create() failed: %v", err)
	}

	shims, err := gen.List()
	if err != nil {
		t.Fatalf("List() failed: %v", err)
	}
	if len(shims) != 1 {
		t.Fatalf("expected 1 shim (hidden excluded), got %d: %v", len(shims), shims)
	}
	if shims[0] != "visible" {
		t.Fatalf("expected 'visible', got %q", shims[0])
	}
}

func TestGetPath(t *testing.T) {
	gen, tempDir := setupTestGenerator(t)

	got := gen.GetPath("mytool")
	want := filepath.Join(tempDir, "bin", "mytool")
	if got != want {
		t.Fatalf("GetPath() = %q, want %q", got, want)
	}
}

func TestValidateShimDir_InPath(t *testing.T) {
	gen, tempDir := setupTestGenerator(t)

	shimDir := filepath.Join(tempDir, "bin")
	t.Setenv("PATH", shimDir+":/usr/bin:/bin")

	if err := gen.ValidateShimDir(); err != nil {
		t.Fatalf("ValidateShimDir() should pass when shimdir is in PATH: %v", err)
	}
}

func TestValidateShimDir_NotInPath(t *testing.T) {
	gen, _ := setupTestGenerator(t)

	t.Setenv("PATH", "/usr/bin:/bin")

	err := gen.ValidateShimDir()
	if err == nil {
		t.Fatal("expected error when shimdir is not in PATH")
	}
	if !strings.Contains(err.Error(), "not in PATH") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMetadataSaveAndLoad(t *testing.T) {
	gen, _ := setupTestGenerator(t)

	meta := Metadata{
		BinaryName:     "jq",
		InstallCommand: "apt-get install -y jq",
		InstallMode:    "command",
		BaseImage:      "ubuntu:22.04",
		OutputImage:    "tuprwre-jq:latest",
		InstalledAt:    "2026-01-01T00:00:00Z",
	}

	if err := gen.SaveMetadata(meta); err != nil {
		t.Fatalf("SaveMetadata() failed: %v", err)
	}

	loaded, err := gen.LoadMetadata("jq")
	if err != nil {
		t.Fatalf("LoadMetadata() failed: %v", err)
	}

	if loaded.BinaryName != meta.BinaryName {
		t.Fatalf("BinaryName = %q, want %q", loaded.BinaryName, meta.BinaryName)
	}
	if loaded.InstallCommand != meta.InstallCommand {
		t.Fatalf("InstallCommand = %q, want %q", loaded.InstallCommand, meta.InstallCommand)
	}
	if loaded.BaseImage != meta.BaseImage {
		t.Fatalf("BaseImage = %q, want %q", loaded.BaseImage, meta.BaseImage)
	}
}

func TestMetadataRoundTripsWorkspaceField(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &config.Config{
		BaseDir: tmpDir,
		ShimDir: filepath.Join(tmpDir, "bin"),
	}
	if err := os.MkdirAll(cfg.ShimDir, 0o755); err != nil {
		t.Fatalf("failed to create shim dir: %v", err)
	}

	gen := NewGenerator(cfg)
	meta := Metadata{
		BinaryName:  "tool",
		Workspace:   "/home/user/project",
		InstalledAt: "2026-03-01T00:00:00Z",
	}

	if err := gen.SaveMetadata(meta); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	loaded, err := gen.LoadMetadata("tool")
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if loaded.Workspace != "/home/user/project" {
		t.Fatalf("expected workspace '/home/user/project', got %q", loaded.Workspace)
	}
}

func TestLoadMetadata_NotFound(t *testing.T) {
	gen, _ := setupTestGenerator(t)

	_, err := gen.LoadMetadata("nonexistent")
	if err == nil {
		t.Fatal("expected error loading nonexistent metadata")
	}
}

func TestRemoveMetadata(t *testing.T) {
	gen, _ := setupTestGenerator(t)

	meta := Metadata{
		BinaryName:  "jq",
		BaseImage:   "ubuntu:22.04",
		OutputImage: "tuprwre-jq:latest",
	}

	if err := gen.SaveMetadata(meta); err != nil {
		t.Fatalf("SaveMetadata() failed: %v", err)
	}

	if err := gen.RemoveMetadata("jq"); err != nil {
		t.Fatalf("RemoveMetadata() failed: %v", err)
	}

	_, err := gen.LoadMetadata("jq")
	if err == nil {
		t.Fatal("expected error loading removed metadata")
	}
}
