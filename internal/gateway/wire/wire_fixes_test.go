package wire

import "testing"

// TestAnthropicStreamAbortedIsIncomplete proves that a stream cut off before
// content_block_stop records an incomplete call carrying the partial arguments
// rather than a fabricated, complete "{}" call.
func TestAnthropicStreamAbortedIsIncomplete(t *testing.T) {
	var got []ToolCall
	ex := NewResponseExtractor("/v1/messages", "text/event-stream", func(c ToolCall) { got = append(got, c) })
	body := "event: content_block_start\n" +
		`data: {"index":0,"content_block":{"type":"tool_use","id":"toolu_abort","name":"Bash"}}` + "\n\n" +
		"event: content_block_delta\n" +
		`data: {"index":0,"delta":{"type":"input_json_delta","partial_json":"{\"command\":\"ls"}}` + "\n\n"
	feed(t, ex, []byte(body), byteAtATime)

	if len(got) != 1 {
		t.Fatalf("got %d calls, want 1: %+v", len(got), got)
	}
	if got[0].Complete {
		t.Error("aborted call reported Complete=true")
	}
	if string(got[0].Arguments) != `{"command":"ls` {
		t.Errorf("arguments = %q, want the partial payload", got[0].Arguments)
	}
}

// TestAnthropicStreamAbortedWithoutDeltasHasNoFabricatedArguments proves that
// an aborted call with no deltas keeps empty Arguments instead of "{}".
func TestAnthropicStreamAbortedWithoutDeltasHasNoFabricatedArguments(t *testing.T) {
	var got []ToolCall
	ex := NewResponseExtractor("/v1/messages", "text/event-stream", func(c ToolCall) { got = append(got, c) })
	body := "event: content_block_start\n" +
		`data: {"index":0,"content_block":{"type":"tool_use","id":"toolu_nodeltas","name":"Bash"}}` + "\n\n"
	feed(t, ex, []byte(body), byteAtATime)

	if len(got) != 1 {
		t.Fatalf("got %d calls, want 1: %+v", len(got), got)
	}
	if got[0].Complete {
		t.Error("aborted call reported Complete=true")
	}
	if len(got[0].Arguments) != 0 {
		t.Errorf("arguments = %q, want empty for an incomplete call", got[0].Arguments)
	}
}

func TestOpenAIResponsesStreamAbortedIsIncomplete(t *testing.T) {
	var got []ToolCall
	ex := NewResponseExtractor("/v1/responses", "text/event-stream", func(c ToolCall) { got = append(got, c) })
	body := "event: response.output_item.added\n" +
		`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","id":"fc_abort","call_id":"call_abort","name":"get_weather","arguments":""}}` + "\n\n" +
		"event: response.function_call_arguments.delta\n" +
		`data: {"type":"response.function_call_arguments.delta","item_id":"fc_abort","output_index":0,"delta":"{\"city\":"}` + "\n\n"
	feed(t, ex, []byte(body), byteAtATime)

	if len(got) != 1 {
		t.Fatalf("got %d calls, want 1: %+v", len(got), got)
	}
	if got[0].Complete {
		t.Error("aborted call reported Complete=true")
	}
	if string(got[0].Arguments) != `{"city":` {
		t.Errorf("arguments = %q, want the partial payload", got[0].Arguments)
	}
}

func TestOpenAIChatStreamAbortedIsIncomplete(t *testing.T) {
	var got []ToolCall
	ex := NewResponseExtractor("/v1/chat/completions", "text/event-stream", func(c ToolCall) { got = append(got, c) })
	body := `data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_abort","function":{"name":"get_weather","arguments":"{\"city\":"}}]},"finish_reason":null}]}` + "\n\n"
	feed(t, ex, []byte(body), byteAtATime)

	if len(got) != 1 {
		t.Fatalf("got %d calls, want 1: %+v", len(got), got)
	}
	if got[0].Complete {
		t.Error("aborted call reported Complete=true")
	}
	if string(got[0].Arguments) != `{"city":` {
		t.Errorf("arguments = %q, want the partial payload", got[0].Arguments)
	}
}

// TestPathMatchingRespectsSegmentBoundary checks that an endpoint is matched
// only as a whole trailing segment sequence.
func TestPathMatchingRespectsSegmentBoundary(t *testing.T) {
	emit := func(ToolCall) {}
	cases := []struct {
		path string
		want bool
	}{
		{"/v1/messages", true},
		{"/proxy/v1/messages", true},
		{"/x/v1/messages", true},
		{"/x/v1/messagesfoo", false},
		{"/v1/messages/count_tokens", false},
		{"/v1/responses", true},
		{"/v1/responses/resp_123", false},
		{"/x/v1/responsesfoo", false},
		{"/v1/chat/completions", true},
		{"/v1/chat/completionsfoo", false},
	}
	for _, c := range cases {
		got := NewResponseExtractor(c.path, "text/event-stream", emit) != nil
		if got != c.want {
			t.Errorf("NewResponseExtractor(%q) matched = %v, want %v", c.path, got, c.want)
		}
	}
}
