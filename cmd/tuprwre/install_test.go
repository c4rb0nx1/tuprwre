package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/c4rb0nx1/tuprwre/internal/config"
	"github.com/spf13/cobra"
)

func TestParseInstallCommandFromArgv(t *testing.T) {
	tests := []struct {
		name      string
		argv      []string
		want      string
		wantHas   bool
		wantError bool
	}{
		{
			name:    "quotedForm",
			argv:    []string{"tuprwre", "install", "--", "echo AAA BBB"},
			want:    "echo AAA BBB",
			wantHas: true,
		},
		{
			name:    "rawForm",
			argv:    []string{"tuprwre", "install", "--", "echo", "AAA", "BBB"},
			want:    "echo AAA BBB",
			wantHas: true,
		},
		{
			name:      "missingCommandAfterDash",
			argv:      []string{"tuprwre", "install", "--"},
			wantHas:   false,
			wantError: true,
		},
		{
			name:    "notInstallCommand",
			argv:    []string{"tuprwre", "status"},
			want:    "",
			wantHas: false,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got, has, err := parseInstallCommandFromArgv(tc.argv)
			if tc.wantError {
				if err == nil {
					t.Fatal("expected error but got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if has != tc.wantHas {
				t.Fatalf("has mismatch: got=%v want=%v", has, tc.wantHas)
			}
			if got != tc.want {
				t.Fatalf("command mismatch: got=%q want=%q", got, tc.want)
			}
		})
	}
}

func TestRunInstallUsesRawArgvCommand(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("TUPRWRE_DIR", tempHome)

	cases := []struct {
		name          string
		argv          []string
		argsFromCobra []string
		want          string
	}{
		{
			name:          "quotedForm",
			argv:          []string{"tuprwre", "install", "--", "echo AAA BBB"},
			argsFromCobra: []string{"echo AAA BBB"},
			want:          "echo AAA BBB",
		},
		{
			name:          "rawForm",
			argv:          []string{"tuprwre", "install", "--", "echo", "AAA", "BBB"},
			argsFromCobra: []string{"echo", "AAA", "BBB"},
			want:          "echo AAA BBB",
		},
	}

	origFlow := installFlow
	origReader := installArgsReader
	origScript := installScriptPath
	t.Cleanup(func() {
		installFlow = origFlow
		installArgsReader = origReader
		installScriptPath = origScript
	})
	installScriptPath = ""

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			installArgsReader = func() []string { return tc.argv }
			called := false
			got := ""
			installFlow = func(cmd *cobra.Command, _ *config.Config, req installRequest) error {
				called = true
				got = req.installCommand
				return nil
			}
			if err := runInstall(&cobra.Command{}, tc.argsFromCobra); err != nil {
				t.Fatalf("runInstall failed: %v", err)
			}
			if !called {
				t.Fatal("expected installFlow to be called")
			}
			if got != tc.want {
				t.Fatalf("unexpected command: got=%q want=%q", got, tc.want)
			}
		})
	}
}

func TestRunInstallUsesScriptMode(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("TUPRWRE_DIR", tempHome)

	scriptPath := filepath.Join(tempHome, "install.sh")
	if err := os.WriteFile(scriptPath, []byte("echo from script\n"), 0o644); err != nil {
		t.Fatalf("write script: %v", err)
	}

	origFlow := installFlow
	origReader := installArgsReader
	origScript := installScriptPath
	t.Cleanup(func() {
		installFlow = origFlow
		installArgsReader = origReader
		installScriptPath = origScript
	})

	testCases := []struct {
		name string
		args []string
	}{
		{
			name: "no script args",
			args: nil,
		},
		{
			name: "with script args",
			args: []string{"--flag", "value"},
		},
	}

	installScriptPath = scriptPath
	installArgsReader = func() []string { return []string{"tuprwre", "install", "--script", scriptPath} }

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			captured := installRequest{}
			installFlow = func(cmd *cobra.Command, _ *config.Config, req installRequest) error {
				captured = req
				return nil
			}

			if err := runInstall(&cobra.Command{}, tc.args); err != nil {
				t.Fatalf("runInstall failed: %v", err)
			}

			if got, want := captured.installScriptPath, scriptPath; got != want {
				t.Fatalf("script path mismatch: got=%q want=%q", got, want)
			}
			if got := len(captured.installScriptContent); got == 0 {
				t.Fatal("expected script content to be captured")
			}
			if len(tc.args) != len(captured.installScriptArgs) {
				t.Fatalf("script args length mismatch: got=%d want=%d", len(captured.installScriptArgs), len(tc.args))
			}
		})
	}
}

