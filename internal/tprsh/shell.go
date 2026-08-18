package tprsh

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"

	"mvdan.cc/sh/v3/expand"
	"mvdan.cc/sh/v3/interp"
	"mvdan.cc/sh/v3/syntax"
)

// Shell is a hardened interception shell. It parses commands with mvdan.cc/sh
// and executes them in-process, vetting every command against the allowlist
// before exec, resetting the child environment, and auditing every attempt.
type Shell struct {
	workspace string
	audit     *Auditor
	stdin     io.Reader
	stdout    io.Writer
	stderr    io.Writer
}

// New builds a Shell rooted at workspace, writing audit records via auditor.
func New(workspace string, auditor *Auditor) (*Shell, error) {
	abs, err := filepath.Abs(workspace)
	if err != nil {
		return nil, err
	}
	return &Shell{
		workspace: abs,
		audit:     auditor,
		stdin:     os.Stdin,
		stdout:    os.Stdout,
		stderr:    os.Stderr,
	}, nil
}

// SetIO overrides the standard streams (used by tests).
func (s *Shell) SetIO(in io.Reader, out, err io.Writer) {
	s.stdin, s.stdout, s.stderr = in, out, err
}

// lockedEnv is the reset environment handed to every child: a fixed PATH, a
// HOME pointing at the workspace (so tools can't read attacker-planted
// dotfiles in a real home), and nothing from the LD_*/BASH_ENV class.
func (s *Shell) lockedEnv() expand.Environ {
	return expand.ListEnviron(
		"PATH=/usr/bin:/bin:/usr/sbin:/sbin",
		"HOME="+s.workspace,
		"TERM=dumb",
		"LANG=C",
	)
}

// Run parses and executes src. A script rejected by the static pass or by a
// runtime handler returns a *DenyError; a normal non-zero exit returns the
// interpreter's exit-status error.
func (s *Shell) Run(ctx context.Context, src string) error {
	file, err := syntax.NewParser(syntax.Variant(syntax.LangPOSIX)).Parse(
		strings.NewReader(src), "tprsh")
	if err != nil {
		return deny("parse error: %v", err)
	}

	if err := staticReject(file, s.workspace); err != nil {
		de := err.(*DenyError)
		_ = s.audit.Append(DecisionDeny, "<script>", []string{src}, s.workspace, de.Reason, 0)
		return err
	}

	runner, err := interp.New(
		interp.Dir(s.workspace),
		interp.Env(s.lockedEnv()),
		interp.StdIO(s.stdin, s.stdout, s.stderr),
		interp.CallHandler(s.callHandler),
		interp.OpenHandler(s.openHandler),
		interp.ExecHandlers(s.execMiddleware),
	)
	if err != nil {
		return err
	}
	return runner.Run(ctx, file)
}

// callHandler fires on every simple command (builtins included). It refuses
// dangerous builtins and records intent for everything else.
func (s *Shell) callHandler(ctx context.Context, args []string) ([]string, error) {
	hc := interp.HandlerCtx(ctx)
	name := args[0]
	if dangerousBuiltins[name] {
		_ = s.audit.Append(DecisionDeny, name, args, hc.Dir, "dangerous builtin", 0)
		return nil, deny("builtin %q is not permitted", name)
	}
	_ = s.audit.Append(DecisionStart, name, args, hc.Dir, "", 0)
	return args, nil
}

// execMiddleware is the allowlist chokepoint for external commands. Returning
// a *DenyError without calling next means the command never runs.
func (s *Shell) execMiddleware(next interp.ExecHandlerFunc) interp.ExecHandlerFunc {
	return func(ctx context.Context, args []string) error {
		hc := interp.HandlerCtx(ctx)
		cmd := args[0]

		if err := CheckPolicy(cmd, args[1:], s.workspace); err != nil {
			return s.denyExec(cmd, args, hc.Dir, err.(*DenyError).Reason)
		}
		if _, err := s.resolveBinary(cmd); err != nil {
			return s.denyExec(cmd, args, hc.Dir, err.(*DenyError).Reason)
		}

		_ = s.audit.Append(DecisionAllow, cmd, args, hc.Dir, "", 0)
		runErr := next(ctx, args)
		exit := 0
		if runErr != nil {
			exit = 1
		}
		_ = s.audit.Append(DecisionFinish, cmd, args, hc.Dir, "", exit)
		return runErr
	}
}

func (s *Shell) denyExec(cmd string, args []string, cwd, reason string) error {
	_ = s.audit.Append(DecisionDeny, cmd, args, cwd, reason, 0)
	return deny("%s", reason)
}

// resolveBinary finds cmd in a trusted bin dir, resolves symlinks, and
// confirms the real binary still lives in a trusted dir — so a symlink named
// after an allowed tool but pointing elsewhere is refused.
func (s *Shell) resolveBinary(cmd string) (string, error) {
	for _, dir := range trustedBinDirs {
		cand := filepath.Join(dir, cmd)
		if _, err := os.Stat(cand); err != nil {
			continue
		}
		real, err := filepath.EvalSymlinks(cand)
		if err != nil {
			return "", deny("cannot resolve %s: %v", cmd, err)
		}
		if !isTrustedDir(filepath.Dir(real)) {
			return "", deny("%s resolves outside trusted dirs: %s", cmd, real)
		}
		return real, nil
	}
	return "", deny("%s not found in trusted bin dirs", cmd)
}

// openHandler gates redirection-backed file opens at runtime, backstopping the
// static pass. Only /dev/null and workspace-internal paths are permitted.
func (s *Shell) openHandler(ctx context.Context, path string, flag int, perm os.FileMode) (io.ReadWriteCloser, error) {
	if path == "/dev/null" {
		return os.OpenFile(os.DevNull, flag, perm)
	}
	if !withinWorkspace(path, s.workspace) {
		hc := interp.HandlerCtx(ctx)
		_ = s.audit.Append(DecisionDeny, "<open>", []string{path}, hc.Dir, "open outside workspace", 0)
		return nil, deny("open outside workspace: %s", path)
	}
	abs := path
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(s.workspace, path)
	}
	return os.OpenFile(abs, flag, perm)
}
