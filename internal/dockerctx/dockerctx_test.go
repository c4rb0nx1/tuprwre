package dockerctx

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func writeContextStore(t *testing.T, dir, contextName, host string) {
	t.Helper()

	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"currentContext":"`+contextName+`"}`), 0o644); err != nil {
		t.Fatalf("write config.json: %v", err)
	}

	sum := sha256.Sum256([]byte(contextName))
	metaDir := filepath.Join(dir, "contexts", "meta", hex.EncodeToString(sum[:]))
	if err := os.MkdirAll(metaDir, 0o755); err != nil {
		t.Fatalf("mkdir meta dir: %v", err)
	}
	meta := `{"Name":"` + contextName + `","Endpoints":{"docker":{"Host":"` + host + `","SkipTLSVerify":false}}}`
	if err := os.WriteFile(filepath.Join(metaDir, "meta.json"), []byte(meta), 0o644); err != nil {
		t.Fatalf("write meta.json: %v", err)
	}
}

func TestCurrentContextHostResolvesEndpoint(t *testing.T) {
	dir := t.TempDir()
	writeContextStore(t, dir, "colima", "unix:///tmp/colima.sock")
	t.Setenv("DOCKER_CONFIG", dir)

	if got := CurrentContextHost(); got != "unix:///tmp/colima.sock" {
		t.Fatalf("CurrentContextHost() = %q, want unix:///tmp/colima.sock", got)
	}
}

func TestCurrentContextHostIgnoresDefaultContext(t *testing.T) {
	dir := t.TempDir()
	writeContextStore(t, dir, "default", "unix:///should/not/be/used.sock")
	t.Setenv("DOCKER_CONFIG", dir)

	if got := CurrentContextHost(); got != "" {
		t.Fatalf("CurrentContextHost() = %q, want empty for default context", got)
	}
}

func TestCurrentContextHostMissingStore(t *testing.T) {
	t.Setenv("DOCKER_CONFIG", t.TempDir())

	if got := CurrentContextHost(); got != "" {
		t.Fatalf("CurrentContextHost() = %q, want empty when no config exists", got)
	}
}

func TestClientOptsPrefersDockerHostEnv(t *testing.T) {
	dir := t.TempDir()
	writeContextStore(t, dir, "colima", "unix:///tmp/colima.sock")
	t.Setenv("DOCKER_CONFIG", dir)
	t.Setenv("DOCKER_HOST", "unix:///tmp/env-wins.sock")

	// With DOCKER_HOST set, only FromEnv + negotiation are returned; the
	// context host must not be appended (it would override DOCKER_HOST).
	if got := len(ClientOpts()); got != 2 {
		t.Fatalf("ClientOpts() returned %d opts, want 2 when DOCKER_HOST is set", got)
	}
}
