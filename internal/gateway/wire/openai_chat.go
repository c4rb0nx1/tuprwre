package wire

import (
	"encoding/json"
	"sort"
)

// newOpenAIChat builds a response extractor for OpenAI Chat Completions.
func newOpenAIChat(streaming bool, emit EmitToolCall) Extractor {
	if streaming {
		c := &openAIChatStream{emit: emit, calls: make(map[int]*partialCall)}
		c.sc = newSSEScanner(c.onEvent)
		return c
	}
	c := &jsonBody{max: maxBodyBytes}
	c.finish = func(body []byte) { c.errs += parseOpenAIChatMessage(body, emit) }
	return c
}

// openAIChatStream extracts tool_calls from a Chat Completions SSE stream.
type openAIChatStream struct {
	sc    *sseScanner
	emit  EmitToolCall
	calls map[int]*partialCall
	errs  int
}

func (c *openAIChatStream) Write(p []byte) (int, error) { return c.sc.Write(p) }

func (c *openAIChatStream) Finish() {
	c.sc.finish()
	// Any call still in flight never saw finish_reason=tool_calls or [DONE]:
	// the stream ended early (or the client disconnected), so it is incomplete.
	c.flushAll(false)
}

func (c *openAIChatStream) Errors() int { return c.errs }

func (c *openAIChatStream) onEvent(_, data string) {
	if data == "[DONE]" {
		c.flushAll(true)
		return
	}
	var chunk struct {
		Choices []struct {
			Delta struct {
				ToolCalls []struct {
					Index    int    `json:"index"`
					ID       string `json:"id"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"delta"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
	}
	if !decodeObject(data, &chunk) {
		c.errs++
		return
	}
	for _, choice := range chunk.Choices {
		for _, tc := range choice.Delta.ToolCalls {
			call := c.calls[tc.Index]
			if call == nil {
				call = &partialCall{}
				c.calls[tc.Index] = call
			}
			if tc.ID != "" {
				call.id = tc.ID
			}
			if tc.Function.Name != "" {
				if call.name == "" {
					call.name = tc.Function.Name
				} else {
					call.name += tc.Function.Name
				}
			}
			call.appendArgs([]byte(tc.Function.Arguments))
		}
		if choice.FinishReason == "tool_calls" {
			c.flushAll(true)
		}
	}
}

// flushAll emits every in-flight call. complete is true only when the caller
// saw a terminating event (finish_reason=tool_calls or [DONE]).
func (c *openAIChatStream) flushAll(complete bool) {
	idxs := make([]int, 0, len(c.calls))
	for idx := range c.calls {
		idxs = append(idxs, idx)
	}
	sort.Ints(idxs)
	for _, idx := range idxs {
		call := c.calls[idx]
		delete(c.calls, idx)
		if c.emit != nil {
			c.emit(call.toolCall(ProtocolOpenAIChat, complete))
		}
	}
}

// parseOpenAIChatMessage emits tool_calls from a non-streaming Chat
// Completions JSON body.
func parseOpenAIChatMessage(body []byte, emit EmitToolCall) int {
	var resp struct {
		Choices []struct {
			Message struct {
				ToolCalls []struct {
					ID       string `json:"id"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return 1
	}
	for _, choice := range resp.Choices {
		for _, tc := range choice.Message.ToolCalls {
			c := &partialCall{id: tc.ID, name: tc.Function.Name}
			c.appendArgs([]byte(tc.Function.Arguments))
			if emit != nil {
				emit(c.toolCall(ProtocolOpenAIChat, true))
			}
		}
	}
	return 0
}
