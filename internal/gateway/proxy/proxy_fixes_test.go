package proxy

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/c4rb0nx1/tuprwre/internal/event"
	"github.com/c4rb0nx1/tuprwre/internal/gateway"
)

// waitForEvents blocks until the proxy reports at least n emitted events.
func waitForEvents(t *testing.T, p *Proxy, n int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if p.Stats().EventsEmitted >= n {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d emitted events (stats=%+v)", n, p.Stats())
}

// TestProxyNegotiatesGzipAndStillExtracts proves the proxy drops the client's
// Accept-Encoding so Go's transport negotiates gzip itself and decompresses the
// body for both the client and the extractor.
func TestProxyNegotiatesGzipAndStillExtracts(t *testing.T) {
	const body = `{"content":[{"type":"tool_use","id":"toolu_gz","name":"Bash","input":{"command":"pwd"}}]}`
	var sawGzip atomic.Bool

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			sawGzip.Store(true)
			w.Header().Set("Content-Encoding", "gzip")
			gz := gzip.NewWriter(w)
			_, _ = gz.Write([]byte(body))
			_ = gz.Close()
			return
		}
		_, _ = io.WriteString(w, body)
	}))
	defer upstream.Close()

	sink := gateway.NewMemorySink()
	front, proxy := newTestProxy(t, upstream.URL, sink)

	req, err := http.NewRequest(http.MethodPost, front.URL+"/v1/messages", bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	// A real harness supplies its own Accept-Encoding; the proxy must not
	// forward it verbatim or the body stays compressed.
	req.Header.Set("Accept-Encoding", "gzip")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	got, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if string(got) != body {
		t.Fatalf("client body not decompressed: %q", got)
	}
	if !sawGzip.Load() {
		t.Error("upstream never saw a gzip negotiation")
	}

	if err := proxy.Close(context.Background()); err != nil {
		t.Fatalf("close: %v", err)
	}
	events := sink.Events()
	if len(events) != 1 {
		t.Fatalf("recorded %d events, want 1: %+v", len(events), events)
	}
	if events[0].ToolCallID != "toolu_gz" || string(events[0].Arguments) != `{"command":"pwd"}` {
		t.Errorf("unexpected event: %+v", events[0])
	}
}

// TestProxySkipsExtractorForEncodedResponse proves that a body still labelled
// with an encoding Go's transport did not decode is never fed to a parser. The
// body is a valid Anthropic tool-use payload, so an unguarded extractor would
// emit one event; asserting zero events therefore discriminates the guard.
func TestProxySkipsExtractorForEncodedResponse(t *testing.T) {
	const body = `{"content":[{"type":"tool_use","id":"toolu_br","name":"Bash","input":{"command":"pwd"}}]}`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Brotli is never transparently decoded by net/http.
		w.Header().Set("Content-Encoding", "br")
		_, _ = io.WriteString(w, body)
	}))
	defer upstream.Close()

	sink := gateway.NewMemorySink()
	front, proxy := newTestProxy(t, upstream.URL, sink)

	resp, err := http.Post(front.URL+"/v1/messages", "application/json", bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	got, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if string(got) != body {
		t.Fatalf("client body altered: %q", got)
	}
	if err := proxy.Close(context.Background()); err != nil {
		t.Fatalf("close: %v", err)
	}
	if events := sink.Events(); len(events) != 0 {
		t.Fatalf("encoded body was parsed: %+v", events)
	}
	if stats := proxy.Stats(); stats.ExtractorErrors == 0 {
		t.Error("expected encoded response to be counted as an extractor error")
	}
}

