// Package proxy implements a record-only, pass-through reverse proxy that sits
// between a harness and its LLM API via a base-URL override. It forwards
// traffic byte-for-byte, never modifies it, and passively extracts tool-call
// intent from responses and tool results from requests.
//
// Extraction is best-effort and must never affect forwarding: extractor writes
// are isolated with panic recovery and their errors are ignored.
package proxy

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
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
	// ErrorLog, when set, receives ReverseProxy internal errors (which never
	// contain header values).
	ErrorLog *log.Logger
}

// New builds the reverse proxy described by cfg. It returns an error only when
// the configuration is unusable.
func New(cfg Config) (*httputil.ReverseProxy, error) {
	if cfg.Upstream == nil {
		return nil, errors.New("gateway/proxy: upstream base URL is required")
	}
	rp := httputil.NewSingleHostReverseProxy(cfg.Upstream)
	// FlushInterval < 0 disables buffering so that SSE chunks reach the
	// client immediately, preserving the upstream's streaming behavior.
	rp.FlushInterval = -1
	if cfg.ErrorLog != nil {
		rp.ErrorLog = cfg.ErrorLog
	}
	rp.Transport = &recordingTransport{
		base:      http.DefaultTransport,
		sink:      cfg.Sink,
		sessionID: cfg.SessionID,
	}
	rp.ModifyResponse = func(resp *http.Response) error {
		if cfg.Sink == nil {
			return nil
		}
		path := ""
		if resp.Request != nil && resp.Request.URL != nil {
			path = resp.Request.URL.Path
		}
		ex := wire.NewResponseExtractor(path, resp.Header.Get("Content-Type"), responseEmitter(cfg.Sink, cfg.SessionID))
		if ex == nil {
			return nil
		}
		resp.Body = &recordBody{inner: resp.Body, ex: ex}
		return nil
	}
	return rp, nil
}

// recordingTransport passively records request bodies before forwarding them.
type recordingTransport struct {
	base      http.RoundTripper
	sink      gateway.Sink
	sessionID string
}

func (t *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.sink != nil && req.URL != nil && req.Body != nil && req.Body != http.NoBody {
		if ex := wire.NewRequestExtractor(req.URL.Path, requestEmitter(t.sink, t.sessionID)); ex != nil {
			req.Body = &teeReadCloser{reader: io.TeeReader(req.Body, ex), closer: req.Body, ex: ex}
		}
	}
	return t.base.RoundTrip(req)
}

// teeReadCloser forwards reads unchanged while feeding a tee, and finalizes the
// extractor when the body is closed by the transport.
type teeReadCloser struct {
	reader io.Reader
	closer io.Closer
	ex     wire.Extractor
	once   sync.Once
}

func (t *teeReadCloser) Read(p []byte) (int, error) { return t.reader.Read(p) }

func (t *teeReadCloser) Close() error {
	t.once.Do(func() { safeFinish(t.ex) })
	return t.closer.Close()
}

// recordBody tees a response body into an extractor as it is streamed to the
// client, finalizing at EOF or Close.
type recordBody struct {
	inner io.ReadCloser
	ex    wire.Extractor
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

func (b *recordBody) finish() { b.once.Do(func() { safeFinish(b.ex) }) }

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

// responseEmitter builds a tool-call emitter that records events into sink.
func responseEmitter(sink gateway.Sink, sessionID string) wire.EmitToolCall {
	return func(call wire.ToolCall) {
		e := event.New(event.SourceGateway, event.KindToolCallIntent, time.Now())
		e.SessionID = sessionID
		e.Protocol = call.Protocol
		e.ToolCallID = call.ToolCallID
		e.ToolName = call.ToolName
		e.Arguments = json.RawMessage(append([]byte(nil), call.Arguments...))
		e.Complete = call.Complete
		e.Truncated = call.Truncated
		_ = sink.Emit(e)
	}
}

// requestEmitter builds a tool-result emitter that records events into sink.
func requestEmitter(sink gateway.Sink, sessionID string) wire.EmitToolResult {
	return func(result wire.ToolResult) {
		e := event.New(event.SourceGateway, event.KindToolResult, time.Now())
		e.SessionID = sessionID
		e.Protocol = result.Protocol
		e.ToolCallID = result.ToolCallID
		e.Result = json.RawMessage(append([]byte(nil), result.Content...))
		e.IsError = result.IsError
		_ = sink.Emit(e)
	}
}
