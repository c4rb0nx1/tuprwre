package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func runShellWithTestHarness(t *testing.T, argv []string, commandFlag string, configure func(stdout, stderr *bytes.Buffer)) (int, string, string, error) {
	t.Helper()

	prevCommand := shellCommand
	prevIntercept := append([]string{}, shellIntercept...)
	prevAllow := append([]string{}, shellAllow...)
	prevExec := shellExec
	prevExit := shellExit
	prevArgsReader := shellArgsReader
	prevStdin := shellStdin
	prevStdout := shellStdout
	prevStderr := shellStderr

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	exitCode := -1

	shellCommand = commandFlag
	shellExec = execCommandForTests
	shellArgsReader = func() []string { return argv }
	shellStdin = strings.NewReader("")
	shellStdout = stdout
	shellStderr = stderr
	_ = os.Setenv("HOME", t.TempDir())
	shellExit = func(code int) {
		exitCode = code
	}

	if configure != nil {
		configure(stdout, stderr)
	}

	t.Cleanup(func() {
		shellCommand = prevCommand
		shellIntercept = prevIntercept
		shellAllow = prevAllow
		shellExec = prevExec
		shellExit = prevExit
		shellArgsReader = prevArgsReader
		shellStdin = prevStdin
		shellStdout = prevStdout
		shellStderr = prevStderr
	})

	cmd := &cobra.Command{Use: "shell"}
	cmd.Flags().String("command", "", "")
	if commandFlag != "" {
		if err := cmd.Flags().Set("command", commandFlag); err != nil {
			t.Fatalf("set command flag: %v", err)
		}
	}

	runErr := runShell(cmd, nil)
	return exitCode, stdout.String(), stderr.String(), runErr
}

func execCommandForTests(name string, args ...string) *exec.Cmd {
	return exec.Command(name, args...)
}

func TestParseShellCommandFromArgv(t *testing.T) {
	tests := []struct {
		name      string
		argv      []string
		want      string
		wantSet   bool
		wantError bool
	}{
		{name: "short flag", argv: []string{"tuprwre", "shell", "-c", "echo hi"}, want: "echo hi", wantSet: true},
		{name: "long flag", argv: []string{"tuprwre", "shell", "--command", "echo hi"}, want: "echo hi", wantSet: true},
		{name: "short equals", argv: []string{"tuprwre", "shell", "-c=echo hi"}, want: "echo hi", wantSet: true},
		{name: "long equals", argv: []string{"tuprwre", "shell", "--command=echo hi"}, want: "echo hi", wantSet: true},
		{name: "missing payload", argv: []string{"tuprwre", "shell", "-c"}, wantError: true},
		{name: "not set", argv: []string{"tuprwre", "shell"}, wantSet: false},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got, set, err := parseShellCommandFromArgv(tc.argv)
			if tc.wantError {
				if err == nil {
					t.Fatal("expected error but got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if set != tc.wantSet {
				t.Fatalf("set mismatch: got=%v want=%v", set, tc.wantSet)
			}
			if got != tc.want {
				t.Fatalf("command mismatch: got=%q want=%q", got, tc.want)
			}
		})
	}
}

func TestRunShellCommandModeWorksAndIsSilent(t *testing.T) {
	exitCode, stdout, stderr, err := runShellWithTestHarness(
		t,
		[]string{"tuprwre", "shell", "-c", "printf 'hello\n'"},
		"",
		nil,
	)

	if err != nil {
		t.Fatalf("runShell returned error: %v", err)
	}
	if exitCode != -1 {
		t.Fatalf("unexpected forced exit code: %d", exitCode)
	}
	if stdout != "hello\n" {
		t.Fatalf("unexpected stdout: %q", stdout)
	}
	if strings.Contains(stderr, "[tuprwre] Starting protected shell") || strings.Contains(stderr, "[tuprwre] Exited protected shell") {
		t.Fatalf("stderr contains forbidden banner text: %q", stderr)
	}
}

func TestShellInterceptFlag(t *testing.T) {
	shellIntercept = []string{"brew"}

	exitCode, stdout, stderr, err := runShellWithTestHarness(
		t,
		[]string{"tuprwre", "shell", "-c", `if [ -f "$TUPRWRE_WRAPPER_DIR/brew" ] ; then echo "ok"; fi`},
		"",
		nil,
	)

	if err != nil {
		t.Fatalf("runShell returned error: %v", err)
	}
	if exitCode != -1 {
		t.Fatalf("unexpected forced exit code: %d", exitCode)
	}
	if stdout != "ok\n" {
		t.Fatalf("unexpected stdout: %q", stdout)
	}
	if stderr != "" {
		t.Fatalf("unexpected stderr: %q", stderr)
	}
}

