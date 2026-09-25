// Command tprsh-gateway runs the record-only LLM gateway as a standalone
// process: a pass-through reverse proxy in front of an LLM API that records
// tool-call intent and tool results to a JSONL sink. It is a research
// component of the tprsh runtime, not a production security boundary.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/c4rb0nx1/tuprwre/internal/gateway"
	"github.com/c4rb0nx1/tuprwre/internal/gateway/proxy"
)

// shutdownBound bounds each graceful-shutdown phase: waiting for in-flight
// HTTP handlers and draining the recording pipeline.
const shutdownBound = 5 * time.Second

// options holds the validated command-line configuration.
type options struct {
	listen       string
	upstream     *url.URL
	logPath      string
	logDefaulted bool
	sessionID    string
	noRedact     bool
	noLog        bool
	allowRemote  bool
}

// parseFlags parses and validates args. Warnings (not errors) are written to
// stderr; the caller owns process exit. It returns a fully populated,
// validated options value or a descriptive error.
func parseFlags(args []string, stderr io.Writer) (*options, error) {
	fs := flag.NewFlagSet("tprsh-gateway", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var (
		listen      = fs.String("listen", "127.0.0.1:0", "address to listen on (loopback only unless --allow-remote)")
		upstream    = fs.String("upstream", "", "upstream LLM API base URL (required), e.g. https://api.anthropic.com")
		logPath     = fs.String("log", "", "path to the JSONL event log (created 0600, parent dir 0700); default: $XDG_STATE_HOME/tprsh/gateway/<session-id>.jsonl")
		noLog       = fs.Bool("no-log", false, "disable recording entirely; no log file is written (mutually exclusive with --log)")
		sessionID   = fs.String("session-id", "", "session identifier attached to every event (default: random)")
		noRedact    = fs.Bool("no-redact", false, "disable redaction of secret-looking values (records raw payloads)")
		allowRemote = fs.Bool("allow-remote", false, "permit listening on a non-loopback address")
	)
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	if fs.NArg() > 0 {
		return nil, fmt.Errorf("unexpected positional arguments: %s", strings.Join(fs.Args(), " "))
	}

	opts := &options{
		listen:      *listen,
		logPath:     *logPath,
		sessionID:   *sessionID,
		noRedact:    *noRedact,
		noLog:       *noLog,
		allowRemote: *allowRemote,
	}
	if *upstream == "" {
		return nil, errors.New("--upstream is required")
	}
	u, err := url.Parse(*upstream)
	if err != nil {
		return nil, fmt.Errorf("invalid --upstream %q: %w", *upstream, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("invalid --upstream %q: scheme must be http or https", *upstream)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("invalid --upstream %q: host is required", *upstream)
	}
	opts.upstream = u

	if _, _, err := net.SplitHostPort(opts.listen); err != nil {
		return nil, fmt.Errorf("invalid --listen %q: %w", opts.listen, err)
	}
	if !opts.allowRemote && !isLoopbackListen(opts.listen) {
		return nil, fmt.Errorf("--listen %q is not a loopback address; pass --allow-remote to bind it", opts.listen)
	}

	if opts.sessionID == "" {
		id, err := randomSessionID()
		if err != nil {
			return nil, fmt.Errorf("generate session id: %w", err)
		}
		opts.sessionID = id
	}

	if opts.noLog && opts.logPath != "" {
		return nil, errors.New("--no-log and --log are mutually exclusive")
	}
	switch {
	case opts.noLog:
		opts.logPath = ""
		fmt.Fprintln(stderr, "tprsh-gateway: WARNING: recording disabled (--no-log); the gateway will forward traffic without writing events")
	case opts.logPath == "":
		path, err := defaultLogPath(os.Getenv("XDG_STATE_HOME"), opts.sessionID)
		if err != nil {
			return nil, err
		}
		opts.logPath = path
		opts.logDefaulted = true
	}

	if opts.noRedact {
		fmt.Fprintln(stderr, "tprsh-gateway: WARNING: redaction disabled (--no-redact); recorded payloads may contain credentials")
	}
	return opts, nil
}

// defaultLogPath returns the default JSONL log path for a session:
// $XDG_STATE_HOME/tprsh/gateway/<session-id>.jsonl, or
// ~/.local/state/tprsh/gateway/<session-id>.jsonl when XDG_STATE_HOME is unset.
func defaultLogPath(stateHome, sessionID string) (string, error) {
	if stateHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve default log path: %w", err)
		}
		stateHome = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(stateHome, "tprsh", "gateway", sessionID+".jsonl"), nil
}

// isLoopbackListen reports whether the listen address binds a loopback host.
// An empty or wildcard host binds every interface and is not loopback.
func isLoopbackListen(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if host == "" {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// randomSessionID returns a 128-bit random hex identifier.
func randomSessionID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func main() {
	opts, err := parseFlags(os.Args[1:], os.Stderr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "tprsh-gateway: %v\n", err)
		os.Exit(2)
	}
	if err := run(opts); err != nil {
		fmt.Fprintf(os.Stderr, "tprsh-gateway: %v\n", err)
		os.Exit(1)
	}
}

// run builds and serves the gateway until SIGINT/SIGTERM, then shuts down
// gracefully within shutdownBound and prints the final recording stats as one
// JSON line on stderr.
func run(opts *options) error {
	sink, err := openSink(opts.logPath, opts.logDefaulted, os.Stderr)
	if err != nil {
		return err
	}
	if sink != nil {
		defer sink.Close()
		fmt.Fprintf(os.Stderr, "tprsh-gateway: recording events to %s\n", opts.logPath)
	}

	var (
		recordSink gateway.Sink
		prior      []proxy.ResultKey
	)
	if sink != nil {
		recordSink = sink
		prior = loadPriorResults(opts.logPath, opts.sessionID, os.Stderr)
	}
	p, err := proxy.New(proxy.Config{
		Upstream:         opts.upstream,
		Sink:             recordSink,
		SessionID:        opts.sessionID,
		DisableRedaction: opts.noRedact,
		ErrorLog:         newLogger(os.Stderr),
		PriorResults:     prior,
	})
	if err != nil {
		return err
	}

	ln, err := net.Listen("tcp", opts.listen)
	if err != nil {
		return err
	}
	// One machine-readable line so a supervising script can discover the port
	// of the ephemeral listener.
	fmt.Printf("listening http://%s\n", printableAddr(ln.Addr().String()))

	srv := &http.Server{Handler: p}
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()

	sigCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	select {
	case <-sigCtx.Done():
	case err := <-serveErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			// Bound the drain here too: a stuck stream must not hang shutdown.
			_ = closeBounded(p, shutdownBound)
			_ = sink.Close()
			return err
		}
	}

	// Stop accepting connections and let in-flight handlers finish, then drain
	// the recording pipeline. Each phase is bounded independently.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownBound)
	_ = srv.Shutdown(shutdownCtx)
	cancel()

	closeErr := closeBounded(p, shutdownBound)
	if closeErr != nil {
		fmt.Fprintf(os.Stderr, "tprsh-gateway: shutdown did not drain within %s: %v\n", shutdownBound, closeErr)
	}

	stats, err := json.Marshal(p.Stats())
	if err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, string(stats))
	return nil
}

