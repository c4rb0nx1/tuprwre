// Package rules holds tprsh's small, fixed rule set for irreversible or
// high-risk actions. Rules are deterministic, local and explainable: each
// verdict names the rule and the reason. They are the fixed layer under which
// any classifier plugin sits; this package contains no classifier.
//
// Tiers:
//
//   - green: no rule matched.
//   - yellow: worth recording and reviewing (e.g. a credential read, a force
//     push to a non-protected branch, kubectl without an explicit context).
//   - red: an irreversible action that should pause the agent (terraform/tofu
//     apply or destroy, kubectl delete/apply on a prod context, force push or
//     delete of a protected branch, rm outside the workspace, credential
//     read followed by network egress).
//
// Protected branches and prod contexts are configurable (Config); the
// package-level functions use DefaultConfig.
package rules

import (
	"path"
	"strings"

	"github.com/c4rb0nx1/tuprwre/internal/sensor"
)

// Tier is a verdict severity.
type Tier string

// Tiers, from least to most severe.
const (
	Green  Tier = "green"
	Yellow Tier = "yellow"
	Red    Tier = "red"
)

// Rank orders tiers: green 0, yellow 1, red 2. An unknown tier ranks as
// green.
func (t Tier) Rank() int {
	switch t {
	case Red:
		return 2
	case Yellow:
		return 1
	}
	return 0
}

// Rule identifiers.
const (
	RuleIaCApply              = "iac-apply"
	RuleKubectlProd           = "kubectl-prod"
	RuleKubectlContextUnknown = "kubectl-context-unknown"
	RuleGitForcePushProtected = "git-force-push-protected"
	RuleGitForcePush          = "git-force-push"
	RuleGitDeleteProtected    = "git-delete-protected"
	RuleRmOutsideWorkspace    = "rm-outside-workspace"
	RuleRmWorkspaceUnknown    = "rm-workspace-unknown"
	RuleCredentialRead        = "credential-read"
	RuleCredentialThenEgress  = "credential-then-egress"
)

