package event

import (
	"encoding/json"
	"testing"
	"time"
)

func TestNewSetsVersionSourceKind(t *testing.T) {
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.FixedZone("x", 3600))
	e := New(SourceGateway, KindToolCallIntent, at)

	if e.Version != SchemaVersion {
		t.Errorf("version = %q, want %q", e.Version, SchemaVersion)
	}
	if e.ID == "" {
		t.Error("ID is empty")
	}
	if e.Source != SourceGateway {
		t.Errorf("source = %q, want %q", e.Source, SourceGateway)
	}
	if e.Kind != KindToolCallIntent {
		t.Errorf("kind = %q, want %q", e.Kind, KindToolCallIntent)
	}
	if !e.Time.Equal(at) {
		t.Errorf("time = %v, want %v", e.Time, at)
	}
	if e.Time.Location() != time.UTC {
		t.Errorf("time location = %v, want UTC", e.Time.Location())
	}
}

func TestNewIDsAreUnique(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 1000; i++ {
		e := New(SourceSensor, KindExec, time.Now())
		if seen[e.ID] {
			t.Fatalf("duplicate ID %q", e.ID)
		}
		seen[e.ID] = true
	}
}

func TestEventJSONRoundTrip(t *testing.T) {
	e := Event{
		Version:    SchemaVersion,
		ID:         "abc",
		Time:       time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		SessionID:  "sess-1",
		Source:     SourceGateway,
		Kind:       KindToolCallIntent,
		Protocol:   "anthropic",
		ToolCallID: "toolu_1",
		ToolName:   "Bash",
		Arguments:  json.RawMessage(`{"command":"ls"}`),
		Complete:   true,
	}

	raw, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got Event
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Protocol != "anthropic" || got.ToolName != "Bash" || got.ToolCallID != "toolu_1" {
		t.Fatalf("round trip mismatch: %+v", got)
	}
	if string(got.Arguments) != `{"command":"ls"}` {
		t.Fatalf("arguments = %s", got.Arguments)
	}
}

func TestOmittedFieldsAbsentFromJSON(t *testing.T) {
	e := Event{Version: SchemaVersion, ID: "x", Source: SourceShell, Kind: KindProcExit}
	raw, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, field := range []string{"tool_name", "arguments", "protocol", "result", "truncated", "complete", "sensor", "process", "file", "net", "exit"} {
		if contains(string(raw), `"`+field+`":`) {
			t.Errorf("expected %q to be omitted, JSON = %s", field, raw)
		}
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// completeField reports whether raw JSON contains a "complete" key.
func completeField(raw []byte) bool { return contains(string(raw), `"complete"`) }

// TestCompleteFieldScopedToToolCallIntent proves "complete" is serialized only
// for tool-call intents, always present for them (including false).
func TestCompleteFieldScopedToToolCallIntent(t *testing.T) {
	incomplete := New(SourceGateway, KindToolCallIntent, time.Now())
	raw, err := json.Marshal(incomplete)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !contains(string(raw), `"complete":false`) {
		t.Errorf("incomplete intent missing complete:false: %s", raw)
	}

	complete := New(SourceGateway, KindToolCallIntent, time.Now())
	complete.Complete = true
	raw, err = json.Marshal(complete)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !contains(string(raw), `"complete":true`) {
		t.Errorf("complete intent missing complete:true: %s", raw)
	}

	for _, kind := range []Kind{KindToolResult, KindExec, KindFileWrite} {
		e := New(SourceGateway, kind, time.Now())
		raw, err := json.Marshal(e)
		if err != nil {
			t.Fatalf("marshal %s: %v", kind, err)
		}
		if completeField(raw) {
			t.Errorf("kind %s serialized a complete field: %s", kind, raw)
		}
	}
}