// loadPriorResults returns the tool results an earlier gateway already
// recorded for sessionID in the log being appended to, so a restart does not
// re-record the conversation history the harness resends. A missing or empty
// log yields nothing; a read failure is only warned about, since recording
// matters more than dedup.
func loadPriorResults(path, sessionID string, stderr io.Writer) []proxy.ResultKey {
	f, err := os.Open(path)
	if err != nil {
		if !os.IsNotExist(err) {
			fmt.Fprintf(stderr, "tprsh-gateway: WARNING: cannot read %s for dedup: %v\n", path, err)
		}
		return nil
	}
	defer f.Close()
	prior, err := proxy.PriorResults(f, sessionID)
	if err != nil {
		fmt.Fprintf(stderr, "tprsh-gateway: WARNING: reading %s for dedup: %v\n", path, err)
	}
	if len(prior) > 0 {
		fmt.Fprintf(stderr, "tprsh-gateway: resuming session %s: %d tool results already in the log will not be re-recorded\n", sessionID, len(prior))
	}
	return prior
}

// closeBounded stops recording and waits for the pipeline to drain, bounded by
// bound. Both shutdown paths use it so neither can hang on a stuck stream.
func closeBounded(p *proxy.Proxy, bound time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), bound)
	defer cancel()
	return p.Close(ctx)
}

// openSink opens the JSONL sink, creating the parent directory 0700. It also
// tightens permissions that a previous run (or another process) may have left
// loose: an existing log file is chmod'ed to 0600, and, when manageDir is set
// (the default state directory this process owns), an existing parent directory
// to 0700. Each tightening emits a warning on stderr. An empty path disables
// recording (the proxy still forwards traffic).
//
// The parent directory of an explicit --log path is never chmod'ed: it may be a
// shared location such as /tmp that this process does not own.
func openSink(path string, manageDir bool, stderr io.Writer) (*gateway.FileSink, error) {
	if path == "" {
		return nil, nil
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, err
		}
		if manageDir {
			if err := tightenDir(dir, stderr); err != nil {
				return nil, err
			}
		}
	}
	if err := tightenFile(path, stderr); err != nil {
		return nil, err
	}
	return gateway.NewFileSink(path)
}

// tightenDir chmods an existing directory to 0700 when it grants group or other
// access, warning first. The directory must be one this process owns.
func tightenDir(dir string, stderr io.Writer) error {
	info, err := os.Stat(dir)
	if err != nil {
		return err
	}
	if info.Mode().Perm()&0o077 == 0 {
		return nil
	}
	fmt.Fprintf(stderr, "tprsh-gateway: WARNING: tightening directory %s to 0700 (was %04o)\n", dir, info.Mode().Perm())
	return os.Chmod(dir, 0o700)
}

// tightenFile chmods an existing log file to 0600 when it grants group or other
// access, warning first. A file this process creates is already 0600.
func tightenFile(path string, stderr io.Writer) error {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if info.Mode().Perm()&0o077 == 0 {
		return nil
	}
	fmt.Fprintf(stderr, "tprsh-gateway: WARNING: tightening log file %s to 0600 (was %04o)\n", path, info.Mode().Perm())
	return os.Chmod(path, 0o600)
}

// newLogger returns the logger for gateway-internal errors (sink failures,
// extractor error counts); it never receives payload or header values.
func newLogger(w io.Writer) *log.Logger { return log.New(w, "", log.LstdFlags) }

// printableAddr renders a net.Addr's host:port. A nil host (wildcard bind)
// is rendered as "0.0.0.0" so the printed URL is syntactically valid.
func printableAddr(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	if host == "" || host == "::" {
		host = "0.0.0.0"
	}
	return net.JoinHostPort(host, port)
}
