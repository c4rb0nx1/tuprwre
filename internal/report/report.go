package report

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/netip"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/c4rb0nx1/tuprwre/internal/event"
	"github.com/c4rb0nx1/tuprwre/internal/rules"
	"github.com/c4rb0nx1/tuprwre/internal/sensor"
)

// Defaults for Options.
const (
	DefaultWindow = 10 * time.Minute
	DefaultSlack  = 2 * time.Second
)

// NoSession labels events that carry no session id.
const NoSession = "(none)"

// Options configures Build.
type Options struct {
	// Workspace is the agent's workspace directory for every session. When
	// empty, each session's workspace is inferred from the working
	// directory of its root process; when that is unknown too, the rm rule
	// falls back to its conservative no-workspace mode.
	Workspace string
	// Window bounds how long after a tool call an effect can still be
	// attributed to it when no tool result was recorded. Default
	// DefaultWindow.
	Window time.Duration
	// Slack widens every attribution window on both sides to absorb clock
	// and recording skew between the gateway and the sensor. Default
	// DefaultSlack.
	Slack time.Duration
	// Rules configures the fixed rules (protected branches, prod
	// contexts). Nil means rules.DefaultConfig.
	Rules *rules.Config
	// IgnorePaths are directory prefixes whose file writes are expected
	// background activity (e.g. a harness's own state or cache
	// directories). Such writes are marked Ignored and are never covert
	// candidates. Credential reads are never ignored.
	IgnorePaths []string
	// TaintWindow bounds the credential-then-egress rule: egress is red
	// only within this long after the most recent credential read. Zero
	// means the rest of the session.
	TaintWindow time.Duration
}

// Report is the reconciled view of all sessions.
type Report struct {
	Load     LoadStats  `json:"load"`
	Sessions []*Session `json:"sessions"`
	// Review summarizes the optional classifier pass; nil when none ran.
	Review *ReviewStats `json:"classifier_review,omitempty"`
}

// Worst returns the most severe tier across all sessions.
func (r *Report) Worst() rules.Tier {
	worst := rules.Green
	for _, s := range r.Sessions {
		if s.Worst.Rank() > worst.Rank() {
			worst = s.Worst
		}
	}
	return worst
}

// Session groups one harness session's intents and effects.
type Session struct {
	ID string `json:"session_id"`
	// Workspace is the workspace the rules used; "" when unknown.
	Workspace string `json:"workspace,omitempty"`
	// WorkspaceInferred reports that Workspace came from the session's root
	// process rather than Options.
	WorkspaceInferred bool      `json:"workspace_inferred,omitempty"`
	Intents           []*Intent `json:"intents"`
	Effects           []*Effect `json:"effects"`
	// Covert lists the IDs of effects that no tool call explains, in time
	// order: candidates for covert actions.
	Covert []string `json:"covert_candidates"`
	// Tiers counts intents and effects per tier.
	Tiers map[rules.Tier]int `json:"tiers"`
	// Worst is the most severe tier of any intent or effect.
	Worst rules.Tier `json:"worst"`
}

// Intent is a tool call the model asked for.
type Intent struct {
	ID         string    `json:"id"`
	Time       time.Time `json:"time"`
	ToolCallID string    `json:"tool_call_id"`
	ToolName   string    `json:"tool_name"`
	// Summary is the command, path or compact arguments.
	Summary  string `json:"summary"`
	Complete bool   `json:"complete"`
	// ResultTime is when the harness returned the tool result, if seen.
	ResultTime    *time.Time `json:"result_time,omitempty"`
	ResultIsError bool       `json:"result_is_error,omitempty"`
	// Effects is the number of effects attributed to this call.
	Effects int `json:"effects"`
	rules.Verdict
	// Classifier is the optional classifier's view of the command.
	Classifier *Assessment `json:"classifier,omitempty"`
	// ResultClassifier is its view of the tool result (prompt injection).
	ResultClassifier *Assessment `json:"result_classifier,omitempty"`

	facts      intentFacts
	res        rules.Result
	resultText string
}

// Attribution explains why an effect is linked to a tool call.
type Attribution struct {
	ToolCallID string `json:"tool_call_id"`
	// How is one of: "command-line" (a process ran the call's command
	// line, e.g. sh -c), "binary" (a process ran a program the call
	// names), "descendant" (a descendant of an attributed process),
	// "process" (an effect of an attributed process), "path" (a file the
	// call names), "address" (an address the call names).
	How string `json:"how"`
}

