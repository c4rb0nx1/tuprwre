// Package dockerctx resolves the Docker daemon endpoint the same way the
// docker CLI does: DOCKER_HOST wins, then the CLI's current context (as
// recorded by `docker context use`), then the SDK default socket. The Go SDK
// alone only honors DOCKER_HOST, which silently strands users of Colima,
// Docker Desktop contexts, and remote contexts.
package dockerctx

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/docker/docker/client"
)

// ClientOpts returns Docker client options honoring DOCKER_HOST first and the
// docker CLI's current context second.
func ClientOpts() []client.Opt {
	opts := []client.Opt{client.FromEnv, client.WithAPIVersionNegotiation()}
	if os.Getenv("DOCKER_HOST") != "" {
		return opts
	}
	if host := CurrentContextHost(); host != "" {
		opts = append(opts, client.WithHost(host))
	}
	return opts
}

// CurrentContextHost returns the docker endpoint of the CLI's current
// context, or "" when the context is unset, "default", or unreadable.
func CurrentContextHost() string {
	dockerDir := os.Getenv("DOCKER_CONFIG")
	if dockerDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		dockerDir = filepath.Join(home, ".docker")
	}

	cfgRaw, err := os.ReadFile(filepath.Join(dockerDir, "config.json"))
	if err != nil {
		return ""
	}
	var cfg struct {
		CurrentContext string `json:"currentContext"`
	}
	if err := json.Unmarshal(cfgRaw, &cfg); err != nil {
		return ""
	}
	if cfg.CurrentContext == "" || cfg.CurrentContext == "default" {
		return ""
	}

	// Context metadata lives under a directory named by the SHA-256 of the
	// context name — the same layout the docker CLI context store uses.
	sum := sha256.Sum256([]byte(cfg.CurrentContext))
	metaRaw, err := os.ReadFile(filepath.Join(dockerDir, "contexts", "meta", hex.EncodeToString(sum[:]), "meta.json"))
	if err != nil {
		return ""
	}
	var meta struct {
		Endpoints map[string]struct {
			Host string `json:"Host"`
		} `json:"Endpoints"`
	}
	if err := json.Unmarshal(metaRaw, &meta); err != nil {
		return ""
	}
	return meta.Endpoints["docker"].Host
}
