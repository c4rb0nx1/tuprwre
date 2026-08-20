package tprsh

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// runConfined executes script through a confined /bin/bash, standing in for an
// allowlisted binary that has been subverted. Using bash builtins (redirection,
// read) rather than external commands isolates each mechanism: a failure is
// attributable to the file rule under test, not to the exec restriction.
func runConfined(t *testing.T, c Confiner, script string) (string, error) {
	t.Helper()
	argv, err := c.Wrap("/bin/bash", []string{"/bin/bash", "-c", script})
	if err != nil {
		t.Fatalf("wrap: %v", err)
	}
	out, err := exec.Command(argv[0], argv[1:]...).CombinedOutput()
	return string(out), err
}

func newTestConfiner(t *testing.T) (c Confiner, workspace, auditDir, credDir string) {
	t.Helper()
	if runtime.GOOS != "darwin" {
		t.Skip("seatbelt backend is macOS-only; Linux backend not implemented yet")
	}

	workspace = CanonicalDir(t.TempDir())
	auditDir = CanonicalDir(t.TempDir())
	credDir = CanonicalDir(t.TempDir())

	if err := os.WriteFile(filepath.Join(credDir, "credentials"), []byte("SECRET_KEY\n"), 0o600); err != nil {
		t.Fatalf("seed creds: %v", err)
	}
	if err := os.WriteFile(filepath.Join(auditDir, "audit.jsonl"), []byte("{\"seq\":1}\n"), 0o600); err != nil {
		t.Fatalf("seed audit: %v", err)
	}

	c, err := NewConfiner(ConfineOptions{
		Mode:      SandboxWorkspaceWrite,
		Workspace: workspace,
		NoWrite:   []string{auditDir},
		NoRead:    []string{credDir},
	})
	if err != nil {
		t.Skipf("confiner unavailable: %v", err)
	}
	return c, workspace, auditDir, credDir
}

// TestConfinedCannotSpawn is the property the audit log's completeness rests
// on: a subverted approved binary must not be able to exec anything else, or
// the ledger would claim "kubectl ran" while unlogged commands executed.
func TestConfinedCannotSpawn(t *testing.T) {
	c, _, _, _ := newTestConfiner(t)

	out, err := runConfined(t, c, "/bin/echo SPAWNED")
	if err == nil && strings.Contains(out, "SPAWNED") {
		t.Fatalf("confined process spawned a child binary: %q", out)
	}
	t.Logf("spawn blocked as expected: %s", strings.TrimSpace(out))
}

// TestConfinedCannotReadCredentials proves credential stores stay unreadable
// even to an approved binary.
func TestConfinedCannotReadCredentials(t *testing.T) {
	c, _, _, credDir := newTestConfiner(t)

	out, _ := runConfined(t, c, "read x < "+filepath.Join(credDir, "credentials")+"; echo GOT:$x")
	if strings.Contains(out, "SECRET_KEY") {
		t.Fatalf("confined process read protected credentials: %q", out)
	}
	t.Logf("credential read blocked as expected: %s", strings.TrimSpace(out))
}

// TestConfinedCannotWriteAuditLog proves audit integrity is enforced by the
// kernel rather than assumed from the child not knowing the path.
func TestConfinedCannotWriteAuditLog(t *testing.T) {
	c, _, auditDir, _ := newTestConfiner(t)
	logPath := filepath.Join(auditDir, "audit.jsonl")

	before, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}

	out, _ := runConfined(t, c, "echo TAMPERED > "+logPath+" && echo WROTE")
	after, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("re-read audit log: %v", err)
	}
	if string(before) != string(after) {
		t.Fatalf("confined process modified the audit log: %q", string(after))
	}
	if strings.Contains(out, "WROTE") {
		t.Fatalf("confined process reported a successful audit-log write: %q", out)
	}
	t.Logf("audit-log write blocked as expected: %s", strings.TrimSpace(out))
}

// TestConfinedCanStillWorkInWorkspace guards against a profile so strict that
// legitimate work fails — confinement must not break the happy path.
func TestConfinedCanStillWorkInWorkspace(t *testing.T) {
	c, workspace, _, _ := newTestConfiner(t)
	target := filepath.Join(workspace, "out.txt")

	if _, err := runConfined(t, c, "echo hello > "+target); err != nil {
		t.Fatalf("confined write inside workspace failed: %v", err)
	}
	data, err := os.ReadFile(target)
	if err != nil || !strings.Contains(string(data), "hello") {
		t.Fatalf("expected workspace write to succeed, got %q err=%v", string(data), err)
	}
}

// TestConfinerFailsClosed verifies an unsupported platform refuses rather than
// silently running unconfined.
func TestConfinerFailsClosed(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("darwin has a backend; fail-closed path is for platforms without one")
	}
	if _, err := NewConfiner(ConfineOptions{Mode: SandboxReadOnly, Workspace: t.TempDir()}); err == nil {
		t.Fatal("expected NewConfiner to fail closed with no backend available")
	}
}

// TestSandboxNoneIsPassthrough keeps the opt-in property honest.
func TestSandboxNoneIsPassthrough(t *testing.T) {
	c, err := NewConfiner(ConfineOptions{Mode: SandboxNone})
	if err != nil {
		t.Fatalf("none mode should never error: %v", err)
	}
	argv, err := c.Wrap("/bin/echo", []string{"echo", "hi"})
	if err != nil {
		t.Fatalf("wrap: %v", err)
	}
	if len(argv) != 2 || argv[0] != "echo" {
		t.Fatalf("none mode altered argv: %v", argv)
	}
}
