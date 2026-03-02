//go:build integration

package integration

import (
	"strings"
	"testing"
)

func TestShellInterceptsBlockedCommand(t *testing.T) {
	_, env := setupTest(t)

	stdout, stderr, exitCode := runBinary(t, env,
		"shell", "-c", "apt-get install -y something",
	)

	if exitCode == 0 {
		t.Fatalf("expected non-zero exit code for blocked command, got 0\nstdout: %s\nstderr: %s", stdout, stderr)
	}

	if !strings.Contains(stderr, "[tuprwre] Intercepted") {
		t.Fatalf("expected stderr to contain interception message, got:\nstderr: %s", stderr)
	}

	if !strings.Contains(stderr, "tuprwre install") {
		t.Fatalf("expected stderr to contain 'tuprwre install' guidance, got:\nstderr: %s", stderr)
	}

	if strings.TrimSpace(stdout) != "" {
		t.Fatalf("expected empty stdout for blocked command, got: %q", stdout)
	}
}
