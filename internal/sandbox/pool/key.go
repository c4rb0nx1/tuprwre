package pool

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"sort"
	"strings"
)

// PoolKey identifies when a warm container can be reused.
type PoolKey struct {
	Image     string
	NoNetwork bool
	Memory    int64
	CPUs      float64
	User      string
	Binds     []string
	Runtime   string
}

// CanonicalizePath resolves symlinks (when possible) and normalizes the path.
func CanonicalizePath(p string) string {
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return filepath.Clean(resolved)
	}
	return filepath.Clean(p)
}

// Hash returns a stable, filesystem-safe hash for this pool key.
func (k PoolKey) Hash() string {
	binds := make([]string, 0, len(k.Binds))
	for _, bind := range k.Binds {
		binds = append(binds, canonicalizeBind(bind))
	}
	sort.Strings(binds)

	type canonicalKey struct {
		Image     string   `json:"image"`
		NoNetwork bool     `json:"no_network"`
		Memory    int64    `json:"memory"`
		CPUs      float64  `json:"cpus"`
		User      string   `json:"user"`
		Binds     []string `json:"binds"`
		Runtime   string   `json:"runtime"`
	}

	payload, err := json.Marshal(canonicalKey{
		Image:     k.Image,
		NoNetwork: k.NoNetwork,
		Memory:    k.Memory,
		CPUs:      k.CPUs,
		User:      k.User,
		Binds:     binds,
		Runtime:   k.Runtime,
	})
	if err != nil {
		payload = []byte("{}")
	}

	hash := sha256.Sum256(payload)
	return hex.EncodeToString(hash[:8])
}

func canonicalizeBind(bind string) string {
	parts := strings.Split(bind, ":")
	for i := range parts {
		parts[i] = CanonicalizePath(parts[i])
	}
	return strings.Join(parts, ":")
}

// Labels returns Docker labels used to identify pooled warm containers.
func (k PoolKey) Labels() map[string]string {
	return map[string]string{
		"tuprwre.pool":         "true",
		"tuprwre.pool.key":     k.Hash(),
		"tuprwre.pool.image":   k.Image,
		"tuprwre.pool.version": "1",
	}
}