func TestShellAllowFlag(t *testing.T) {
	shellAllow = []string{"curl"}

	exitCode, stdout, stderr, err := runShellWithTestHarness(
		t,
		[]string{"tuprwre", "shell", "-c", `if [ -f "$TUPRWRE_WRAPPER_DIR/apt" ] && [ ! -f "$TUPRWRE_WRAPPER_DIR/curl" ] ; then echo "ok"; fi`},
		"",
		nil,
	)

	if err != nil {
		t.Fatalf("runShell returned error: %v", err)
	}
	if exitCode != -1 {
		t.Fatalf("unexpected forced exit code: %d", exitCode)
	}
	if stdout != "ok\n" {
		t.Fatalf("unexpected stdout: %q", stdout)
	}
	if stderr != "" {
		t.Fatalf("unexpected stderr: %q", stderr)
	}
}

func TestGenerateWrappersCustomList(t *testing.T) {
	tmpDir := t.TempDir()
	if err := generateWrappers(tmpDir, []string{"brew", "cargo"}); err != nil {
		t.Fatalf("generateWrappers returned error: %v", err)
	}

	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatalf("read temp dir: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("unexpected wrapper count: %d", len(entries))
	}

	brewPath := filepath.Join(tmpDir, "brew")
	cargoPath := filepath.Join(tmpDir, "cargo")
	brewScript, err := os.ReadFile(brewPath)
	if err != nil {
		t.Fatalf("read brew wrapper: %v", err)
	}
	cargoScript, err := os.ReadFile(cargoPath)
	if err != nil {
		t.Fatalf("read cargo wrapper: %v", err)
	}
	if !strings.Contains(string(brewScript), "tuprwre wrapper for brew") {
		t.Fatalf("brew wrapper content missing brew reference")
	}
	if !strings.Contains(string(cargoScript), "tuprwre wrapper for cargo") {
		t.Fatalf("cargo wrapper content missing cargo reference")
	}
}

func TestRunShellCommandModeEchoHiExactStdout(t *testing.T) {
	exitCode, stdout, stderr, err := runShellWithTestHarness(
		t,
		[]string{"tuprwre", "shell", "-c", "echo hi"},
		"",
		nil,
	)

	if err != nil {
		t.Fatalf("runShell returned error: %v", err)
	}
	if exitCode != -1 {
		t.Fatalf("unexpected forced exit code: %d", exitCode)
	}
	if stdout != "hi\n" {
		t.Fatalf("unexpected stdout: %q", stdout)
	}
	if stderr != "" {
		t.Fatalf("unexpected stderr: %q", stderr)
	}
}

func TestRunShellCommandModeJSONStdoutPurity(t *testing.T) {
	exitCode, stdout, stderr, err := runShellWithTestHarness(
		t,
		[]string{"tuprwre", "shell", "-c", `printf '{"ok":true}'`},
		"",
		nil,
	)

	if err != nil {
		t.Fatalf("runShell returned error: %v", err)
	}
	if exitCode != -1 {
		t.Fatalf("unexpected forced exit code: %d", exitCode)
	}
	if stdout != `{"ok":true}` {
		t.Fatalf("stdout is not JSON-pure: %q", stdout)
	}
	if stderr != "" {
		t.Fatalf("unexpected stderr noise: %q", stderr)
	}
}

func TestRunShellCommandModePropagatesEnv(t *testing.T) {
	originalPath := os.Getenv("PATH")
	if err := os.Setenv("SHELL", "/bin/sh"); err != nil {
		t.Fatalf("set SHELL: %v", err)
	}
	if err := os.Setenv("PATH", "/usr/bin:/bin"); err != nil {
		t.Fatalf("set PATH: %v", err)
	}
	if err := os.Setenv("TUPRWRE_SESSION_ID", "sess-123"); err != nil {
		t.Fatalf("set session id: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Unsetenv("TUPRWRE_SESSION_ID")
		_ = os.Setenv("PATH", originalPath)
	})

	_, stdout, stderr, err := runShellWithTestHarness(
		t,
		[]string{"tuprwre", "shell", "-c", `printf "%s|%s|%s|%s" "$TUPRWRE_SHELL" "$TUPRWRE_WRAPPER_DIR" "$TUPRWRE_SESSION_ID" "$PATH"`},
		"",
		nil,
	)

	if err != nil {
		t.Fatalf("runShell returned error: %v", err)
	}
	if stderr != "" {
		t.Fatalf("unexpected stderr: %q", stderr)
	}

	parts := strings.Split(stdout, "|")
	if len(parts) != 4 {
		t.Fatalf("unexpected env output format: %q", stdout)
	}
	if parts[0] != "1" {
		t.Fatalf("TUPRWRE_SHELL mismatch: %q", parts[0])
	}
	if parts[1] == "" {
		t.Fatal("TUPRWRE_WRAPPER_DIR must be non-empty")
	}
	if parts[2] != "sess-123" {
		t.Fatalf("TUPRWRE_SESSION_ID mismatch: %q", parts[2])
	}

	expectedPrefix := parts[1] + string(os.PathListSeparator)
	if !strings.HasPrefix(parts[3], expectedPrefix) {
		t.Fatalf("PATH is not hijacked with wrapper prefix: %q", parts[3])
	}
	if !strings.Contains(parts[3], "/usr/bin:/bin") {
		t.Fatalf("PATH lost original components: %q", parts[3])
	}
}

