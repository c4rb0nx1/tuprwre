package tprsh

import (
	"encoding/json"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// readSettings loads the settings file a srtConfiner generated.
func readSettings(t *testing.T, c *srtConfiner) srtSettings {
	t.Helper()
	data, err := os.ReadFile(c.settingsPath)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	var s srtSettings
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatalf("unmarshal settings: %v", err)
	}
	return s
}

func TestSRTSettingsWorkspaceWrite(t *testing.T) {
	ws := t.TempDir()
	auditDir := t.TempDir()
	credDir := t.TempDir()

	c, err := newSRT(ConfineOptions{
		Mode:      SandboxWorkspaceWrite,
		Workspace: ws,
		NoWrite:   []string{auditDir},
		NoRead:    []string{credDir},
	})
	if err != nil {
		t.Fatalf("newSRT: %v", err)
	}
	t.Cleanup(func() { os.Remove(c.settingsPath) })
	s := readSettings(t, c)

	if !slices.Contains(s.Filesystem.AllowWrite, ws) {
		t.Errorf("workspace %q missing from allowWrite %v", ws, s.Filesystem.AllowWrite)
	}
	for _, sink := range []string{"/dev/null", "/dev/stdout", "/dev/stderr"} {
		if !slices.Contains(s.Filesystem.AllowWrite, sink) {
			t.Errorf("device sink %q missing from allowWrite", sink)
		}
	}
	if !slices.Contains(s.Filesystem.DenyWrite, auditDir) {
		t.Errorf("audit dir %q missing from denyWrite %v", auditDir, s.Filesystem.DenyWrite)
	}
	// The settings file itself must be tamper-proof against the child.
	if !slices.Contains(s.Filesystem.DenyWrite, c.settingsPath) {
		t.Errorf("settings path %q missing from denyWrite %v", c.settingsPath, s.Filesystem.DenyWrite)
	}
	if !slices.Contains(s.Filesystem.DenyRead, credDir) {
		t.Errorf("cred dir %q missing from denyRead %v", credDir, s.Filesystem.DenyRead)
	}
	// Network open by default: wildcard, not empty.
	if !slices.Equal(s.Network.AllowedDomains, []string{"*"}) {
		t.Errorf("open network should be [\"*\"], got %v", s.Network.AllowedDomains)
	}
}

func TestSRTSettingsReadOnlyNoNetwork(t *testing.T) {
	ws := t.TempDir()
	c, err := newSRT(ConfineOptions{Mode: SandboxReadOnly, Workspace: ws, NoNetwork: true})
	if err != nil {
		t.Fatalf("newSRT: %v", err)
	}
	t.Cleanup(func() { os.Remove(c.settingsPath) })
	s := readSettings(t, c)

	if slices.Contains(s.Filesystem.AllowWrite, ws) {
		t.Errorf("read-only mode must not allow workspace writes, got %v", s.Filesystem.AllowWrite)
	}
	if len(s.Network.AllowedDomains) != 0 {
		t.Errorf("no-network should produce an empty allowlist, got %v", s.Network.AllowedDomains)
	}
}

func TestSRTWrapQuotesEveryToken(t *testing.T) {
	c, err := newSRT(ConfineOptions{Mode: SandboxReadOnly, Workspace: t.TempDir()})
	if err != nil {
		t.Fatalf("newSRT: %v", err)
	}
	t.Cleanup(func() { os.Remove(c.settingsPath) })

	argv, err := c.Wrap("/usr/bin/grep", []string{"grep", "-r", "a b; rm -rf /", "."})
	if err != nil {
		t.Fatalf("wrap: %v", err)
	}
	if argv[0] != "srt" || argv[1] != "--settings" || argv[2] != c.settingsPath {
		t.Fatalf("unexpected prefix: %v", argv)
	}
	if len(argv) != 4 {
		t.Fatalf("want a single command-string argument, got %d args: %v", len(argv), argv)
	}
	cmd := argv[3]
	if !strings.Contains(cmd, `'a b; rm -rf /'`) {
		t.Errorf("hostile argument not quoted intact: %q", cmd)
	}
}

func TestShQuote(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", "''"},
		{"plain", "'plain'"},
		{"has space", "'has space'"},
		{"semi;colon", "'semi;colon'"},
		{"don't", `'don'\''t'`},
		{"$HOME", "'$HOME'"},
		{"`whoami`", "'`whoami`'"},
	}
	for _, tc := range cases {
		if got := shQuote(tc.in); got != tc.want {
			t.Errorf("shQuote(%q) = %s, want %s", tc.in, got, tc.want)
		}
	}
	// The quoted forms must round-trip through a real shell unchanged.
	for _, tc := range cases {
		out, err := exec.Command("/bin/sh", "-c", "printf %s "+shQuote(tc.in)).Output()
		if err != nil {
			t.Fatalf("sh round-trip of %q: %v", tc.in, err)
		}
		if string(out) != tc.in {
			t.Errorf("round-trip of %q through sh gave %q", tc.in, string(out))
		}
	}
}

func TestNewConfinerBackendSelection(t *testing.T) {
	base := ConfineOptions{Mode: SandboxReadOnly, Workspace: t.TempDir()}

	bogus := base
	bogus.Backend = "gvisor"
	if _, err := NewConfiner(bogus); err == nil {
		t.Error("unknown backend must be rejected, not defaulted")
	}

	if _, err := exec.LookPath("srt"); err != nil {
		forced := base
		forced.Backend = "srt"
		if _, err := NewConfiner(forced); err == nil {
			t.Error("forced srt backend must fail closed when srt is not installed")
		}
	}

	if runtime.GOOS == "darwin" && (&seatbelt{opts: base}).Available() {
		auto := base
		auto.Backend = "auto"
		c, err := NewConfiner(auto)
		if err != nil {
			t.Fatalf("auto on darwin with sandbox-exec present: %v", err)
		}
		if c.Name() != "seatbelt" {
			t.Errorf("auto on darwin must prefer seatbelt (exec pinning), got %q", c.Name())
		}
	}
}
