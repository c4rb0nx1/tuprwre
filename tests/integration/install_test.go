//go:build integration

package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallAndShimExecution(t *testing.T) {
	tuprwreDir, env := setupTest(t)
	shimDir := filepath.Join(tuprwreDir, "bin")

	stdout, stderr, exitCode := runBinary(t, env,
		"install", "--base-image", testImage, "--", "apk add --no-cache jq",
	)
	if exitCode != 0 {
		t.Fatalf("install failed (exit %d):\nstdout: %s\nstderr: %s", exitCode, stdout, stderr)
	}

	shimPath := filepath.Join(shimDir, "jq")
	info, err := os.Stat(shimPath)
	if err != nil {
		t.Fatalf("shim not created at %s: %v", shimPath, err)
	}
	if info.Mode()&0o111 == 0 {
		t.Fatalf("shim is not executable: mode=%v", info.Mode())
	}

	metadataPath := filepath.Join(tuprwreDir, "metadata", "jq.json")
	if _, err := os.Stat(metadataPath); err != nil {
		t.Fatalf("metadata not created at %s: %v", metadataPath, err)
	}

	jqCmd := exec.Command(shimPath, "--version")
	jqCmd.Env = env
	jqOut, err := jqCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("jq --version via shim failed: %v\noutput: %s", err, string(jqOut))
	}
	if !strings.Contains(string(jqOut), "jq-") {
		t.Fatalf("unexpected jq --version output: %q", string(jqOut))
	}
}
