package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestDoctorCommandJSONModeOutputsHealthyReport(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("TUPRWRE_DIR", tempHome)
	t.Setenv("TUPRWRE_RUNTIME", "docker")
	shimDir := filepath.Join(tempHome, "bin")
	originalPath := os.Getenv("PATH")
	t.Setenv("PATH", shimDir+string(os.PathListSeparator)+filepath.Clean("/usr/bin"))

	prevLookPath := doctorLookPath
	prevRunCommand := doctorRunCommand
	prevJSON := doctorJSON
	t.Cleanup(func() {
		doctorLookPath = prevLookPath
		doctorRunCommand = prevRunCommand
		doctorJSON = prevJSON
		t.Setenv("PATH", originalPath)
	})

	doctorLookPath = func(name string) (string, error) {
		if name == "tuprwre" {
			return "/usr/local/bin/tuprwre", nil
		}
		return "", fmt.Errorf("not found: %s", name)
	}
	doctorRunCommand = func(name string, args ...string) ([]byte, error) {
		switch name {
		case "/usr/local/bin/tuprwre":
			if len(args) == 1 && args[0] == "--version" {
				return []byte("tuprwre 0.0.5\n"), nil
			}
		case "docker":
			if len(args) >= 1 && args[0] == "version" {
				return []byte("25.0.1\n"), nil
			}
		}
		return nil, fmt.Errorf("unexpected command: %s %v", name, args)
	}

	doctorJSON = true

	out := &bytes.Buffer{}
	cmd := &cobra.Command{}
	cmd.SetOut(out)
	if err := runDoctor(cmd, nil); err != nil {
		t.Fatalf("runDoctor returned error: %v", err)
	}

	var payload struct {
		Healthy bool `json:"healthy"`
	}
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("invalid json output: %v", err)
	}
	if !payload.Healthy {
		t.Fatalf("expected healthy report, got unhealthy: %q", out.String())
	}
}

func TestDoctorCommandFailsOnInvalidRuntime(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("TUPRWRE_DIR", tempHome)
	t.Setenv("TUPRWRE_RUNTIME", "bad-runtime")
	shimDir := filepath.Join(tempHome, "bin")
	originalPath := os.Getenv("PATH")
	t.Setenv("PATH", shimDir+string(os.PathListSeparator)+filepath.Clean("/usr/bin"))

	prevLookPath := doctorLookPath
	prevRunCommand := doctorRunCommand
	prevJSON := doctorJSON
	t.Cleanup(func() {
		doctorLookPath = prevLookPath
		doctorRunCommand = prevRunCommand
		doctorJSON = prevJSON
		t.Setenv("PATH", originalPath)
	})

	doctorLookPath = func(name string) (string, error) {
		if name == "tuprwre" {
			return "/usr/local/bin/tuprwre", nil
		}
		return "", fmt.Errorf("not found: %s", name)
	}
	doctorRunCommand = func(name string, args ...string) ([]byte, error) {
		if name == "/usr/local/bin/tuprwre" && len(args) == 1 && args[0] == "--version" {
			return []byte("tuprwre 0.0.5\n"), nil
		}
		return nil, fmt.Errorf("unexpected command: %s %v", name, args)
	}

	out := &bytes.Buffer{}
	cmd := &cobra.Command{}
	cmd.SetOut(out)
	err := runDoctor(cmd, nil)
	if err == nil {
		t.Fatal("expected runDoctor to fail for invalid runtime")
	}
	if !strings.Contains(out.String(), "invalid TUPRWRE_RUNTIME") {
		t.Fatalf("expected invalid runtime message, got %q", out.String())
	}
}

func TestDoctorActiveBinaryAndPathEntryHelpers(t *testing.T) {
	if !isPathEntry("/tmp/my-shims", "/usr/local/bin:/tmp/my-shims:/usr/bin") {
		t.Fatal("expected shim path to be detected in PATH list")
	}
	if isPathEntry("/tmp/my-shims", "/usr/local/bin:/usr/bin") {
		t.Fatal("unexpected shim path match")
	}
}

func TestDoctorWritableStateDirCheck(t *testing.T) {
	tmp := t.TempDir()
	writable := filepath.Join(tmp, "writable")
	if err := os.MkdirAll(writable, 0o755); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	check := doctorCheckWritableDir(writable, "state dir")
	if check.Status != doctorStatusPass {
		t.Fatalf("expected writable dir check pass, got %q: %q", check.Status, check.Message)
	}
}
