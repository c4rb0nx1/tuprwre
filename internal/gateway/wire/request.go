package wire

import "encoding/json"

// newAnthropicRequest extracts tool_result blocks from a Messages request.
func newAnthropicRequest(emit EmitToolResult) Extractor {
	j := newJSONBody(maxBodyBytes, nil)
	j.finish = func(body []byte) { j.errs += parseAnthropicRequest(body, emit) }
	return j
}

func parseAnthropicRequest(body []byte, emit EmitToolResult) int {
	var req struct {
		Messages []struct {
			// Content is a string or an array of blocks depending on the
			// message; decode it lazily.
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return 1
	}
	for _, msg := range req.Messages {
		var blocks []struct {
			Type      string          `json:"type"`
			ToolUseID string          `json:"tool_use_id"`
			Content   json.RawMessage `json:"content"`
			IsError   bool            `json:"is_error"`
		}
		if err := json.Unmarshal(msg.Content, &blocks); err != nil {
			continue
		}
		for _, block := range blocks {
			if block.Type != "tool_result" {
				continue
			}
			if emit != nil {
				emit(ToolResult{
					Protocol:   ProtocolAnthropic,
					ToolCallID: block.ToolUseID,
					Content:    clipContent(block.Content),
					IsError:    block.IsError,
				})
			}
		}
	}
	return 0
}

// newOpenAIResponsesRequest extracts function_call_output items from a
// Responses request.
func newOpenAIResponsesRequest(emit EmitToolResult) Extractor {
	j := newJSONBody(maxBodyBytes, nil)
	j.finish = func(body []byte) { j.errs += parseOpenAIResponsesRequest(body, emit) }
	return j
}

func parseOpenAIResponsesRequest(body []byte, emit EmitToolResult) int {
	var req struct {
		Input []struct {
			Type   string          `json:"type"`
			CallID string          `json:"call_id"`
			Output json.RawMessage `json:"output"`
		} `json:"input"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return 1
	}
	for _, item := range req.Input {
		if item.Type != "function_call_output" {
			continue
		}
		if emit != nil {
			emit(ToolResult{
				Protocol:   ProtocolOpenAIResponses,
				ToolCallID: item.CallID,
				Content:    clipContent(item.Output),
			})
		}
	}
	return 0
}

// newOpenAIChatRequest extracts role=tool messages from a Chat Completions
// request.
func newOpenAIChatRequest(emit EmitToolResult) Extractor {
	j := newJSONBody(maxBodyBytes, nil)
	j.finish = func(body []byte) { j.errs += parseOpenAIChatRequest(body, emit) }
	return j
}

func parseOpenAIChatRequest(body []byte, emit EmitToolResult) int {
	var req struct {
		Messages []struct {
			Role       string          `json:"role"`
			ToolCallID string          `json:"tool_call_id"`
			Content    json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return 1
	}
	for _, msg := range req.Messages {
		if msg.Role != "tool" {
			continue
		}
		if emit != nil {
			emit(ToolResult{
				Protocol:   ProtocolOpenAIChat,
				ToolCallID: msg.ToolCallID,
				Content:    clipContent(msg.Content),
			})
		}
	}
	return 0
}

// clipContent bounds a tool-result payload to MaxArgumentsBytes.
func clipContent(raw json.RawMessage) []byte {
	if len(raw) == 0 {
		return nil
	}
	if len(raw) > MaxArgumentsBytes {
		raw = raw[:MaxArgumentsBytes]
	}
	return append([]byte(nil), raw...)
}
