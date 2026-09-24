package rules

import (
	"path"
	"strings"
)

// Command is one simple command: its argument vector and the working
// directory it runs in ("" when unknown).
type Command struct {
	Argv []string
	Cwd  string
}

// maxScriptDepth bounds recursion through nested "sh -c" and "$(...)".
const maxScriptDepth = 8

// ParseScript splits a shell script into its simple commands. It is a small,
// best-effort lexer, not a shell: it understands single and double quotes,
// backslash escapes, the separators ; & && || | and newlines, subshell
// parentheses, and command substitution ($(...) and `...`), whose contents
// are parsed as further commands. A "cd DIR" updates the working directory of
// the commands after it. Variables, globs and redirections are left as
// literal words.
func ParseScript(script, cwd string) []Command {
	return parseScript(script, cwd, 0)
}

func parseScript(script, cwd string, depth int) []Command {
	if depth > maxScriptDepth {
		return nil
	}
	var (
		cmds  []Command
		words []string
		cur   strings.Builder
		inTok bool
	)
	flushWord := func() {
		if inTok {
			words = append(words, cur.String())
			cur.Reset()
			inTok = false
		}
	}
	flushCmd := func() {
		flushWord()
		if len(words) > 0 {
			cmds = append(cmds, Command{Argv: words, Cwd: cwd})
			if dir, ok := cdTarget(words); ok {
				cwd = resolve(cwd, dir)
			}
			words = nil
		}
	}
	sub := func(inner string) {
		cmds = append(cmds, parseScript(inner, cwd, depth+1)...)
	}

	r := []rune(script)
	for i := 0; i < len(r); i++ {
		c := r[i]
		switch {
		case c == '\\' && i+1 < len(r):
			i++
			if r[i] != '\n' {
				cur.WriteRune(r[i])
				inTok = true
			}
		case c == '\'':
			inTok = true
			j := i + 1
			for j < len(r) && r[j] != '\'' {
				cur.WriteRune(r[j])
				j++
			}
			i = j
		case c == '"':
			inTok = true
			j := i + 1
			for j < len(r) && r[j] != '"' {
				switch {
				case r[j] == '\\' && j+1 < len(r) && strings.ContainsRune("\"\\$`", r[j+1]):
					j++
					cur.WriteRune(r[j])
				case r[j] == '$' && j+1 < len(r) && r[j+1] == '(':
					end := matchParen(r, j+1)
					sub(string(r[j+2 : end]))
					cur.WriteString(string(r[j:min(end+1, len(r))]))
					j = end
				case r[j] == '`':
					end := j + 1
					for end < len(r) && r[end] != '`' {
						end++
					}
					sub(string(r[j+1 : end]))
					j = end
				default:
					cur.WriteRune(r[j])
				}
				j++
			}
			i = j
		case c == '$' && i+1 < len(r) && r[i+1] == '(':
			end := matchParen(r, i+1)
			sub(string(r[i+2 : end]))
			cur.WriteString(string(r[i:min(end+1, len(r))]))
			inTok = true
			i = end
		case c == '`':
			end := i + 1
			for end < len(r) && r[end] != '`' {
				end++
			}
			sub(string(r[i+1 : end]))
			i = end
		case c == '#' && !inTok:
			for i < len(r) && r[i] != '\n' {
				i++
			}
			flushCmd()
		case c == '&' && (i > 0 && (r[i-1] == '>' || r[i-1] == '<') || i+1 < len(r) && r[i+1] == '>'):
			// Redirection such as 2>&1 or &>file, not a separator.
			cur.WriteRune(c)
			inTok = true
		case c == ';' || c == '&' || c == '|' || c == '\n' || c == '(' || c == ')':
			flushCmd()
		case c == ' ' || c == '\t' || c == '\r':
			flushWord()
		default:
			cur.WriteRune(c)
			inTok = true
		}
	}
	flushCmd()
	return cmds
}

