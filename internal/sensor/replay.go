package sensor

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync/atomic"

	"github.com/c4rb0nx1/tuprwre/internal/event"
	"github.com/c4rb0nx1/tuprwre/internal/gateway"
)

// MaxReplayLineBytes bounds one JSONL record read by Replay. Longer lines are
// skipped and counted as errors so memory stays bounded.
const MaxReplayLineBytes = 4 << 20 // 4 MiB

// Replay is a Sensor that re-emits effect events already in the tprsh schema,
// one JSON object per line, from a reader. It is the reference Sensor: it
// drives contract fixtures and pipelines in tests, and replays a recorded
// sensor log. It performs no translation, so the events it emits are exactly
// as recorded; Record still validates and redacts them.
type Replay struct {
	r      io.Reader
	errors atomic.Int64
}

// NewReplay returns a Replay reading JSONL events from r.
func NewReplay(r io.Reader) *Replay { return &Replay{r: r} }

// Name implements Sensor.
func (*Replay) Name() string { return "replay" }

// Errors returns the number of lines that could not be decoded as an event.
func (p *Replay) Errors() int { return int(p.errors.Load()) }

// Run implements Sensor. Blank lines are ignored; malformed or oversized lines
// are counted and skipped. Cancellation is checked between lines, so a read
// blocked on a slow reader returns only when that read does.
func (p *Replay) Run(ctx context.Context, out gateway.Sink) error {
	br := bufio.NewReaderSize(p.r, 64<<10)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		line, tooLong, err := ReadLine(br, MaxReplayLineBytes)
		if tooLong {
			p.errors.Add(1)
		} else if len(bytes.TrimSpace(line)) > 0 {
			var e event.Event
			if jerr := json.Unmarshal(line, &e); jerr != nil {
				p.errors.Add(1)
			} else {
				_ = out.Emit(e)
			}
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// ReadLine returns the next line from br without its terminator, for
// adapters that read line-delimited native records. When the line exceeds
// limit bytes it is consumed and discarded, and tooLong is set, so memory
// stays bounded. err is io.EOF after the final line.
func ReadLine(br *bufio.Reader, limit int) (line []byte, tooLong bool, err error) {
	var buf []byte
	for {
		chunk, rerr := br.ReadSlice('\n')
		if !tooLong {
			if len(buf)+len(chunk) > limit+1 { // +1 for the newline
				tooLong, buf = true, nil
			} else {
				buf = append(buf, chunk...)
			}
		}
		if errors.Is(rerr, bufio.ErrBufferFull) {
			continue
		}
		return bytes.TrimRight(buf, "\r\n"), tooLong, rerr
	}
}
