//go:build integration

package integration

import (
	"testing"
)

func TestRunNoNetworkBlocked(t *testing.T) {
	_, env := setupTest(t)

	_, _, exitCode := runBinary(t, env,
		"run", "--image", testImage, "--no-network", "--",
		"sh", "-c", "wget -q -O /dev/null http://google.com",
	)
	if exitCode == 0 {
		t.Fatalf("expected non-zero exit code with --no-network, but got 0")
	}

	stdout, stderr, exitCode := runBinary(t, env,
		"run", "--image", testImage, "--",
		"sh", "-c", "echo ok",
	)
	if exitCode != 0 {
		t.Fatalf("baseline run failed (exit %d):\nstdout: %s\nstderr: %s", exitCode, stdout, stderr)
	}
}
