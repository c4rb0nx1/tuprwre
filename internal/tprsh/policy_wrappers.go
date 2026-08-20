package tprsh

import "strings"

// Commands that take another command as an argument — find -exec, xargs, env,
// timeout and friends. Banning them outright is crude: `find -exec wc -c {} +`
// is an ordinary idiom while `find -exec sh {} \;` is an escape, and the
// difference is the inner command, not the outer one. So the policy recurses
// into the inner command instead of refusing the wrapper.
//
// This is one mechanism for a whole category. Whatever the allowlist says about
// `sh` is automatically what it says about every way of reaching `sh`.

// wrapperInner locates the inner command in a wrapper's argument list and
// returns it with its arguments. ok is false when there is no inner command to
// check (which is a denial for wrappers that require one).
type wrapperInner func(rest []string) (cmd string, args []string, ok bool)

// checkWrapper applies the wrapper's own flag policy, then recurses into the
// inner command with the full policy.
func checkWrapper(name string, rest []string, workspace string, policy ArgPolicy, locate wrapperInner) error {
	inner, innerArgs, ok := locate(rest)
	if !ok {
		// No inner command present: judge it as a plain invocation.
		return checkArgs(name, rest, policy, workspace)
	}
	if err := CheckPolicy(inner, innerArgs, workspace); err != nil {
		return deny("%s would run %q, which is not permitted: %s",
			name, inner, err.(*DenyError).Reason)
	}
	return nil
}

// findInner extracts the command following -exec / -execdir, up to ';' or '+'.
func findInner(rest []string) (string, []string, bool) {
	for i, tok := range rest {
		if tok != "-exec" && tok != "-execdir" && tok != "-ok" && tok != "-okdir" {
			continue
		}
		body := rest[i+1:]
		for j, t := range body {
			if t == ";" || t == "\\;" || t == "+" {
				body = body[:j]
				break
			}
		}
		if len(body) == 0 {
			return "", nil, false
		}
		return body[0], body[1:], true
	}
	return "", nil, false
}

// skipFlags returns the index of the first positional token, treating tokens
// listed in valueFlags as consuming the token after them.
func skipFlags(rest []string, valueFlags map[string]bool) int {
	for i := 0; i < len(rest); i++ {
		tok := rest[i]
		if strings.HasPrefix(tok, "-") && tok != "-" {
			if !strings.ContainsRune(tok, '=') && valueFlags[tok] {
				i++
			}
			continue
		}
		return i
	}
	return -1
}

func simpleInner(valueFlags map[string]bool) wrapperInner {
	return func(rest []string) (string, []string, bool) {
		i := skipFlags(rest, valueFlags)
		if i < 0 {
			return "", nil, false
		}
		return rest[i], rest[i+1:], true
	}
}

// envInner skips VAR=value assignments as well as flags. Assignments to
// sensitive variables are refused by the caller before recursion.
func envInner(rest []string) (string, []string, bool) {
	for i := 0; i < len(rest); i++ {
		tok := rest[i]
		if strings.HasPrefix(tok, "-") && tok != "-" {
			continue
		}
		if eq := strings.IndexByte(tok, '='); eq > 0 {
			continue
		}
		return tok, rest[i+1:], true
	}
	return "", nil, false
}

// timeoutInner skips flags and the duration argument.
func timeoutInner(rest []string) (string, []string, bool) {
	i := skipFlags(rest, set("-s", "--signal", "-k", "--kill-after"))
	if i < 0 || i+1 >= len(rest) {
		return "", nil, false
	}
	return rest[i+1], rest[i+2:], true // rest[i] is the duration
}

// Flag sets live outside the allowlist map so the validators can reference them
// without the map referencing the validators referencing the map — Go rejects
// that as an initialization cycle.
var (
	findFlags = ArgPolicy{
		Flags: set("-name", "-iname", "-type", "-maxdepth", "-mindepth", "-path", "-print",
			"-size", "-newer", "-mtime", "-mmin", "-empty", "-not", "-o", "-a",
			"-print0", "-depth", "-follow", "-prune", "-regex", "-iregex", "-ls",
			"-exec", "-execdir"),
		ConfineArgs: true,
	}
	xargsFlags   = ArgPolicy{Flags: set("-I", "-n", "-P", "-0", "-r", "-t", "-L", "-d", "-s", "-E")}
	envFlags     = ArgPolicy{Flags: set("-i", "-u", "-0")}
	timeoutFlags = ArgPolicy{Flags: set("-s", "-k", "--signal", "--kill-after", "--preserve-status")}
	prefixFlags  = ArgPolicy{Flags: set("-n", "--adjustment")}
)

