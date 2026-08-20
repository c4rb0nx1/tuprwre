package tprsh

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestObserveModeRunsButRecords is the property the evidence-gathering phase
// depends on: a command policy would reject still runs, and the ledger says so.
// This is how an operator answers "what breaks if I enforce today?" without
// breaking anything today.
func TestObserveModeRunsButRecords(t *testing.T) {
	sh, auditPath := newTestShell(t)
	sh.SetMode(ModeObserve)

	// `uname` is allowlisted; `env` with a sensitive assignment is not. Both
	// must run, and only the second must be flagged.
	if err := sh.Run(context.Background(), "uname -s"); err != nil {
		t.Fatalf("allowed command failed in observe mode: %v", err)
	}
	if err := sh.Run(context.Background(), "id -u"); err != nil {
		t.Fatalf("allowed command failed in observe mode: %v", err)
	}

	records, err := LoadAndVerify(auditPath)
	if err != nil {
		t.Fatalf("verify audit: %v", err)
	}
	for _, r := range records {
		if r.Event == DecisionDeny {
			t.Fatalf("observe mode must not deny, got: %+v", r)
		}
	}
}

// TestObserveModeShadowsDenials confirms a would-be denial is recorded as a
// shadow verdict rather than enforced.
func TestObserveModeShadowsDenials(t *testing.T) {
	sh, auditPath := newTestShell(t)
	sh.SetMode(ModeObserve)

	// Not in the allowlist. In enforce mode this is a DenyError.
	_ = sh.Run(context.Background(), "sha512sum hello.txt")

	records, err := LoadAndVerify(auditPath)
	if err != nil {
		t.Fatalf("verify audit: %v", err)
	}
	var shadowed bool
	for _, r := range records {
		if r.Event == DecisionShadow && r.Cmd == "sha512sum" {
			shadowed = true
		}
		if r.Event == DecisionDeny {
			t.Fatalf("observe mode denied instead of shadowing: %+v", r)
		}
	}
	if !shadowed {
		t.Fatalf("expected a shadow-deny record for sha512sum, got %+v", records)
	}
}

// TestEnforceRemainsDefault guards against observe mode leaking in as the
// default, which would silently turn the boundary off.
func TestEnforceRemainsDefault(t *testing.T) {
	sh, _ := newTestShell(t)
	if sh.Observing() {
		t.Fatal("a new Shell must enforce by default")
	}
	if err := sh.Run(context.Background(), "sha512sum hello.txt"); !isDeny(err) {
		t.Fatalf("default mode must deny non-allowlisted commands, got %v", err)
	}
}

// TestObserveModeStillRefusesBrokenSandbox: an unusable sandbox is an
// infrastructure failure, not a policy question, so it is refused even while
// observing — otherwise a confinement outage would silently run unconfined.
func TestObserveModeStillRefusesBrokenSandbox(t *testing.T) {
	ws := t.TempDir()
	auditPath := filepath.Join(t.TempDir(), "audit.jsonl")
	if err := os.WriteFile(filepath.Join(ws, "hello.txt"), []byte("hi\n"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	auditor, err := NewAuditor(auditPath)
	if err != nil {
		t.Fatalf("auditor: %v", err)
	}
	t.Cleanup(func() { _ = auditor.Close() })

	sh, err := NewConfined(ws, auditor, brokenConfiner{})
	if err != nil {
		t.Fatalf("shell: %v", err)
	}
	sh.SetMode(ModeObserve)
	sh.SetIO(nil, os.NewFile(0, os.DevNull), os.NewFile(0, os.DevNull))

	if err := sh.Run(context.Background(), "uname -s"); err == nil {
		t.Fatal("a broken sandbox must refuse even in observe mode")
	}
}

type brokenConfiner struct{}

func (brokenConfiner) Wrap(string, []string) ([]string, error) {
	return nil, os.ErrPermission
}
func (brokenConfiner) Available() bool { return false }
func (brokenConfiner) Name() string    { return "broken" }
