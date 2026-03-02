//go:build integration

package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRemoveDeletesShimAndImage(t *testing.T) {
	tuprwreDir, env := setupTest(t)
	shimDir := filepath.Join(tuprwreDir, "bin")

	stdout, stderr, exitCode := runBinary(t, env,
		"install", "--base-image", testImage, "--", "apk add --no-cache jq",
	)
	if exitCode != 0 {
		t.Fatalf("install failed (exit %d):\nstdout: %s\nstderr: %s", exitCode, stdout, stderr)
	}

	shimPath := filepath.Join(shimDir, "jq")
	if _, err := os.Stat(shimPath); err != nil {
		t.Fatalf("shim not created after install: %v", err)
	}

	imageName := extractImageFromOutput(stdout + stderr)

	rmOut, rmErr, rmExit := runBinary(t, env, "remove", "--images", "jq")
	if rmExit != 0 {
		t.Fatalf("remove failed (exit %d):\nstdout: %s\nstderr: %s", rmExit, rmOut, rmErr)
	}

	if _, err := os.Stat(shimPath); !os.IsNotExist(err) {
		t.Fatalf("expected shim to be deleted, but it still exists")
	}

	metadataPath := filepath.Join(tuprwreDir, "metadata", "jq.json")
	if _, err := os.Stat(metadataPath); !os.IsNotExist(err) {
		t.Fatalf("expected metadata to be deleted, but it still exists")
	}

	if imageName != "" && dockerImageExists(t, imageName) {
		t.Fatalf("expected Docker image %q to be removed, but it still exists", imageName)
	}

	listOut, _, _ := runBinary(t, env, "list")
	if strings.Contains(listOut, "jq") {
		t.Fatalf("expected 'jq' to be absent from list after remove, got:\n%s", listOut)
	}
}
