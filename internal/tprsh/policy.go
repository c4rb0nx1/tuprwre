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
	// NumericShorthand permits `-<digits>`, the traditional line-count form
	// accepted by head and tail.
	NumericShorthand bool
	Validate         func(cmd string, rest []string, workspace string) error
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
	"head":  {Flags: set("-n", "-c"), ConfineArgs: true, NumericShorthand: true},
	"wc":    {Flags: set("-l", "-w", "-c", "-m"), ConfineArgs: true},
	// Hashing and inspection: no exec primitive, no write path. Measured as
	// wrongly blocked in the p02 lab run, where the agent burned 4x the tokens
	// hunting for a permitted way to checksum a file.
	"shasum":    {Flags: set("-a", "-b", "-t", "-p"), ConfineArgs: true},
	"sha256sum": {Flags: set("-b", "-t", "--tag"), ConfineArgs: true},
	"sha1sum":   {Flags: set("-b", "-t", "--tag"), ConfineArgs: true},
	"md5":       {Flags: set("-q", "-r"), ConfineArgs: true},
	"md5sum":    {Flags: set("-b", "-t", "--tag"), ConfineArgs: true},
	"cksum":     {ConfineArgs: true},
	"stat":      {Flags: set("-f", "-x", "-c", "--format", "-L", "-t"), ConfineArgs: true},
	"file":      {Flags: set("-b", "-i", "--mime", "-L"), ConfineArgs: true},
	"tail":      {Flags: set("-n", "-c", "-f", "-r", "-q"), ConfineArgs: true, NumericShorthand: true},
	"du":        {Flags: set("-h", "-s", "-k", "-m", "-a", "-c", "-d", "--max-depth"), ConfineArgs: true},
	"df":        {Flags: set("-h", "-k", "-m", "-i", "-P")},
	"diff":      {Flags: set("-u", "-r", "-q", "-i", "-w", "-b", "-N", "--brief", "--unified"), ConfineArgs: true},
	"basename":  {Flags: set("-s", "-a")},
	"dirname":   {},
	"realpath":  {Flags: set("-q", "--relative-to"), ConfineArgs: true},
	"which":     {Flags: set("-a")},
	"printenv":  {},
	"ps":        {Flags: set("-e", "-f", "-a", "-x", "-u", "-o", "aux", "-p", "-A")},
	"sort": {
		// --compress-program and --files0-from run or read arbitrary things,
		// and -o writes; deny-by-default keeps all three out.
		Flags:       set("-n", "-r", "-u", "-k", "-t", "-f", "-h", "-b", "-g", "-V"),
		ConfineArgs: true,
	},
	"uniq": {Flags: set("-c", "-d", "-u", "-i", "-f", "-s"), ConfineArgs: true},
	"cut":  {Flags: set("-d", "-f", "-c", "-b", "-s", "--delimiter", "--fields"), ConfineArgs: true},
	"tr":   {Flags: set("-d", "-s", "-c", "-t")},
	"grep": {
		// No exec primitive in POSIX or GNU grep. Path arguments stay confined.
		Flags: set("-i", "-v", "-n", "-c", "-l", "-L", "-r", "-R", "-E", "-F", "-w", "-x",
			"-q", "-s", "-h", "-H", "-o", "-A", "-B", "-C", "-e", "-f", "--color",
			"--include", "--exclude", "-m", "-a", "-I"),
		ConfineArgs: true,
	},
	"egrep": {Flags: set("-i", "-v", "-n", "-c", "-l", "-r", "-w", "-o", "-q"), ConfineArgs: true},
	"fgrep": {Flags: set("-i", "-v", "-n", "-c", "-l", "-r", "-w", "-o", "-q"), ConfineArgs: true},

	// Commands that take another command: recurse into the inner command
	// rather than refusing the wrapper, so `find -exec wc {} +` works while
	// `find -exec sh {} \;` does not.
	// The command-taking wrappers are wired in init(): their validators recurse
	// through CheckPolicy back into this map, which Go rejects as an
	// initialization cycle if written in the literal.
	"find":    {},
	"xargs":   {},
	"env":     {},
	"timeout": {},
	"nice":    {},
	"nohup":   {},

	// Subcommand CLIs: the danger is the verb (kubectl exec, aws s3 cp, git
	// push), not a flag, so they use tool-specific validators.
	"kubectl": {Validate: kubectlValidate},
	"aws":     {Validate: awsValidate},
	"git":     {Validate: gitValidate},
	"openssl": {Validate: opensslValidate},
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

func init() {
	for name, v := range map[string]func(string, []string, string) error{
		"find":    findValidate,
		"xargs":   xargsValidate,
		"env":     envValidate,
		"timeout": timeoutValidate,
		"nice":    prefixValidate,
		"nohup":   prefixValidate,
	} {
		p := allowlist[name]
		p.Validate = v
		allowlist[name] = p
	}
}

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
			if policy.Flags[flag] {
				continue
			}
			// `du -sh` means `du -s -h`. A bundle is accepted only when every
			// letter in it is separately permitted, which is exactly as strict
			// as writing them out, and avoids enumerating every combination.
			if letters, ok := unbundle(flag); ok {
				allowed := true
				for _, f := range letters {
					if !policy.Flags[f] {
						allowed = false
						break
					}
				}
				if allowed {
					continue
				}
			}
			// `tail -1` is the numeric shorthand for `-n 1`.
			if policy.NumericShorthand && isNumericFlag(flag) {
				continue
			}
			return deny("flag %q is not permitted for %s", flag, cmd)
		}
		if policy.ConfineArgs && !withinWorkspace(a, workspace) {
			return deny("path argument outside the workspace is not permitted: %s", a)
		}
	}
	return nil
}

// unbundle splits a single-dash multi-letter flag into its individual short
// flags. Long flags (--foo) and anything containing a digit or separator are
// not bundles.
func unbundle(flag string) ([]string, bool) {
	if !strings.HasPrefix(flag, "-") || strings.HasPrefix(flag, "--") || len(flag) < 3 {
		return nil, false
	}
	out := make([]string, 0, len(flag)-1)
	for _, r := range flag[1:] {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') {
			return nil, false
		}
		out = append(out, "-"+string(r))
	}
	return out, true
}

// isNumericFlag reports whether flag is of the form -123.
func isNumericFlag(flag string) bool {
	if len(flag) < 2 || flag[0] != '-' {
		return false
	}
	for _, r := range flag[1:] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
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
