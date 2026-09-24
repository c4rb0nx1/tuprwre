package main

import (
	"bytes"
	"encoding/json"
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
