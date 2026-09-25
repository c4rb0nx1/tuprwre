package proxy

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"

	"github.com/c4rb0nx1/tuprwre/internal/event"
)

// maxResumeLineBytes bounds one log record read by PriorResults; longer
// records are skipped.
const maxResumeLineBytes = 32 << 20

// ResultKey identifies a recorded tool result for deduplication.
type ResultKey struct {
	Protocol   string
	ToolCallID string
}

func (k ResultKey) key() string { return k.Protocol + "\x00" + k.ToolCallID }

// PriorResults scans a JSONL event log written by an earlier gateway process
// and returns the tool results it already recorded for sessionID, in log
// order. Passing them as Config.PriorResults stops a restarted gateway that
// appends to the same log from re-recording the conversation history that
// harnesses resend on every turn. Malformed or oversized lines are skipped;
// only a read failure is returned.
func PriorResults(r io.Reader, sessionID string) ([]ResultKey, error) {
	var out []ResultKey
	br := bufio.NewReaderSize(r, 64<<10)
	for {
		line, err := readBoundedLine(br, maxResumeLineBytes)
		if len(line) > 0 {
			var e struct {
				SessionID  string     `json:"session_id"`
				Kind       event.Kind `json:"kind"`
				Protocol   string     `json:"protocol"`
				ToolCallID string     `json:"tool_call_id"`
			}
			if json.Unmarshal(line, &e) == nil && e.Kind == event.KindToolResult &&
				e.ToolCallID != "" && e.SessionID == sessionID {
				out = append(out, ResultKey{Protocol: e.Protocol, ToolCallID: e.ToolCallID})
			}
		}
		if errors.Is(err, io.EOF) {
			return out, nil
		}
		if err != nil {
			return out, err
		}
	}
}

// readBoundedLine returns the next line, or nil for a line longer than limit
// (which is consumed and discarded).
func readBoundedLine(br *bufio.Reader, limit int) ([]byte, error) {
	var buf []byte
	tooLong := false
	for {
		chunk, err := br.ReadSlice('\n')
		if !tooLong {
			if len(buf)+len(chunk) > limit+1 {
				tooLong, buf = true, nil
			} else {
				buf = append(buf, chunk...)
			}
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if tooLong {
			return nil, err
		}
		return bytes.TrimSpace(buf), err
	}
}
