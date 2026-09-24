// Package report reconciles recorded intent (gateway tool calls) with
// recorded effects (sensor events) per session, flags effects that no tool
// call explains (covert-action candidates), and applies the fixed rule set in
// internal/rules to say which tier each item would have landed in.
//
// It reads only the tprsh event schema, never a sensor's or harness's native
// format, and works on gateway logs alone, sensor logs alone, or both.
package report

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"

	"github.com/c4rb0nx1/tuprwre/internal/event"
	"github.com/c4rb0nx1/tuprwre/internal/sensor"
)

// maxLineBytes bounds one JSONL record.
const maxLineBytes = 16 << 20

// LoadStats counts what Load read and discarded.
type LoadStats struct {
	// Events is the number of distinct events kept.
	Events int `json:"events"`
	// Duplicates is the number of records dropped because an event with the
	// same ID was already loaded (e.g. the same log passed twice, or a
	// sensor export replayed).
	Duplicates int `json:"duplicates"`
	// Malformed is the number of lines that were not a JSON event, or were
	// too long.
	Malformed int `json:"malformed"`
	// InvalidEffects is the number of sensor events that violate the effect
	// contract (sensor.Validate) and were dropped.
	InvalidEffects int `json:"invalid_effects"`
	// Ignored is the number of well-formed events of kinds the report does
	// not use.
	Ignored int `json:"ignored"`
}

// Load reads JSONL event logs (gateway and sensor logs may be mixed, in any
// order) and returns the distinct events it can use. Blank lines are skipped;
// malformed records are counted, never fatal. It fails only when a reader
// fails.
func Load(readers ...io.Reader) ([]event.Event, LoadStats, error) {
	var (
		st     LoadStats
		out    []event.Event
		seenID = map[string]bool{}
	)
	for _, r := range readers {
		br := bufio.NewReaderSize(r, 64<<10)
		for {
			line, tooLong, err := sensor.ReadLine(br, maxLineBytes)
			if tooLong {
				st.Malformed++
			} else if len(bytes.TrimSpace(line)) > 0 {
				var e event.Event
				switch jerr := json.Unmarshal(line, &e); {
				case jerr != nil || e.ID == "":
					st.Malformed++
				case seenID[e.ID]:
					st.Duplicates++
				default:
					seenID[e.ID] = true
					if keep, invalid := usable(e); keep {
						out = append(out, e)
					} else if invalid {
						st.InvalidEffects++
					} else {
						st.Ignored++
					}
				}
			}
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return nil, st, err
			}
		}
	}
	st.Events = len(out)
	return out, st, nil
}

// usable reports whether the report uses e, and whether it was rejected as
// an invalid effect.
func usable(e event.Event) (keep, invalid bool) {
	switch {
	case e.Source == event.SourceSensor:
		if sensor.Validate(e) != nil {
			return false, true
		}
		return true, false
	case e.Kind == event.KindToolCallIntent, e.Kind == event.KindToolResult:
		return true, false
	}
	return false, false
}
