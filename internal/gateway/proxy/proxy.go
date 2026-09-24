// Package proxy implements a record-only, pass-through reverse proxy that sits
// between a harness and its LLM API via a base-URL override. It forwards
// traffic byte-for-byte, never modifies it, and passively extracts tool-call
// intent from responses and tool results from requests.
//
// Extraction is best-effort and must never affect forwarding: extractor writes
// are isolated with panic recovery, events are handed to the sink through a
// buffered queue drained by a single goroutine, and request-body parsing runs
// after the body has been forwarded.
package proxy

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/c4rb0nx1/tuprwre/internal/event"
	"github.com/c4rb0nx1/tuprwre/internal/gateway"
	"github.com/c4rb0nx1/tuprwre/internal/gateway/wire"
)

// Config configures the gateway proxy.
type Config struct {
	// Upstream is the LLM API base URL (e.g. https://api.anthropic.com).
	Upstream *url.URL
	// Sink receives extracted events. A nil sink disables recording while
	// still forwarding traffic.
	Sink gateway.Sink
	// SessionID is attached to every emitted event.
	SessionID string
	// ErrorLog, when set, receives ReverseProxy internal errors, sink write
	// failures and extractor error counts (never header or payload values).
	ErrorLog *log.Logger
	// Redactor transforms each event immediately before emission. When nil,
	// DefaultRedactor is applied unless DisableRedaction is set.
	Redactor gateway.Redactor
	// DisableRedaction turns off the default redactor. It has no effect when
	// Redactor is set.
	DisableRedaction bool
}

// Proxy is the record-only reverse proxy. Requests are forwarded by the
// embedded *httputil.ReverseProxy; Stats and Close expose the recording
// pipeline.
type Proxy struct {
	*httputil.ReverseProxy
	emitter *emitter
}

// New builds the reverse proxy described by cfg. It returns an error only when
// the configuration is unusable. Callers should call Close once done so that
// buffered events reach the sink.
func New(cfg Config) (*Proxy, error) {
	if cfg.Upstream == nil {
		return nil, errors.New("gateway/proxy: upstream base URL is required")
	}
	redactor := cfg.Redactor
	if redactor == nil && !cfg.DisableRedaction {
		redactor = gateway.DefaultRedactor
	}
	em := newEmitter(cfg.Sink, redactor, cfg.ErrorLog)

	rp := httputil.NewSingleHostReverseProxy(cfg.Upstream)
	// The default director rewrites only URL.Scheme/Host/Path; without this,
	// req.Host still carries the client-facing host (e.g. 127.0.0.1:<port>)
	// and upstream virtual hosts reject the Host/SNI mismatch.
	director := rp.Director
	rp.Director = func(r *http.Request) {
		director(r)
		r.Host = r.URL.Host
	}
	// FlushInterval < 0 disables buffering so that SSE chunks reach the
	// client immediately, preserving the upstream's streaming behavior.
	rp.FlushInterval = -1
	if cfg.ErrorLog != nil {
		rp.ErrorLog = cfg.ErrorLog
	}
	rp.Transport = &recordingTransport{
		base:      http.DefaultTransport,
		em:        em,
		sessionID: cfg.SessionID,
	}
	rp.ModifyResponse = func(resp *http.Response) error {
		if em.sink == nil {
			return nil
		}
		path := ""
		if resp.Request != nil && resp.Request.URL != nil {
			path = resp.Request.URL.Path
		}
		// Go's transport transparently decompresses only the encodings it
		// negotiated itself. Anything still labelled as encoded must never
		// reach a parser.
		if ce := resp.Header.Get("Content-Encoding"); ce != "" && !strings.EqualFold(ce, "identity") {
			em.addExtractorErrors(1)
			return nil
		}
		ex := wire.NewResponseExtractor(path, resp.Header.Get("Content-Type"), responseEmitter(em, cfg.SessionID))
		if ex == nil {
			return nil
		}
		resp.Body = &recordBody{inner: resp.Body, ex: ex, em: em}
		return nil
	}
	return &Proxy{ReverseProxy: rp, emitter: em}, nil
}

// Stats returns a snapshot of the recording counters.
func (p *Proxy) Stats() Stats { return p.emitter.stats() }

