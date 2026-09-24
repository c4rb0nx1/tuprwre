// Package wire provides passive extractors for LLM API traffic. Each extractor
// consumes response or request bytes incrementally through io.Writer (so it can
// sit on an io.TeeReader without altering the byte stream) and reports
// completed tool calls or tool results through a callback.
//
// Extractors never modify the bytes they observe and never panic on malformed
// input; JSON parse failures are counted and ignored.
package wire

import (
	"io"
	"strings"
)

// MaxArgumentsBytes caps the per-call arguments buffer. Calls whose arguments
// exceed the cap are clipped and reported as truncated so that the extractor's
// memory stays bounded regardless of upstream behavior.
const MaxArgumentsBytes = 1 << 20 // 1 MiB

// maxBodyBytes bounds the buffered body of a non-streaming JSON extractor.
const maxBodyBytes = 16 << 20 // 16 MiB

// Protocol names for extracted calls. They match event.Event.Protocol values.
const (
	ProtocolAnthropic       = "anthropic"
	ProtocolOpenAIResponses = "openai-responses"
	ProtocolOpenAIChat      = "openai-chat"
)

// ToolCall is a completed tool-call intent extracted from a model response.
type ToolCall struct {
	Protocol   string
	ToolCallID string
	ToolName   string
	// Arguments is the concatenated raw JSON arguments. For streaming
	// protocols it is assembled from incremental deltas.
	Arguments []byte
	// Complete is true once the protocol's terminating event was seen.
	Complete bool
	// Truncated is true when Arguments hit MaxArgumentsBytes.
	Truncated bool
}

// ToolResult is a tool result extracted from a harness request.
type ToolResult struct {
	Protocol   string
	ToolCallID string
	// Content is the raw JSON payload of the result, if present.
	Content []byte
	IsError bool
}

// EmitToolCall receives each completed call. Implementations must not assume
// any ordering beyond the order in which the protocol delivered completions.
type EmitToolCall func(ToolCall)

// EmitToolResult receives each extracted tool result.
type EmitToolResult func(ToolResult)

// Extractor consumes bytes passively. Callers must call Finish exactly once,
// after the last Write, to flush any buffered state. Errors reports the number
// of malformed inputs encountered; extraction errors never stop consumption.
type Extractor interface {
	io.Writer
	Finish()
	Errors() int
}

// NewResponseExtractor selects a response-side extractor from the request path
// and response content type. It returns nil when the path is not a recognised
// LLM endpoint. The returned extractor is safe to use as an io.Writer tee
// target.
func NewResponseExtractor(path, contentType string, emit EmitToolCall) Extractor {
	protocol := protocolForPath(path)
	if protocol == "" {
		return nil
	}
	streaming := isEventStream(contentType)
	switch protocol {
	case ProtocolAnthropic:
		return newAnthropic(streaming, emit)
	case ProtocolOpenAIResponses:
		return newOpenAIResponses(streaming, emit)
	case ProtocolOpenAIChat:
		return newOpenAIChat(streaming, emit)
	}
	return nil
}

// NewRequestExtractor selects a request-side extractor from the request path.
// It returns nil when the path is not a recognised LLM endpoint.
func NewRequestExtractor(path string, emit EmitToolResult) Extractor {
	switch protocolForPath(path) {
	case ProtocolAnthropic:
		return newAnthropicRequest(emit)
	case ProtocolOpenAIResponses:
		return newOpenAIResponsesRequest(emit)
	case ProtocolOpenAIChat:
		return newOpenAIChatRequest(emit)
	}
	return nil
}

// protocolForPath maps a request path to a wire protocol.
func protocolForPath(path string) string {
	switch {
	case hasPath(path, "v1/messages"):
		return ProtocolAnthropic
	case hasPath(path, "v1/responses"):
		return ProtocolOpenAIResponses
	case hasPath(path, "v1/chat/completions"):
		return ProtocolOpenAIChat
	}
	return ""
}

// hasPath reports whether p is exactly endpoint or ends with "/"+endpoint,
// matching on segment boundaries. This tolerates an upstream base path prefix
// (e.g. "/proxy/v1/messages") while rejecting paths that merely share a suffix
// (e.g. "/x/v1/messagesfoo"). endpoint is given without a leading slash.
func hasPath(p, endpoint string) bool {
	return p == endpoint || strings.HasSuffix(p, "/"+endpoint)
}
