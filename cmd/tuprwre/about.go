package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

var aboutCmd = &cobra.Command{
	Use:   "about",
	Short: "Show a human-friendly overview of tuprwre",
	Long:  "Prints what tuprwre is, why it exists, and the safest way to use it with agents.",
	RunE:  runAbout,
}

func runAbout(cmd *cobra.Command, args []string) error {
	out := cmd.OutOrStdout()

	_, err := fmt.Fprintf(out, `tuprwre %s

tuprwre is your safety layer for agentic shell access.
It keeps risky install commands off your host and runs them in Docker sandboxes instead.

What it gives you:
- Intercepts dangerous install commands in a protected shell
- Runs installations inside isolated containers
- Discovers installed binaries and creates host-side shims
- Lets those tools feel local while execution stays sandboxed

Quick start:
1) Enter protected mode:   tuprwre shell
2) Install safely:         tuprwre install -- "<install_command>"
3) Use generated tools:    <tool> --help

Tip: use 'tuprwre --help' to explore commands.
`, version)

	return err
}
