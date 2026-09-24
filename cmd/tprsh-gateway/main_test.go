package main

import (
	"bytes"
	"strings"
	"testing"
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
