package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/c4rb0nx1/tuprwre/internal/rules"
)

var (
	gatewayLog = filepath.Join("..", "..", "internal", "report", "testdata", "gateway.jsonl")
	sensorLog  = filepath.Join("..", "..", "internal", "sensor", "tetragon", "testdata", "session.expected.jsonl")
)

func TestParseFlags(t *testing.T) {
	opts, err := parseFlags([]string{"--json", "--fail-on", "red", "--workspace", "/w", "--window", "1m", "a.jsonl", "-"}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if !opts.json || opts.failOn != rules.Red || opts.workspace != "/w" || opts.window != time.Minute ||
		strings.Join(opts.files, ",") != "a.jsonl,-" {
		t.Errorf("opts = %+v", opts)
	}
	for _, bad := range [][]string{nil, {"--fail-on", "green", "x"}, {"--window", "0s", "x"}, {"--slack", "-1s", "x"}} {
		if _, err := parseFlags(bad, &bytes.Buffer{}); err == nil {
			t.Errorf("parseFlags(%q) accepted", bad)
		}
	}
}

func TestRunText(t *testing.T) {
	var out bytes.Buffer
	code, err := run(&options{files: []string{gatewayLog, sensorLog}, window: time.Minute, slack: 2 * time.Second}, nil, &out)
	if err != nil || code != 0 {
		t.Fatalf("code = %d err = %v", code, err)
	}
	for _, want := range []string{"== session sess-tg-1", "covert candidates (5)", "[UNMATCHED]", "<- toolu_01 (command-line)"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q", want)
		}
	}
}

func TestRunFailOnAndStdin(t *testing.T) {
	cases := []struct {
		failOn rules.Tier
		input  string
		code   int
	}{
		{rules.Red, "", exitFailOn},
		{rules.Yellow, "", exitFailOn},
		{rules.Red, `{"version":"0","id":"g","time":"2026-09-21T09:00:00Z","session_id":"s","source":"gateway","kind":"tool_call_intent","tool_call_id":"c","tool_name":"Bash","arguments":{"command":"ls"},"complete":true}`, 0},
	}
	for _, c := range cases {
		files := []string{gatewayLog}
		if c.input != "" {
			files = []string{"-"}
		}
		var out bytes.Buffer
		code, err := run(&options{files: files, json: true, failOn: c.failOn, window: time.Minute, slack: time.Second},
			strings.NewReader(c.input), &out)
		if err != nil || code != c.code {
			t.Errorf("fail-on %s: code = %d err = %v", c.failOn, code, err)
		}
		if !json.Valid(out.Bytes()) {
			t.Errorf("fail-on %s: invalid JSON output", c.failOn)
		}
	}
}

func TestRunMissingFile(t *testing.T) {
	if _, err := run(&options{files: []string{filepath.Join(t.TempDir(), "nope.jsonl")}}, nil, &bytes.Buffer{}); err == nil {
		t.Error("missing file accepted")
	}
}

func TestParseFlagsRulesAndPrecision(t *testing.T) {
	opts, err := parseFlags([]string{"--protected-branch", "stable", "--protected-branch", "hotfix/*", "--prod-context", "live",
		"--ignore-path", "/home/a/.cache", "--taint-window", "5m", "x"}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(opts.rules.ProtectedBranches, ",") != "stable,hotfix/*" || strings.Join(opts.rules.ProdContexts, ",") != "live" ||
		strings.Join(opts.ignore, ",") != "/home/a/.cache" || opts.taint != 5*time.Minute {
		t.Errorf("opts = %+v", opts)
	}
	defaults, err := parseFlags([]string{"x"}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if len(defaults.rules.ProtectedBranches) != len(rules.DefaultConfig.ProtectedBranches) {
		t.Errorf("default branches = %v", defaults.rules.ProtectedBranches)
	}
	if _, err := parseFlags([]string{"--ignore-path", "~/.cache", "x"}, &bytes.Buffer{}); err == nil {
		t.Error("relative ignore path accepted")
	}
	if _, err := parseFlags([]string{"--taint-window", "-1s", "x"}, &bytes.Buffer{}); err == nil {
		t.Error("negative taint window accepted")
	}

	// A custom protected branch changes the verdict end to end.
	var out bytes.Buffer
	opts.files = []string{gatewayLog}
	opts.json = true
	if _, err := run(opts, nil, &out); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), rules.RuleGitForcePushProtected) {
		t.Error("main still protected with --protected-branch stable")
	}
}

// TestRunWithClassifier drives the --classifier path end to end against a
// fake System One server, including fail-open on a dead endpoint.
func TestRunWithClassifier(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fake-key" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var req struct {
			Questions map[string]json.RawMessage `json:"questions"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		answers := map[string]any{}
		for id := range req.Questions {
			answers[id] = map[string]any{"type": "noul", "noul": 0.9}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"answers": answers})
	}))
	defer srv.Close()
	t.Setenv(classifierKeyEnv, "fake-key")

	base := func(url string) *options {
		o, err := parseFlags([]string{"--json", "--classifier", url, gatewayLog, sensorLog}, &bytes.Buffer{})
		if err != nil {
			t.Fatal(err)
		}
		return o
	}
	var out bytes.Buffer
	if _, err := run(base(srv.URL), nil, &out); err != nil {
		t.Fatal(err)
	}
	var rep struct {
		Review struct {
			Asked, Flagged, Errors int
		} `json:"classifier_review"`
	}
	if err := json.Unmarshal(out.Bytes(), &rep); err != nil {
		t.Fatal(err)
	}
	if rep.Review.Asked != 7 || rep.Review.Flagged != 7 || rep.Review.Errors != 0 {
		t.Errorf("review = %+v", rep.Review)
	}

	// A dead endpoint fails open: the report is still produced.
	dead := httptest.NewServer(http.NotFoundHandler())
	deadURL := dead.URL
	dead.Close()
	out.Reset()
	code, err := run(base(deadURL), nil, &out)
	if err != nil || code != 0 || !json.Valid(out.Bytes()) || !strings.Contains(out.String(), `"errors": 7`) {
		t.Errorf("dead classifier: code %d err %v", code, err)
	}

	if _, err := run(base("https://kev.example.com"), nil, &out); err == nil {
		t.Error("remote classifier accepted without --classifier-allow-remote")
	}
	if _, err := parseFlags([]string{"--classifier", "http://127.0.0.1:1", "--classifier-threshold", "1.5", "x"}, &bytes.Buffer{}); err == nil {
		t.Error("threshold > 1 accepted")
	}
}
