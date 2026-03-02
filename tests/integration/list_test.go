//go:build integration

package integration

import (
	"strings"
	"testing"
)

func TestListShowsInstalledTools(t *testing.T) {
	_, env := setupTest(t)

	stdout, stderr, exitCode := runBinary(t, env,
		"install", "--base-image", testImage, "--", "apk add --no-cache jq",
	)
	if exitCode != 0 {
		t.Fatalf("install failed (exit %d):\nstdout: %s\nstderr: %s", exitCode, stdout, stderr)
	}

	listOut, listErr, listExit := runBinary(t, env, "list")
	if listExit != 0 {
		t.Fatalf("list failed (exit %d):\nstdout: %s\nstderr: %s", listExit, listOut, listErr)
	}

	if !strings.Contains(listOut, "jq") {
		t.Fatalf("expected 'jq' in list output, got:\n%s", listOut)
	}
}