func TestRunShellCommandModeExitCodePropagation(t *testing.T) {
	exitCode, _, _, err := runShellWithTestHarness(
		t,
		[]string{"tuprwre", "shell", "-c", "exit 7"},
		"",
		nil,
	)

	if err != nil {
		t.Fatalf("runShell returned error: %v", err)
	}
	if exitCode != 7 {
		t.Fatalf("exit code mismatch: got=%d want=7", exitCode)
	}
}

func TestRunShellCommandModeKeepsBlockedCommandBehavior(t *testing.T) {
	exitCode, _, stderr, err := runShellWithTestHarness(
		t,
		[]string{"tuprwre", "shell", "-c", "apt-get install -y jq"},
		"",
		nil,
	)

	if err != nil {
		t.Fatalf("runShell returned error: %v", err)
	}
	if exitCode == -1 {
		t.Fatal("expected non-zero exit code from blocked command")
	}
	if !strings.Contains(stderr, "[tuprwre] Intercepted: apt-get") {
		t.Fatalf("expected interception message, got: %q", stderr)
	}
	if strings.Contains(stderr, "[tuprwre] Starting protected shell") || strings.Contains(stderr, "[tuprwre] Exited protected shell") {
		t.Fatalf("stderr contains forbidden banner text: %q", stderr)
	}
}

func TestRunShellCommandModeBlockedCommandWritesOnlyBlockReasonToStderr(t *testing.T) {
	exitCode, stdout, stderr, err := runShellWithTestHarness(
		t,
		[]string{"tuprwre", "shell", "-c", "apt-get install -y jq"},
		"",
		nil,
	)

	if err != nil {
		t.Fatalf("runShell returned error: %v", err)
	}
	if exitCode == -1 {
		t.Fatal("expected non-zero exit code from blocked command")
	}
	if stdout != "" {
		t.Fatalf("expected empty stdout, got: %q", stdout)
	}

	expected := "[tuprwre] Intercepted: apt-get install -y jq\n" +
		"[tuprwre] For sandboxed execution, use: tuprwre install -- \"apt-get install -y jq\"\n" +
		"\n" +
		"[tuprwre] Command blocked. Use 'tuprwre install' for safe execution.\n"
	if stderr != expected {
		t.Fatalf("unexpected stderr block output:\nwant=%q\n got=%q", expected, stderr)
	}
}

func TestResolveShellCommandUsesFlagWhenArgvNotSet(t *testing.T) {
	cmd := &cobra.Command{Use: "shell"}
	cmd.Flags().String("command", "", "")
	if err := cmd.Flags().Set("command", "printf ok"); err != nil {
		t.Fatalf("set command flag: %v", err)
	}

	prevArgs := shellArgsReader
	prevCommand := shellCommand
	t.Cleanup(func() {
		shellArgsReader = prevArgs
		shellCommand = prevCommand
	})

	shellCommand = "printf ok"
	shellArgsReader = func() []string { return []string{"tuprwre", "shell"} }

	got, has, err := resolveShellCommand(cmd)
	if err != nil {
		t.Fatalf("resolveShellCommand: %v", err)
	}
	if !has {
		t.Fatal("expected command mode")
	}
	if got != "printf ok" {
		t.Fatalf("command mismatch: got=%q", got)
	}
}

func TestRunShellInteractiveStillPrintsBanners(t *testing.T) {
	exitCode, _, stderr, err := runShellWithTestHarness(
		t,
		[]string{"tuprwre", "shell"},
		"",
		func(stdout, _ *bytes.Buffer) {
			fmt.Fprint(stdout, "")
		},
	)

	if err != nil {
		t.Fatalf("runShell returned error: %v", err)
	}
	if exitCode != -1 {
		t.Fatalf("unexpected forced exit code: %d", exitCode)
	}
	if !strings.Contains(stderr, "[tuprwre] Starting protected shell") {
		t.Fatalf("missing interactive start banner: %q", stderr)
	}
	if !strings.Contains(stderr, "[tuprwre] Exited protected shell") {
		t.Fatalf("missing interactive exit banner: %q", stderr)
	}
}
