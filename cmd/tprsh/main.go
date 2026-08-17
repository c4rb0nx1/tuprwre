// Command tprsh is a proof-of-concept hardened interception shell. It runs a
// single command (-c) or a REPL through the tprsh in-process interpreter,
// enforcing the binary+argument allowlist and writing a tamper-evident audit
// log. This is a research PoC, not the production tuprwre shell.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/c4rb0nx1/tuprwre/internal/tprsh"
)

func main() {
	var (
		cmd       = flag.String("c", "", "command to run (non-interactive)")
		workspace = flag.String("workspace", ".", "workspace directory (only writable/readable tree)")
		auditPath = flag.String("audit", "", "audit log path (default: <workspace>/../tprsh-audit.jsonl)")
	)
	flag.Parse()

	ws, err := filepath.Abs(*workspace)
	if err != nil {
		fatal(err)
	}
	if *auditPath == "" {
		*auditPath = filepath.Join(filepath.Dir(ws), "tprsh-audit.jsonl")
	}

	auditor, err := tprsh.NewAuditor(*auditPath)
	if err != nil {
		fatal(err)
	}
	defer auditor.Close()

	sh, err := tprsh.New(ws, auditor)
	if err != nil {
		fatal(err)
	}

	ctx := context.Background()

	if *cmd != "" {
		os.Exit(run(ctx, sh, *cmd))
	}

	fmt.Fprintf(os.Stderr, "tprsh PoC — workspace %s, audit %s\nType commands; Ctrl-D to exit.\n", ws, *auditPath)
	scan := bufio.NewScanner(os.Stdin)
	for {
		fmt.Fprint(os.Stderr, "tprsh> ")
		if !scan.Scan() {
			break
		}
		run(ctx, sh, scan.Text())
	}
}

func run(ctx context.Context, sh *tprsh.Shell, src string) int {
	err := sh.Run(ctx, src)
	if err == nil {
		return 0
	}
	var de *tprsh.DenyError
	if errors.As(err, &de) {
		fmt.Fprintf(os.Stderr, "tprsh: DENIED — %s\n", de.Reason)
		return 126
	}
	fmt.Fprintf(os.Stderr, "tprsh: %v\n", err)
	return 1
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "tprsh:", err)
	os.Exit(1)
}
