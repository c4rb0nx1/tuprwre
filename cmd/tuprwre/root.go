package main

import (
	"os"

	"github.com/spf13/cobra"
)

var version = "0.1.0-alpha.1"

var rootCmd = &cobra.Command{
	Use:     "tuprwre",
	Short:   "Sandbox shell script installations with transparent execution",
	Version: version,
	Long: `tuprwre provides a safe environment for executing shell scripts by running
installations inside isolated Docker containers. It discovers installed binaries
and generates lightweight shim scripts on the host for transparent execution.

Example:
  # Install a tool through a script
  tuprwre install -- \
    "curl -L https://install.example.com/tool.sh | bash"

  # Run a sandboxed binary (used by shims internally)
  tuprwre run --container tool:latest -- binary --help`,
}

func Execute() error {
	return rootCmd.Execute()
}

func main() {
	if err := Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.AddCommand(aboutCmd)
	rootCmd.AddCommand(cleanCmd)
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(removeCmd)
	rootCmd.AddCommand(installCmd)
	rootCmd.AddCommand(updateCmd)
	rootCmd.AddCommand(doctorCmd)
	rootCmd.AddCommand(runCmd)
	rootCmd.AddCommand(shellCmd)
}
