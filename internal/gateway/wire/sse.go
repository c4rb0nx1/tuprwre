package wire

import (
	"strings"
)

// isEventStream reports whether a content type denotes an SSE stream.
func isEventStream(contentType string) bool {
	return strings.Contains(strings.ToLower(contentType), "text/event-stream")
}

// sseScanner incrementally parses a Server-Sent Events stream. It tolerates
// arbitrary chunk boundaries (including splits inside a CRLF pair), LF or CRLF
// line endings, multi-line data fields, comments and unknown fields. Parsing
// never panics; malformed dispatch data is the consumer's concern.
type sseScanner struct {
	line      []byte
	pendingCR bool
	event     string
	data      []string
	onEvent   func(event, data string)
}

func newSSEScanner(onEvent func(event, data string)) *sseScanner {
	return &sseScanner{onEvent: onEvent}
}

// Write consumes p, emitting a callback for each complete event. It always
// reports the full length written so that a tee never short-reads.
func (s *sseScanner) Write(p []byte) (int, error) {
	for i := 0; i < len(p); i++ {
		b := p[i]
		if s.pendingCR {
			s.pendingCR = false
			if b == '\n' {
				// The CR already terminated the line; swallow the LF.
				continue
			}
		}
		switch b {
		case '\n':
			s.endLine()
		case '\r':
			s.endLine()
			s.pendingCR = true
		default:
			s.line = append(s.line, b)
		}
	}
	return len(p), nil
}

// finish flushes a trailing line that lacked a terminator and dispatches any
// event still buffered.
func (s *sseScanner) finish() {
	s.pendingCR = false
	if len(s.line) > 0 {
		s.endLine()
	}
	s.dispatch()
}

// endLine processes one complete line.
func (s *sseScanner) endLine() {
	line := s.line
	s.line = s.line[:0]
	if len(line) == 0 {
		s.dispatch()
		return
	}
	if line[0] == ':' {
		return // comment
	}
	field := line
	var value []byte
	for i := 0; i < len(line); i++ {
		if line[i] == ':' {
			field = line[:i]
			value = line[i+1:]
			if len(value) > 0 && value[0] == ' ' {
				value = value[1:]
			}
			break
		}
	}
	switch string(field) {
	case "event":
		s.event = string(value)
	case "data":
		s.data = append(s.data, string(value))
	}
	// "id", "retry" and unknown fields are ignored.
}

// dispatch flushes the accumulated event. Per the SSE specification an event
// with no data lines is not dispatched.
func (s *sseScanner) dispatch() {
	if len(s.data) == 0 {
		s.event = ""
		return
	}
	data := strings.Join(s.data, "\n")
	event := s.event
	s.event = ""
	s.data = s.data[:0]
	if s.onEvent != nil {
		s.onEvent(event, data)
	}
}