// Effect is an observed host effect.
type Effect struct {
	ID      string     `json:"id"`
	Time    time.Time  `json:"time"`
	Kind    event.Kind `json:"kind"`
	PID     int        `json:"pid"`
	ExecID  string     `json:"exec_id,omitempty"`
	Summary string     `json:"summary"`
	// Attribution is nil when no tool call explains the effect.
	Attribution *Attribution `json:"attribution,omitempty"`
	// Root marks the exec of a session root process (normally the harness
	// itself), which no tool call is expected to explain.
	Root bool `json:"root,omitempty"`
	// Harness marks an effect of a root process.
	Harness bool `json:"harness,omitempty"`
	// Loopback marks a connection to a loopback address.
	Loopback bool `json:"loopback,omitempty"`
	// Ignored marks a file write under Options.IgnorePaths.
	Ignored bool `json:"ignored,omitempty"`
	// Covert marks a covert-action candidate.
	Covert bool `json:"covert,omitempty"`
	rules.Verdict
	// Classifier is the optional classifier's view of an exec's command.
	Classifier *Assessment `json:"classifier,omitempty"`

	ev  event.Event
	key string
	res rules.Result
}

// Build groups events by session, reconciles intents with effects and applies
// the rules.
func Build(events []event.Event, st LoadStats, opts Options) *Report {
	if opts.Window <= 0 {
		opts.Window = DefaultWindow
	}
	if opts.Slack <= 0 {
		opts.Slack = DefaultSlack
	}
	if opts.Rules == nil {
		opts.Rules = &rules.DefaultConfig
	}
	bySession := map[string][]event.Event{}
	first := map[string]time.Time{}
	for _, e := range events {
		id := e.SessionID
		if id == "" {
			id = NoSession
		}
		bySession[id] = append(bySession[id], e)
		if t, ok := first[id]; !ok || e.Time.Before(t) {
			first[id] = e.Time
		}
	}
	rep := &Report{Load: st}
	for id, evs := range bySession {
		rep.Sessions = append(rep.Sessions, buildSession(id, evs, opts))
	}
	sort.Slice(rep.Sessions, func(i, j int) bool {
		a, b := rep.Sessions[i], rep.Sessions[j]
		if !first[a.ID].Equal(first[b.ID]) {
			return first[a.ID].Before(first[b.ID])
		}
		return a.ID < b.ID
	})
	return rep
}

func buildSession(id string, evs []event.Event, opts Options) *Session {
	sort.SliceStable(evs, func(i, j int) bool { return evs[i].Time.Before(evs[j].Time) })
	s := &Session{ID: id, Tiers: map[rules.Tier]int{}, Covert: []string{}, Worst: rules.Green}

	results := map[string]event.Event{}
	for _, e := range evs {
		switch e.Kind {
		case event.KindToolCallIntent:
			facts := extractFacts(e.Arguments)
			s.Intents = append(s.Intents, &Intent{
				ID: e.ID, Time: e.Time, ToolCallID: e.ToolCallID, ToolName: e.ToolName,
				Summary: facts.summary(e.Arguments), Complete: e.Complete, facts: facts,
			})
		case event.KindToolResult:
			if _, ok := results[e.ToolCallID]; !ok {
				results[e.ToolCallID] = e
			}
		default:
			if e.Source == event.SourceSensor {
				s.Effects = append(s.Effects, &Effect{
					ID: e.ID, Time: e.Time, Kind: e.Kind, PID: e.Process.PID, ExecID: e.Process.ExecID,
					Summary: effectSummary(e), ev: e, key: sensor.ProcessKey(e.Process),
				})
			}
		}
	}
	for _, in := range s.Intents {
		if r, ok := results[in.ToolCallID]; ok && !r.Time.Before(in.Time) {
			t := r.Time
			in.ResultTime, in.ResultIsError = &t, r.IsError
			in.resultText = resultText(r.Result)
		}
	}

	tree := newProcessTree(s.Effects)
	s.Workspace, s.WorkspaceInferred = opts.Workspace, false
	if s.Workspace == "" {
		s.Workspace = tree.inferWorkspace()
		s.WorkspaceInferred = s.Workspace != ""
	}

	attribute(s, tree, opts)
	applyRules(s, opts)

	for _, in := range s.Intents {
		s.count(in.Tier)
	}
	for _, ef := range s.Effects {
		s.count(ef.Tier)
		if ef.Covert {
			s.Covert = append(s.Covert, ef.ID)
		}
	}
	return s
}