func findValidate(_ string, rest []string, workspace string) error {
	return checkWrapper("find", rest, workspace, findFlags, findInner)
}

func xargsValidate(_ string, rest []string, workspace string) error {
	return checkWrapper("xargs", rest, workspace, xargsFlags,
		simpleInner(set("-I", "-n", "-P", "-d", "-L", "-s", "-E", "--replace", "--max-args", "--max-procs")))
}

func envValidate(_ string, rest []string, workspace string) error {
	for _, a := range rest {
		if eq := strings.IndexByte(a, '='); eq > 0 && sensitiveEnv[a[:eq]] {
			return deny("env would set sensitive variable %q", a[:eq])
		}
	}
	return checkWrapper("env", rest, workspace, envFlags, envInner)
}

func timeoutValidate(name string, rest []string, workspace string) error {
	return checkWrapper(name, rest, workspace, timeoutFlags, timeoutInner)
}

func prefixValidate(name string, rest []string, workspace string) error {
	return checkWrapper(name, rest, workspace, prefixFlags,
		simpleInner(set("-n", "--adjustment")))
}

// ---- multi-verb tools: same shape as kubectl and aws ----

var gitReadVerbs = set("log", "status", "diff", "show", "blame", "describe",
	"ls-files", "ls-tree", "cat-file", "rev-parse", "shortlog", "branch",
	"tag", "remote", "config", "grep", "whatchanged", "reflog", "count-objects")

// gitDangerousFlags reach outside the repository or run arbitrary commands:
// -c can set core.pager or an alias that shells out.
var gitDangerousFlags = set("-c", "--exec-path", "--upload-pack", "--receive-pack",
	"-C", "--git-dir", "--work-tree", "--namespace")

var gitValueFlags = set("-C", "--git-dir", "--work-tree", "-c", "--namespace")

func gitValidate(_ string, rest []string, _ string) error {
	for _, a := range rest {
		if gitDangerousFlags[flagBase(a)] {
			return deny("git flag %q can redirect the repository or run a command", flagBase(a))
		}
	}
	verb, idx, ok := resolveVerb(rest, gitValueFlags)
	if !ok {
		return deny("git requires a read-only subcommand")
	}
	if !gitReadVerbs[verb] {
		return deny("git %s is not a read-only operation", verb)
	}
	// `git config` reads only without a value argument, and `git remote`
	// without a subcommand lists; anything further mutates.
	switch verb {
	case "config":
		for _, a := range rest[idx+1:] {
			if a == "--unset" || a == "--add" || a == "--replace-all" || a == "--edit" || a == "-e" {
				return deny("git config %s modifies configuration", a)
			}
		}
		if countPositional(rest[idx+1:]) > 1 {
			return deny("git config with a value modifies configuration")
		}
	case "remote", "branch", "tag":
		if sub, _, ok := resolveVerb(rest[idx+1:], gitValueFlags); ok {
			if sub != "show" && sub != "get-url" && sub != "list" && sub != "-v" {
				return deny("git %s %s is not a read-only operation", verb, sub)
			}
		}
	}
	return nil
}

func countPositional(rest []string) int {
	n := 0
	for _, a := range rest {
		if !strings.HasPrefix(a, "-") {
			n++
		}
	}
	return n
}

// opensslReadVerbs hash or inspect; s_client/s_server/req/genrsa reach the
// network or write key material.
var opensslReadVerbs = set("dgst", "sha256", "sha1", "md5", "base64", "version", "x509", "asn1parse")

func opensslValidate(_ string, rest []string, _ string) error {
	verb, idx, ok := resolveVerb(rest, set("-out", "-in", "-keyout"))
	if !ok {
		return deny("openssl requires a subcommand")
	}
	if !opensslReadVerbs[verb] {
		return deny("openssl %s is not a read-only operation", verb)
	}
	for _, a := range rest[idx+1:] {
		if flagBase(a) == "-out" || flagBase(a) == "-keyout" {
			return deny("openssl %s writes output to a file", flagBase(a))
		}
	}
	return nil
}
