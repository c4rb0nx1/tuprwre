package main

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/c4rb0nx1/tuprwre/internal/config"
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
	t.Cleanup(func() {
		installFlow = origFlow
		installArgsReader = origReader
	})

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
