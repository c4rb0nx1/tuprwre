package proxy

import (
	"context"
	"log"
	"sync"

	"github.com/c4rb0nx1/tuprwre/internal/event"
	"github.com/c4rb0nx1/tuprwre/internal/gateway"
)

// eventQueueSize bounds the number of emitted events buffered between the
// extractors and the single sink writer. When the queue is full new events are
// dropped and counted rather than blocking the forwarding path.
const eventQueueSize = 1024

// seenCapacity bounds the request-side dedup set. LLM clients resend the whole
// conversation on every turn, so entries beyond this many distinct tool calls
// are evicted oldest-first.
const seenCapacity = 10000

// Stats is a snapshot of the proxy's recording counters.
type Stats struct {
	// EventsEmitted counts events successfully handed to the sink.
	EventsEmitted int
	// EventsDropped counts events discarded because the queue was full or the
	// proxy was already closing.
	EventsDropped int
	// SinkErrors counts sink Emit failures.
	SinkErrors int
	// ExtractorErrors counts malformed inputs reported by extractors,
	// summed across every request and response (including request-body
	// overflow beyond the extractor's cap).
	ExtractorErrors int
}

// emitter owns the event pipeline shared by all requests of one proxy: it
// applies the redactor, deduplicates request-side results and hands events to
// the sink from a single writer goroutine so that extraction never blocks
// forwarding.
type emitter struct {
	sink     gateway.Sink
	redactor gateway.Redactor
	errLog   *log.Logger
	seen     *seenSet

	ch chan event.Event
	mu sync.Mutex
	// cond is broadcast whenever in-flight work completes or the drain
	// deadline expires, so close can wait without a WaitGroup Add/Wait race.
	cond *sync.Cond

	// draining is set once close begins; it rejects new tracked work so that
	// the in-flight counter can only decrease to zero.
	draining      bool
	closed        bool
	inflight      int
	eventsEmitted int
	eventsDropped int
	sinkErrors    int
	extractorErrs int

	// writerDone is closed when the sink writer goroutine exits; it is closed
	// at construction when there is no sink.
	writerDone chan struct{}
}

func newEmitter(sink gateway.Sink, redactor gateway.Redactor, errLog *log.Logger) *emitter {
	e := &emitter{
		sink:       sink,
		redactor:   redactor,
		errLog:     errLog,
		seen:       newSeenSet(seenCapacity),
		ch:         make(chan event.Event, eventQueueSize),
		writerDone: make(chan struct{}),
	}
	e.cond = sync.NewCond(&e.mu)
	if sink != nil {
		go e.run()
	} else {
		close(e.writerDone)
	}
	return e
}

// track registers one unit of in-flight recording work (a buffered request
// parse or a response-body extraction) and reports whether it was accepted.
// It returns false once close has begun: callers must not start new work whose
// events could only be dropped.
func (e *emitter) track() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.draining {
		return false
	}
	e.inflight++
	return true
}

// untrack releases one unit registered by track.
func (e *emitter) untrack() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.inflight--
	if e.draining && e.inflight == 0 {
		e.cond.Broadcast()
	}
}

// run drains the event queue into the sink. It is the only goroutine that
// touches the sink.
func (e *emitter) run() {
	defer close(e.writerDone)
	for ev := range e.ch {
		if err := e.sink.Emit(ev); err != nil {
			e.mu.Lock()
			e.sinkErrors++
			e.mu.Unlock()
			e.logf("gateway/proxy: sink emit failed: %v", err)
			continue
		}
		e.mu.Lock()
		e.eventsEmitted++
		e.mu.Unlock()
	}
}

// emit redacts ev and enqueues it. It never blocks: a full queue drops the
// event, and events offered after Close are counted as dropped.
func (e *emitter) emit(ev event.Event) {
	if e.sink == nil {
		return
	}
	if e.redactor != nil {
		ev = e.redactor(ev)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		e.eventsDropped++
		return
	}
	select {
	case e.ch <- ev:
	default:
		e.eventsDropped++
	}
}

// addExtractorErrors records malformed-input errors reported by an extractor.
// Only the count is logged; never payload or header values.
func (e *emitter) addExtractorErrors(n int) {
	if n <= 0 {
		return
	}
	e.mu.Lock()
	e.extractorErrs += n
	e.mu.Unlock()
	e.logf("gateway/proxy: extractor rejected %d malformed input(s)", n)
}

// stats returns a snapshot of the counters.
func (e *emitter) stats() Stats {
	e.mu.Lock()
	defer e.mu.Unlock()
	return Stats{
		EventsEmitted:   e.eventsEmitted,
		EventsDropped:   e.eventsDropped,
		SinkErrors:      e.sinkErrors,
		ExtractorErrors: e.extractorErrs,
	}
}

// close stops accepting events, waits for all in-flight recording work
// (request-body parses and response-body extractions) to finish so their
// events reach the queue, then drains the queue into the sink. Waiting is
// bounded by ctx: if the sink is stuck, close returns ctx's error having
// closed the queue but before the writer drained it. A close that begins
// while a response body is still streaming blocks until that body finishes
// (EOF or Close) or ctx expires; it never permanently hangs on its own.
func (e *emitter) close(ctx context.Context) error {
	e.mu.Lock()
	e.draining = true
	e.mu.Unlock()

	drainErr := e.waitInflight(ctx)

	// Stop accepting events even when the drain timed out, so late emitters
	// are counted as dropped rather than enqueued after Close returns.
	e.mu.Lock()
	if !e.closed {
		e.closed = true
		close(e.ch)
	}
	e.mu.Unlock()

	if drainErr != nil {
		return drainErr
	}
	select {
	case <-e.writerDone:
		return nil
	default:
	}
	select {
	case <-e.writerDone:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// waitInflight blocks until no tracked work remains, or ctx is done. It
// returns ctx's error only when it gave up waiting; a full drain always
// reports success.
func (e *emitter) waitInflight(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.inflight == 0 {
		return nil
	}
	// Broadcast on cancellation so a cond.Wait that would otherwise park past
	// the deadline wakes up and re-checks ctx.
	stop := context.AfterFunc(ctx, func() {
		e.mu.Lock()
		e.cond.Broadcast()
		e.mu.Unlock()
	})
	defer stop()
	for e.inflight > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		e.cond.Wait()
	}
	return nil
}

func (e *emitter) logf(format string, args ...any) {
	if e.errLog != nil {
		e.errLog.Printf(format, args...)
	}
}

// seenSet is a concurrency-safe, bounded set with FIFO eviction. add reports
// whether the key was newly inserted.
type seenSet struct {
	mu       sync.Mutex
	seen     map[string]struct{}
	ring     []string
	next     int
	capacity int
	full     bool
}

func newSeenSet(capacity int) *seenSet {
	return &seenSet{
		seen:     make(map[string]struct{}, capacity),
		ring:     make([]string, capacity),
		capacity: capacity,
	}
}

func (s *seenSet) add(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.seen[key]; ok {
		return false
	}
	if s.full {
		delete(s.seen, s.ring[s.next])
	}
	s.ring[s.next] = key
	s.seen[key] = struct{}{}
	s.next++
	if s.next == s.capacity {
		s.next = 0
		s.full = true
	}
	return true
}
