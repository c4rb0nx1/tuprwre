package wire

import "encoding/json"

// jsonBody passively buffers a non-streaming JSON body and hands it to finish
// once at Finish. It bounds memory at max bytes; a larger body is discarded and
// counted as an error rather than retained.
type jsonBody struct {
	buf      []byte
	max      int
	overflow bool
	errs     int
	finish   func(body []byte)
}

func newJSONBody(max int, finish func(body []byte)) *jsonBody {
	return &jsonBody{max: max, finish: finish}
}

func (j *jsonBody) Write(p []byte) (int, error) {
	if !j.overflow {
		if len(j.buf)+len(p) > j.max {
			j.overflow = true
			j.buf = nil
		} else {
			j.buf = append(j.buf, p...)
		}
	}
	return len(p), nil
}

func (j *jsonBody) Finish() {
	if j.overflow {
		j.errs++
		j.buf = nil
		return
	}
	if j.finish != nil && len(j.buf) > 0 {
		j.finish(j.buf)
	}
	j.buf = nil
}

func (j *jsonBody) Errors() int { return j.errs }

// partialCall accumulates a tool call under construction during streaming.
type partialCall struct {
	id       string
	name     string
	args     []byte
	trunc    bool
	complete bool
}

// appendArgs appends delta to the accumulated arguments, honoring the per-call
// cap. Once truncated no further bytes are retained.
func (c *partialCall) appendArgs(delta []byte) {
	if c.trunc || len(delta) == 0 {
		return
	}
	remaining := MaxArgumentsBytes - len(c.args)
	if remaining <= 0 {
		c.trunc = true
		return
	}
	if len(delta) > remaining {
		c.args = append(c.args, delta[:remaining]...)
		c.trunc = true
		return
	}
	c.args = append(c.args, delta...)
}

// toolCall materializes the accumulated state for emission.
func (c *partialCall) toolCall(protocol string) ToolCall {
	args := c.args
	if len(args) == 0 {
		args = []byte("{}")
	}
	return ToolCall{
		Protocol:   protocol,
		ToolCallID: c.id,
		ToolName:   c.name,
		Arguments:  append([]byte(nil), args...),
		Complete:   true,
		Truncated:  c.trunc,
	}
}

// decodeObject unmarshals data into v, reporting (and counting) failures via
// the returned bool rather than panicking.
func decodeObject(data string, v any) bool {
	return json.Unmarshal([]byte(data), v) == nil
}
