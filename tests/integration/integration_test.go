//go:build integration

package integration

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/docker/docker/client"
)

var binaryPath string

const testImage = "alpine:3.19"

func TestMain(m *testing.M) {
	tmpDir, err := os.MkdirTemp("", "tuprwre-integration-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create temp dir: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintln(os.Stderr, "=== Building tuprwre binary ===")
	binaryPath = filepath.Join(tmpDir, "tuprwre")
	buildCmd := exec.Command("go", "build", "-o", binaryPath, "./cmd/tuprwre")
	buildCmd.Dir = findProjectRoot()
	buildCmd.Stdout = os.Stderr
	buildCmd.Stderr = os.Stderr
	if err := buildCmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to build binary: %v\n", err)
		os.Exit(1)
	}

	if !dockerAvailable() {
		fmt.Fprintln(os.Stderr, "skipping: Docker daemon not available")
		os.Exit(0)
	}

	fmt.Fprintf(os.Stderr, "=== Pre-pulling %s ===\n", testImage)
	pullCmd := exec.Command("docker", "pull", testImage)
	pullCmd.Stdout = os.Stderr
	pullCmd.Stderr = os.Stderr
	if err := pullCmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to pre-pull %s: %v\n", testImage, err)
		os.Exit(1)
	}

	fmt.Fprintln(os.Stderr, "=== Running integration tests ===")
	code := m.Run()
	os.RemoveAll(tmpDir)
	os.Exit(code)
}

func dockerAvailable() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return false
	}
	defer cli.Close()

	_, err = cli.Ping(ctx)
	return err == nil
}

func findProjectRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		return "."
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "."
		}
		dir = parent
	}
}
