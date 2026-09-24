# Synthetic wire fixtures

Every file in this directory is **synthetic**: hand-authored to match the
documented wire format of each API. None was captured from a live service and
none contains credentials, tokens, or real user data.

- `anthropic_stream.sse` — Anthropic Messages streaming (LF)
- `anthropic_stream_crlf.sse` — same, CRLF line endings
- `anthropic_nonstream.json` — Anthropic Messages non-streaming
- `openai_responses_stream.sse` — OpenAI Responses streaming
- `openai_responses_nonstream.json` — OpenAI Responses non-streaming
- `openai_chat_stream.sse` — OpenAI Chat Completions streaming
- `openai_chat_nonstream.json` — OpenAI Chat Completions non-streaming
- `anthropic_request.json` — request-side tool_result blocks
- `openai_responses_request.json` — request-side function_call_output items
- `openai_chat_request.json` — request-side role=tool messages
- `malformed.sse` — deliberately malformed JSON payloads
