package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/c4rb0nx1/tuprwre/internal/config"
	"github.com/spf13/cobra"
)

var (
	shellCommand    string
	shellIntercept  []string
	shellAllow      []string
	shellExec                 = exec.Command
	shellExit                 = os.Exit
	shellArgsReader           = func() []string { return os.Args }
	shellStdin      io.Reader = os.Stdin
	shellStdout     io.Writer = os.Stdout
	shellStderr     io.Writer = os.Stderr
)

// shellCmd represents the shell command
var shellCmd = &cobra.Command{
	Use:   "shell [-c <command>]",
	Short: "Spawn an interactive shell with command interception enabled",
	Long: `Starts a new subshell with a modified PATH that intercepts dangerous commands
(apt, pip, curl, etc.), blocks them, and guides users to run them via tuprwre install.

Modes:
  - Interactive (no -c): starts a protected shell session.
  - Non-interactive (-c "<cmd>"): runs one command as a POSIX proxy shell.

In -c mode, tuprwre must remain silent (no banner/session text on stdout/stderr),
except stderr output when a command is explicitly blocked by interception wrappers.

This mode is designed for AI agents and users who want guardrails around
installation commands while keeping non-intercepted commands unchanged.

Example:
  # Enter the protected shell (interactive)
  tuprwre shell

  # Now dangerous commands are intercepted
  $ apt-get install -y jq
  [tuprwre] Intercepted: apt-get install -y jq
  [tuprwre] For sandboxed execution, use: tuprwre install -- "apt-get install -y jq"
  
  # Type 'exit' to leave the shell and return to normal environment

  # Non-interactive proxy mode
  tuprwre shell -c "echo hello"

  # IDE/TUI automation-friendly usage
  tuprwre shell -c "npm run build"`,
	RunE: runShell,
}

func runShell(cmd *cobra.Command, args []string) error {
	commandString, hasCommand, err := resolveShellCommand(cmd)
	if err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	interceptList := cfg.InterceptCommands
	for _, command := range shellIntercept {
		found := false
		for _, existing := range interceptList {
			if existing == command {
				found = true
				break
			}
		}
		if !found {
			interceptList = append(interceptList, command)
		}
	}

	if len(shellAllow) > 0 {
		allowSet := make(map[string]bool, len(shellAllow))
		for _, command := range shellAllow {
			allowSet[command] = true
		}
		filtered := make([]string, 0, len(interceptList))
		for _, command := range interceptList {
			if !allowSet[command] {
				filtered = append(filtered, command)
			}
		}
		interceptList = filtered
	}

	// Create a temporary directory for wrapper scripts
	wrapperDir, err := os.MkdirTemp("", "tuprwre-shell-*")
	if err != nil {
		return fmt.Errorf("failed to create wrapper directory: %w", err)
	}
	defer cleanupWrapperDir(wrapperDir)

	// Generate wrapper scripts for intercepted commands
	if err := generateWrappers(wrapperDir, interceptList); err != nil {
		return fmt.Errorf("failed to generate wrapper scripts: %w", err)
	}

	// Determine which shell to use
	shell := determineShell()

	// Prepare modified PATH with wrapper directory at the front
	newPath := wrapperDir + string(os.PathListSeparator) + os.Getenv("PATH")

	// Prepare environment
	env := os.Environ()
	env = setEnvVar(env, "PATH", newPath)
	env = setEnvVar(env, "TUPRWRE_SHELL", "1")
	env = setEnvVar(env, "TUPRWRE_WRAPPER_DIR", wrapperDir)
	env = setEnvVar(env, "TUPRWRE_SESSION_ID", os.Getenv("TUPRWRE_SESSION_ID"))
	env = setEnvVar(env, "BASH_SILENCE_DEPRECATION_WARNING", "1")

	if !hasCommand {
		fmt.Fprintf(shellStderr, "[tuprwre] Starting protected shell (%s)...\n", shell)

		interceptPreview := strings.Join(interceptList, ", ")
		if len(interceptList) > 5 {
			interceptPreview = strings.Join(interceptList[:5], ", ") + ", ..."
		}
		fmt.Fprintf(shellStderr, "[tuprwre] Dangerous commands (%s) are intercepted\n", interceptPreview)
		fmt.Fprintln(shellStderr, "[tuprwre] Type 'exit' to return to normal shell")
	}

	// Spawn the subshell
	childCmd := shellExec(shell)
	if hasCommand {
		childCmd = shellExec(shell, "-c", commandString)
	}
	childCmd.Env = env
	childCmd.Stdin = shellStdin
	childCmd.Stdout = shellStdout
	childCmd.Stderr = shellStderr

	// Run the shell and wait for it to exit
	if err := childCmd.Run(); err != nil {
		// Exit code is expected when user types 'exit'
		if exitErr, ok := err.(*exec.ExitError); ok {
			shellExit(exitErr.ExitCode())
			return nil
		}
		return fmt.Errorf("shell execution failed: %w", err)
	}

	if !hasCommand {
		fmt.Fprintln(shellStderr, "\n[tuprwre] Exited protected shell")
	}
	return nil
}

