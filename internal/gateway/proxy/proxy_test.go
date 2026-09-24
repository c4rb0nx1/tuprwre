package proxy

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/c4rb0nx1/tuprwre/internal/event"
	"github.com/c4rb0nx1/tuprwre/internal/gateway"
)

func newTestProxy(t *testing.T, upstream string, sink gateway.Sink) *httptest.Server {
	t.Helper()
	u, err := url.Parse(upstream)
	if err != nil {
		t.Fatalf("parse upstream: %v", err)
	}
	rp, err := New(Config{Upstream: u, Sink: sink, SessionID: "sess-test"})
	if err != nil {
		t.Fatalf("new proxy: %v", err)
	}
	srv := httptest.NewServer(rp)
	t.Cleanup(srv.Close)
	return srv
}

// TestProxyStreamsIncrementallyAndRecords drives a byte-identical SSE
// pass-through, proving that the client receives the first event before the
// upstream has finished and that the expected call is recorded.
func TestProxyStreamsIncrementallyAndRecords(t *testing.T) {
	firstChunk := ": synthetic upstream comment\n" +
		"event: content_block_start\n" +
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_proxy","name":"Bash"}}` + "\n\n"
	secondChunk := "event: content_block_delta\n" +
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"command\":\"echo hi\"}"}}` + "\n\n" +
		"event: content_block_stop\n" +
		`data: {"type":"content_block_stop","index":0}` + "\n\n"

	release := make(chan struct{})
	var mu sync.Mutex
	var upstreamBody bytes.Buffer

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl, _ := w.(http.Flusher)
		mu.Lock()
		upstreamBody.WriteString(firstChunk)
		mu.Unlock()
		_, _ = io.WriteString(w, firstChunk)
		fl.Flush()
		<-release
		mu.Lock()
		upstreamBody.WriteString(secondChunk)
		mu.Unlock()
		_, _ = io.WriteString(w, secondChunk)
		fl.Flush()
	}))
	defer upstream.Close()

	sink := gateway.NewMemorySink()
	front := newTestProxy(t, upstream.URL, sink)

	resp, err := http.Post(front.URL+"/v1/messages", "application/json", bytes.NewReader([]byte(`{"model":"x"}`)))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()

	// Read exactly the first chunk; if the proxy buffered, this blocks.
	firstBytes := make([]byte, len(firstChunk))
	type readResult struct {
		n   int
		err error
	}
	done := make(chan readResult, 1)
	go func() {
		n, err := io.ReadFull(resp.Body, firstBytes)
		done <- readResult{n, err}
	}()
	select {
	case r := <-done:
		if r.err != nil {
			t.Fatalf("read first chunk: %v (%d bytes)", r.err, r.n)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("first event did not arrive before upstream finished (proxy buffered the stream)")
	}
	close(release)

	rest, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read rest: %v", err)
	}
	clientBody := append(append([]byte(nil), firstBytes...), rest...)

	mu.Lock()
	wantBody := append([]byte(nil), upstreamBody.Bytes()...)
	mu.Unlock()

	if !bytes.Equal(clientBody, wantBody) {
		t.Fatalf("client bytes differ from upstream\n got: %q\nwant: %q", clientBody, wantBody)
	}
	if !bytes.Equal(wantBody, []byte(firstChunk+secondChunk)) {
		t.Fatalf("upstream body mismatch")
	}

	events := sink.Events()
	if len(events) != 1 {
		t.Fatalf("recorded %d events, want 1: %+v", len(events), events)
	}
	e := events[0]
	if e.Kind != event.KindToolCallIntent || e.Source != event.SourceGateway {
		t.Errorf("unexpected event kind/source: %+v", e)
	}
	if e.SessionID != "sess-test" {
		t.Errorf("session id = %q", e.SessionID)
	}
	if e.ToolCallID != "toolu_proxy" || e.ToolName != "Bash" {
		t.Errorf("unexpected call: %+v", e)
	}
	if string(e.Arguments) != `{"command":"echo hi"}` {
		t.Errorf("arguments = %s", e.Arguments)
	}
	if !e.Complete {
		t.Error("expected complete call")
	}
}

// TestProxyRecordsRequestToolResult verifies request-side extraction without
// altering the forwarded request body.
func TestProxyRecordsRequestToolResult(t *testing.T) {
	var received bytes.Buffer
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		received.Write(body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"content":[]}`)
	}))
	defer upstream.Close()

	sink := gateway.NewMemorySink()
	front := newTestProxy(t, upstream.URL, sink)

	reqBody := `{"model":"x","messages":[{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_x","content":"ok"}]}]}`
	resp, err := http.Post(front.URL+"/v1/messages", "application/json", bytes.NewReader([]byte(reqBody)))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	resp.Body.Close()

	if received.String() != reqBody {
		t.Fatalf("forwarded body altered\n got: %s\nwant: %s", received.String(), reqBody)
	}

	events := sink.Events()
	if len(events) != 1 {
		t.Fatalf("recorded %d events, want 1: %+v", len(events), events)
	}
	if events[0].Kind != event.KindToolResult || events[0].ToolCallID != "toolu_x" {
		t.Errorf("unexpected event: %+v", events[0])
	}
}

// TestProxyNonStreamingJSON records tools from a non-SSE JSON response.
func TestProxyNonStreamingJSON(t *testing.T) {
	body := `{"content":[{"type":"tool_use","id":"toolu_ns","name":"Bash","input":{"command":"pwd"}}]}`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	}))
	defer upstream.Close()

	sink := gateway.NewMemorySink()
	front := newTestProxy(t, upstream.URL, sink)

	resp, err := http.Post(front.URL+"/v1/messages", "application/json", bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	got, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(got) != body {
		t.Fatalf("body altered: %s", got)
	}

	events := sink.Events()
	if len(events) != 1 || events[0].ToolCallID != "toolu_ns" || string(events[0].Arguments) != `{"command":"pwd"}` {
		t.Fatalf("unexpected events: %+v", events)
	}
}

// TestProxyNilSinkForwards confirms that forwarding works without a sink.
func TestProxyNilSinkForwards(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer upstream.Close()

	front := newTestProxy(t, upstream.URL, nil)
	resp, err := http.Get(front.URL + "/v1/messages")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	got, _ := io.ReadAll(resp.Body)
	if string(got) != `{"ok":true}` {
		t.Fatalf("body = %s", got)
	}
}

func TestNewRequiresUpstream(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Fatal("expected error for missing upstream")
	}
}
