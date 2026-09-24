package main

import (
	"bytes"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/c4rb0nx1/tuprwre/internal/gateway/proxy"
)

func TestParseFlagsDefaults(t *testing.T) {
	var stderr bytes.Buffer
	opts, err := parseFlags([]string{"--upstream", "https://api.anthropic.com"}, &stderr)
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	if opts.listen != "127.0.0.1:0" {
		t.Errorf("listen = %q, want 127.0.0.1:0", opts.listen)
	}
	if opts.upstream.String() != "https://api.anthropic.com" {
		t.Errorf("upstream = %q", opts.upstream)
	}
	if opts.sessionID == "" {
		t.Error("sessionID is empty, want a generated value")
	}
	if opts.noRedact || opts.allowRemote {
		t.Errorf("noRedact=%v allowRemote=%v, want both false", opts.noRedact, opts.allowRemote)
	}
	if stderr.Len() != 0 {
		t.Errorf("unexpected stderr: %q", stderr.String())
	}
}

func TestParseFlagsRequiresUpstream(t *testing.T) {
	if _, err := parseFlags(nil, &bytes.Buffer{}); err == nil {
		t.Fatal("expected an error when --upstream is missing")
	}
}

func TestParseFlagsRejectsBadUpstream(t *testing.T) {
	for _, upstream := range []string{"ftp://example.com", "not a url", "https://"} {
		t.Run(upstream, func(t *testing.T) {
			var stderr bytes.Buffer
			if _, err := parseFlags([]string{"--upstream", upstream}, &stderr); err == nil {
				t.Fatalf("expected an error for --upstream %q", upstream)
			}
		})
	}
}

func TestParseFlagsRefusesRemoteListen(t *testing.T) {
	for _, listen := range []string{"0.0.0.0:8080", ":8080", "192.0.2.1:9000", "[::]:8080"} {
		t.Run(listen, func(t *testing.T) {
			_, err := parseFlags([]string{"--upstream", "http://127.0.0.1:1", "--listen", listen}, &bytes.Buffer{})
			if err == nil {
				t.Fatalf("expected an error for non-loopback --listen %q", listen)
			}
		})
	}
}

func TestParseFlagsAcceptsLoopbackListen(t *testing.T) {
	for _, listen := range []string{"127.0.0.1:0", "localhost:8080", "[::1]:0"} {
		t.Run(listen, func(t *testing.T) {
			opts, err := parseFlags([]string{"--upstream", "http://127.0.0.1:1", "--listen", listen}, &bytes.Buffer{})
			if err != nil {
				t.Fatalf("parseFlags: %v", err)
			}
			if opts.listen != listen {
				t.Errorf("listen = %q, want %q", opts.listen, listen)
			}
		})
	}
}

