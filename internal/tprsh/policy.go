package tprsh

import (
	"fmt"
	"path/filepath"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// DenyError is returned when a command or script is refused. It carries a
// human reason and is distinguishable via errors.As so callers can tell a
// policy denial from a normal non-zero exit.
type DenyError struct {
	Reason string
}

func (e *DenyError) Error() string { return "tprsh: denied: " + e.Reason }

func deny(format string, a ...any) *DenyError {
	return &DenyError{Reason: fmt.Sprintf(format, a...)}
}

// ArgPolicy is the per-binary argument allowlist. Flags are deny-by-default:
// any token starting with '-' that is not in Flags is refused, which is what
// structurally blocks escape flags like find -exec or git -c without needing
// to enumerate them. ConfineArgs requires every positional path argument to
// resolve inside the workspace. Validate, when set, replaces the generic
// flag/confine check with a tool-specific policy (used for subcommand CLIs
// like kubectl and aws whose danger lives in the verb, not the flags).
type ArgPolicy struct {
	Flags       map[string]bool
	ConfineArgs bool
	Validate    func(cmd string, rest []string, workspace string) error
}

// CheckPolicy is the pure policy decision for one command: allowlist
// membership, no path separator in the command name, then either the
// tool-specific validator or the generic flag/confine check. It performs no
// execution, so it is testable without the binary being installed.
func CheckPolicy(cmd string, rest []string, workspace string) error {
	if strings.ContainsRune(cmd, '/') {
		return deny("command names with '/' are not permitted: %s", cmd)
	}
	policy, ok := allowlist[cmd]
	if !ok {
		return deny("command not in allowlist")
	}
	if policy.Validate != nil {
		return policy.Validate(cmd, rest, workspace)
	}
	return checkArgs(cmd, rest, policy, workspace)
}

// trustedBinDirs are the only directories an allowlisted binary may resolve
// into, so a symlink named `uname` pointing at /tmp/evil fails.
var trustedBinDirs = []string{"/usr/bin", "/bin", "/usr/sbin", "/sbin"}

// allowlist maps a bare command name to its argument policy. Everything not
// present is denied. These are read-only, escape-free tools (no shell-out
// flag, no interpreter). Deliberately excludes sh/bash/python/awk/find-with-exec.
var allowlist = map[string]ArgPolicy{
	"uname": {Flags: set("-a", "-s", "-r", "-m", "-n", "-p", "-v")},
	"id":    {Flags: set("-u", "-g", "-n", "-G", "-r")},
	"date":  {Flags: set("-u", "-R", "-Iseconds")},
	"ls":    {Flags: set("-l", "-a", "-h", "-la", "-al", "-R", "-1", "-lh"), ConfineArgs: true},
	"cat":   {Flags: set("-n", "-b"), ConfineArgs: true},
	"head":  {Flags: set("-n", "-c"), ConfineArgs: true},
	"wc":    {Flags: set("-l", "-w", "-c", "-m"), ConfineArgs: true},
	// find is allowlisted but its escape flags (-exec/-execdir/-delete/-fprintf)
	// are absent from Flags, so deny-by-default refuses them while ordinary
	// searches pass. This is the rssh/scponly lesson: gate arguments, not names.
	"find": {Flags: set("-name", "-iname", "-type", "-maxdepth", "-mindepth", "-path", "-print")},
	// Subcommand CLIs: the danger is the verb (kubectl exec, aws s3 cp), not a
	// flag, so they use tool-specific validators enforcing read-only verbs.
	"kubectl": {Validate: kubectlValidate},
	"aws":     {Validate: awsValidate},
}

// dangerousBuiltins are interpreter builtins refused at the call handler. exec
// would replace the process with an unrestricted one; eval/source/. run
// arbitrary text; trap/enable/command reach around the policy.
var dangerousBuiltins = set("exec", "eval", "source", ".", "trap", "enable",
	"command", "builtin", "mapfile", "readarray", "set", "shopt", "export", "unset", "alias",
	// bash declaration builtins can set variables the plain-assignment check
	// would otherwise inspect, so they are refused outright.
	"declare", "typeset", "readonly", "local")

// sensitiveEnv are variable names that must never be assigned: they redirect
// code loading or command resolution (the LD_PRELOAD / BASH_ENV / PATH class).
var sensitiveEnv = set("LD_PRELOAD", "LD_LIBRARY_PATH", "LD_AUDIT", "DYLD_INSERT_LIBRARIES",
	"DYLD_LIBRARY_PATH", "BASH_ENV", "ENV", "IFS", "PATH", "SHELL", "PS1", "PROMPT_COMMAND",
	"GIT_PAGER", "PAGER", "PERL5LIB", "PYTHONPATH", "PYTHONSTARTUP")

func set(xs ...string) map[string]bool {
	m := make(map[string]bool, len(xs))
	for _, x := range xs {
		m[x] = true
	}
	return m
}

// staticReject walks the AST before execution and refuses whole scripts that
// contain escape-enabling grammar: command/process substitution, sensitive
// assignments, fd-dup or out-of-tree redirections, user functions, and
// backtick subst. This is a coarse pre-filter (dynamic dispatch can still hide
// intent), not the boundary — the runtime handlers are.
func staticReject(node syntax.Node, workspace string) error {
	var rejErr error
	syntax.Walk(node, func(n syntax.Node) bool {
		if rejErr != nil {
			return false
		}
		switch x := n.(type) {
		case *syntax.CmdSubst:
			rejErr = deny("command substitution $(...) is not permitted")
		case *syntax.ProcSubst:
			rejErr = deny("process substitution <(...) is not permitted")
		case *syntax.FuncDecl:
			rejErr = deny("function declarations are not permitted")
		case *syntax.CoprocClause:
			// bash-only: spawns a background process outside the exec handler.
			rejErr = deny("coproc is not permitted")
		case *syntax.ParamExp:
			// bash-only: ${!var} resolves a variable name held in another
			// variable, which hides the real target from static inspection.
			if x.Excl {
				rejErr = deny("indirect variable expansion ${!...} is not permitted")
			}
		case *syntax.Assign:
			if x.Name != nil && sensitiveEnv[x.Name.Value] {
				rejErr = deny("assignment to sensitive variable %q is not permitted", x.Name.Value)
			}
		case *syntax.Redirect:
			if err := checkRedirect(x, workspace); err != nil {
				rejErr = err
			}
		}
		return rejErr == nil
	})
	return rejErr
}

// deviceSinks are the standard character devices a redirection may target
// outside the workspace. They carry no data off the machine and `2>/dev/null`
// is ubiquitous, so refusing them only produces false denials.
var deviceSinks = set("/dev/null", "/dev/stdout", "/dev/stderr", "/dev/zero")

// checkRedirect permits literal file redirections that resolve inside the
// workspace, plus the device sinks above. Here-documents and here-strings
// carry inline content rather than a path and touch no file, so they pass.
// fd-dups (>&) and non-literal targets are refused.
func checkRedirect(r *syntax.Redirect, workspace string) error {
	switch r.Op {
	case syntax.DplOut, syntax.DplIn:
		return deny("file-descriptor duplication redirection is not permitted")
	case syntax.Hdoc, syntax.DashHdoc, syntax.WordHdoc:
		// Inline stdin content, not a filesystem path.
		return nil
	}
	if r.Word == nil {
		return deny("redirection with no target is not permitted")
	}
	lit := r.Word.Lit()
	if lit == "" {
		return deny("redirection target must be a literal path")
	}
	if deviceSinks[lit] {
		return nil
	}
	if !withinWorkspace(lit, workspace) {
		return deny("redirection outside the workspace is not permitted: %s", lit)
	}
	return nil
}

// withinWorkspace reports whether path (resolved against workspace) stays
// inside it after cleaning, defeating ../ traversal.
func withinWorkspace(path, workspace string) bool {
	abs := path
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(workspace, abs)
	}
	abs = filepath.Clean(abs)
	rel, err := filepath.Rel(workspace, abs)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// checkArgs applies a binary's argument policy: reject any command containing a
// path separator (forcing allowlist names), deny-by-default on flags, and
// confine positional paths to the workspace when required.
func checkArgs(cmd string, args []string, policy ArgPolicy, workspace string) error {
	if strings.ContainsRune(cmd, '/') {
		return deny("command names with '/' are not permitted: %s", cmd)
	}
	for _, a := range args {
		if strings.HasPrefix(a, "-") && a != "-" {
			flag := a
			if i := strings.IndexByte(flag, '='); i >= 0 {
				flag = flag[:i]
			}
			if !policy.Flags[flag] {
				return deny("flag %q is not permitted for %s", flag, cmd)
			}
			continue
		}
		if policy.ConfineArgs && !withinWorkspace(a, workspace) {
			return deny("path argument outside the workspace is not permitted: %s", a)
		}
	}
	return nil
}

// isTrustedDir reports whether dir is one of the trusted binary directories,
// after symlink resolution.
func isTrustedDir(dir string) bool {
	for _, t := range trustedBinDirs {
		if dir == t {
			return true
		}
	}
	return false
}
