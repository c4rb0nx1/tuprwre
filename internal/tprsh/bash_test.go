package tprsh

import (
	"context"
	"testing"
)

// TestBashSyntaxAccepted covers constructs an agent will emit that POSIX mode
// rejected: bash conditionals, arrays, here-strings, ANSI-C quoting, and the
// ubiquitous 2>/dev/null.
func TestBashSyntaxAccepted(t *testing.T) {
	accepted := []string{
		"[[ -f hello.txt ]]",
		"arr=(1 2 3)",
		"cat <<< hello",
		"cat hello.txt 2>/dev/null",
		"uname -s > /dev/null",
		"cat hello.txt > out.txt 2>/dev/null",
		"if [[ -f hello.txt ]]; then wc -l hello.txt; fi",
		"for f in *.txt; do wc -c $f; done",
		"cat hello.txt | wc -l",
		"uname -s && id -u",
	}
	for _, src := range accepted {
		t.Run(src, func(t *testing.T) {
			sh, _ := newTestShell(t)
			if err := sh.Run(context.Background(), src); err != nil {
				t.Fatalf("expected %q to be accepted, got: %v", src, err)
			}
		})
	}
}

// TestBashOnlyEscapesBlocked covers escape constructs that only exist once the
// grammar widens to bash — the price of accepting bash syntax.
func TestBashOnlyEscapesBlocked(t *testing.T) {
	blocked := map[string]string{
		"indirect expansion":                "cat ${!x}",
		"coproc":                            "coproc uname -s",
		"declare":                           "declare -x LD_PRELOAD=/tmp/e.so",
		"typeset":                           "typeset -x PATH=/tmp",
		"proc subst bash":                   "cat <(uname -s)",
		"cmd subst bash":                    "echo $(uname -s)",
		"redir to etc":                      "uname -s > /etc/passwd",
		"herestring is fine but cmd is not": "sh <<< 'id'",
	}
	for name, src := range blocked {
		t.Run(name, func(t *testing.T) {
			sh, _ := newTestShell(t)
			err := sh.Run(context.Background(), src)
			if err == nil {
				t.Fatalf("expected %q to be denied, it ran", src)
			}
			if !isDeny(err) {
				t.Fatalf("expected DenyError for %q, got %T: %v", src, err, err)
			}
		})
	}
}
