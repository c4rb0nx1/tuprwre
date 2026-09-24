package wire

import (
	"encoding/json"
	"sort"
)

// newAnthropic builds a response extractor for the Anthropic Messages API.
func newAnthropic(streaming bool, emit EmitToolCall) Extractor {
	if streaming {
		a := &anthropicStream{emit: emit, blocks: make(map[int]*partialCall)}
		a.sc = newSSEScanner(a.onEvent)
		return a
	}
	a := &jsonBody{max: maxBodyBytes}
	a.finish = func(body []byte) { a.errs += parseAnthropicContent(body, emit) }
	return a
}

// anthropicStream extracts tool_use blocks from a Messages SSE stream.
type anthropicStream struct {
	sc     *sseScanner
	emit   EmitToolCall
	blocks map[int]*partialCall
	errs   int
}

func (a *anthropicStream) Write(p []byte) (int, error) {
	return a.sc.Write(p)
}

func (a *anthropicStream) Finish() {
	a.sc.finish()
	// Flush any tool_use blocks that never received a content_block_stop,
	// in index order for determinism.
	idxs := make([]int, 0, len(a.blocks))
	for idx := range a.blocks {
		idxs = append(idxs, idx)
	}
	sort.Ints(idxs)
	for _, idx := range idxs {
		a.flush(idx)
	}
}

func (a *anthropicStream) Errors() int { return a.errs }

func (a *anthropicStream) onEvent(event, data string) {
	switch event {
	case "content_block_start":
		var ev struct {
			Index        int `json:"index"`
			ContentBlock struct {
				Type string `json:"type"`
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"content_block"`
		}
		if !decodeObject(data, &ev) {
			a.errs++
			return
		}
		if ev.ContentBlock.Type != "tool_use" {
			return
		}
		a.blocks[ev.Index] = &partialCall{id: ev.ContentBlock.ID, name: ev.ContentBlock.Name}
	case "content_block_delta":
		var ev struct {
			Index int `json:"index"`
			Delta struct {
				Type        string `json:"type"`
				PartialJSON string `json:"partial_json"`
			} `json:"delta"`
		}
		if !decodeObject(data, &ev) {
			a.errs++
			return
		}
		if ev.Delta.Type != "input_json_delta" {
			return
		}
		if c := a.blocks[ev.Index]; c != nil {
			c.appendArgs([]byte(ev.Delta.PartialJSON))
		}
	case "content_block_stop":
		var ev struct {
			Index int `json:"index"`
		}
		if !decodeObject(data, &ev) {
			a.errs++
			return
		}
		if a.blocks[ev.Index] != nil {
			a.flush(ev.Index)
		}
	}
}

func (a *anthropicStream) flush(idx int) {
	c := a.blocks[idx]
	if c == nil {
		return
	}
	delete(a.blocks, idx)
	if a.emit != nil {
		a.emit(c.toolCall(ProtocolAnthropic))
	}
}

// parseAnthropicContent emits tool_use blocks from a non-streaming Messages
// JSON body. It returns the number of parse errors.
func parseAnthropicContent(body []byte, emit EmitToolCall) int {
	var resp struct {
		Content []struct {
			Type  string          `json:"type"`
			ID    string          `json:"id"`
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input"`
		} `json:"content"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return 1
	}
	for _, c := range resp.Content {
		if c.Type != "tool_use" {
			continue
		}
		args := c.Input
		if len(args) == 0 {
			args = []byte("{}")
		}
		trunc := false
		if len(args) > MaxArgumentsBytes {
			args = args[:MaxArgumentsBytes]
			trunc = true
		}
		if emit != nil {
			emit(ToolCall{
				Protocol:   ProtocolAnthropic,
				ToolCallID: c.ID,
				ToolName:   c.Name,
				Arguments:  append([]byte(nil), args...),
				Complete:   true,
				Truncated:  trunc,
			})
		}
	}
	return 0
}
