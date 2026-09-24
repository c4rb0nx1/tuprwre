package report

import (
	"bytes"
	"encoding/json"
	"path"
	"sort"
	"strings"

	"github.com/c4rb0nx1/tuprwre/internal/rules"
)

// intentFacts is what the report can learn from a tool call's arguments
// without knowing which harness or tool produced it.
type intentFacts struct {
	// scripts are shell command strings (e.g. Bash {"command": "..."}).
	scripts []string
	// argvs are commands given as argument vectors (e.g. Codex shell
	// {"command": ["bash", "-lc", "..."]}).
	argvs [][]string
	// cwd is a working directory named in the arguments, if any.
	cwd string
	// paths are file paths named in the arguments.
	paths []string
	// strs are all string values, for address matching.
	strs []string
}

// commandKeys name argument fields that hold a command, across harnesses.
var commandKeys = map[string]bool{"command": true, "cmd": true, "script": true, "commands": true}

// cwdKeys name argument fields that hold a working directory.
var cwdKeys = map[string]bool{"cwd": true, "workdir": true, "working_directory": true, "dir": true}

// extractFacts walks a tool call's JSON arguments. It is deliberately
// harness-agnostic: it looks for conventional key names and path-shaped
// strings rather than specific tool schemas.
func extractFacts(raw json.RawMessage) intentFacts {
	var f intentFacts
	if len(raw) == 0 {
		return f
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return f
	}
	// Some harnesses send arguments as a JSON-encoded string.
	if s, ok := v.(string); ok {
		var inner any
		if json.Unmarshal([]byte(s), &inner) == nil {
			v = inner
		}
	}
	f.walk("", v)
	return f
}

func (f *intentFacts) walk(key string, v any) {
	lk := strings.ToLower(key)
	switch t := v.(type) {
	case string:
		f.strs = append(f.strs, t)
		switch {
		case commandKeys[lk]:
			f.scripts = append(f.scripts, t)
		case cwdKeys[lk]:
			f.cwd = t
		case strings.Contains(lk, "path") || strings.Contains(lk, "file"):
			f.paths = append(f.paths, t)
		case looksLikePath(t):
			f.paths = append(f.paths, t)
		}
	case []any:
		if commandKeys[lk] {
			if argv, ok := stringSlice(t); ok {
				f.argvs = append(f.argvs, argv)
				f.strs = append(f.strs, argv...)
				return
			}
		}
		for _, x := range t {
			f.walk(key, x)
		}
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			f.walk(k, t[k])
		}
	}
}

func stringSlice(xs []any) ([]string, bool) {
	out := make([]string, 0, len(xs))
	for _, x := range xs {
		s, ok := x.(string)
		if !ok {
			return nil, false
		}
		out = append(out, s)
	}
	return out, len(out) > 0
}

// looksLikePath reports whether s is a single absolute or home-relative path.
func looksLikePath(s string) bool {
	return (strings.HasPrefix(s, "/") || strings.HasPrefix(s, "~/")) && !strings.ContainsAny(s, " \t\n")
}

// commands returns every command the intent runs, as parsed simple commands
// resolved against the intent's cwd (or base when it names none).
func (f intentFacts) commands(base string) []rules.Command {
	cwd := f.cwd
	if cwd == "" {
		cwd = base
	}
	var out []rules.Command
	for _, s := range f.scripts {
		for _, c := range rules.ParseScript(s, cwd) {
			out = append(out, rules.Expand(c)...)
		}
	}
	for _, a := range f.argvs {
		out = append(out, rules.Expand(rules.Command{Argv: a, Cwd: cwd})...)
	}
	return out
}

// binaries returns the base names of every program the intent runs.
func (f intentFacts) binaries() map[string]bool {
	out := map[string]bool{}
	for _, c := range f.commands("") {
		if len(c.Argv) > 0 {
			out[path.Base(c.Argv[0])] = true
		}
	}
	return out
}

// commandLines returns each command as whitespace-normalized text, for
// matching against an observed "sh -c" command line.
func (f intentFacts) commandLines() []string {
	var out []string
	for _, s := range f.scripts {
		if n := normalizeSpace(s); n != "" {
			out = append(out, n)
		}
	}
	for _, a := range f.argvs {
		if n := normalizeSpace(strings.Join(a, " ")); n != "" {
			out = append(out, n)
		}
		// A shell argv's script is what the observed process line carries.
		if len(a) >= 3 && strings.HasPrefix(a[len(a)-2], "-") {
			if n := normalizeSpace(a[len(a)-1]); n != "" {
				out = append(out, n)
			}
		}
	}
	return out
}

// classify applies the rule set to the intent.
func (f intentFacts) classify(workspace string) rules.Result {
	res := rules.Result{Verdict: rules.GreenVerdict}
	for _, c := range f.commands(workspace) {
		r := rules.Classify(c, workspace)
		res.Verdict = rules.Worse(res.Verdict, r.Verdict)
		res.CredentialPaths = append(res.CredentialPaths, r.CredentialPaths...)
		res.Egress = res.Egress || r.Egress
	}
	for _, p := range f.paths {
		if isSensitive(p) {
			res.CredentialPaths = append(res.CredentialPaths, p)
			res.Verdict = rules.Worse(res.Verdict, rules.Verdict{Tier: rules.Yellow, Rule: rules.RuleCredentialRead,
				Reason: "tool call names credential file " + p})
		}
	}
	if len(res.CredentialPaths) > 0 && res.Egress {
		res.Verdict = rules.Worse(res.Verdict, rules.Verdict{Tier: rules.Red, Rule: rules.RuleCredentialThenEgress,
			Reason: "tool call reads " + res.CredentialPaths[0] + " and sends data off the host"})
	}
	return res
}

// summary is a one-line description of the intent for display.
func (f intentFacts) summary(raw json.RawMessage) string {
	switch {
	case len(f.scripts) > 0:
		return f.scripts[0]
	case len(f.argvs) > 0:
		return strings.Join(f.argvs[0], " ")
	case len(f.paths) > 0:
		return f.paths[0]
	}
	var b bytes.Buffer
	if json.Compact(&b, raw) == nil {
		return b.String()
	}
	return string(raw)
}

func normalizeSpace(s string) string { return strings.Join(strings.Fields(s), " ") }
