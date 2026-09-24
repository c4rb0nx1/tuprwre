// Package event defines the versioned, JSON-encoded record schema emitted by
// the tprsh runtime.
//
// Events are the unit of the local-first record/react/rebound layer: any
// harness, sensor or shell can emit them, and downstream reactors consume a
// single stable shape. The schema is deliberately minimal; fields that do not
// apply to a given Kind are omitted.
package event

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"time"
)

// SchemaVersion is the current version of the Event wire schema. It is bumped
// only on a breaking change to field semantics.
const SchemaVersion = "0"

// Source identifies the component that produced an event.
type Source string

const (
	// SourceGateway identifies the local reverse proxy that sits between a
	// harness and its LLM API.
	SourceGateway Source = "gateway"
	// SourceSensor identifies a passive observation source (e.g. a file or
	// process sensor).
	SourceSensor Source = "sensor"
	// SourceShell identifies a shell/interception source.
	SourceShell Source = "shell"
)

// Kind identifies the type of observation an event records.
type Kind string

const (
	// KindToolCallIntent records that a harness asked its model to invoke a
	// tool. Populated by the gateway's response-side extractors.
	KindToolCallIntent Kind = "tool_call_intent"
	// KindToolResult records that a harness sent a tool result back to its
	// model. Populated by the gateway's request-side extractors.
	KindToolResult Kind = "tool_result"
	// KindExec records a process execution.
	KindExec Kind = "exec"
	// KindFileWrite records a file write.
	KindFileWrite Kind = "file_write"
	// KindFileReadSensitive records a read of a sensitive file.
	KindFileReadSensitive Kind = "file_read_sensitive"
	// KindNetConnect records an outbound network connection.
	KindNetConnect Kind = "net_connect"
	// KindProcExit records a process exit.
	KindProcExit Kind = "proc_exit"
)

// Event is a single record in the runtime event stream.
//
// The struct is flat and shared across all Kinds; a field is populated only
// when it is meaningful for the event's Kind. All fields are JSON-tagged.
type Event struct {
	// Version is the schema version; always SchemaVersion for events produced
	// by this package.
	Version string `json:"version"`
	// ID is an opaque, unique event identifier.
	ID string `json:"id"`
	// Time is the wall-clock time at which the event was produced (UTC).
	Time time.Time `json:"time"`
	// SessionID groups events belonging to one harness session. Optional.
	SessionID string `json:"session_id,omitempty"`
	// Source identifies the producer.
	Source Source `json:"source"`
	// Kind identifies the observation.
	Kind Kind `json:"kind"`

	// --- KindToolCallIntent fields ---

	// Protocol names the wire protocol the call was extracted from, e.g.
	// "anthropic", "openai-responses" or "openai-chat".
	Protocol string `json:"protocol,omitempty"`
	// ToolCallID is the provider-supplied tool call identifier.
	ToolCallID string `json:"tool_call_id,omitempty"`
	// ToolName is the requested tool name.
	ToolName string `json:"tool_name,omitempty"`
	// Arguments is the raw JSON arguments object for a tool call.
	Arguments json.RawMessage `json:"arguments,omitempty"`
	// Complete reports whether the arguments were fully received. For
	// streaming protocols it is false until the terminating event arrived.
	Complete bool `json:"complete,omitempty"`
	// Truncated reports that the arguments exceeded the extractor's per-call
	// cap and were clipped.
	Truncated bool `json:"truncated,omitempty"`

	// --- KindToolResult fields ---

	// Result is the raw JSON payload of a tool result (the content the
	// harness sent back for a prior tool call).
	Result json.RawMessage `json:"result,omitempty"`
	// IsError reports that the tool result was flagged as an error.
	IsError bool `json:"is_error,omitempty"`
}

// New builds an event with the current schema version, a fresh random ID and
// the supplied production time. Callers that do not care about the timestamp
// may pass time.Now().UTC().
func New(source Source, kind Kind, at time.Time) Event {
	return Event{
		Version: SchemaVersion,
		ID:      newID(),
		Time:    at.UTC(),
		Source:  source,
		Kind:    kind,
	}
}

// newID returns a 128-bit random hex identifier. It falls back to an empty
// string only if the system entropy source fails.
func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return ""
	}
	return hex.EncodeToString(b[:])
}
