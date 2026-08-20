package tprsh

import (
	"context"
	"io"
	"os"
	"strings"

	"mvdan.cc/sh/v3/interp"
	"mvdan.cc/sh/v3/syntax"
)

// CheckResult is the verdict for one script, suitable for driving an external
// gate such as an agent-harness pre-execution hook.
type CheckResult struct {
	Allowed  bool
	Reason   string
	Commands []string
}

// Check evaluates a script against policy without running any of it. It uses
// the same parser and the same handlers as Run, so a verdict here matches what
// Run would decide — no second grammar, which is the failure that produced
// lshell's escape CVEs.
//
// Evaluation is a dry run: external commands are policy-checked and skipped
// rather than executed, and writes are discarded, so checking a script has no
// side effects. Builtins and control flow still run in-process, which keeps cwd
// and variable state accurate for the commands that follow.
func (s *Shell) Check(ctx context.Context, src string) CheckResult {
	file, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(
		strings.NewReader(src), "tprsh")
	if err != nil {
		return CheckResult{Reason: "parse error: " + err.Error()}
	}
	if err := staticReject(file, s.workspace); err != nil {
		return CheckResult{Reason: err.(*DenyError).Reason}
	}

	var seen []string
	var firstDenial string

	runner, err := interp.New(
		interp.Dir(s.workspace),
		interp.Env(s.lockedEnv()),
		interp.StdIO(nil, io.Discard, io.Discard),
		interp.CallHandler(func(_ context.Context, args []string) ([]string, error) {
			if dangerousBuiltins[args[0]] && firstDenial == "" {
				firstDenial = "builtin " + args[0] + " is not permitted"
			}
			return args, nil
		}),
		// Writes are suppressed so a check never mutates the workspace;
		// reads pass through so conditionals behave as they would at runtime.
		interp.OpenHandler(func(ctx context.Context, path string, flag int, perm os.FileMode) (io.ReadWriteCloser, error) {
			if flag&(os.O_WRONLY|os.O_RDWR|os.O_CREATE|os.O_APPEND|os.O_TRUNC) != 0 {
				return discardFile{}, nil
			}
			return s.openHandler(ctx, path, flag, perm)
		}),
		interp.ExecHandlers(func(next interp.ExecHandlerFunc) interp.ExecHandlerFunc {
			return func(ctx context.Context, args []string) error {
				seen = append(seen, strings.Join(args, " "))
				if err := CheckPolicy(args[0], args[1:], s.workspace); err != nil {
					if firstDenial == "" {
						firstDenial = err.(*DenyError).Reason
					}
					return interp.NewExitStatus(1)
				}
				if _, err := s.resolveBinary(args[0]); err != nil {
					if firstDenial == "" {
						firstDenial = err.(*DenyError).Reason
					}
					return interp.NewExitStatus(1)
				}
				// Approved: report success without running anything.
				return nil
			}
		}),
	)
	if err != nil {
		return CheckResult{Reason: "interpreter error: " + err.Error()}
	}
	_ = runner.Run(ctx, file)

	if firstDenial != "" {
		return CheckResult{Reason: firstDenial, Commands: seen}
	}
	return CheckResult{Allowed: true, Commands: seen}
}

// discardFile satisfies the redirection target interface while dropping writes.
type discardFile struct{}

func (discardFile) Read([]byte) (int, error)    { return 0, io.EOF }
func (discardFile) Write(p []byte) (int, error) { return len(p), nil }
func (discardFile) Close() error                { return nil }
