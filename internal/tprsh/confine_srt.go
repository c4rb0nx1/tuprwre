package tprsh

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// srtSettings mirrors the subset of @anthropic-ai/sandbox-runtime's settings
// file that tprsh drives. srt's posture matches ours: writes and network are
// deny-by-default, reads are allow-by-default with a deny list.
type srtSettings struct {
	Network    srtNetwork    `json:"network"`
	Filesystem srtFilesystem `json:"filesystem"`
}

type srtNetwork struct {
	AllowedDomains []string `json:"allowedDomains"`
}

type srtFilesystem struct {
	DenyRead   []string `json:"denyRead,omitempty"`
	AllowWrite []string `json:"allowWrite"`
	DenyWrite  []string `json:"denyWrite,omitempty"`
}

// srtConfiner delegates confinement to Anthropic's sandbox-runtime CLI
// (npm install -g @anthropic-ai/sandbox-runtime): Seatbelt on macOS,
// bubblewrap on Linux, WFP on Windows, with a host-side proxy for network
// rules. It does NOT pin process-exec to the approved binary the way the
// in-house seatbelt backend does, so on macOS it is the fallback, not the
// default: choosing it trades exec pinning for platform reach.
type srtConfiner struct {
	opts         ConfineOptions
	settingsPath string
}

// newSRT writes the srt settings file derived from opts. The file is added to
// its own denyWrite list, so even a settings file placed inside the workspace
// cannot be rewritten by the confined child: denyWrite takes precedence over
// allowWrite in srt.
func newSRT(opts ConfineOptions) (*srtConfiner, error) {
	f, err := os.CreateTemp("", "tprsh-srt-*.json")
	if err != nil {
		return nil, fmt.Errorf("srt settings: %w", err)
	}
	path := f.Name()

	// Parity with the seatbelt profile: device sinks stay writable in every
	// mode, the workspace only under workspace-write.
	allowWrite := []string{"/dev/null", "/dev/stdout", "/dev/stderr"}
	if opts.Mode == SandboxWorkspaceWrite && opts.Workspace != "" {
		allowWrite = append(allowWrite, opts.Workspace)
	}

	// srt's network model is allow-only. "no network" is the empty allowlist;
	// an open network is expressed as the wildcard domain. Finer per-host
	// allowlists are srt's real advantage over the seatbelt backend and get
	// surfaced once policy carries network intent.
	allowedDomains := []string{"*"}
	if opts.NoNetwork {
		allowedDomains = []string{}
	}

	settings := srtSettings{
		Network: srtNetwork{AllowedDomains: allowedDomains},
		Filesystem: srtFilesystem{
			DenyRead:   opts.NoRead,
			AllowWrite: allowWrite,
			DenyWrite:  append(append([]string{}, opts.NoWrite...), path),
		},
	}

	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		f.Close()
		return nil, err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return nil, fmt.Errorf("srt settings: %w", err)
	}
	if err := f.Close(); err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return nil, err
	}
	return &srtConfiner{opts: opts, settingsPath: path}, nil
}

func (c *srtConfiner) Name() string { return "srt" }

// Available probes functionally, like the seatbelt backend: srt existing on
// PATH is not enough (its Linux backend needs bubblewrap, its proxies need to
// start), so the check is "can it actually run true".
func (c *srtConfiner) Available() bool {
	if _, err := exec.LookPath("srt"); err != nil {
		return false
	}
	truePath, err := exec.LookPath("true")
	if err != nil {
		return false
	}
	argv, err := c.Wrap(truePath, []string{"true"})
	if err != nil {
		return false
	}
	return exec.Command(argv[0], argv[1:]...).Run() == nil
}

// Wrap hands srt a single shell-quoted command string, the invocation form
// its README documents (`srt [flags] "<command>"`). Every token is quoted, so
// if srt runs the string through a shell nothing in an argument can inject,
// and if a future srt execs it directly the call fails loudly rather than
// running unconfined.
func (c *srtConfiner) Wrap(binary string, argv []string) ([]string, error) {
	parts := make([]string, 0, len(argv))
	parts = append(parts, shQuote(binary))
	for _, a := range argv[1:] {
		parts = append(parts, shQuote(a))
	}
	return []string{"srt", "--settings", c.settingsPath, strings.Join(parts, " ")}, nil
}

// shQuote wraps s in single quotes, the only POSIX quoting form with no
// escape processing inside; embedded single quotes use the '\” idiom.
func shQuote(s string) string {
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