// TestProxyRewritesHostHeaderToUpstream proves the outbound Host matches
// the upstream authority rather than the client-facing listener.
func TestProxyRewritesHostHeaderToUpstream(t *testing.T) {
	hostCh := make(chan string, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hostCh <- r.Host
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer upstream.Close()

	front, _ := newTestProxy(t, upstream.URL, nil)
	resp, err := http.Get(front.URL + "/v1/messages")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	resp.Body.Close()

	want := mustParseURL(t, upstream.URL).Host
	select {
	case got := <-hostCh:
		if got != want {
			t.Errorf("upstream Host = %q, want %q", got, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("upstream did not see the request")
	}
}

// TestProxyDeduplicatesToolResultsAcrossRequests proves that results resent in
// a growing conversation are recorded only once per proxy instance.
func TestProxyDeduplicatesToolResultsAcrossRequests(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"content":[]}`)
	}))
	defer upstream.Close()

	sink := gateway.NewMemorySink()
	front, proxy := newTestProxy(t, upstream.URL, sink)

	post := func(body string) {
		t.Helper()
		resp, err := http.Post(front.URL+"/v1/messages", "application/json", bytes.NewReader([]byte(body)))
		if err != nil {
			t.Fatalf("post: %v", err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}

	post(`{"messages":[{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_a","content":"one"}]}]}`)
	waitForEvents(t, proxy, 1)
	post(`{"messages":[` +
		`{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_a","content":"one"}]},` +
		`{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_b","content":"two"}]}]}`)
	waitForEvents(t, proxy, 2)

	if err := proxy.Close(context.Background()); err != nil {
		t.Fatalf("close: %v", err)
	}
	counts := map[string]int{}
	for _, e := range sink.Events() {
		if e.Kind != event.KindToolResult {
			t.Fatalf("unexpected kind: %+v", e)
		}
		counts[e.ToolCallID]++
	}
	if counts["toolu_a"] != 1 || counts["toolu_b"] != 1 || len(counts) != 2 {
		t.Fatalf("recorded %d tool results, want one each: %v", len(sink.Events()), counts)
	}
}

// blockingSink blocks every Emit until release is closed, letting the test
// prove that the forwarding path is not coupled to sink latency.
type blockingSink struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func newBlockingSink() *blockingSink {
	return &blockingSink{started: make(chan struct{}), release: make(chan struct{})}
}

func (s *blockingSink) Emit(event.Event) error {
	s.once.Do(func() { close(s.started) })
	<-s.release
	return nil
}

// TestProxyBlockingSinkDoesNotStallClient proves that a stalled sink cannot
// delay delivery of the remaining SSE stream.
func TestProxyBlockingSinkDoesNotStallClient(t *testing.T) {
	chunk1 := "event: content_block_start\n" +
		`data: {"index":0,"content_block":{"type":"tool_use","id":"toolu_block","name":"Bash"}}` + "\n\n" +
		"event: content_block_stop\n" +
		`data: {"index":0}` + "\n\n"
	chunk2 := "event: message_stop\n" +
		`data: {"type":"message_stop"}` + "\n\n"

	allowRest := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		_, _ = io.WriteString(w, chunk1)
		fl.Flush()
		<-allowRest
		_, _ = io.WriteString(w, chunk2)
		fl.Flush()
	}))
	defer upstream.Close()

	sink := newBlockingSink()
	front, proxy := newTestProxy(t, upstream.URL, sink)

	resp, err := http.Post(front.URL+"/v1/messages", "application/json", bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()

	// Wait until the writer goroutine is parked inside the sink.
	select {
	case <-sink.started:
	case <-time.After(3 * time.Second):
		t.Fatal("sink never started emitting")
	}

	// Release the upstream's remaining bytes while the sink is still blocked.
	close(allowRest)

	type result struct {
		body []byte
		err  error
	}
	done := make(chan result, 1)
	go func() {
		b, err := io.ReadAll(resp.Body)
		done <- result{body: b, err: err}
	}()

	select {
	case r := <-done:
		if r.err != nil {
			t.Fatalf("read: %v", r.err)
		}
		if string(r.body) != chunk1+chunk2 {
			t.Fatalf("client body mismatch: %q", r.body)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("client stream stalled behind the blocking sink")
	}

	close(sink.release)
	if err := proxy.Close(context.Background()); err != nil {
		t.Fatalf("close: %v", err)
	}
}

// TestProxyRedactsToolResultByDefault proves the default redactor is applied
// before emission and keeps the payload valid JSON.
func TestProxyRedactsToolResultByDefault(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"content":[]}`)
	}))
	defer upstream.Close()

	sink := gateway.NewMemorySink()
	front, proxy := newTestProxy(t, upstream.URL, sink)

	body := `{"messages":[{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_secret","content":{"password":"hunter2"}}]}]}`
	resp, err := http.Post(front.URL+"/v1/messages", "application/json", bytes.NewReader([]byte(body)))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	resp.Body.Close()
	if err := proxy.Close(context.Background()); err != nil {
		t.Fatalf("close: %v", err)
	}

	events := sink.Events()
	if len(events) != 1 {
		t.Fatalf("recorded %d events, want 1", len(events))
	}
	e := events[0]
	if !e.Redacted || e.RedactionCount == 0 {
		t.Fatalf("event not redacted: %+v", e)
	}
	if strings.Contains(string(e.Result), "hunter2") {
		t.Errorf("secret survived redaction: %s", e.Result)
	}
	var parsed map[string]string
	if err := json.Unmarshal(e.Result, &parsed); err != nil {
		t.Fatalf("result is not valid JSON: %v (%s)", err, e.Result)
	}
	if !strings.Contains(parsed["password"], "[REDACTED:") {
		t.Errorf("password = %q", parsed["password"])
	}
}

// TestProxyDisableRedactionRecordsVerbatim proves the explicit opt-out.
func TestProxyDisableRedactionRecordsVerbatim(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"content":[]}`)
	}))
	defer upstream.Close()

	sink := gateway.NewMemorySink()
	front, proxy := newTestProxyConfig(t, Config{
		Upstream:         mustParseURL(t, upstream.URL),
		Sink:             sink,
		SessionID:        "sess-test",
		DisableRedaction: true,
	})

	body := `{"messages":[{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_secret","content":{"password":"hunter2"}}]}]}`
	resp, err := http.Post(front.URL+"/v1/messages", "application/json", bytes.NewReader([]byte(body)))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	resp.Body.Close()
	if err := proxy.Close(context.Background()); err != nil {
		t.Fatalf("close: %v", err)
	}

	events := sink.Events()
	if len(events) != 1 {
		t.Fatalf("recorded %d events, want 1", len(events))
	}
	if events[0].Redacted {
		t.Error("event marked redacted with redaction disabled")
	}
	if !strings.Contains(string(events[0].Result), "hunter2") {
		t.Errorf("result unexpectedly altered: %s", events[0].Result)
	}
}

// TestProxySurfacesRequestOverflowInStats proves a request body larger than the
// extractor cap is visible in Stats rather than silently dropped.
func TestProxySurfacesRequestOverflowInStats(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"content":[]}`)
	}))
	defer upstream.Close()

	sink := gateway.NewMemorySink()
	front, proxy := newTestProxy(t, upstream.URL, sink)

	// 16 MiB + slack, past the request extractor's cap.
	big := bytes.Repeat([]byte("a"), (16<<20)+(64<<10))
	resp, err := http.Post(front.URL+"/v1/messages", "application/json", bytes.NewReader(big))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	resp.Body.Close()
	if err := proxy.Close(context.Background()); err != nil {
		t.Fatalf("close: %v", err)
	}
	if stats := proxy.Stats(); stats.ExtractorErrors == 0 {
		t.Error("request-body overflow not surfaced in Stats")
	}
}

// failingSink always fails, so the test can assert sink errors are counted and
// logged without leaking payload values.
type failingSink struct{}

func (failingSink) Emit(event.Event) error { return errors.New("sink write failed") }

func TestProxySinkErrorCountedAndLogged(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"content":[{"type":"tool_use","id":"toolu_err","name":"Bash","input":{"command":"printf hunter2"}}]}`)
	}))
	defer upstream.Close()

	var logs bytes.Buffer
	front, proxy := newTestProxyConfig(t, Config{
		Upstream:  mustParseURL(t, upstream.URL),
		Sink:      failingSink{},
		SessionID: "sess-test",
		ErrorLog:  log.New(&logs, "", 0),
	})

	resp, err := http.Post(front.URL+"/v1/messages", "application/json", bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if err := proxy.Close(context.Background()); err != nil {
		t.Fatalf("close: %v", err)
	}

	if stats := proxy.Stats(); stats.SinkErrors != 1 {
		t.Errorf("SinkErrors = %d, want 1 (stats=%+v)", stats.SinkErrors, stats)
	}
	if !strings.Contains(logs.String(), "sink emit failed") {
		t.Errorf("sink error not logged: %q", logs.String())
	}
	if strings.Contains(logs.String(), "hunter2") {
		t.Errorf("log leaked payload values: %q", logs.String())
	}
}

// TestProxyCloseWaitsForOpenStreamingResponse proves Close does not close the
// event queue while a response body is still streaming: the streaming
// response's event is recorded once the stream ends.
func TestProxyCloseWaitsForOpenStreamingResponse(t *testing.T) {
	firstChunk := "event: content_block_start\n" +
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_close","name":"Bash"}}` + "\n\n" +
		"event: content_block_delta\n" +
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"command\":\"pwd\"}"}}` + "\n\n"
	secondChunk := "event: content_block_stop\n" +
		`data: {"type":"content_block_stop","index":0}` + "\n\n" +
		"event: message_stop\n" +
		`data: {"type":"message_stop"}` + "\n\n"

	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		_, _ = io.WriteString(w, firstChunk)
		fl.Flush()
		<-release
		_, _ = io.WriteString(w, secondChunk)
		fl.Flush()
	}))
	defer upstream.Close()

	sink := gateway.NewMemorySink()
	front, proxy := newTestProxy(t, upstream.URL, sink)

	resp, err := http.Post(front.URL+"/v1/messages", "application/json", bytes.NewReader([]byte(`{"model":"x"}`)))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()

	// Consume exactly the first chunk so the response is provably mid-stream
	// and the proxy has installed (and tracked) its response recorder.
	first := make([]byte, len(firstChunk))
	if _, err := io.ReadFull(resp.Body, first); err != nil {
		t.Fatalf("read first chunk: %v", err)
	}

	closeErr := make(chan error, 1)
	go func() { closeErr <- proxy.Close(context.Background()) }()

	// Let Close begin draining while the stream is still open; it must wait.
	time.Sleep(50 * time.Millisecond)

	close(release)
	rest, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read rest: %v", err)
	}
	if got := string(first) + string(rest); got != firstChunk+secondChunk {
		t.Fatalf("client body mismatch: %q", got)
	}

	select {
	case err := <-closeErr:
		if err != nil {
			t.Fatalf("close: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Close did not return after the response completed")
	}

	events := sink.Events()
	if len(events) != 1 {
		t.Fatalf("recorded %d events, want the streaming response's event: %+v", len(events), events)
	}
	if !events[0].Complete || events[0].ToolCallID != "toolu_close" {
		t.Errorf("unexpected event: %+v", events[0])
	}
}

// TestProxyCloseConcurrentWithTraffic stresses Close against concurrent
// streaming requests. It must not panic or hang, and a later emit must never
// target a closed channel. Run under -race.
func TestProxyCloseConcurrentWithTraffic(t *testing.T) {
	chunk := "event: content_block_start\n" +
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_stress","name":"Bash"}}` + "\n\n" +
		"event: content_block_stop\n" +
		`data: {"type":"content_block_stop","index":0}` + "\n\n"

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		for i := 0; i < 3; i++ {
			_, _ = io.WriteString(w, chunk)
			fl.Flush()
			time.Sleep(time.Millisecond)
		}
	}))
	defer upstream.Close()

	sink := gateway.NewMemorySink()
	front, proxy := newTestProxy(t, upstream.URL, sink)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := http.Post(front.URL+"/v1/messages", "application/json", bytes.NewReader([]byte(`{}`)))
			if err != nil {
				return
			}
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}()
	}

	// Close while the requests above are mid-flight.
	time.Sleep(500 * time.Microsecond)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := proxy.Close(ctx); err != nil {
		t.Fatalf("close: %v", err)
	}
	wg.Wait()
}