// matchParen returns the index of the ')' closing the '(' at open, or the
// last index when unbalanced.
func matchParen(r []rune, open int) int {
	depth := 0
	for i := open; i < len(r); i++ {
		switch r[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return len(r) - 1
}

// cdTarget reports the directory of a "cd DIR" command.
func cdTarget(argv []string) (string, bool) {
	if len(argv) == 2 && argv[0] == "cd" && !strings.HasPrefix(argv[1], "-") {
		return argv[1], true
	}
	return "", false
}

// shells are interpreters whose -c argument is a script.
var shells = map[string]bool{"sh": true, "bash": true, "zsh": true, "dash": true, "ksh": true, "fish": true, "ash": true}

// wrappers run their trailing arguments as a command.
var wrappers = map[string]bool{
	"sudo": true, "doas": true, "env": true, "nohup": true, "time": true, "nice": true,
	"ionice": true, "timeout": true, "command": true, "exec": true, "xargs": true, "stdbuf": true,
	"setsid": true,
}

// Expand resolves a command into the simple commands it actually runs:
// leading VAR=value assignments and wrappers (sudo, env, nohup, timeout,
// xargs, ...) are peeled off, and a shell invoked with -c has its script
// parsed. Commands that are neither are returned as is.
func Expand(c Command) []Command {
	return expand(c, 0)
}

func expand(c Command, depth int) []Command {
	if depth > maxScriptDepth {
		return nil
	}
	argv := c.Argv
	for len(argv) > 0 {
		name := path.Base(argv[0])
		switch {
		case isAssignment(argv[0]):
			argv = argv[1:]
			continue
		case shells[name]:
			if script, ok := shellScript(argv[1:]); ok {
				var out []Command
				for _, sc := range parseScript(script, c.Cwd, depth+1) {
					out = append(out, expand(sc, depth+1)...)
				}
				return out
			}
		case wrappers[name]:
			argv = skipWrapperOptions(name, argv[1:])
			continue
		}
		break
	}
	if len(argv) == 0 {
		return nil
	}
	return []Command{{Argv: argv, Cwd: c.Cwd}}
}

// shellScript returns the script of a shell invocation such as
// "-c SCRIPT", "-lc SCRIPT" or "-e -c SCRIPT". When a sensor split the script
// on spaces, the remaining words are rejoined.
func shellScript(args []string) (string, bool) {
	for i, a := range args {
		if !strings.HasPrefix(a, "-") || a == "-" || a == "--" {
			return "", false
		}
		if !strings.HasPrefix(a, "--") && strings.Contains(a, "c") {
			if i+1 < len(args) {
				return strings.Join(args[i+1:], " "), true
			}
			return "", false
		}
	}
	return "", false
}

// skipWrapperOptions drops a wrapper's own options (and, for timeout, its
// duration) so the wrapped command comes first.
func skipWrapperOptions(name string, args []string) []string {
	for len(args) > 0 {
		a := args[0]
		switch {
		case a == "--":
			return args[1:]
		case strings.HasPrefix(a, "-"):
			args = args[1:]
			// Options with a separate value.
			if (name == "sudo" && (a == "-u" || a == "-g" || a == "-C")) ||
				(name == "nice" && a == "-n") || (name == "env" && (a == "-u" || a == "-C")) ||
				(name == "timeout" && (a == "-s" || a == "-k")) || (name == "xargs" && (a == "-I" || a == "-n" || a == "-P" || a == "-L" || a == "-d")) {
				if len(args) > 0 {
					args = args[1:]
				}
			}
		case name == "env" && isAssignment(a):
			args = args[1:]
		case name == "timeout":
			return args[1:] // the duration
		default:
			return args
		}
	}
	return args
}

func isAssignment(w string) bool {
	eq := strings.IndexByte(w, '=')
	if eq <= 0 {
		return false
	}
	for i, r := range w[:eq] {
		if !(r == '_' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || i > 0 && r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

// resolve joins p onto dir lexically. "~" and "$HOME" prefixes are kept as
// written because the home directory is not known.
func resolve(dir, p string) string {
	switch {
	case p == "" || isHomePath(p) || strings.HasPrefix(p, "/"):
		return cleanKeepHome(p)
	case dir == "":
		return p
	}
	return path.Clean(path.Join(dir, p))
}

func isHomePath(p string) bool {
	return p == "~" || strings.HasPrefix(p, "~/") || p == "$HOME" || strings.HasPrefix(p, "$HOME/") ||
		strings.HasPrefix(p, "${HOME}")
}

func cleanKeepHome(p string) string {
	if strings.HasPrefix(p, "/") {
		return path.Clean(p)
	}
	return p
}