func TestParseFlagsAllowRemote(t *testing.T) {
	opts, err := parseFlags([]string{"--upstream", "http://127.0.0.1:1", "--listen", "0.0.0.0:8080", "--allow-remote"}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	if !opts.allowRemote {
		t.Error("allowRemote = false, want true")
	}
}

func TestParseFlagsRejectsBadListen(t *testing.T) {
	if _, err := parseFlags([]string{"--upstream", "http://127.0.0.1:1", "--listen", "127.0.0.1"}, &bytes.Buffer{}); err == nil {
		t.Fatal("expected an error for a --listen without a port")
	}
}

func TestParseFlagsNoRedactWarns(t *testing.T) {
	var stderr bytes.Buffer
	opts, err := parseFlags([]string{"--upstream", "http://127.0.0.1:1", "--no-redact"}, &stderr)
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	if !opts.noRedact {
		t.Error("noRedact = false, want true")
	}
	if !strings.Contains(stderr.String(), "redaction disabled") {
		t.Errorf("expected a redaction warning on stderr, got %q", stderr.String())
	}
}

func TestParseFlagsDefaultsLogPath(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	var stderr bytes.Buffer
	opts, err := parseFlags([]string{"--upstream", "http://127.0.0.1:1"}, &stderr)
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	want := filepath.Join(state, "tprsh", "gateway", opts.sessionID+".jsonl")
	if opts.logPath != want {
		t.Errorf("logPath = %q, want %q", opts.logPath, want)
	}
	if !opts.logDefaulted {
		t.Error("logDefaulted = false, want true for a defaulted path")
	}
	if stderr.Len() != 0 {
		t.Errorf("unexpected stderr: %q", stderr.String())
	}
}

func TestDefaultLogPathFallback(t *testing.T) {
	got, err := defaultLogPath("", "sess-1")
	if err != nil {
		t.Fatalf("defaultLogPath: %v", err)
	}
	suffix := filepath.Join(".local", "state", "tprsh", "gateway", "sess-1.jsonl")
	if !strings.HasSuffix(got, suffix) {
		t.Errorf("defaultLogPath = %q, want suffix %q", got, suffix)
	}
}

func TestDefaultLogPathUsesStateHome(t *testing.T) {
	got, err := defaultLogPath("/state", "abc")
	if err != nil {
		t.Fatalf("defaultLogPath: %v", err)
	}
	want := filepath.Join("/state", "tprsh", "gateway", "abc.jsonl")
	if got != want {
		t.Errorf("defaultLogPath = %q, want %q", got, want)
	}
}

func TestParseFlagsExplicitLogPreserved(t *testing.T) {
	var stderr bytes.Buffer
	opts, err := parseFlags([]string{"--upstream", "http://127.0.0.1:1", "--log", "/tmp/x.jsonl"}, &stderr)
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	if opts.logPath != "/tmp/x.jsonl" {
		t.Errorf("logPath = %q, want /tmp/x.jsonl", opts.logPath)
	}
	if opts.logDefaulted {
		t.Error("logDefaulted = true, want false for an explicit path")
	}
}

func TestParseFlagsNoLogDisablesRecording(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	var stderr bytes.Buffer
	opts, err := parseFlags([]string{"--upstream", "http://127.0.0.1:1", "--no-log"}, &stderr)
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	if opts.logPath != "" {
		t.Errorf("logPath = %q, want empty", opts.logPath)
	}
	if !opts.noLog {
		t.Error("noLog = false, want true")
	}
	if !strings.Contains(stderr.String(), "recording disabled") {
		t.Errorf("expected a recording-disabled warning on stderr, got %q", stderr.String())
	}
}

func TestParseFlagsNoLogConflictsWithLog(t *testing.T) {
	_, err := parseFlags([]string{"--upstream", "http://127.0.0.1:1", "--no-log", "--log", "/tmp/x.jsonl"}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected an error when both --no-log and --log are set")
	}
}

func TestOpenSinkCreatesDir0700(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "gateway")
	path := filepath.Join(dir, "s.jsonl")
	sink, err := openSink(path, true, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("openSink: %v", err)
	}
	defer sink.Close()
	assertPerm(t, dir, 0o700)
	assertPerm(t, path, 0o600)
}

func TestOpenSinkTightensLooseFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	var stderr bytes.Buffer
	sink, err := openSink(path, false, &stderr)
	if err != nil {
		t.Fatalf("openSink: %v", err)
	}
	defer sink.Close()
	assertPerm(t, path, 0o600)
	if !strings.Contains(stderr.String(), "tightening log file") {
		t.Errorf("expected a tightening warning, got %q", stderr.String())
	}
}

func TestOpenSinkTightensManagedDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "gateway")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	path := filepath.Join(dir, "s.jsonl")
	var stderr bytes.Buffer
	sink, err := openSink(path, true, &stderr)
	if err != nil {
		t.Fatalf("openSink: %v", err)
	}
	defer sink.Close()
	assertPerm(t, dir, 0o700)
	if !strings.Contains(stderr.String(), "tightening directory") {
		t.Errorf("expected a directory tightening warning, got %q", stderr.String())
	}
}

func TestOpenSinkLeavesExplicitDirAlone(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "shared")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	path := filepath.Join(dir, "s.jsonl")
	var stderr bytes.Buffer
	sink, err := openSink(path, false, &stderr)
	if err != nil {
		t.Fatalf("openSink: %v", err)
	}
	defer sink.Close()
	assertPerm(t, dir, 0o755)
}

func TestCloseBoundedReturns(t *testing.T) {
	up, err := url.Parse("http://127.0.0.1:1")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	p, err := proxy.New(proxy.Config{Upstream: up})
	if err != nil {
		t.Fatalf("proxy.New: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- closeBounded(p, 5*time.Second) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("closeBounded: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("closeBounded did not return")
	}
}

func assertPerm(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Errorf("%s mode = %04o, want %04o", path, got, want)
	}
}

func TestParseFlagsRandomSessionIDsDiffer(t *testing.T) {
	a, err := parseFlags([]string{"--upstream", "http://127.0.0.1:1"}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	b, err := parseFlags([]string{"--upstream", "http://127.0.0.1:1"}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	if a.sessionID == b.sessionID {
		t.Errorf("two generated session ids are equal: %q", a.sessionID)
	}
}
