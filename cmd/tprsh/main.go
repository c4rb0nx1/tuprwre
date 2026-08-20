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
		sandbox   = flag.String("sandbox", "none", "confinement for approved commands: none|read-only|workspace-write")
		check     = flag.Bool("check", false, "evaluate policy for -c and exit 0 (allow) or 2 (deny) without running anything")
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

	// The audit log is denied to the confined child even if it sits inside the
	// workspace, so its integrity is enforced rather than assumed.
	confiner, err := tprsh.NewConfiner(tprsh.ConfineOptions{
		Mode:      tprsh.SandboxMode(*sandbox),
		Workspace: tprsh.CanonicalDir(ws),
		NoWrite:   []string{tprsh.CanonicalDir(filepath.Dir(*auditPath))},
		NoRead:    tprsh.DefaultProtectedReadPaths(),
	})
	if err != nil {
		fatal(err)
	}

	sh, err := tprsh.NewConfined(ws, auditor, confiner)
	if err != nil {
		fatal(err)
	}
	if *sandbox != "none" {
		fmt.Fprintf(os.Stderr, "tprsh: confinement=%s (%s)\n", *sandbox, confiner.Name())
	}

	ctx := context.Background()

	// Check mode is the gate an external harness hook calls: it renders a
	// verdict using the same parser and policy as execution, but runs nothing.
	if *check {
		res := sh.Check(ctx, *cmd)
		if res.Allowed {
			fmt.Println("ALLOW")
			os.Exit(0)
		}
		fmt.Println(res.Reason)
		os.Exit(2)
	}

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
