// Command tprsh-sensor translates an OS sensor's native event stream into the
// tprsh effect-event schema and records it as JSONL. It observes only: it
// never blocks or alters what the sensor reports.
//
// Usage:
//
//	tetra getevents -o json | tprsh-sensor tetragon --session-id S
//	tprsh-sensor tetragon --input /var/run/cilium/tetragon/tetragon.log --log out.jsonl
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/c4rb0nx1/tuprwre/internal/gateway"
	"github.com/c4rb0nx1/tuprwre/internal/sensor"
	"github.com/c4rb0nx1/tuprwre/internal/sensor/tetragon"
)

// cancelGrace bounds how long a cancelled run waits for a read blocked on the
// input before exiting anyway.
const cancelGrace = 2 * time.Second

const usage = `usage: tprsh-sensor <adapter> [flags]

adapters:
  tetragon   Cilium Tetragon JSON export (tetra getevents -o json, or the export file)

flags:
`

// options holds the validated command-line configuration.
type options struct {
	adapter   string
	input     string
	logPath   string
	sessionID string
	noRedact  bool
	rootPID   int
}

// parseFlags parses and validates args. Warnings go to stderr.
func parseFlags(args []string, stderr io.Writer) (*options, error) {
	fs := flag.NewFlagSet("tprsh-sensor", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprint(stderr, usage)
		fs.PrintDefaults()
	}
	var (
		input     = fs.String("input", "-", `native event stream to read; "-" is stdin`)
		logPath   = fs.String("log", "", "JSONL effect log (created 0600); default: $XDG_STATE_HOME/tprsh/sensor/<session-id>.jsonl")
		sessionID = fs.String("session-id", "", "session id stamped on every event, to pair with tprsh-gateway --session-id (default: random)")
		noRedact  = fs.Bool("no-redact", false, "disable redaction of secret-looking argv values (records raw argv)")
		rootPID   = fs.Int("root-pid", 0, "record only this process and its descendants (e.g. the harness pid); 0 records everything")
	)
	if len(args) == 0 || args[0] == "" || args[0][0] == '-' {
		if len(args) > 0 && (args[0] == "-h" || args[0] == "--help" || args[0] == "-help") {
			fs.Usage()
			return nil, flag.ErrHelp
		}
		fs.Usage()
		return nil, errors.New("missing adapter name")
	}
	opts := &options{adapter: args[0]}
	if opts.adapter != tetragon.Name {
		return nil, fmt.Errorf("unknown adapter %q (supported: %s)", opts.adapter, tetragon.Name)
	}
	if err := fs.Parse(args[1:]); err != nil {
		return nil, err
	}
	if fs.NArg() > 0 {
		return nil, fmt.Errorf("unexpected arguments: %v", fs.Args())
	}
	opts.input, opts.logPath, opts.sessionID, opts.noRedact, opts.rootPID = *input, *logPath, *sessionID, *noRedact, *rootPID
	if opts.rootPID < 0 {
		return nil, fmt.Errorf("--root-pid %d: want a positive pid", opts.rootPID)
	}
	if opts.sessionID == "" {
		var b [16]byte
		if _, err := rand.Read(b[:]); err != nil {
			return nil, err
		}
		opts.sessionID = hex.EncodeToString(b[:])
	}
	if opts.logPath == "" {
		p, err := defaultLogPath(os.Getenv("XDG_STATE_HOME"), opts.sessionID)
		if err != nil {
			return nil, err
		}
		opts.logPath = p
	}
	if opts.noRedact {
		fmt.Fprintln(stderr, "tprsh-sensor: WARNING: redaction disabled (--no-redact); recorded argv may contain credentials")
	}
	return opts, nil
}

// defaultLogPath returns $XDG_STATE_HOME/tprsh/sensor/<session-id>.jsonl, or
// ~/.local/state/tprsh/sensor/<session-id>.jsonl when XDG_STATE_HOME is unset.
func defaultLogPath(stateHome, sessionID string) (string, error) {
	if stateHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve default log path: %w", err)
		}
		stateHome = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(stateHome, "tprsh", "sensor", sessionID+".jsonl"), nil
}

func main() {
	opts, err := parseFlags(os.Args[1:], os.Stderr)
	if errors.Is(err, flag.ErrHelp) {
		os.Exit(0)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "tprsh-sensor:", err)
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, opts, os.Stdin, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "tprsh-sensor:", err)
		os.Exit(1)
	}
}

// run records one adapter stream until the input ends or ctx is cancelled.
func run(ctx context.Context, opts *options, stdin io.Reader, stderr io.Writer) error {
	in := stdin
	if opts.input != "-" {
		f, err := os.Open(opts.input)
		if err != nil {
			return err
		}
		defer f.Close()
		in = f
	}
	if err := os.MkdirAll(filepath.Dir(opts.logPath), 0o700); err != nil {
		return err
	}
	sink, err := gateway.NewFileSink(opts.logPath)
	if err != nil {
		return err
	}
	defer sink.Close()
	fmt.Fprintf(stderr, "tprsh-sensor: adapter %s session %s log %s\n", opts.adapter, opts.sessionID, opts.logPath)

	var out gateway.Sink = sink
	var subtree *sensor.SubtreeFilter
	if opts.rootPID > 0 {
		subtree = sensor.NewSubtreeFilter(opts.rootPID, sink)
		out = subtree
		fmt.Fprintf(stderr, "tprsh-sensor: recording only pid %d and its descendants\n", opts.rootPID)
	}

	s := tetragon.New(in, tetragon.Options{SessionID: opts.sessionID})
	type outcome struct {
		st  sensor.Stats
		err error
	}
	done := make(chan outcome, 1)
	go func() {
		st, err := sensor.Record(ctx, s, out, sensor.Options{DisableRedaction: opts.noRedact})
		done <- outcome{st, err}
	}()

	var res outcome
	select {
	case res = <-done:
	case <-ctx.Done():
		select {
		case res = <-done:
		case <-time.After(cancelGrace):
			// Run is blocked in a read on the input; the sink holds every
			// event emitted so far.
			res.err = ctx.Err()
		}
	}
	outside := 0
	if subtree != nil {
		outside = subtree.Dropped()
	}
	// emitted counts events that passed validation, including any the
	// subtree filter then left out (outside_subtree).
	fmt.Fprintf(stderr, "tprsh-sensor: stats emitted=%d invalid=%d sink_errors=%d native_errors=%d ignored=%d outside_subtree=%d\n",
		res.st.EventsEmitted, res.st.EventsInvalid, res.st.SinkErrors, s.Errors(), s.Ignored(), outside)
	if errors.Is(res.err, context.Canceled) {
		return nil
	}
	return res.err
}