func (s *Session) count(t rules.Tier) {
	s.Tiers[t]++
	if t.Rank() > s.Worst.Rank() {
		s.Worst = t
	}
}

// processTree indexes a session's processes.
type processTree struct {
	parent map[string]string // key -> parent key
	execs  map[string]*Effect
	// roots are execs that start the session's process tree: an exec
	// whose parent was never exec'd here and has no other such child.
	roots map[string]bool
	// harness holds the keys of processes treated as the harness itself:
	// roots, and unseen parents with several orphan children (a
	// long-lived process that was already running when the sensor
	// started, e.g. a Tetragon procFS process).
	harness map[string]bool
	order   []*Effect // exec effects in time order
}

func newProcessTree(effects []*Effect) *processTree {
	t := &processTree{parent: map[string]string{}, execs: map[string]*Effect{}, roots: map[string]bool{}, harness: map[string]bool{}}
	for _, ef := range effects {
		if pk := sensor.ParentKey(ef.ev.Process); pk != "" {
			if _, ok := t.parent[ef.key]; !ok {
				t.parent[ef.key] = pk
			}
		}
		if ef.Kind == event.KindExec {
			if _, ok := t.execs[ef.key]; !ok {
				t.execs[ef.key] = ef
				t.order = append(t.order, ef)
			}
		}
	}
	// Orphans are execs whose parent was never exec'd in this session. A
	// lone orphan is the session's launch (normally the harness). Several
	// orphans of one unseen parent are children of a process that predates
	// the sensor; that parent is the harness and its children stay
	// ordinary processes, so they can still be covert candidates.
	orphans := map[string][]string{}
	for _, ef := range t.order {
		pk := t.parent[ef.key]
		if _, ok := t.execs[pk]; !ok {
			orphans[pk] = append(orphans[pk], ef.key)
		}
	}
	for pk, kids := range orphans {
		switch {
		case len(kids) == 1 || pk == "":
			for _, k := range kids {
				t.roots[k], t.harness[k] = true, true
			}
		default:
			t.harness[pk] = true
		}
	}
	return t
}

// inferWorkspace returns the working directory of the earliest root exec,
// or failing that of the earliest child of an unseen harness process.
func (t *processTree) inferWorkspace() string {
	for _, ef := range t.order {
		if t.roots[ef.key] && ef.ev.Process.Cwd != "" {
			return ef.ev.Process.Cwd
		}
	}
	for _, ef := range t.order {
		if t.harness[t.parent[ef.key]] && ef.ev.Process.Cwd != "" {
			return ef.ev.Process.Cwd
		}
	}
	return ""
}

// ancestors returns the keys above k, nearest first, bounded against cycles.
func (t *processTree) ancestors(k string) []string {
	var out []string
	seen := map[string]bool{k: true}
	for p := t.parent[k]; p != "" && !seen[p] && len(out) < 64; p = t.parent[p] {
		seen[p] = true
		out = append(out, p)
	}
	return out
}

// resultText renders a recorded tool result as plain text: a JSON string is
// unquoted, anything else is kept as compact JSON.
func resultText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return strings.TrimSpace(s)
	}
	var b bytes.Buffer
	if json.Compact(&b, raw) == nil {
		return b.String()
	}
	return string(raw)
}

// window is the span of time in which an intent's effects may occur.
func (in *Intent) window(opts Options) (time.Time, time.Time) {
	start := in.Time.Add(-opts.Slack)
	end := in.Time.Add(opts.Window)
	if in.ResultTime != nil {
		end = *in.ResultTime
	}
	return start, end.Add(opts.Slack)
}

// candidates returns intents whose window contains t, latest first.
func candidates(s *Session, t time.Time, opts Options) []*Intent {
	var out []*Intent
	for i := len(s.Intents) - 1; i >= 0; i-- {
		in := s.Intents[i]
		start, end := in.window(opts)
		if !t.Before(start) && !t.After(end) {
			out = append(out, in)
		}
	}
	return out
}

