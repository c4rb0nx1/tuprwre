package gateway

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/c4rb0nx1/tuprwre/internal/event"
)

func TestMemorySinkStoresCopy(t *testing.T) {
	s := NewMemorySink()
	e := event.New(event.SourceGateway, event.KindToolCallIntent, time.Now())
	if err := s.Emit(e); err != nil {
		t.Fatalf("emit: %v", err)
	}
	got := s.Events()
	if len(got) != 1 || got[0].ID != e.ID {
		t.Fatalf("unexpected events: %+v", got)
	}
	// Mutating the returned slice must not affect the sink.
	got[0].ID = "mutated"
	if s.Events()[0].ID == "mutated" {
		t.Error("Events() did not return a copy")
	}
}

func TestFileSinkWritesJSONL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	s, err := NewFileSink(path)
	if err != nil {
		t.Fatalf("new file sink: %v", err)
	}
	defer s.Close()

	for i := 0; i < 3; i++ {
		e := event.New(event.SourceGateway, event.KindToolCallIntent, time.Now())
		e.ToolName = "Bash"
		if err := s.Emit(e); err != nil {
			t.Fatalf("emit: %v", err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()

	lines := 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var e event.Event
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			t.Fatalf("line %d not JSON: %v", lines, err)
		}
		if e.Version != event.SchemaVersion || e.ToolName != "Bash" {
			t.Fatalf("unexpected event: %+v", e)
		}
		lines++
	}
	if lines != 3 {
		t.Fatalf("lines = %d, want 3", lines)
	}
}

// TestFileSinkKeepsHTMLCharactersLiteral proves recorded commands and
// payloads are written as-is, not as </>/& escapes, including
// after redaction re-encodes a payload.
func TestFileSinkKeepsHTMLCharactersLiteral(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	sink, err := NewFileSink(path)
	if err != nil {
		t.Fatal(err)
	}
	e := event.New(event.SourceGateway, event.KindToolCallIntent, time.Now())
	e.Arguments = json.RawMessage(`{"command":"make <all> && echo done","token":"fake-tok-12345678"}`)
	e = DefaultRedactor(e)
	if err := sink.Emit(e); err != nil {
		t.Fatal(err)
	}
	if err := sink.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `make <all> && echo done`) || strings.Contains(string(raw), "\\u00") {
		t.Errorf("log = %s", raw)
	}
	if !e.Redacted {
		t.Error("payload was not redacted")
	}
}
