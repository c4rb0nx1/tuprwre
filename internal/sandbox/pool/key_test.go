package pool

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestPoolKeyHash_Deterministic(t *testing.T) {
	key := PoolKey{
		Image:     "alpine:3.19",
		NoNetwork: true,
		Memory:    256 * 1024 * 1024,
		CPUs:      1.5,
		User:      "1000:1000",
		Binds:     []string{"/a", "/b"},
		Runtime:   "docker",
	}

	h1 := key.Hash()
	h2 := key.Hash()
	if h1 != h2 {
		t.Fatalf("Hash() should be deterministic, got %q and %q", h1, h2)
	}
}

func TestPoolKeyHash_DifferentImageDifferentHash(t *testing.T) {
	base := PoolKey{Image: "alpine:3.19", Runtime: "docker"}
	other := PoolKey{Image: "alpine:3.18", Runtime: "docker"}

	if base.Hash() == other.Hash() {
		t.Fatalf("expected different hash for different images")
	}
}

func TestPoolKeyHash_DifferentNoNetworkDifferentHash(t *testing.T) {
	base := PoolKey{Image: "alpine:3.19", NoNetwork: false, Runtime: "docker"}
	other := PoolKey{Image: "alpine:3.19", NoNetwork: true, Runtime: "docker"}

	if base.Hash() == other.Hash() {
		t.Fatalf("expected different hash for different NoNetwork values")
	}
}

func TestPoolKeyHash_BindPathsCanonicalizedMacVarPrivateVar(t *testing.T) {
	k1 := PoolKey{Image: "alpine:3.19", Binds: []string{"/var/foo"}, Runtime: "docker"}
	k2 := PoolKey{Image: "alpine:3.19", Binds: []string{"/private/var/foo"}, Runtime: "docker"}

	c1 := CanonicalizePath("/var/foo")
	c2 := CanonicalizePath("/private/var/foo")

	if c1 == c2 {
		if k1.Hash() != k2.Hash() {
			t.Fatalf("expected equal hashes when canonicalized bind paths are equal: %q vs %q", c1, c2)
		}
		return
	}

	if c1 != filepath.Clean("/var/foo") {
		t.Fatalf("CanonicalizePath(/var/foo) = %q, want %q", c1, filepath.Clean("/var/foo"))
	}
	if c2 != filepath.Clean("/private/var/foo") {
		t.Fatalf("CanonicalizePath(/private/var/foo) = %q, want %q", c2, filepath.Clean("/private/var/foo"))
	}
}

func TestPoolKeyHash_BindOrderDoesNotMatter(t *testing.T) {
	k1 := PoolKey{Image: "alpine:3.19", Binds: []string{"/a", "/b"}, Runtime: "docker"}
	k2 := PoolKey{Image: "alpine:3.19", Binds: []string{"/b", "/a"}, Runtime: "docker"}

	if k1.Hash() != k2.Hash() {
		t.Fatalf("expected equal hashes for different bind order")
	}
}

func TestPoolKeyHash_Is16HexCharacters(t *testing.T) {
	key := PoolKey{Image: "alpine:3.19", Runtime: "docker"}
	h := key.Hash()

	if len(h) != 16 {
		t.Fatalf("hash length = %d, want 16", len(h))
	}
	if _, err := hex.DecodeString(h); err != nil {
		t.Fatalf("hash is not valid hex: %q (%v)", h, err)
	}
}

func TestCanonicalizePath_CleansDotDotAndDoubleSlash(t *testing.T) {
	tmp := t.TempDir()
	raw := filepath.Join(tmp, "a") + "/../b//c"

	got := CanonicalizePath(raw)
	want := filepath.Clean(raw)

	if got != want {
		t.Fatalf("CanonicalizePath(%q) = %q, want %q", raw, got, want)
	}
}

func TestCanonicalizePath_ResolvesSymlinkWhenExists(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "real")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatalf("failed to create target dir: %v", err)
	}

	link := filepath.Join(tmp, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("failed to create symlink: %v", err)
	}

	got := CanonicalizePath(link)
	want := filepath.Clean(target)
	if resolved, err := filepath.EvalSymlinks(target); err == nil {
		want = filepath.Clean(resolved)
	}
	if got != want {
		t.Fatalf("CanonicalizePath(%q) = %q, want %q", link, got, want)
	}
}

func TestCanonicalizePath_FallsBackToCleanForBrokenSymlink(t *testing.T) {
	tmp := t.TempDir()
	brokenTarget := filepath.Join(tmp, "missing-target")
	link := filepath.Join(tmp, "broken-link")
	if err := os.Symlink(brokenTarget, link); err != nil {
		t.Fatalf("failed to create broken symlink: %v", err)
	}

	got := CanonicalizePath(link)
	want := filepath.Clean(link)
	if got != want {
		t.Fatalf("CanonicalizePath(%q) = %q, want %q", link, got, want)
	}
}
