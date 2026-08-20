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

// Mode selects whether policy decisions are enforced or merely recorded.
type Mode string

const (
	// ModeEnforce denies commands that fail policy. The default.
	ModeEnforce Mode = "enforce"
	// ModeObserve runs every command but records the verdict policy would
	// have reached. Nothing is blocked: this is for building an evidence base
	// before committing to an allowlist, the same way SELinux permissive mode
	// precedes enforcing. It is NOT a security boundary and must be labelled
	// as such wherever it is surfaced.
	ModeObserve Mode = "observe"
)

// Shell is a hardened interception shell. It parses commands with mvdan.cc/sh
// and executes them in-process, vetting every command against the allowlist
// before exec, resetting the child environment, and auditing every attempt.
type Shell struct {
	workspace string
	audit     *Auditor
	confiner  Confiner
	mode      Mode
	stdin     io.Reader
	stdout    io.Writer
	stderr    io.Writer
}

// New builds a Shell rooted at workspace, writing audit records via auditor.
// Approved commands run unconfined; use NewConfined to add an OS sandbox.
func New(workspace string, auditor *Auditor) (*Shell, error) {
	return NewConfined(workspace, auditor, nopConfiner{})
}

// NewConfined builds a Shell that wraps every approved command with confiner.
func NewConfined(workspace string, auditor *Auditor, confiner Confiner) (*Shell, error) {
	abs, err := filepath.Abs(workspace)
	if err != nil {
		return nil, err
	}
	if confiner == nil {
		confiner = nopConfiner{}
	}
	return &Shell{
		workspace: abs,
		audit:     auditor,
		confiner:  confiner,
		mode:      ModeEnforce,
		stdin:     os.Stdin,
		stdout:    os.Stdout,
		stderr:    os.Stderr,
	}, nil
}

// SetMode switches between enforcing and observe-only policy.
func (s *Shell) SetMode(m Mode) {
	if m == "" {
		m = ModeEnforce
	}
	s.mode = m
}

// Observing reports whether policy decisions are recorded but not enforced.
func (s *Shell) Observing() bool { return s.mode == ModeObserve }

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
	// Bash rather than POSIX: agents emit bash-isms ([[ ]], arrays, <<<) and
	// the transparency goal says they should not have to know otherwise. The
	// wider grammar is compensated for in staticReject, which refuses the
	// bash-only constructs that enable escapes.
	file, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(
		strings.NewReader(src), "tprsh")
	if err != nil {
		return deny("parse error: %v", err)
	}

	if err := staticReject(file, s.workspace); err != nil {
		de := err.(*DenyError)
		if s.mode == ModeObserve {
			_ = s.audit.Append(DecisionShadow, "<script>", []string{src}, s.workspace, de.Reason, 0)
		} else {
			_ = s.audit.Append(DecisionDeny, "<script>", []string{src}, s.workspace, de.Reason, 0)
			return err
		}
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
		if s.mode == ModeObserve {
			_ = s.audit.Append(DecisionShadow, name, args, hc.Dir, "dangerous builtin", 0)
			return args, nil
		}
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
			if blocked, res := s.refuse(cmd, args, hc.Dir, err.(*DenyError).Reason); blocked {
				return res
			}
			// Observe mode: policy would have denied this, but the point of
			// observing is to learn what real agents need, so it runs anyway.
			return next(ctx, args)
		}
		resolved, err := s.resolveBinary(cmd)
		if err != nil {
			if blocked, res := s.refuse(cmd, args, hc.Dir, err.(*DenyError).Reason); blocked {
				return res
			}
			return next(ctx, args)
		}

		// Confine the approved command. The audit records the argv the agent
		// asked for; the sandbox wrapper is an execution detail and must not
		// appear in the ledger.
		execArgs, err := s.confiner.Wrap(resolved, args)
		if err != nil {
			// A sandbox that cannot be applied is refused even while observing:
			// this is an infrastructure failure, not a policy question.
			_ = s.audit.Append(DecisionDeny, cmd, args, hc.Dir, "sandbox unavailable: "+err.Error(), 0)
			return deny("sandbox unavailable: %v", err)
		}

		_ = s.audit.Append(DecisionAllow, cmd, args, hc.Dir, "", 0)
		runErr := next(ctx, execArgs)
		exit := 0
		if runErr != nil {
			exit = 1
		}
		_ = s.audit.Append(DecisionFinish, cmd, args, hc.Dir, "", exit)
		return runErr
	}
}

// refuse records a failed policy decision. In enforce mode it returns
// blocked=true with the error to return; in observe mode it records the verdict
// that would have been reached and lets the caller proceed.
func (s *Shell) refuse(cmd string, args []string, cwd, reason string) (blocked bool, err error) {
	if s.mode == ModeObserve {
		_ = s.audit.Append(DecisionShadow, cmd, args, cwd, reason, 0)
		return false, nil
	}
	_ = s.audit.Append(DecisionDeny, cmd, args, cwd, reason, 0)
	return true, deny("%s", reason)
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
		if s.mode == ModeObserve {
			_ = s.audit.Append(DecisionShadow, "<open>", []string{path}, hc.Dir, "open outside workspace", 0)
			return os.OpenFile(path, flag, perm)
		}
		_ = s.audit.Append(DecisionDeny, "<open>", []string{path}, hc.Dir, "open outside workspace", 0)
		return nil, deny("open outside workspace: %s", path)
	}
	abs := path
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(s.workspace, path)
	}
	return os.OpenFile(abs, flag, perm)
}
