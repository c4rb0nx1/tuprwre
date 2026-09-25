package proxy

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/c4rb0nx1/tuprwre/internal/gateway"
)

// TestRestartDoesNotRerecordResults runs two gateway lifetimes against one
// log: the second, seeded from the first's log, records only the new result.
func TestRestartDoesNotRerecordResults(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"content":[]}`)
	}))
	defer upstream.Close()
	logPath := filepath.Join(t.TempDir(), "gw.jsonl")

	lifetime := func(prior []ResultKey, body string, want int) {
		t.Helper()
		sink, err := gateway.NewFileSink(logPath)
		if err != nil {
			t.Fatal(err)
		}
		front, p := newTestProxyConfig(t, Config{Upstream: mustParseURL(t, upstream.URL), Sink: sink, SessionID: "sess-test", PriorResults: prior})
		resp, err := http.Post(front.URL+"/v1/messages", "application/json", bytes.NewReader([]byte(body)))
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if want > 0 {
			waitForEvents(t, p, want)
		}
		if err := p.Close(context.Background()); err != nil {
			t.Fatal(err)
		}
		if err := sink.Close(); err != nil {
			t.Fatal(err)
		}
	}

	lifetime(nil, `{"messages":[{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_a","content":"one"}]}]}`, 1)

	f, err := os.Open(logPath)
	if err != nil {
		t.Fatal(err)
	}
	prior, err := PriorResults(f, "sess-test")
	f.Close()
	if err != nil || len(prior) != 1 || prior[0] != (ResultKey{Protocol: "anthropic", ToolCallID: "toolu_a"}) {
		t.Fatalf("prior = %+v err = %v", prior, err)
	}

	lifetime(prior, `{"messages":[`+
		`{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_a","content":"one"}]},`+
		`{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_b","content":"two"}]}]}`, 1)

	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if a, b := strings.Count(string(raw), `"toolu_a"`), strings.Count(string(raw), `"toolu_b"`); a != 1 || b != 1 {
		t.Errorf("log has toolu_a x%d, toolu_b x%d; want one each:\n%s", a, b, raw)
	}
}

func TestPriorResultsFiltersAndSkips(t *testing.T) {
	huge := `{"kind":"tool_result","session_id":"s","tool_call_id":"big","result":"` + strings.Repeat("x", maxResumeLineBytes) + `"}`
	input := strings.Join([]string{
		`{"kind":"tool_result","session_id":"s","protocol":"openai-chat","tool_call_id":"c1"}`,
		`{"kind":"tool_result","session_id":"other","protocol":"openai-chat","tool_call_id":"c2"}`,
		`{"kind":"tool_call_intent","session_id":"s","protocol":"openai-chat","tool_call_id":"c3"}`,
		`{"kind":"tool_result","session_id":"s","protocol":"openai-chat"}`,
		`not json`,
		huge,
		`{"kind":"tool_result","session_id":"s","protocol":"anthropic","tool_call_id":"c4"}`,
	}, "\n")
	got, err := PriorResults(strings.NewReader(input), "s")
	if err != nil {
		t.Fatal(err)
	}
	want := []ResultKey{{"openai-chat", "c1"}, {"anthropic", "c4"}}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("got %+v, want %+v", got, want)
	}
}