// attribute links effects to tool calls.
func attribute(s *Session, tree *processTree, opts Options) {
	byKey := map[string]*Attribution{}
	byCall := map[string]*Intent{}
	for _, in := range s.Intents {
		byCall[in.ToolCallID] = in
	}
	inherited := func(k string) *Attribution {
		for _, a := range tree.ancestors(k) {
			if at, ok := byKey[a]; ok {
				return &Attribution{ToolCallID: at.ToolCallID, How: "descendant"}
			}
		}
		return nil
	}

	// Executions first, in time order, so descendants inherit.
	for _, ef := range tree.order {
		at := inherited(ef.key)
		if at == nil {
			at = matchExec(ef, candidates(s, ef.Time, opts))
		}
		if at != nil {
			byKey[ef.key] = at
			ef.Attribution = at
		}
	}
	for _, ef := range s.Effects {
		switch {
		case ef.Kind == event.KindExec:
			// Done above.
		case byKey[ef.key] != nil:
			ef.Attribution = &Attribution{ToolCallID: byKey[ef.key].ToolCallID, How: "process"}
		default:
			if at := inherited(ef.key); at != nil {
				ef.Attribution = at
			} else {
				ef.Attribution = matchByMention(ef, candidates(s, ef.Time, opts))
			}
		}
	}
	for _, ef := range s.Effects {
		ef.Root = ef.Kind == event.KindExec && tree.roots[ef.key] && ef.Attribution == nil
		ef.Harness = tree.harness[ef.key] && ef.Kind != event.KindExec
		ef.Loopback = ef.Kind == event.KindNetConnect && isLoopback(ef.ev.Net.DstAddr)
		ef.Ignored = ef.Kind == event.KindFileWrite && ignoredPath(ef.ev.File.Path, opts.IgnorePaths)
		ef.Covert = ef.Attribution == nil && !ef.Root && !ef.Loopback && !ef.Ignored && ef.Kind != event.KindProcExit
		if ef.Attribution != nil {
			if in := byCall[ef.Attribution.ToolCallID]; in != nil {
				in.Effects++
			}
		}
	}
}

// matchExec links an execution to the latest candidate intent that ran its
// command line or names its program.
func matchExec(ef *Effect, cands []*Intent) *Attribution {
	p := ef.ev.Process
	line := strings.Fields(strings.Join(p.Argv, " "))
	for _, in := range cands {
		for _, cl := range in.facts.commandLines() {
			if containsTokens(line, strings.Fields(cl)) {
				return &Attribution{ToolCallID: in.ToolCallID, How: "command-line"}
			}
		}
	}
	names := []string{path.Base(p.Binary)}
	if len(p.Argv) > 0 {
		names = append(names, path.Base(p.Argv[0]))
	}
	for _, in := range cands {
		bins := in.facts.binaries()
		for _, n := range names {
			if n != "" && n != "." && n != "/" && bins[n] {
				return &Attribution{ToolCallID: in.ToolCallID, How: "binary"}
			}
		}
	}
	return nil
}

