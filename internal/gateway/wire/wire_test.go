package wire

import (
	"bytes"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}

// feed pushes data through ex in chunks chosen by next, then calls Finish.
func feed(t *testing.T, ex Extractor, data []byte, next func(i int) int) {
	t.Helper()
	for i := 0; i < len(data); {
		n := next(i)
		if n <= 0 {
			n = 1
		}
		end := i + n
		if end > len(data) {
			end = len(data)
		}
		if _, err := ex.Write(data[i:end]); err != nil {
			t.Fatalf("write at %d: %v", i, err)
		}
		i = end
	}
	ex.Finish()
}

func byteAtATime(int) int { return 1 }

func randomSizes(seed int64) func(i int) int {
	r := rand.New(rand.NewSource(seed))
	return func(int) int { return 1 + r.Intn(7) }
}

func assertCall(t *testing.T, got, want ToolCall) {
	t.Helper()
	if got.Protocol != want.Protocol {
		t.Errorf("protocol = %q, want %q", got.Protocol, want.Protocol)
	}
	if got.ToolCallID != want.ToolCallID {
		t.Errorf("tool call id = %q, want %q", got.ToolCallID, want.ToolCallID)
	}
	if got.ToolName != want.ToolName {
		t.Errorf("tool name = %q, want %q", got.ToolName, want.ToolName)
	}
	if string(got.Arguments) != string(want.Arguments) {
		t.Errorf("arguments = %s, want %s", got.Arguments, want.Arguments)
	}
	if got.Complete != want.Complete || got.Truncated != want.Truncated {
		t.Errorf("flags = complete:%v truncated:%v, want complete:%v truncated:%v",
			got.Complete, got.Truncated, want.Complete, want.Truncated)
	}
}

// anthropicFixtureCalls returns the calls expected from anthropic_stream.sse.
func anthropicFixtureCalls() []ToolCall {
	return []ToolCall{
		{Protocol: ProtocolAnthropic, ToolCallID: "toolu_synth_01", ToolName: "Bash", Arguments: []byte(`{"command":"ls -la"}`), Complete: true},
		{Protocol: ProtocolAnthropic, ToolCallID: "toolu_synth_02", ToolName: "Read", Arguments: []byte(`{"file_path":"/etc/hosts"}`), Complete: true},
	}
}