// Close stops recording, waits for in-flight request parses and drains the
// event queue into the sink. It does not close the sink.
func (p *Proxy) Close() error {
	p.emitter.close()
	return nil
}

// recordingTransport passively records request bodies before forwarding them.
type recordingTransport struct {
	base      http.RoundTripper
	em        *emitter
	sessionID string
}

func (t *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Remove the client's Accept-Encoding so that Go's transport negotiates
	// gzip itself and transparently decompresses for both the client and the
	// extractor. Forwarding the client's value verbatim leaves resp.Body
	// encoded, which would feed compressed bytes to the parsers.
	req.Header.Del("Accept-Encoding")

	if t.em.sink != nil && req.URL != nil && req.Body != nil && req.Body != http.NoBody {
		if ex := wire.NewRequestExtractor(req.URL.Path, requestEmitter(t.em, t.sessionID)); ex != nil {
			req.Body = &teeReadCloser{reader: io.TeeReader(req.Body, ex), closer: req.Body, ex: ex, em: t.em}
		}
	}
	return t.base.RoundTrip(req)
}

// teeReadCloser forwards reads unchanged while feeding a tee. The buffered
// parse runs after the body has been forwarded, in its own goroutine, so that
// JSON decoding of up to the extractor's cap never delays the response.
type teeReadCloser struct {
	reader io.Reader
	closer io.Closer
	ex     wire.Extractor
	em     *emitter
	once   sync.Once
}

func (t *teeReadCloser) Read(p []byte) (int, error) { return t.reader.Read(p) }

func (t *teeReadCloser) Close() error {
	t.once.Do(func() {
		t.em.parseWG.Add(1)
		go func() {
			defer t.em.parseWG.Done()
			safeFinish(t.ex)
			t.em.addExtractorErrors(t.ex.Errors())
		}()
	})
	return t.closer.Close()
}

// recordBody tees a response body into an extractor as it is streamed to the
// client, finalizing at EOF or Close.
type recordBody struct {
	inner io.ReadCloser
	ex    wire.Extractor
	em    *emitter
	once  sync.Once
}

func (b *recordBody) Read(p []byte) (int, error) {
	n, err := b.inner.Read(p)
	if n > 0 {
		safeWrite(b.ex, p[:n])
	}
	if err != nil {
		b.finish()
	}
	return n, err
}

func (b *recordBody) Close() error {
	b.finish()
	return b.inner.Close()
}

func (b *recordBody) finish() {
	b.once.Do(func() {
		safeFinish(b.ex)
		b.em.addExtractorErrors(b.ex.Errors())
	})
}

// safeWrite and safeFinish guarantee that a misbehaving extractor can never
// propagate a panic into the forwarding path.
func safeWrite(ex wire.Extractor, p []byte) {
	defer func() { _ = recover() }()
	_, _ = ex.Write(p)
}

func safeFinish(ex wire.Extractor) {
	defer func() { _ = recover() }()
	ex.Finish()
}

// responseEmitter builds a tool-call emitter that records events into em.
func responseEmitter(em *emitter, sessionID string) wire.EmitToolCall {
	return func(call wire.ToolCall) {
		e := event.New(event.SourceGateway, event.KindToolCallIntent, time.Now())
		e.SessionID = sessionID
		e.Protocol = call.Protocol
		e.ToolCallID = call.ToolCallID
		e.ToolName = call.ToolName
		e.Arguments = json.RawMessage(append([]byte(nil), call.Arguments...))
		e.Complete = call.Complete
		e.Truncated = call.Truncated
		em.emit(e)
	}
}

// requestEmitter builds a tool-result emitter that records events into em,
// skipping results already recorded for this proxy instance. LLM clients
// resend the whole conversation every turn, so without dedup every historical
// result would be recorded again on each request.
func requestEmitter(em *emitter, sessionID string) wire.EmitToolResult {
	return func(result wire.ToolResult) {
		if result.ToolCallID != "" && !em.seen.add(result.Protocol+"\x00"+result.ToolCallID) {
			return
		}
		e := event.New(event.SourceGateway, event.KindToolResult, time.Now())
		e.SessionID = sessionID
		e.Protocol = result.Protocol
		e.ToolCallID = result.ToolCallID
		e.Result = json.RawMessage(append([]byte(nil), result.Content...))
		e.IsError = result.IsError
		em.emit(e)
	}
}
