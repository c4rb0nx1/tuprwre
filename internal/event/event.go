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
	// It applies only to KindToolCallIntent: MarshalJSON emits the field for
	// intents only, always present (including false) so an incomplete call is
	// visible, and omits it for every other Kind.
	Complete bool `json:"complete"`
	// Truncated reports that the arguments exceeded the extractor's per-call
	// cap and were clipped.
	Truncated bool `json:"truncated,omitempty"`

	// --- KindToolResult fields ---

	// Result is the raw JSON payload of a tool result (the content the
	// harness sent back for a prior tool call).
	Result json.RawMessage `json:"result,omitempty"`
	// IsError reports that the tool result was flagged as an error.
	IsError bool `json:"is_error,omitempty"`

	// --- Effect fields (Source == SourceSensor) ---

	// Sensor names the adapter that observed an effect, e.g. "tetragon" or
	// "eslogger". It identifies the producer only; no consumer may depend on
	// the adapter's native record format.
	Sensor string `json:"sensor,omitempty"`
	// Process is the OS process that caused an effect. It is set on every
	// effect Kind (exec, file_write, file_read_sensitive, net_connect,
	// proc_exit).
	Process *Process `json:"process,omitempty"`
	// File is the file an effect touched. Set for file_write and
	// file_read_sensitive only.
	File *File `json:"file,omitempty"`
	// Net is the connection an effect opened. Set for net_connect only.
	Net *Net `json:"net,omitempty"`
	// Exit is the termination status of a process. Set for proc_exit only.
	Exit *Exit `json:"exit,omitempty"`

	// --- Redaction fields ---

	// Redacted reports that the redactor replaced at least one secret-looking
	// substring in Arguments or Result.
	Redacted bool `json:"redacted,omitempty"`
	// RedactionCount is the number of individual replacements the redactor
	// made in Arguments and Result.
	RedactionCount int `json:"redaction_count,omitempty"`
}

// Process describes the OS process behind an effect event. For effect events
// the enclosing Event.Time is the time the sensor observed the effect, not the
// time the record was written.
type Process struct {
	// ExecID is a sensor-assigned identity for one execution. Unlike PID it
	// is not reused, so it is the key for building a process tree.
	ExecID string `json:"exec_id,omitempty"`
	// ParentExecID is the ExecID of the parent execution, when known.
	ParentExecID string `json:"parent_exec_id,omitempty"`
	// PID is the kernel process id.
	PID int `json:"pid"`
	// PPID is the parent's kernel process id, when known.
	PPID int `json:"ppid,omitempty"`
	// UID is the real user id. It is a pointer so that root (0) stays
	// distinguishable from unknown.
	UID *uint32 `json:"uid,omitempty"`
	// Binary is the absolute path of the executed image.
	Binary string `json:"binary,omitempty"`
	// Argv is the argument vector as passed to execve, including argv[0].
	Argv []string `json:"argv,omitempty"`
	// Cwd is the working directory at exec time, when known.
	Cwd string `json:"cwd,omitempty"`
}

// File describes the file touched by a file effect.
type File struct {
	// Path is the absolute path as resolved by the sensor.
	Path string `json:"path"`
}

// Net describes an outbound connection.
type Net struct {
	// Protocol is the transport, "tcp" or "udp".
	Protocol string `json:"protocol"`
	// SrcAddr and SrcPort are the local endpoint, when known.
	SrcAddr string `json:"src_addr,omitempty"`
	SrcPort int    `json:"src_port,omitempty"`
	// DstAddr is the remote IP address in textual form.
	DstAddr string `json:"dst_addr"`
	// DstPort is the remote port.
	DstPort int `json:"dst_port"`
}

// Exit describes how a process terminated. At least one of Code and Signal is
// set.
type Exit struct {
	// Code is the exit status of a normal exit. It is a pointer so that a
	// successful exit (0) stays distinguishable from a signal death.
	Code *int `json:"code,omitempty"`
	// Signal names the terminating signal, e.g. "SIGKILL".
	Signal string `json:"signal,omitempty"`
}

// MarshalJSON serializes the event. The "complete" field is meaningful only
// for tool_call_intent events, so it is emitted only then; for every other
// Kind it is omitted, matching the package rule that fields not applicable to
// a Kind are absent.
func (e Event) MarshalJSON() ([]byte, error) {
	// eventJSON is the plain field set; it still carries the "complete" field,
	// which the non-intent wrapper shadows with a never-set, omitted pointer.
	type eventJSON Event
	if e.Kind == KindToolCallIntent {
		return json.Marshal(eventJSON(e))
	}
	return json.Marshal(struct {
		eventJSON
		Complete *struct{} `json:"complete,omitempty"`
	}{eventJSON: eventJSON(e)})
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
