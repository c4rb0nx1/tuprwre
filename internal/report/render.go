package report

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/c4rb0nx1/tuprwre/internal/rules"
)

// timeFormat is the clock format used in text output and reasons (UTC).
const timeFormat = "15:04:05.000"

// maxSummary truncates long commands in text output.
const maxSummary = 100

// WriteJSON writes the report as indented JSON.
func WriteJSON(w io.Writer, r *Report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(r)
}

// WriteText writes a human-readable report.
func WriteText(w io.Writer, r *Report) error {
	tw := &textWriter{w: w}
	l := r.Load
	tw.printf("tprsh-report: %d events, %d sessions (%d duplicate, %d malformed, %d invalid effect, %d ignored)\n",
		l.Events, len(r.Sessions), l.Duplicates, l.Malformed, l.InvalidEffects, l.Ignored)
	tw.printf("tiers are would-be verdicts of the fixed rule set; nothing was blocked\n")

	for _, s := range r.Sessions {
		tw.printf("\n== session %s\n", s.ID)
		ws := s.Workspace
		switch {
		case ws == "":
			ws = "unknown"
		case s.WorkspaceInferred:
			ws += " (inferred)"
		}
		tw.printf("   workspace %s\n", ws)
		tw.printf("   worst %s   red %d  yellow %d  green %d   covert candidates %d\n",
			label(s.Worst), s.Tiers[rules.Red], s.Tiers[rules.Yellow], s.Tiers[rules.Green], len(s.Covert))

		tw.printf("\n   tool intents (%d)\n", len(s.Intents))
		if len(s.Intents) == 0 {
			tw.printf("     none recorded (no gateway log for this session?)\n")
		}
		for _, in := range s.Intents {
			status := ""
			switch {
			case !in.Complete:
				status = "  [incomplete]"
			case in.ResultTime == nil:
				status = "  [no result]"
			case in.ResultIsError:
				status = "  [error result]"
			}
			tw.printf("     %s  %-6s  %s %s: %s  (effects %d)%s\n", clock(in.Time), label(in.Tier), in.ToolCallID,
				in.ToolName, truncate(in.Summary), in.Effects, status)
			tw.reason(in.Verdict)
		}

		tw.printf("\n   effects (%d)\n", len(s.Effects))
		if len(s.Effects) == 0 {
			tw.printf("     none recorded (no sensor log for this session?)\n")
		}
		for _, ef := range s.Effects {
			line := fmt.Sprintf("     %s  %-6s  %-19s pid %-6d %s  %s", clock(ef.Time), label(ef.Tier), ef.Kind, ef.PID,
				truncate(ef.Summary), link(ef))
			tw.printf("%s\n", strings.TrimRight(line, " "))
			tw.reason(ef.Verdict)
		}

		tw.printf("\n   covert candidates (%d): effects no tool call explains\n", len(s.Covert))
		if len(s.Intents) == 0 && len(s.Covert) > 0 {
			tw.printf("     (no tool intents recorded for this session, so every non-root effect is unmatched)\n")
		}
		byID := map[string]*Effect{}
		for _, ef := range s.Effects {
			byID[ef.ID] = ef
		}
		for _, id := range s.Covert {
			ef := byID[id]
			who := ""
			if ef.Harness {
				who = "  (by the harness process)"
			}
			tw.printf("     %s  %-6s  %-19s pid %-6d %s%s\n", clock(ef.Time), label(ef.Tier), ef.Kind, ef.PID, truncate(ef.Summary), who)
		}
	}
	return tw.err
}

type textWriter struct {
	w   io.Writer
	err error
}

func (t *textWriter) printf(format string, args ...any) {
	if t.err == nil {
		_, t.err = fmt.Fprintf(t.w, format, args...)
	}
}

func (t *textWriter) reason(v rules.Verdict) {
	if v.Tier != rules.Green {
		t.printf("                                 %s: %s\n", v.Rule, v.Reason)
	}
}

func link(ef *Effect) string {
	switch {
	case ef.Attribution != nil:
		return "<- " + ef.Attribution.ToolCallID + " (" + ef.Attribution.How + ")"
	case ef.Root:
		return "[session root]"
	case ef.Loopback:
		return "[loopback]"
	case ef.Covert:
		return "[UNMATCHED]"
	}
	return ""
}

func label(t rules.Tier) string {
	switch t {
	case rules.Red:
		return "RED"
	case rules.Yellow:
		return "YELLOW"
	}
	return "green"
}

func clock(t time.Time) string { return t.UTC().Format(timeFormat) }

func truncate(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if r := []rune(s); len(r) > maxSummary {
		return string(r[:maxSummary-1]) + "…"
	}
	return s
}
