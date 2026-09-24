// Package sensor defines how OS-effect observations enter the tprsh event
// stream.
//
// The gateway records a harness's *intent* (tool calls and results on the LLM
// wire). A Sensor records *effects*: process executions, file writes, reads of
// sensitive files, outbound connections and process exits. Concrete adapters
// wrap an existing tool (Tetragon on Linux, eslogger on macOS, later our own
// eBPF) and translate its native records into event.Event values. The adapter
// is the only code that knows the native format; everything downstream sees
// the versioned event schema and the contract enforced by Validate.
//
// Sensors observe; they never block, confine or alter what they observe.
package sensor

import (
	"context"
	"sync/atomic"

	"github.com/c4rb0nx1/tuprwre/internal/event"
	"github.com/c4rb0nx1/tuprwre/internal/gateway"
)

// Sensor is a source of effect events.
type Sensor interface {
	// Name identifies the adapter, e.g. "tetragon". Record stamps it on
	// events whose Sensor field is empty.
	Name() string
	// Run reads the underlying source and hands each effect event to out
	// until the source is exhausted or ctx is cancelled. It returns nil on
	// a clean end of the source and ctx.Err() on cancellation. Malformed
	// native records must be counted and skipped, never returned as an
	// error; Run fails only when the source itself cannot be read.
	Run(ctx context.Context, out gateway.Sink) error
}

// Options configures Record.
type Options struct {
	// Redactor transforms each valid event before it reaches the sink.
	// When nil, gateway.DefaultRedactor is applied unless DisableRedaction
	// is set.
	Redactor gateway.Redactor
	// DisableRedaction turns off the default redactor. It has no effect
	// when Redactor is set.
	DisableRedaction bool
}

// Stats counts what Record did with a sensor's events.
type Stats struct {
	// EventsEmitted is the number of events accepted by the sink.
	EventsEmitted int64
	// EventsInvalid is the number of events dropped because they violated
	// the effect contract (see Validate).
	EventsInvalid int64
	// SinkErrors is the number of events the sink rejected.
	SinkErrors int64
}

// Record runs s, stamps the sensor name, validates, redacts and forwards each
// event to sink. Redaction is on by default so that nothing a sensor observes
// is persisted unredacted. Events that violate the contract are dropped and
// counted rather than persisted, so downstream consumers can rely on it.
// Record returns Run's error together with the counters.
func Record(ctx context.Context, s Sensor, sink gateway.Sink, opts Options) (Stats, error) {
	redactor := opts.Redactor
	if redactor == nil && !opts.DisableRedaction {
		redactor = gateway.DefaultRedactor
	}
	r := &recorder{name: s.Name(), sink: sink, redactor: redactor}
	err := s.Run(ctx, r)
	return Stats{
		EventsEmitted: r.emitted.Load(),
		EventsInvalid: r.invalid.Load(),
		SinkErrors:    r.sinkErrors.Load(),
	}, err
}

// recorder is the gateway.Sink handed to a Sensor by Record. It is safe for
// concurrent use as long as the wrapped sink is.
type recorder struct {
	name     string
	sink     gateway.Sink
	redactor gateway.Redactor

	emitted, invalid, sinkErrors atomic.Int64
}

// Emit implements gateway.Sink. It reports contract violations and sink
// failures to the sensor as errors, but a sensor may ignore them: Record
// counts both regardless.
func (r *recorder) Emit(e event.Event) error {
	if e.Sensor == "" {
		e.Sensor = r.name
	}
	if err := Validate(e); err != nil {
		r.invalid.Add(1)
		return err
	}
	if r.redactor != nil {
		e = r.redactor(e)
	}
	if err := r.sink.Emit(e); err != nil {
		r.sinkErrors.Add(1)
		return err
	}
	r.emitted.Add(1)
	return nil
}
