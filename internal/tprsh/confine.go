package tprsh

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// SandboxMode is the file-effect policy applied to an approved command. The
// vocabulary matches the one Docker's dsh settled on, so profiles are
// comparable across implementations.
type SandboxMode string

const (
	// SandboxNone runs approved commands unconfined (language boundary only).
	SandboxNone SandboxMode = "none"
	// SandboxReadOnly permits writes only to device sinks.
	SandboxReadOnly SandboxMode = "read-only"
	// SandboxWorkspaceWrite additionally permits writes under the workspace.
	SandboxWorkspaceWrite SandboxMode = "workspace-write"
)

// Confiner wraps an approved command's argv so the launched process is
// confined by the OS. Wrap is called only after policy has approved the
// command, so it never decides *whether* something runs, only how tightly.
type Confiner interface {
	Wrap(binary string, argv []string) ([]string, error)
	Available() bool
	Name() string
}

// ConfineOptions describes what a confined process may touch.
type ConfineOptions struct {
	Mode SandboxMode
	// Workspace is the only writable tree under SandboxWorkspaceWrite. It must
	// already be symlink-resolved: Seatbelt matches resolved paths, so on macOS
	// /tmp and /private/tmp are the same place and an unresolved root silently
	// fails to match.
	Workspace string
	// NoWrite are paths the process must never write, even inside the
	// workspace — the audit log lives here, which is what turns its integrity
	// from "the child does not know the path" into "the kernel refuses".
	NoWrite []string
	// NoRead are paths the process must never read: credential stores.
	//
	// This is deliberately a blocklist rather than a workspace allowlist.
	// Confining reads to the workspace is not achievable with Seatbelt: any
	// broad file-read denial kills the process during dynamic loading with
	// SIGABRT and no diagnostic, because every runtime reads an
	// version-dependent set of loader caches and frameworks. Read confinement
	// belongs to the Linux backend, where a mount namespace simply does not
	// contain the paths you did not bind, so there is no allowlist to get
	// wrong. dsh reaches the same conclusion: its whole vocabulary governs
	// writes.
	NoRead []string

	// NoNetwork denies all outbound network access. Verified effective, but
	// it is all-or-nothing: Seatbelt cannot express a host allowlist, so a
	// workload that legitimately needs the network (an SDK call, a package
	// index) must run with the network open and is then only as contained as
	// an egress proxy makes it.
	NoNetwork bool

	// ExecAlso permits additional exec targets beyond the approved binary.
	// Required because macOS binaries re-exec through shim chains --
	// /usr/bin/python3 reaches the framework interpreter through two hops, and
	// /bin/sh execs its bash variant -- and a single-binary allowlist stops
	// them from starting at all.
	ExecAlso []string
}

// NewConfiner returns a Confiner for this platform, or an error when a
// confining mode was requested and no backend can deliver it. Failing closed
// is deliberate: a silently-unconfined process would make the audit log's
// completeness claim false without anyone noticing.
func NewConfiner(opts ConfineOptions) (Confiner, error) {
	if opts.Mode == "" || opts.Mode == SandboxNone {
		return nopConfiner{}, nil
	}
	for _, p := range append(append([]string{opts.Workspace}, opts.NoWrite...), opts.NoRead...) {
		if strings.ContainsAny(p, `"\`) {
			return nil, fmt.Errorf("sandbox path contains unsupported characters: %q", p)
		}
	}

	switch runtime.GOOS {
	case "darwin":
		sb := &seatbelt{opts: opts}
		if !sb.Available() {
			return nil, fmt.Errorf("sandbox mode %q requested but sandbox-exec is unavailable", opts.Mode)
		}
		return sb, nil
	default:
		return nil, fmt.Errorf("sandbox mode %q requested but no backend exists for %s", opts.Mode, runtime.GOOS)
	}
}

type nopConfiner struct{}

func (nopConfiner) Wrap(_ string, argv []string) ([]string, error) { return argv, nil }
func (nopConfiner) Available() bool                                { return true }
func (nopConfiner) Name() string                                   { return "none" }

// seatbelt confines via macOS sandbox-exec. Apple marks the CLI deprecated but
// ships it on every release; Available() is a functional probe rather than a
// version check so the failure is detected rather than assumed.
type seatbelt struct {
	opts ConfineOptions
}

func (s *seatbelt) Name() string { return "seatbelt" }

func (s *seatbelt) Available() bool {
	if _, err := exec.LookPath("sandbox-exec"); err != nil {
		return false
	}
	probe := exec.Command("sandbox-exec", "-p", "(version 1)(allow default)", "/usr/bin/true")
	return probe.Run() == nil
}

// Wrap returns sandbox-exec plus a generated profile plus the original argv.
// binary must be the resolved absolute path of the approved command: the
// profile permits exec of exactly that file and nothing else, so a subverted
// binary cannot spawn a shell — which is what keeps the audit log's "these are
// all the commands that ran" claim true.
func (s *seatbelt) Wrap(binary string, argv []string) ([]string, error) {
	profile, err := s.profile(binary)
	if err != nil {
		return nil, err
	}
	wrapped := []string{"sandbox-exec", "-p", profile, binary}
	return append(wrapped, argv[1:]...), nil
}

func (s *seatbelt) profile(binary string) (string, error) {
	var b strings.Builder
	b.WriteString("(version 1)(allow default)")

	// Writes: deny everything, then re-permit the sinks and, in
	// workspace-write, the workspace itself.
	b.WriteString("(deny file-write*)")
	b.WriteString(`(allow file-write* (literal "/dev/null") (literal "/dev/stdout") (literal "/dev/stderr"))`)
	if s.opts.Mode == SandboxWorkspaceWrite && s.opts.Workspace != "" {
		fmt.Fprintf(&b, `(allow file-write* (subpath "%s"))`, s.opts.Workspace)
	}

	// Re-deny the audit log and any other protected path, after the workspace
	// grant so it wins even when the log sits inside the workspace tree.
	for _, p := range s.opts.NoWrite {
		fmt.Fprintf(&b, `(deny file-write* (subpath "%s"))`, p)
	}
	for _, p := range s.opts.NoRead {
		fmt.Fprintf(&b, `(deny file-read* (subpath "%s"))`, p)
	}

	if s.opts.NoNetwork {
		b.WriteString("(deny network*)")
	}

	// Exec: only the approved binary and any declared re-exec targets. A
	// blanket (deny process-exec*) would block sandbox-exec's own launch of
	// the target, so the allow is required.
	abs, err := filepath.Abs(binary)
	if err != nil {
		return "", err
	}
	b.WriteString(`(deny process-exec*)(allow process-exec (literal "` + abs + `")`)
	for _, extra := range s.opts.ExecAlso {
		if extra == "" {
			continue
		}
		fmt.Fprintf(&b, ` (literal "%s")`, extra)
	}
	b.WriteString(")")

	return b.String(), nil
}

// CanonicalDir resolves symlinks so a path matches what the kernel sees.
// Required on macOS where /tmp is a symlink to /private/tmp and Seatbelt
// compares resolved paths.
func CanonicalDir(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved
	}
	return abs
}

// DefaultProtectedReadPaths are credential stores an agent has no business
// reading through a shimmed tool.
func DefaultProtectedReadPaths() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	var out []string
	for _, rel := range []string{".aws", ".ssh", ".kube", ".config/gcloud", ".docker"} {
		p := filepath.Join(home, rel)
		if _, err := os.Stat(p); err == nil {
			out = append(out, CanonicalDir(p))
		}
	}
	return out
}
