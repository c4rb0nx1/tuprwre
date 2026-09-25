// Package gateway holds the harness-agnostic LLM record layer: a passive,
// record-only reverse proxy and the sinks that persist extracted events.
package gateway

import (
	"encoding/json"
	"os"
	"sync"

	"github.com/c4rb0nx1/tuprwre/internal/event"
)

// Sink receives events emitted by the gateway. Implementations must be safe
// for concurrent use because response and request extractors may emit from
// different goroutines.
type Sink interface {
	Emit(event.Event) error
}

// MemorySink retains events in memory. It is intended for tests and for
// short-lived in-process reactors.
type MemorySink struct {
	mu     sync.Mutex
	events []event.Event
}

// NewMemorySink returns an empty in-memory sink.
func NewMemorySink() *MemorySink { return &MemorySink{} }

// Emit appends e to the retained slice.
func (s *MemorySink) Emit(e event.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, e)
	return nil
}

// Events returns a copy of the retained events.
func (s *MemorySink) Events() []event.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]event.Event, len(s.events))
	copy(out, s.events)
	return out
}

// FileSink writes one JSON object per line to a file (JSONL).
type FileSink struct {
	mu  sync.Mutex
	f   *os.File
	enc *json.Encoder
}

// NewFileSink opens path for appending, creating it if necessary.
func NewFileSink(path string) (*FileSink, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	enc := json.NewEncoder(f)
	// Keep <, > and & literal so recorded commands read as written.
	enc.SetEscapeHTML(false)
	return &FileSink{f: f, enc: enc}, nil
}

// Emit encodes e as a single JSON line.
func (s *FileSink) Emit(e event.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.enc.Encode(e)
}

// Close flushes and closes the underlying file.
func (s *FileSink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.f == nil {
		return nil
	}
	err := s.f.Close()
	s.f = nil
	return err
}