func TestRunInstallScriptModeValidation(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("TUPRWRE_DIR", tempHome)

	origFlow := installFlow
	origReader := installArgsReader
	origScript := installScriptPath
	t.Cleanup(func() {
		installFlow = origFlow
		installArgsReader = origReader
		installScriptPath = origScript
	})
	installFlow = func(cmd *cobra.Command, c *config.Config, req installRequest) error { return nil }

	missingScript := filepath.Join(tempHome, "missing.sh")
	installScriptPath = missingScript
	installArgsReader = func() []string { return []string{"tuprwre", "install", "--script", missingScript} }
	if err := runInstall(&cobra.Command{}, nil); err == nil {
		t.Fatal("expected missing script path error")
	} else if !strings.Contains(err.Error(), "script file not found") {
		t.Fatalf("expected script file not found error, got: %v", err)
	}

	unreadableDir := filepath.Join(tempHome, "dir")
	if err := os.Mkdir(unreadableDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	installScriptPath = unreadableDir
	installArgsReader = func() []string { return []string{"tuprwre", "install", "--script", unreadableDir} }
	if err := runInstall(&cobra.Command{}, nil); err == nil {
		t.Fatal("expected unreadable script error")
	} else if !strings.Contains(err.Error(), "failed to read script") {
		t.Fatalf("expected read error, got: %v", err)
	}

	installScriptPath = filepath.Join(tempHome, "install.sh")
	if err := os.WriteFile(installScriptPath, []byte("echo ok\n"), 0o644); err != nil {
		t.Fatalf("write script: %v", err)
	}
	captured := installRequest{}
	installFlow = func(cmd *cobra.Command, _ *config.Config, req installRequest) error {
		captured = req
		return nil
	}
	installArgsReader = func() []string {
		return []string{"tuprwre", "install", "--script", installScriptPath, "--", "--verbose", "--dry-run"}
	}
	if err := runInstall(&cobra.Command{}, []string{"--verbose", "--dry-run"}); err != nil {
		t.Fatalf("expected script args after -- to work, got error: %v", err)
	}
	if got, want := len(captured.installScriptArgs), 2; got != want {
		t.Fatalf("unexpected script arg count after --: got=%d want=%d", got, want)
	}
	if captured.installScriptArgs[0] != "--verbose" || captured.installScriptArgs[1] != "--dry-run" {
		t.Fatalf("unexpected script args after --: %#v", captured.installScriptArgs)
	}
}

func TestRunInstallResumeContainerSkipsResourceResolution(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("TUPRWRE_DIR", tempHome)

	origFlow := installFlow
	origReader := installArgsReader
	origContainerID := installContainerID
	origMemoryLimit := installMemoryLimit
	t.Cleanup(func() {
		installFlow = origFlow
		installArgsReader = origReader
		installContainerID = origContainerID
		installMemoryLimit = origMemoryLimit
	})

	installContainerID = "fake-container-id"
	installMemoryLimit = "not-a-size"
	installArgsReader = func() []string { return []string{"tuprwre", "install", "--", "echo", "ok"} }

	called := false
	var gotReq installRequest
	installFlow = func(_ *cobra.Command, _ *config.Config, req installRequest) error {
		called = true
		gotReq = req
		return nil
	}

	if err := runInstall(&cobra.Command{}, nil); err != nil {
		t.Fatalf("runInstall failed: %v", err)
	}
	if !called {
		t.Fatal("expected installFlow to be called")
	}
	if gotReq.containerID != installContainerID {
		t.Fatalf("unexpected containerID: got=%q want=%q", gotReq.containerID, installContainerID)
	}
	if gotReq.memoryLimit != installMemoryLimit {
		t.Fatalf("unexpected memory limit: got=%q want=%q", gotReq.memoryLimit, installMemoryLimit)
	}
}