// TestProxyCloseRespectsContextOnStuckSink proves Close returns the context
// error instead of hanging when the sink never returns.
func TestProxyCloseRespectsContextOnStuckSink(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"content":[{"type":"tool_use","id":"toolu_stuck","name":"Bash","input":{"command":"pwd"}}]}`)
	}))
	defer upstream.Close()

	sink := newBlockingSink()
	front, proxy := newTestProxy(t, upstream.URL, sink)

	resp, err := http.Post(front.URL+"/v1/messages", "application/json", bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	select {
	case <-sink.started:
	case <-time.After(3 * time.Second):
		t.Fatal("sink never started emitting")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	err = proxy.Close(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Close error = %v, want context.DeadlineExceeded", err)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("Close took %v, ignoring the context bound", elapsed)
	}
	// Unblock the writer goroutine so it can exit.
	close(sink.release)
}

// TestEncodedGuardIgnoresNonLLMPaths proves the Content-Encoding guard runs
// only after the path selects an extractor: an encoded response on a non-LLM
// endpoint must not inflate ExtractorErrors.
func TestEncodedGuardIgnoresNonLLMPaths(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Encoding", "br")
		_, _ = io.WriteString(w, "not-json")
	}))
	defer upstream.Close()

	sink := gateway.NewMemorySink()
	front, proxy := newTestProxy(t, upstream.URL, sink)

	resp, err := http.Get(front.URL + "/health")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	if err := proxy.Close(context.Background()); err != nil {
		t.Fatalf("close: %v", err)
	}
	if stats := proxy.Stats(); stats.ExtractorErrors != 0 {
		t.Errorf("non-LLM path charged %d extractor error(s)", stats.ExtractorErrors)
	}
	if events := sink.Events(); len(events) != 0 {
		t.Errorf("non-LLM path recorded events: %+v", events)
	}
}
