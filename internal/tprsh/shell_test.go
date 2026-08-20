package tprsh

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// newTestShell builds a Shell over a fresh workspace with an audit log kept in
// a separate directory (never inside the workspace the child can reach).
func newTestShell(t *testing.T) (*Shell, string) {
	t.Helper()
	ws := t.TempDir()
	auditDir := t.TempDir()
	auditPath := filepath.Join(auditDir, "audit.jsonl")

	if err := os.WriteFile(filepath.Join(ws, "hello.txt"), []byte("hi\n"), 0o600); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}

	auditor, err := NewAuditor(auditPath)
	if err != nil {
		t.Fatalf("new auditor: %v", err)
	}
	t.Cleanup(func() { _ = auditor.Close() })

	sh, err := New(ws, auditor)
	if err != nil {
		t.Fatalf("new shell: %v", err)
	}
	sh.SetIO(nil, io.Discard, io.Discard)
	return sh, auditPath
}

func isDeny(err error) bool {
	var de *DenyError
	return errors.As(err, &de)
}

// TestAllowedCommands: the curated read-only tools run with safe arguments.
func TestAllowedCommands(t *testing.T) {
	allowed := []string{
		"uname -s",
		"id -u",
		"date -u",
		"find . -maxdepth 1 -type f",
		"cat hello.txt",
		"head -n 1 hello.txt",
		"uname -s > out.txt", // redirection inside the workspace is permitted
	}
	for _, src := range allowed {
		t.Run(src, func(t *testing.T) {
			sh, _ := newTestShell(t)
			if err := sh.Run(context.Background(), src); err != nil {
				t.Fatalf("expected %q to run, got error: %v", src, err)
			}
		})
	}
}

// TestEscapesBlocked walks the escape taxonomy from the research and asserts
// every class is refused with a DenyError.
func TestEscapesBlocked(t *testing.T) {
	cases := map[string]string{
		// C1 — non-allowlisted interpreters / shells
		"bare sh":          "sh",
		"bash -c":          "bash -c 'id'",
		"python -c":        "python3 -c 'import os; os.system(\"sh\")'",
		"awk system":       "awk 'BEGIN{system(\"sh\")}'",
		"absolute /bin/sh": "/bin/sh",
		// C1 — the wrapper recurses: the inner command decides. `-exec cat` is
		// permitted (see TestWrapperRecursion); `-exec sh` is the escape.
		"find -exec sh":      "find . -type f -exec sh {} \\;",
		"find -exec python3": "find . -type f -exec python3 {} \\;",
		"find -delete":       "find . -delete",
		"xargs sh -c":        "ls | xargs sh -c 'id'",
		"env LD_PRELOAD":     "env LD_PRELOAD=/tmp/e.so ls",
		"timeout to sh":      "timeout 5 sh -c id",
		"git push":           "git push origin main",
		"git -c pager":       "git -c core.pager=sh log",
		"openssl s_client":   "openssl s_client -connect evil.com:443",
		// C2 — command / process substitution (parser-level)
		"cmd subst":  "echo $(id)",
		"backticks":  "id `whoami`",
		"proc subst": "cat <(id)",
		// C3 — environment attacks
		"LD_PRELOAD assign": "LD_PRELOAD=/tmp/e.so id",
		"PATH assign":       "PATH=/tmp id",
		"IFS assign":        "IFS=x id",
		// C4 — builtin / feature abuse
		"exec builtin":  "exec sh",
		"eval builtin":  "eval 'id'",
		"redir outside": "uname -s > /tmp/tprsh_evil",
		// C5 — path confinement
		"cat /etc/passwd": "cat /etc/passwd",
		"ls parent":       "ls ../",
		// chaining — later denied segment still refused
		"chain to sh": "uname -s; sh",
	}
	for name, src := range cases {
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

// TestEnvIsReset verifies the child environment carries no LD_* injection
// vector and a locked PATH.
func TestEnvIsReset(t *testing.T) {
	sh, _ := newTestShell(t)
	env := sh.lockedEnv()
	if got := env.Get("LD_PRELOAD").String(); got != "" {
		t.Fatalf("LD_PRELOAD leaked into child env: %q", got)
	}
	if got := env.Get("PATH").String(); got != "/usr/bin:/bin:/usr/sbin:/sbin" {
		t.Fatalf("PATH not locked, got %q", got)
	}
}

// TestDenyIsAudited confirms a refused command leaves a deny record.
func TestDenyIsAudited(t *testing.T) {
	sh, auditPath := newTestShell(t)
	_ = sh.Run(context.Background(), "sh")

	records, err := LoadAndVerify(auditPath)
	if err != nil {
		t.Fatalf("verify audit: %v", err)
	}
	found := false
	for _, r := range records {
		if r.Event == DecisionDeny && r.Cmd == "sh" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no deny record for sh in audit log: %+v", records)
	}
}

// TestAllowIsAudited confirms an allowed command leaves allow + finish records.
func TestAllowIsAudited(t *testing.T) {
	sh, auditPath := newTestShell(t)
	if err := sh.Run(context.Background(), "uname -s"); err != nil {
		t.Fatalf("run: %v", err)
	}
	records, err := LoadAndVerify(auditPath)
	if err != nil {
		t.Fatalf("verify audit: %v", err)
	}
	var allow, finish bool
	for _, r := range records {
		if r.Cmd == "uname" && r.Event == DecisionAllow {
			allow = true
		}
		if r.Cmd == "uname" && r.Event == DecisionFinish {
			finish = true
		}
	}
	if !allow || !finish {
		t.Fatalf("expected allow+finish for uname, got allow=%v finish=%v", allow, finish)
	}
}
