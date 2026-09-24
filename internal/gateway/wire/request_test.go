package wire

import (
	"testing"
)

func TestAnthropicRequestExtractor(t *testing.T) {
	var got []ToolResult
	ex := NewRequestExtractor("/v1/messages", func(r ToolResult) { got = append(got, r) })
	if ex == nil {
		t.Fatal("extractor is nil")
	}
	feed(t, ex, readFixture(t, "anthropic_request.json"), byteAtATime)

	if len(got) != 2 {
		t.Fatalf("got %d results, want 2: %+v", len(got), got)
	}
	if got[0].ToolCallID != "toolu_synth_01" || got[0].IsError {
		t.Errorf("result 0 = %+v", got[0])
	}
	if string(got[0].Content) != `"ok\n/path"` {
		t.Errorf("result 0 content = %s", got[0].Content)
	}
	if got[1].ToolCallID != "toolu_synth_02" || !got[1].IsError {
		t.Errorf("result 1 = %+v", got[1])
	}
	if got[0].Protocol != ProtocolAnthropic {
		t.Errorf("protocol = %q", got[0].Protocol)
	}
}

func TestOpenAIResponsesRequestExtractor(t *testing.T) {
	var got []ToolResult
	ex := NewRequestExtractor("/v1/responses", func(r ToolResult) { got = append(got, r) })
	feed(t, ex, readFixture(t, "openai_responses_request.json"), byteAtATime)

	if len(got) != 1 {
		t.Fatalf("got %d results, want 1: %+v", len(got), got)
	}
	if got[0].ToolCallID != "call_synth_01" {
		t.Errorf("tool call id = %q", got[0].ToolCallID)
	}
	if string(got[0].Content) != `"{\"temp_c\":21}"` {
		t.Errorf("content = %s", got[0].Content)
	}
}

func TestOpenAIChatRequestExtractor(t *testing.T) {
	var got []ToolResult
	ex := NewRequestExtractor("/v1/chat/completions", func(r ToolResult) { got = append(got, r) })
	feed(t, ex, readFixture(t, "openai_chat_request.json"), byteAtATime)

	if len(got) != 1 {
		t.Fatalf("got %d results, want 1: %+v", len(got), got)
	}
	if got[0].ToolCallID != "call_synth_01" {
		t.Errorf("tool call id = %q", got[0].ToolCallID)
	}
	if string(got[0].Content) != `"sunny"` {
		t.Errorf("content = %s", got[0].Content)
	}
}

func TestRequestExtractorUnknownPath(t *testing.T) {
	if ex := NewRequestExtractor("/healthz", func(ToolResult) {}); ex != nil {
		t.Error("expected nil extractor for unrelated path")
	}
}

func TestRequestExtractorMalformedJSON(t *testing.T) {
	ex := NewRequestExtractor("/v1/messages", func(ToolResult) {})
	feed(t, ex, []byte("{not json"), byteAtATime)
	if ex.Errors() == 0 {
		t.Error("expected parse error to be counted")
	}
}