// Verdict is the outcome of applying the rules.
type Verdict struct {
	Tier   Tier   `json:"tier"`
	Rule   string `json:"rule,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// GreenVerdict is the verdict when no rule matched.
var GreenVerdict = Verdict{Tier: Green}

// Worse returns the more severe of a and b, preferring a on a tie.
func Worse(a, b Verdict) Verdict {
	if b.Tier.Rank() > a.Tier.Rank() {
		return b
	}
	return a
}

// Result is the classification of one command, with the facts the
// cross-event rule (credential read then egress) needs.
type Result struct {
	Verdict
	// CredentialPaths lists credential files the command names.
	CredentialPaths []string `json:"credential_paths,omitempty"`
	// Egress reports that the command can send data off the host.
	Egress bool `json:"egress,omitempty"`
}

// Config holds the site-specific inputs of the rule set.
type Config struct {
	// ProtectedBranches are branch names that must not be force-pushed or
	// deleted. A trailing "*" matches any suffix ("release/*"). A leading
	// "refs/heads/" on the pushed ref is ignored.
	ProtectedBranches []string
	// ProdContexts are case-insensitive substrings that mark a kubectl
	// context as production.
	ProdContexts []string
}

// DefaultConfig is the configuration used by the package-level functions.
var DefaultConfig = Config{
	ProtectedBranches: []string{"main", "master", "trunk", "develop", "prod", "production", "release/*", "release-*"},
	ProdContexts:      []string{"prod"},
}

// Classify applies the rule set to a command using DefaultConfig.
func Classify(c Command, workspace string) Result { return DefaultConfig.Classify(c, workspace) }

// ClassifyScript classifies each command of a shell script using
// DefaultConfig.
func ClassifyScript(script, cwd, workspace string) Result {
	return DefaultConfig.ClassifyScript(script, cwd, workspace)
}

// ProtectedBranch reports whether DefaultConfig protects branch b.
func ProtectedBranch(b string) bool { return DefaultConfig.ProtectedBranch(b) }

// Classify applies the rule set to a command. Wrappers and "sh -c" scripts
// are expanded first (see Expand), and the worst verdict of the resulting
// simple commands wins. workspace is the agent's workspace directory; ""
// means unknown, which makes the rm rule conservative.
func (cfg *Config) Classify(c Command, workspace string) Result {
	var res Result
	res.Verdict = GreenVerdict
	for _, sc := range Expand(c) {
		r := cfg.classifySimple(sc, workspace)
		res.Verdict = Worse(res.Verdict, r.Verdict)
		res.CredentialPaths = append(res.CredentialPaths, r.CredentialPaths...)
		res.Egress = res.Egress || r.Egress
	}
	if len(res.CredentialPaths) > 0 && res.Egress {
		res.Verdict = Worse(res.Verdict, Verdict{Red, RuleCredentialThenEgress,
			"command reads " + res.CredentialPaths[0] + " and sends data off the host"})
	}
	return res
}

// ClassifyScript parses a shell script and classifies each command in it.
func (cfg *Config) ClassifyScript(script, cwd, workspace string) Result {
	var res Result
	res.Verdict = GreenVerdict
	cmds := ParseScript(script, cwd)
	for _, c := range cmds {
		r := cfg.Classify(c, workspace)
		res.Verdict = Worse(res.Verdict, r.Verdict)
		res.CredentialPaths = append(res.CredentialPaths, r.CredentialPaths...)
		res.Egress = res.Egress || r.Egress
	}
	if len(res.CredentialPaths) > 0 && res.Egress {
		res.Verdict = Worse(res.Verdict, Verdict{Red, RuleCredentialThenEgress,
			"script reads " + res.CredentialPaths[0] + " and sends data off the host"})
	}
	return res
}

// classifySimple applies the rules to one already-expanded simple command.
func (cfg *Config) classifySimple(c Command, workspace string) Result {
	res := Result{Verdict: GreenVerdict}
	if len(c.Argv) == 0 {
		return res
	}
	name := path.Base(c.Argv[0])
	args := c.Argv[1:]

	for _, a := range args {
		if p := credentialOperand(a); p != "" {
			res.CredentialPaths = append(res.CredentialPaths, p)
		}
	}
	if len(res.CredentialPaths) > 0 {
		res.Verdict = Verdict{Yellow, RuleCredentialRead, name + " touches credential file " + res.CredentialPaths[0]}
	}
	res.Egress = isEgress(name, args)

	var v Verdict
	switch name {
	case "terraform", "tofu", "terragrunt":
		v = iacRule(name, args)
	case "kubectl":
		v = cfg.kubectlRule(args)
	case "git":
		v = cfg.gitRule(args)
	case "rm":
		v = rmRule(args, c.Cwd, workspace)
	}
	res.Verdict = Worse(res.Verdict, v)
	return res
}

// credentialOperand returns the credential path an argument names, looking
// inside "--flag=PATH" and curl-style "@PATH" forms.
func credentialOperand(a string) string {
	if i := strings.IndexByte(a, '='); i >= 0 && strings.HasPrefix(a, "-") {
		a = a[i+1:]
	}
	a = strings.TrimPrefix(a, "@")
	if sensor.IsSensitivePath(a) {
		return a
	}
	return ""
}

// egressTools always talk to a remote host.
var egressTools = map[string]bool{
	"curl": true, "wget": true, "nc": true, "ncat": true, "netcat": true, "socat": true,
	"scp": true, "sftp": true, "ssh": true, "telnet": true, "ftp": true, "http": true, "https": true,
}

func isEgress(name string, args []string) bool {
	if egressTools[name] {
		return true
	}
	if name == "rsync" {
		for _, a := range args {
			if strings.HasPrefix(a, "rsync://") || (!strings.HasPrefix(a, "-") && strings.Contains(a, ":")) {
				return true
			}
		}
	}
	return false
}

// firstPositional returns the index of the first argument that is not a flag
// or the value of a flag listed in withValue.
func firstPositional(args []string, withValue map[string]bool) int {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			if i+1 < len(args) {
				return i + 1
			}
			return -1
		case strings.HasPrefix(a, "-"):
			if !strings.Contains(a, "=") && withValue[a] {
				i++
			}
		default:
			return i
		}
	}
	return -1
}

func iacRule(name string, args []string) Verdict {
	i := firstPositional(args, nil)
	if i < 0 {
		return GreenVerdict
	}
	sub := args[i]
	if name == "terragrunt" && (sub == "run-all" || sub == "run") {
		if j := firstPositional(args[i+1:], nil); j >= 0 {
			sub = args[i+1+j]
		}
	}
	if sub == "apply" || sub == "destroy" {
		return Verdict{Red, RuleIaCApply, name + " " + sub + " changes real infrastructure"}
	}
	return GreenVerdict
}

var kubectlValueFlags = map[string]bool{
	"--context": true, "--namespace": true, "-n": true, "--kubeconfig": true, "--cluster": true,
	"--user": true, "-s": true, "--server": true, "--token": true, "-l": true, "--selector": true,
	"-f": true, "--filename": true, "-o": true, "--output": true, "-c": true, "--container": true,
	"-k": true, "--kustomize": true,
}

func (cfg *Config) kubectlRule(args []string) Verdict {
	i := firstPositional(args, kubectlValueFlags)
	if i < 0 {
		return GreenVerdict
	}
	verb := args[i]
	if verb != "delete" && verb != "apply" {
		return GreenVerdict
	}
	ctx, found := flagValue(args, "--context")
	switch {
	case !found:
		return Verdict{Yellow, RuleKubectlContextUnknown, "kubectl " + verb + " without --context runs against the current context"}
	case cfg.prodContext(ctx):
		return Verdict{Red, RuleKubectlProd, "kubectl " + verb + " on prod context " + ctx}
	}
	return GreenVerdict
}

// flagValue returns the value of a long flag given as "--f v" or "--f=v".
func flagValue(args []string, flag string) (string, bool) {
	for i, a := range args {
		if a == "--" {
			break
		}
		if a == flag && i+1 < len(args) {
			return args[i+1], true
		}
		if strings.HasPrefix(a, flag+"=") {
			return a[len(flag)+1:], true
		}
	}
	return "", false
}

func (cfg *Config) prodContext(ctx string) bool {
	lower := strings.ToLower(ctx)
	for _, p := range cfg.ProdContexts {
		if p != "" && strings.Contains(lower, strings.ToLower(p)) {
			return true
		}
	}
	return false
}

// ProtectedBranch reports whether branch b matches cfg.ProtectedBranches.
func (cfg *Config) ProtectedBranch(b string) bool {
	b = strings.TrimPrefix(b, "refs/heads/")
	for _, p := range cfg.ProtectedBranches {
		if prefix, ok := strings.CutSuffix(p, "*"); ok {
			if strings.HasPrefix(b, prefix) && len(b) > len(prefix) {
				return true
			}
		} else if b == p {
			return true
		}
	}
	return false
}

var gitGlobalValueFlags = map[string]bool{"-C": true, "-c": true, "--git-dir": true, "--work-tree": true, "--namespace": true}

var gitPushValueFlags = map[string]bool{"-o": true, "--push-option": true, "--repo": true, "--receive-pack": true, "--exec": true}

func (cfg *Config) gitRule(args []string) Verdict {
	i := firstPositional(args, gitGlobalValueFlags)
	if i < 0 || args[i] != "push" {
		return GreenVerdict
	}
	args = args[i+1:]

	var (
		force, del, mirror bool
		positional         []string
	)
	for j := 0; j < len(args); j++ {
		a := args[j]
		switch {
		case a == "--force" || strings.HasPrefix(a, "--force-with-lease"):
			force = true
		case a == "--delete":
			del = true
		case a == "--mirror":
			mirror = true
		case strings.HasPrefix(a, "--"):
			if !strings.Contains(a, "=") && gitPushValueFlags[a] {
				j++
			}
		case strings.HasPrefix(a, "-") && len(a) > 1:
			if gitPushValueFlags[a] {
				j++
				continue
			}
			force = force || strings.ContainsRune(a[1:], 'f')
			del = del || strings.ContainsRune(a[1:], 'd')
		default:
			positional = append(positional, a)
		}
	}
	if mirror {
		return Verdict{Red, RuleGitForcePushProtected, "git push --mirror overwrites every remote ref, including protected branches"}
	}

	var refspecs []string
	if len(positional) > 1 {
		refspecs = positional[1:]
	}
	for _, rs := range refspecs {
		plus := strings.HasPrefix(rs, "+")
		rs = strings.TrimPrefix(rs, "+")
		src, dst, hasColon := strings.Cut(rs, ":")
		if !hasColon {
			dst = src
		}
		switch {
		case (del || (hasColon && src == "")) && cfg.ProtectedBranch(dst):
			return Verdict{Red, RuleGitDeleteProtected, "git push deletes protected branch " + dst}
		case (force || plus) && cfg.ProtectedBranch(dst):
			return Verdict{Red, RuleGitForcePushProtected, "git push --force to protected branch " + dst}
		}
	}
	if !force {
		for _, rs := range refspecs {
			if strings.HasPrefix(rs, "+") {
				force = true
			}
		}
	}
	if force {
		if len(refspecs) == 0 {
			return Verdict{Yellow, RuleGitForcePush, "git push --force to the current branch (branch not named)"}
		}
		return Verdict{Yellow, RuleGitForcePush, "git push --force rewrites remote history"}
	}
	return GreenVerdict
}

// tempDirs are outside any workspace but safe to clean.
var tempDirs = []string{"/tmp", "/var/tmp", "/dev/shm"}

// systemDirs are never part of an agent workspace.
var systemDirs = []string{"/bin", "/boot", "/dev", "/etc", "/lib", "/lib64", "/opt", "/proc", "/root", "/sbin", "/sys", "/usr", "/var"}

func rmRule(args []string, cwd, workspace string) Verdict {
	var operands []string
	for i, a := range args {
		if a == "--" {
			operands = append(operands, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(a, "-") || a == "-" {
			operands = append(operands, a)
		}
	}
	worst := GreenVerdict
	for _, op := range operands {
		worst = Worse(worst, rmOperand(op, cwd, workspace))
	}
	return worst
}

func rmOperand(op, cwd, workspace string) Verdict {
	base := cwd
	if base == "" {
		base = workspace
	}
	target := resolve(base, op)
	outside := Verdict{Red, RuleRmOutsideWorkspace, "rm " + op + " is outside the workspace"}

	if isHomePath(target) {
		if workspace != "" && isHomePath(workspace) && within(target, workspace) {
			return GreenVerdict
		}
		return outside
	}
	if !strings.HasPrefix(target, "/") {
		// Relative with no known base: only an upward escape is suspect.
		if target == ".." || strings.HasPrefix(target, "../") {
			return Verdict{Yellow, RuleRmWorkspaceUnknown, "rm " + op + " escapes an unknown working directory"}
		}
		return GreenVerdict
	}
	for _, t := range tempDirs {
		if within(target, t) && target != t {
			return GreenVerdict
		}
	}
	if workspace != "" && strings.HasPrefix(workspace, "/") {
		if within(target, path.Clean(workspace)) {
			return GreenVerdict
		}
		return outside
	}
	if target == "/" {
		return outside
	}
	for _, s := range systemDirs {
		if within(target, s) {
			return outside
		}
	}
	return Verdict{Yellow, RuleRmWorkspaceUnknown, "rm " + op + " with no known workspace"}
}

// within reports whether p is dir or below it, lexically.
func within(p, dir string) bool {
	if dir == "/" {
		return strings.HasPrefix(p, "/")
	}
	return p == dir || strings.HasPrefix(p, dir+"/")
}
