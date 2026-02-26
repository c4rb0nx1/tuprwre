package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

// dangerousCommands lists commands that should be intercepted and routed through tuprwre
var dangerousCommands = []string{
	"apt",
	"apt-get",
	"npm",
	"pip",
	"pip3",
	"curl",
	"wget",
}

// shellCmd represents the shell command
var shellCmd = &cobra.Command{
	Use:   "shell",
	Short: "Spawn an interactive shell with command interception enabled",
	Long: `Starts a new subshell with a modified PATH that intercepts dangerous commands
(apt, npm, pip, curl, etc.) and routes them through tuprwre for safe sandboxed execution.

This mode is designed for AI agents and users who want transparent sandboxing
of installation commands without manually typing 'tuprwre install'.

Example:
  # Enter the protected shell
  tuprwre shell

  # Now dangerous commands are intercepted
  $ npm install -g some-package
  [tuprwre] Intercepted: npm install -g some-package
  [tuprwre] This would be routed through: tuprwre install -- "npm install -g some-package"
  
  # Type 'exit' to leave the shell and return to normal environment`,
	RunE: runShell,
}

func runShell(cmd *cobra.Command, args []string) error {
	// Create a temporary directory for wrapper scripts
	wrapperDir, err := os.MkdirTemp("", "tuprwre-shell-*")
	if err != nil {
		return fmt.Errorf("failed to create wrapper directory: %w", err)
	}
	defer cleanupWrapperDir(wrapperDir)

	// Generate wrapper scripts for dangerous commands
	if err := generateWrappers(wrapperDir); err != nil {
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

	fmt.Fprintf(os.Stderr, "[tuprwre] Starting protected shell (%s)...\n", shell)
	fmt.Fprintln(os.Stderr, "[tuprwre] Dangerous commands (apt, npm, pip, curl, etc.) are intercepted")
	fmt.Fprintln(os.Stderr, "[tuprwre] Type 'exit' to return to normal shell")

	// Spawn the subshell
	childCmd := exec.Command(shell)
	childCmd.Env = env
	childCmd.Stdin = os.Stdin
	childCmd.Stdout = os.Stdout
	childCmd.Stderr = os.Stderr

	// Run the shell and wait for it to exit
	if err := childCmd.Run(); err != nil {
		// Exit code is expected when user types 'exit'
		if exitErr, ok := err.(*exec.ExitError); ok {
			os.Exit(exitErr.ExitCode())
		}
		return fmt.Errorf("shell execution failed: %w", err)
	}

	fmt.Fprintln(os.Stderr, "\n[tuprwre] Exited protected shell")
	return nil
}

// generateWrappers creates wrapper scripts for dangerous commands
func generateWrappers(wrapperDir string) error {
	for _, cmdName := range dangerousCommands {
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