func TestAnthropicStreamByteAtATime(t *testing.T) {
	var got []ToolCall
	ex := NewResponseExtractor("/v1/messages", "text/event-stream", func(c ToolCall) { got = append(got, c) })
	if ex == nil {
		t.Fatal("extractor is nil")
	}
	feed(t, ex, readFixture(t, "anthropic_stream.sse"), byteAtATime)

	want := anthropicFixtureCalls()
	if len(got) != len(want) {
		t.Fatalf("got %d calls, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		assertCall(t, got[i], want[i])
	}
	if ex.Errors() != 0 {
		t.Errorf("errors = %d, want 0", ex.Errors())
	}
}

func TestAnthropicStreamRandomSplits(t *testing.T) {
	for seed := int64(0); seed < 250; seed++ {
		var got []ToolCall
		ex := NewResponseExtractor("/v1/messages", "text/event-stream", func(c ToolCall) { got = append(got, c) })
		feed(t, ex, readFixture(t, "anthropic_stream.sse"), randomSizes(seed))

		want := anthropicFixtureCalls()
		if len(got) != len(want) {
			t.Fatalf("seed %d: got %d calls, want %d: %+v", seed, len(got), len(want), got)
		}
		for i := range want {
			assertCall(t, got[i], want[i])
		}
	}
}

func TestAnthropicStreamCRLF(t *testing.T) {
	var got []ToolCall
	ex := NewResponseExtractor("/v1/messages", "text/event-stream", func(c ToolCall) { got = append(got, c) })
	feed(t, ex, readFixture(t, "anthropic_stream_crlf.sse"), byteAtATime)

	want := anthropicFixtureCalls()
	if len(got) != len(want) {
		t.Fatalf("got %d calls, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		assertCall(t, got[i], want[i])
	}
}

func TestAnthropicNonStreaming(t *testing.T) {
	var got []ToolCall
	ex := NewResponseExtractor("/v1/messages", "application/json", func(c ToolCall) { got = append(got, c) })
	feed(t, ex, readFixture(t, "anthropic_nonstream.json"), byteAtATime)

	want := []ToolCall{{
		Protocol: ProtocolAnthropic, ToolCallID: "toolu_synth_01", ToolName: "Bash",
		Arguments: []byte(`{"command":"pwd"}`), Complete: true,
	}}
	if len(got) != len(want) {
		t.Fatalf("got %d calls, want %d: %+v", len(got), len(want), got)
	}
	assertCall(t, got[0], want[0])
}

func TestOpenAIResponsesStreamByteAtATime(t *testing.T) {
	var got []ToolCall
	ex := NewResponseExtractor("/v1/responses", "text/event-stream", func(c ToolCall) { got = append(got, c) })
	feed(t, ex, readFixture(t, "openai_responses_stream.sse"), byteAtATime)

	want := ToolCall{
		Protocol: ProtocolOpenAIResponses, ToolCallID: "call_synth_01", ToolName: "get_weather",
		Arguments: []byte(`{"city":"Berlin"}`), Complete: true,
	}
	if len(got) != 1 {
		t.Fatalf("got %d calls, want 1: %+v", len(got), got)
	}
	assertCall(t, got[0], want)
}

func TestOpenAIResponsesStreamRandomSplits(t *testing.T) {
	for seed := int64(0); seed < 150; seed++ {
		var got []ToolCall
		ex := NewResponseExtractor("/v1/responses", "text/event-stream", func(c ToolCall) { got = append(got, c) })
		feed(t, ex, readFixture(t, "openai_responses_stream.sse"), randomSizes(seed))
		if len(got) != 1 || string(got[0].Arguments) != `{"city":"Berlin"}` {
			t.Fatalf("seed %d: got %+v", seed, got)
		}
	}
}

func TestOpenAIResponsesNonStreaming(t *testing.T) {
	var got []ToolCall
	ex := NewResponseExtractor("/v1/responses", "application/json", func(c ToolCall) { got = append(got, c) })
	feed(t, ex, readFixture(t, "openai_responses_nonstream.json"), byteAtATime)

	if len(got) != 1 {
		t.Fatalf("got %d calls, want 1: %+v", len(got), got)
	}
	assertCall(t, got[0], ToolCall{
		Protocol: ProtocolOpenAIResponses, ToolCallID: "call_synth_01", ToolName: "get_weather",
		Arguments: []byte(`{"city":"Berlin"}`), Complete: true,
	})
}

func TestOpenAIChatStreamByteAtATime(t *testing.T) {
	var got []ToolCall
	ex := NewResponseExtractor("/v1/chat/completions", "text/event-stream", func(c ToolCall) { got = append(got, c) })
	feed(t, ex, readFixture(t, "openai_chat_stream.sse"), byteAtATime)

	want := ToolCall{
		Protocol: ProtocolOpenAIChat, ToolCallID: "call_synth_01", ToolName: "get_weather",
		Arguments: []byte(`{"city":"Paris"}`), Complete: true,
	}
	if len(got) != 1 {
		t.Fatalf("got %d calls, want 1: %+v", len(got), got)
	}
	assertCall(t, got[0], want)
}

func TestOpenAIChatStreamRandomSplits(t *testing.T) {
	for seed := int64(0); seed < 150; seed++ {
		var got []ToolCall
		ex := NewResponseExtractor("/v1/chat/completions", "text/event-stream", func(c ToolCall) { got = append(got, c) })
		feed(t, ex, readFixture(t, "openai_chat_stream.sse"), randomSizes(seed))
		if len(got) != 1 || string(got[0].Arguments) != `{"city":"Paris"}` {
			t.Fatalf("seed %d: got %+v", seed, got)
		}
	}
}

func TestOpenAIChatNonStreaming(t *testing.T) {
	var got []ToolCall
	ex := NewResponseExtractor("/v1/chat/completions", "application/json", func(c ToolCall) { got = append(got, c) })
	feed(t, ex, readFixture(t, "openai_chat_nonstream.json"), byteAtATime)

	if len(got) != 1 {
		t.Fatalf("got %d calls, want 1: %+v", len(got), got)
	}
	assertCall(t, got[0], ToolCall{
		Protocol: ProtocolOpenAIChat, ToolCallID: "call_synth_01", ToolName: "get_weather",
		Arguments: []byte(`{"city":"Paris"}`), Complete: true,
	})
}

func TestResponseExtractorSelection(t *testing.T) {
	emit := func(ToolCall) {}
	if ex := NewResponseExtractor("/unrelated", "application/json", emit); ex != nil {
		t.Error("expected nil extractor for unrelated path")
	}
	if ex := NewResponseExtractor("/proxy/v1/messages", "text/event-stream", emit); ex == nil {
		t.Error("expected extractor for base-path-prefixed messages path")
	}
}

func TestMultiLineDataField(t *testing.T) {
	var got []ToolCall
	ex := NewResponseExtractor("/v1/messages", "text/event-stream", func(c ToolCall) { got = append(got, c) })
	body := ": comment\n\n" +
		"event: content_block_start\n" +
		"data: {\"index\":0,\"content_block\":{\"type\":\"tool_use\",\n" +
		"data: \"id\":\"toolu_multi\",\"name\":\"Echo\"}}\n\n" +
		"event: content_block_stop\n" +
		"data: {\"index\":0}\n\n"
	feed(t, ex, []byte(body), byteAtATime)
	if len(got) != 1 {
		t.Fatalf("got %d calls, want 1: %+v", len(got), got)
	}
	if got[0].ToolCallID != "toolu_multi" || got[0].ToolName != "Echo" {
		t.Fatalf("unexpected call: %+v", got[0])
	}
	if string(got[0].Arguments) != "{}" {
		t.Fatalf("arguments = %s, want {}", got[0].Arguments)
	}
}

func TestMalformedInputCountsErrorsAndDoesNotPanic(t *testing.T) {
	var got []ToolCall
	ex := NewResponseExtractor("/v1/messages", "text/event-stream", func(c ToolCall) { got = append(got, c) })
	feed(t, ex, readFixture(t, "malformed.sse"), byteAtATime)
	if ex.Errors() == 0 {
		t.Error("expected malformed payloads to be counted")
	}

	// A wholly invalid JSON body must be counted, not panic.
	jsonEx := NewResponseExtractor("/v1/messages", "application/json", func(ToolCall) {})
	feed(t, jsonEx, []byte("{not json"), byteAtATime)
	if jsonEx.Errors() == 0 {
		t.Error("expected invalid JSON body to be counted")
	}
}

func TestArgumentsTruncatedAtCap(t *testing.T) {
	var got []ToolCall
	ex := NewResponseExtractor("/v1/messages", "text/event-stream", func(c ToolCall) { got = append(got, c) })

	var b strings.Builder
	b.WriteString("event: content_block_start\n")
	b.WriteString(`data: {"index":0,"content_block":{"type":"tool_use","id":"toolu_big","name":"Big"}}` + "\n\n")
	// Emit more than the cap in deltas.
	chunk := strings.Repeat("A", 4096)
	remaining := MaxArgumentsBytes + 8192
	for remaining > 0 {
		n := len(chunk)
		if n > remaining {
			n = remaining
		}
		b.WriteString("event: content_block_delta\n")
		b.WriteString(`data: {"index":0,"delta":{"type":"input_json_delta","partial_json":"` + chunk[:n] + `"}}` + "\n\n")
		remaining -= n
	}
	b.WriteString("event: content_block_stop\n")
	b.WriteString(`data: {"index":0}` + "\n\n")

	feed(t, ex, []byte(b.String()), byteAtATime)

	if len(got) != 1 {
		t.Fatalf("got %d calls, want 1", len(got))
	}
	if !got[0].Truncated {
		t.Error("expected truncated flag")
	}
	if len(got[0].Arguments) != MaxArgumentsBytes {
		t.Errorf("arguments len = %d, want %d", len(got[0].Arguments), MaxArgumentsBytes)
	}
}

func TestInvalidJSONExtractorBoundsMemory(t *testing.T) {
	// A body larger than the cap must be dropped and counted rather than
	// retained; the extractor must still report the full write.
	ex := NewResponseExtractor("/v1/messages", "application/json", func(ToolCall) {})
	big := bytes.Repeat([]byte("x"), maxBodyBytes+1)
	n, err := ex.Write(big)
	if err != nil || n != len(big) {
		t.Fatalf("write = (%d,%v), want (%d,nil)", n, err, len(big))
	}
	ex.Finish()
	if ex.Errors() == 0 {
		t.Error("expected overflow to be counted as an error")
	}
}
