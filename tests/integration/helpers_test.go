//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func setupTest(t *testing.T) (tuprwreDir string, env []string) {
	t.Helper()

	tuprwreDir = t.TempDir()
	shimDir := filepath.Join(tuprwreDir, "bin")
	if err := os.MkdirAll(shimDir, 0o755); err != nil {
		t.Fatalf("failed to create shim dir: %v", err)
	}

	shellVal := os.Getenv("SHELL")
	if shellVal == "" {
		shellVal = "/bin/sh"
	}

	env = []string{
		"TUPRWRE_DIR=" + tuprwreDir,
		"HOME=" + t.TempDir(),
		"PATH=" + shimDir + ":" + filepath.Dir(binaryPath) + ":" + os.Getenv("PATH"),
		"SHELL=" + shellVal,
	}
	if host := os.Getenv("DOCKER_HOST"); host != "" {
		env = append(env, "DOCKER_HOST="+host)
	}

	t.Cleanup(func() {
		cleanupTestImages(t, tuprwreDir)
	})

	return tuprwreDir, env
}

func runBinary(t *testing.T, env []string, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, binaryPath, args...)
	cmd.Env = env

	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	err := cmd.Run()
	stdout = outBuf.String()
	stderr = errBuf.String()

	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			t.Fatalf("command timed out after 2m: tuprwre %s", strings.Join(args, " "))
		}
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			t.Fatalf("failed to run binary: %v", err)
		}
	}

	return stdout, stderr, exitCode
}

func cleanupTestImages(t *testing.T, tuprwreDir string) {
	t.Helper()

	metadataDir := filepath.Join(tuprwreDir, "metadata")
	entries, err := os.ReadDir(metadataDir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		data, err := os.ReadFile(filepath.Join(metadataDir, entry.Name()))
		if err != nil {
			continue
		}

		var meta struct {
			OutputImage string `json:"output_image"`
		}
		if err := json.Unmarshal(data, &meta); err != nil || meta.OutputImage == "" {
			continue
		}

		rmiCmd := exec.Command("docker", "rmi", "-f", meta.OutputImage)
		_ = rmiCmd.Run()
	}
}

func dockerImageExists(t *testing.T, imageName string) bool {
	t.Helper()
	cmd := exec.Command("docker", "image", "inspect", imageName)
	return cmd.Run() == nil
}

func extractImageFromOutput(output string) string {
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, "Committing container to image:") {
			parts := strings.SplitN(line, "Committing container to image:", 2)
			if len(parts) == 2 {
				return strings.TrimSpace(parts[1])
			}
		}
	}
	return ""
}