// containsTokens reports whether sub occurs as a contiguous run in words.
func containsTokens(words, sub []string) bool {
	if len(sub) == 0 {
		return false
	}
	for i := 0; i+len(sub) <= len(words); i++ {
		match := true
		for j := range sub {
			if words[i+j] != sub[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// matchByMention links a file or network effect to the latest candidate
// intent that names its path or address.
func matchByMention(ef *Effect, cands []*Intent) *Attribution {
	switch ef.Kind {
	case event.KindFileWrite, event.KindFileReadSensitive:
		target := ef.ev.File.Path
		for _, in := range cands {
			for _, p := range in.facts.paths {
				if path.Clean(p) == target || (!strings.HasPrefix(p, "/") && strings.HasSuffix(target, "/"+strings.TrimPrefix(path.Clean(p), "~/"))) {
					return &Attribution{ToolCallID: in.ToolCallID, How: "path"}
				}
			}
			for _, sc := range in.facts.scripts {
				if strings.Contains(sc, target) {
					return &Attribution{ToolCallID: in.ToolCallID, How: "path"}
				}
			}
		}
	case event.KindNetConnect:
		addr := ef.ev.Net.DstAddr
		for _, in := range cands {
			for _, s := range in.facts.strs {
				if strings.Contains(s, addr) {
					return &Attribution{ToolCallID: in.ToolCallID, How: "address"}
				}
			}
		}
	}
	return nil
}

// applyRules applies the fixed rules, then the cross-event rule: after a
// credential read, later egress is red (within Options.TaintWindow, when set).
func applyRules(s *Session, opts Options) {
	type item struct {
		t        time.Time
		res      *rules.Result
		verdict  *rules.Verdict
		credRead bool
		egress   bool
	}
	var items []item
	for _, in := range s.Intents {
		in.res = in.facts.classify(opts.Rules, s.Workspace)
		in.Verdict = in.res.Verdict
		items = append(items, item{in.Time, &in.res, &in.Verdict, len(in.res.CredentialPaths) > 0, in.res.Egress})
	}
	for _, ef := range s.Effects {
		ef.res = classifyEffect(ef, opts.Rules, s.Workspace)
		ef.Verdict = ef.res.Verdict
		egress := ef.res.Egress || (ef.Kind == event.KindNetConnect && !ef.Loopback)
		items = append(items, item{ef.Time, &ef.res, &ef.Verdict, len(ef.res.CredentialPaths) > 0, egress})
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].t.Before(items[j].t) })

	// first and last are the earliest and the most recent credential reads
	// seen so far. Without a window the whole rest of the session is
	// tainted and the first read is cited; with one, only egress within
	// the window after the most recent read is.
	var first, last *item
	for i := range items {
		it := &items[i]
		if last != nil && it.egress && it.t.After(last.t) {
			cause := first
			if opts.TaintWindow > 0 {
				cause = last
			}
			if opts.TaintWindow <= 0 || it.t.Sub(last.t) <= opts.TaintWindow {
				*it.verdict = rules.Worse(*it.verdict, rules.Verdict{Tier: rules.Red, Rule: rules.RuleCredentialThenEgress,
					Reason: "network egress after credential read of " + cause.res.CredentialPaths[0] + " at " + cause.t.UTC().Format(timeFormat)})
			}
		}
		if it.credRead {
			if first == nil {
				first = it
			}
			last = it
		}
	}
}

// ignoredPath reports whether p lies under one of the prefixes.
func ignoredPath(p string, prefixes []string) bool {
	for _, pre := range prefixes {
		pre = strings.TrimSuffix(path.Clean(pre), "/")
		if pre != "" && pre != "." && (p == pre || strings.HasPrefix(p, pre+"/")) {
			return true
		}
	}
	return false
}

// classifyEffect applies the rules to one effect.
func classifyEffect(ef *Effect, cfg *rules.Config, workspace string) rules.Result {
	switch ef.Kind {
	case event.KindExec:
		p := ef.ev.Process
		return cfg.Classify(rules.Command{Argv: p.Argv, Cwd: p.Cwd}, workspace)
	case event.KindFileReadSensitive:
		return rules.Result{
			Verdict:         rules.Verdict{Tier: rules.Yellow, Rule: rules.RuleCredentialRead, Reason: "read of credential file " + ef.ev.File.Path},
			CredentialPaths: []string{ef.ev.File.Path},
		}
	}
	return rules.Result{Verdict: rules.GreenVerdict}
}

func isLoopback(addr string) bool {
	a, err := netip.ParseAddr(addr)
	return err == nil && a.Unmap().IsLoopback()
}

func isSensitive(p string) bool { return sensor.IsSensitivePath(p) }

// effectSummary is a one-line description of an effect.
func effectSummary(e event.Event) string {
	p := e.Process
	prog := path.Base(p.Binary)
	if prog == "." || prog == "/" {
		prog = ""
	}
	switch e.Kind {
	case event.KindExec:
		args := p.Argv
		if len(args) > 0 {
			args = args[1:]
		}
		return strings.TrimSpace(p.Binary + " " + strings.Join(args, " "))
	case event.KindFileWrite, event.KindFileReadSensitive:
		return strings.TrimSpace(prog + " " + e.File.Path)
	case event.KindNetConnect:
		return strings.TrimSpace(fmt.Sprintf("%s %s %s", prog, e.Net.Protocol, netip.AddrPortFrom(mustAddr(e.Net.DstAddr), uint16(e.Net.DstPort))))
	case event.KindProcExit:
		if e.Exit.Signal != "" {
			return strings.TrimSpace(prog + " killed by " + e.Exit.Signal)
		}
		return strings.TrimSpace(fmt.Sprintf("%s exit %d", prog, *e.Exit.Code))
	}
	return prog
}

func mustAddr(s string) netip.Addr {
	a, _ := netip.ParseAddr(s)
	return a
}
