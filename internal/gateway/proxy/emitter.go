package proxy

import (
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

	closed        bool
	eventsEmitted int
	eventsDropped int
	sinkErrors    int
	extractorErrs int

	writerWG sync.WaitGroup
	parseWG  sync.WaitGroup
}

func newEmitter(sink gateway.Sink, redactor gateway.Redactor, errLog *log.Logger) *emitter {
	e := &emitter{
		sink:     sink,
		redactor: redactor,
		errLog:   errLog,
		seen:     newSeenSet(seenCapacity),
		ch:       make(chan event.Event, eventQueueSize),
	}
	if sink != nil {
		e.writerWG.Add(1)
		go e.run()
	}
	return e
}

// run drains the event queue into the sink. It is the only goroutine that
// touches the sink.
func (e *emitter) run() {
	defer e.writerWG.Done()
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

// close stops accepting events once all in-flight request parses have
// finished, then drains the queue into the sink.
func (e *emitter) close() {
	e.parseWG.Wait()
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return
	}
	e.closed = true
	close(e.ch)
	e.mu.Unlock()
	e.writerWG.Wait()
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
