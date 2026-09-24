package wire

import (
	"encoding/json"
	"strconv"
)

// newOpenAIResponses builds a response extractor for the OpenAI Responses API.
func newOpenAIResponses(streaming bool, emit EmitToolCall) Extractor {
	if streaming {
		r := &openAIResponsesStream{emit: emit, calls: make(map[string]*partialCall)}
		r.sc = newSSEScanner(r.onEvent)
		return r
	}
	r := &jsonBody{max: maxBodyBytes}
	r.finish = func(body []byte) { r.errs += parseOpenAIResponsesOutput(body, emit) }
	return r
}

// openAIResponsesStream extracts function_call items from a Responses SSE
// stream (response.output_item.* and response.function_call_arguments.*).
type openAIResponsesStream struct {
	sc    *sseScanner
	emit  EmitToolCall
	calls map[string]*partialCall
	order []string
	errs  int
}

func (r *openAIResponsesStream) Write(p []byte) (int, error) { return r.sc.Write(p) }

func (r *openAIResponsesStream) Finish() {
	r.sc.finish()
	for _, key := range r.order {
		r.flush(key)
	}
}

func (r *openAIResponsesStream) Errors() int { return r.errs }

func (r *openAIResponsesStream) onEvent(event, data string) {
	etype := event
	if etype == "" {
		var t struct {
			Type string `json:"type"`
		}
		if decodeObject(data, &t) {
			etype = t.Type
		}
	}
	switch etype {
	case "response.output_item.added":
		var ev struct {
			OutputIndex int `json:"output_index"`
			Item        struct {
				Type      string `json:"type"`
				ID        string `json:"id"`
				CallID    string `json:"call_id"`
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			} `json:"item"`
		}
		if !decodeObject(data, &ev) {
			r.errs++
			return
		}
		if ev.Item.Type != "function_call" {
			return
		}
		key := itemKey(ev.Item.ID, ev.OutputIndex)
		if _, ok := r.calls[key]; !ok {
			r.order = append(r.order, key)
		}
		c := &partialCall{id: ev.Item.CallID, name: ev.Item.Name}
		if c.id == "" {
			c.id = ev.Item.ID
		}
		c.appendArgs([]byte(ev.Item.Arguments))
		r.calls[key] = c
	case "response.function_call_arguments.delta":
		var ev struct {
			ItemID      string `json:"item_id"`
			OutputIndex int    `json:"output_index"`
			Delta       string `json:"delta"`
		}
		if !decodeObject(data, &ev) {
			r.errs++
			return
		}
		if c := r.calls[itemKey(ev.ItemID, ev.OutputIndex)]; c != nil {
			c.appendArgs([]byte(ev.Delta))
		}
	case "response.function_call_arguments.done":
		var ev struct {
			ItemID      string `json:"item_id"`
			OutputIndex int    `json:"output_index"`
			Arguments   string `json:"arguments"`
		}
		if !decodeObject(data, &ev) {
			r.errs++
			return
		}
		if c := r.calls[itemKey(ev.ItemID, ev.OutputIndex)]; c != nil {
			c.args = nil
			c.trunc = false
			c.appendArgs([]byte(ev.Arguments))
		}
	case "response.output_item.done":
		var ev struct {
			OutputIndex int `json:"output_index"`
			Item        struct {
				Type      string `json:"type"`
				ID        string `json:"id"`
				CallID    string `json:"call_id"`
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			} `json:"item"`
		}
		if !decodeObject(data, &ev) {
			r.errs++
			return
		}
		if ev.Item.Type != "function_call" {
			return
		}
		key := itemKey(ev.Item.ID, ev.OutputIndex)
		c := r.calls[key]
		if c == nil {
			c = &partialCall{id: ev.Item.CallID, name: ev.Item.Name}
			if c.id == "" {
				c.id = ev.Item.ID
			}
			r.order = append(r.order, key)
			r.calls[key] = c
		}
		if ev.Item.Arguments != "" {
			c.args = nil
			c.trunc = false
			c.appendArgs([]byte(ev.Item.Arguments))
		}
		r.flush(key)
	}
}

func (r *openAIResponsesStream) flush(key string) {
	c := r.calls[key]
	if c == nil {
		return
	}
	delete(r.calls, key)
	if r.emit != nil {
		r.emit(c.toolCall(ProtocolOpenAIResponses))
	}
}

// itemKey identifies an in-flight call. Item IDs are stable across the added,
// delta and done events; output_index is only a fallback.
func itemKey(id string, index int) string {
	if id != "" {
		return "id:" + id
	}
	return "idx:" + strconv.Itoa(index)
}

// parseOpenAIResponsesOutput emits function_call items from a non-streaming
// Responses JSON body.
func parseOpenAIResponsesOutput(body []byte, emit EmitToolCall) int {
	var resp struct {
		Output []struct {
			Type      string `json:"type"`
			CallID    string `json:"call_id"`
			ID        string `json:"id"`
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"output"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return 1
	}
	for _, item := range resp.Output {
		if item.Type != "function_call" {
			continue
		}
		c := &partialCall{id: item.CallID, name: item.Name}
		if c.id == "" {
			c.id = item.ID
		}
		c.appendArgs([]byte(item.Arguments))
		if emit != nil {
			emit(c.toolCall(ProtocolOpenAIResponses))
		}
	}
	return 0
}
