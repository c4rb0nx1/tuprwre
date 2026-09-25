package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/c4rb0nx1/tuprwre/internal/event"
	"github.com/c4rb0nx1/tuprwre/internal/sensor"
)

func TestParseFlags(t *testing.T) {
	var stderr bytes.Buffer
	opts, err := parseFlags([]string{"tetragon", "--session-id", "s1", "--log", "/x/y.jsonl"}, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	if opts.adapter != "tetragon" || opts.sessionID != "s1" || opts.logPath != "/x/y.jsonl" || opts.input != "-" || opts.noRedact {
		t.Errorf("opts = %+v", opts)
	}

	t.Setenv("XDG_STATE_HOME", "/state")
	opts, err = parseFlags([]string{"tetragon"}, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	if opts.sessionID == "" || opts.logPath != filepath.Join("/state", "tprsh", "sensor", opts.sessionID+".jsonl") {
		t.Errorf("defaults = %+v", opts)
	}

	for _, bad := range [][]string{nil, {"--log", "x"}, {"falco"}, {"tetragon", "extra"}, {"tetragon", "--root-pid", "-3"}} {
		if _, err := parseFlags(bad, &bytes.Buffer{}); err == nil {
			t.Errorf("parseFlags(%q) accepted", bad)
		}
	}

	stderr.Reset()
	if _, err := parseFlags([]string{"tetragon", "--no-redact"}, &stderr); err != nil || !strings.Contains(stderr.String(), "WARNING") {
		t.Errorf("no-redact: err=%v stderr=%q", err, stderr.String())
	}
}

// TestRunRecordsFixture drives the binary's run loop over the Tetragon
// fixture and proves the written log replays as valid, redacted effects.
func TestRunRecordsFixture(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "sub", "effects.jsonl")
	opts := &options{
		adapter:   "tetragon",
		input:     filepath.Join("..", "..", "internal", "sensor", "tetragon", "testdata", "session.jsonl"),
		logPath:   logPath,
		sessionID: "sess-cli",
	}
	var stderr bytes.Buffer
	if err := run(context.Background(), opts, nil, &stderr); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(stderr.String(), "emitted=25 invalid=0 sink_errors=0 native_errors=0 ignored=3 outside_subtree=0") {
		t.Errorf("stderr = %q", stderr.String())
	}
	info, err := os.Stat(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("log mode = %04o", info.Mode().Perm())
	}
	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("fake-token-0123456789")) {
		t.Error("token survived redaction")
	}
	f, err := os.Open(logPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	rp := sensor.NewReplay(f)
	var n int
	st, err := sensor.Record(context.Background(), rp, countSink{&n}, sensor.Options{DisableRedaction: true})
	if err != nil || st.EventsEmitted != 25 || st.EventsInvalid != 0 {
		t.Errorf("replay stats = %+v err = %v", st, err)
	}
}

// TestRunCancelledWhileBlocked proves a cancelled run returns even when the
// input read never completes.
func TestRunCancelledWhileBlocked(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	// Cancel only after Run is blocked reading the silent pipe.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	time.AfterFunc(100*time.Millisecond, cancel)
	opts := &options{adapter: "tetragon", input: "-", logPath: filepath.Join(t.TempDir(), "e.jsonl"), sessionID: "s"}
	start := time.Now()
	if err := run(ctx, opts, r, &bytes.Buffer{}); err != nil {
		t.Errorf("run: %v", err)
	}
	if d := time.Since(start); d > cancelGrace+time.Second {
		t.Errorf("run took %v after cancel", d)
	}
}

type countSink struct{ n *int }

func (c countSink) Emit(event.Event) error { *c.n++; return nil }

// TestRunRootPID proves --root-pid keeps only the requested subtree: rooting
// at the terraform wrapper (bash, pid 4001) keeps bash, terraform and their
// effects.
func TestRunRootPID(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "effects.jsonl")
	opts := &options{
		adapter:   "tetragon",
		input:     filepath.Join("..", "..", "internal", "sensor", "tetragon", "testdata", "session.jsonl"),
		logPath:   logPath,
		sessionID: "s",
		rootPID:   4001,
	}
	var stderr bytes.Buffer
	if err := run(context.Background(), opts, nil, &stderr); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stderr.String(), "outside_subtree=20") {
		t.Errorf("stderr = %q", stderr.String())
	}
	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range bytes.Split(bytes.TrimSpace(raw), []byte("\n")) {
		if !bytes.Contains(line, []byte(`"pid":4001`)) && !bytes.Contains(line, []byte(`"pid":4002`)) {
			t.Errorf("event outside subtree recorded: %s", line)
		}
	}
	if n := bytes.Count(raw, []byte("\n")); n != 5 {
		t.Errorf("recorded %d events, want 5", n)
	}
}
