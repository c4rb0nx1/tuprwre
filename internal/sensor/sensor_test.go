package sensor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/c4rb0nx1/tuprwre/internal/event"
	"github.com/c4rb0nx1/tuprwre/internal/gateway"
)

var fixedTime = time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)

// Replay must satisfy the interface it is the reference for.
var _ Sensor = (*Replay)(nil)

func openFixture(t *testing.T, name string) *os.File {
	t.Helper()
	f, err := os.Open(filepath.Join(contractDir, name))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}

// TestRecordReplaysSessionRedacted drives the scenario fixture through the
// full Record pipeline and proves every event survives and argv is redacted
// by default.
func TestRecordReplaysSessionRedacted(t *testing.T) {
	sink := gateway.NewMemorySink()
	rp := NewReplay(openFixture(t, "session.jsonl"))

	st, err := Record(context.Background(), rp, sink, Options{})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	if st.EventsEmitted != 10 || st.EventsInvalid != 0 || st.SinkErrors != 0 || rp.Errors() != 0 {
		t.Fatalf("stats = %+v, replay errors = %d", st, rp.Errors())
	}

	events := sink.Events()
	var curl event.Event
	for _, e := range events {
		if err := Validate(e); err != nil {
			t.Errorf("%s: recorded event violates contract: %v", e.ID, err)
		}
		if e.Kind == event.KindExec && e.Process.Binary == "/usr/bin/curl" {
			curl = e
		}
	}
	if curl.ID == "" {
		t.Fatal("curl exec not recorded")
	}
	joined := strings.Join(curl.Process.Argv, " ")
	if strings.Contains(joined, "fake-token-0123456789") {
		t.Errorf("bearer token survived redaction: %q", joined)
	}
	if !curl.Redacted || curl.RedactionCount != 1 {
		t.Errorf("redacted=%v count=%d, want true 1", curl.Redacted, curl.RedactionCount)
	}
}

// TestRecordDisableRedaction proves the explicit opt-out records argv
// verbatim.
func TestRecordDisableRedaction(t *testing.T) {
	sink := gateway.NewMemorySink()
	if _, err := Record(context.Background(), NewReplay(openFixture(t, "session.jsonl")), sink,
		Options{DisableRedaction: true}); err != nil {
		t.Fatal(err)
	}
	for _, e := range sink.Events() {
		if e.Redacted {
			t.Errorf("%s marked redacted with redaction disabled", e.ID)
		}
	}
}

// TestRecordDropsInvalidAndStampsName proves contract violations never reach
// the sink, and an empty Sensor field is filled from Name.
func TestRecordDropsInvalidAndStampsName(t *testing.T) {
	good := `{"version":"0","id":"a","time":"2026-09-20T10:00:00Z","source":"sensor","kind":"exec","process":{"pid":5,"binary":"/bin/ls"}}`
	bad := `{"version":"0","id":"b","time":"2026-09-20T10:00:00Z","source":"sensor","kind":"exec","process":{"pid":5,"binary":"ls"}}`
	input := strings.Join([]string{good, "", "not json", bad, "  "}, "\n")

	sink := gateway.NewMemorySink()
	rp := NewReplay(strings.NewReader(input))
	st, err := Record(context.Background(), rp, sink, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if st.EventsEmitted != 1 || st.EventsInvalid != 1 || rp.Errors() != 1 {
		t.Fatalf("stats = %+v, replay errors = %d", st, rp.Errors())
	}
	if got := sink.Events()[0]; got.ID != "a" || got.Sensor != "replay" {
		t.Errorf("recorded %+v, want id a stamped sensor replay", got)
	}
}

type failSink struct{}

func (failSink) Emit(event.Event) error { return errors.New("disk full") }

// TestRecordCountsSinkErrors proves sink failures are counted, not fatal.
func TestRecordCountsSinkErrors(t *testing.T) {
	st, err := Record(context.Background(), NewReplay(openFixture(t, "session.jsonl")), failSink{}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if st.SinkErrors != 10 || st.EventsEmitted != 0 {
		t.Errorf("stats = %+v", st)
	}
}

// TestReplayStopsOnCancel proves a cancelled context ends Run with ctx.Err().
func TestReplayStopsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	sink := gateway.NewMemorySink()
	err := NewReplay(openFixture(t, "session.jsonl")).Run(ctx, sink)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
	if n := len(sink.Events()); n != 0 {
		t.Errorf("emitted %d events after cancel", n)
	}
}

// TestReplaySkipsOversizedLine proves a line past MaxReplayLineBytes is
// counted and skipped without losing the records around it.
func TestReplaySkipsOversizedLine(t *testing.T) {
	ev := func(id string) string {
		return `{"version":"0","id":"` + id + `","time":"2026-09-20T10:00:00Z","source":"sensor","kind":"proc_exit","sensor":"t","process":{"pid":1},"exit":{"signal":"SIGTERM"}}`
	}
	huge := `{"pad":"` + strings.Repeat("x", MaxReplayLineBytes) + `"}`
	input := ev("before") + "\r\n" + huge + "\n" + ev("after") // no trailing newline

	sink := gateway.NewMemorySink()
	rp := NewReplay(strings.NewReader(input))
	if err := rp.Run(context.Background(), sink); err != nil {
		t.Fatal(err)
	}
	got := sink.Events()
	if len(got) != 2 || got[0].ID != "before" || got[1].ID != "after" || rp.Errors() != 1 {
		t.Fatalf("got %d events (%+v), errors %d", len(got), got, rp.Errors())
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("broken pipe") }

// TestReplayReturnsSourceError proves an unreadable source is Run's error.
func TestReplayReturnsSourceError(t *testing.T) {
	err := NewReplay(errReader{}).Run(context.Background(), gateway.NewMemorySink())
	if err == nil || !strings.Contains(err.Error(), "broken pipe") {
		t.Errorf("err = %v", err)
	}
}
