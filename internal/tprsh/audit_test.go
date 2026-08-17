package tprsh

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAuditChainVerifies a clean chain passes verification.
func TestAuditChainVerifies(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	a, err := NewAuditor(path)
	if err != nil {
		t.Fatalf("new auditor: %v", err)
	}
	_ = a.Append(DecisionStart, "uname", []string{"uname", "-s"}, "/ws", "", 0)
	_ = a.Append(DecisionAllow, "uname", []string{"uname", "-s"}, "/ws", "", 0)
	_ = a.Append(DecisionDeny, "sh", []string{"sh"}, "/ws", "command not in allowlist", 0)
	_ = a.Close()

	records, err := LoadAndVerify(path)
	if err != nil {
		t.Fatalf("expected clean chain, got: %v", err)
	}
	if len(records) != 3 {
		t.Fatalf("expected 3 records, got %d", len(records))
	}
}

// TestAuditTamperDetected proves editing an interior record breaks the chain.
func TestAuditTamperDetected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	a, _ := NewAuditor(path)
	_ = a.Append(DecisionAllow, "uname", []string{"uname", "-s"}, "/ws", "", 0)
	_ = a.Append(DecisionDeny, "sh", []string{"sh"}, "/ws", "denied", 0)
	_ = a.Append(DecisionAllow, "id", []string{"id", "-u"}, "/ws", "", 0)
	_ = a.Close()

	// Tamper: rewrite the middle record's reason to hide the denial.
	data, _ := os.ReadFile(path)
	tampered := strings.Replace(string(data), `"denied"`, `"allowed"`, 1)
	if tampered == string(data) {
		t.Fatal("tamper substitution did not apply")
	}
	if err := os.WriteFile(path, []byte(tampered), 0o600); err != nil {
		t.Fatalf("rewrite: %v", err)
	}

	if _, err := LoadAndVerify(path); err == nil {
		t.Fatal("expected tamper to break the hash chain, verification passed")
	}
}