func resolveShellCommand(cmd *cobra.Command) (string, bool, error) {
	hasCommand := cmd.Flags().Changed("command")
	commandString := shellCommand

	rawCommand, rawHasCommand, err := parseShellCommandFromArgv(shellArgsReader())
	if err != nil {
		return "", false, err
	}
	if rawHasCommand {
		return rawCommand, true, nil
	}

	return commandString, hasCommand, nil
}

func parseShellCommandFromArgv(argv []string) (string, bool, error) {
	shellIndex := -1
	for i, arg := range argv {
		if arg == "shell" {
			shellIndex = i
			break
		}
	}
	if shellIndex == -1 {
		return "", false, nil
	}

	for i := shellIndex + 1; i < len(argv); i++ {
		arg := argv[i]
		switch {
		case arg == "-c" || arg == "--command":
			if i+1 >= len(argv) {
				return "", false, fmt.Errorf("missing command string for %s", arg)
			}
			return argv[i+1], true, nil
		case strings.HasPrefix(arg, "-c="):
			return strings.TrimPrefix(arg, "-c="), true, nil
		case strings.HasPrefix(arg, "--command="):
			return strings.TrimPrefix(arg, "--command="), true, nil
		}
	}

	return "", false, nil
}

// generateWrappers creates wrapper scripts for dangerous commands
func generateWrappers(wrapperDir string, commands []string) error {
	for _, cmdName := range commands {
		wrapperPath := filepath.Join(wrapperDir, cmdName)
		wrapperScript := generateWrapperScript(cmdName)

		if err := os.WriteFile(wrapperPath, []byte(wrapperScript), 0755); err != nil {
			return fmt.Errorf("failed to create wrapper for %s: %w", cmdName, err)
		}
	}
	return nil
}

// generateWrapperScript creates the content of a wrapper script
func generateWrapperScript(cmdName string) string {
	return fmt.Sprintf(`#!/bin/sh
# tuprwre wrapper for %s
# This script intercepts the command and routes it through tuprwre

echo "[tuprwre] Intercepted: %s $*" >&2
echo "[tuprwre] For sandboxed execution, use: tuprwre install -- \"%s $*\"" >&2
echo "" >&2

# For MVP, we just show the message and do NOT execute the command
# In the future, this could prompt the user or auto-route through tuprwre
echo "[tuprwre] Command blocked. Use 'tuprwre install' for safe execution." >&2
exit 1
`, cmdName, cmdName, cmdName)
}

// determineShell returns the shell to use (bash, zsh, or sh)
func determineShell() string {
	// Check if user has a preferred shell
	if shell := os.Getenv("SHELL"); shell != "" {
		return shell
	}

	// Try to find a suitable shell
	for _, sh := range []string{"bash", "zsh", "sh"} {
		if path, err := exec.LookPath(sh); err == nil {
			return path
		}
	}

	// Fallback to sh
	return "/bin/sh"
}

// setEnvVar updates or adds an environment variable in the env slice
func setEnvVar(env []string, key, value string) []string {
	prefix := key + "="
	for i, e := range env {
		if strings.HasPrefix(e, prefix) {
			env[i] = prefix + value
			return env
		}
	}
	return append(env, prefix+value)
}

// cleanupWrapperDir removes the temporary wrapper directory
func cleanupWrapperDir(dir string) {
	os.RemoveAll(dir)
}

func init() {
	shellCmd.Flags().StringVarP(&shellCommand, "command", "c", "", "Run command string in non-interactive mode")
	shellCmd.Flags().StringArrayVar(&shellIntercept, "intercept", nil, "Additional commands to intercept")
	shellCmd.Flags().StringArrayVar(&shellAllow, "allow", nil, "Commands to exclude from interception")
}
