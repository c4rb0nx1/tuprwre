package gateway

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/c4rb0nx1/tuprwre/internal/event"
)

// fakeSecret builds a credential-shaped string without embedding a real secret
// literal in the source.
func fakeSecret(prefix, body string) string { return prefix + body }

func TestDefaultRedactorPatterns(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "aws access key id",
			in:   "key=" + fakeSecret("AKIA", "IOSFODNN7EXAMPL1"),
			want: "[REDACTED:aws_access_key_id]",
		},
		{
			name: "aws secret access key",
			in:   `aws_secret_access_key = ` + strings.Repeat("a", 40),
			want: "[REDACTED:aws_secret_access_key]",
		},
		{
			name: "private key pem",
			in:   "-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAAKCAQEA\n-----END RSA PRIVATE KEY-----",
			want: "[REDACTED:private_key]",
		},
		{
			name: "bearer token",
			in:   "Authorization: Bearer " + strings.Repeat("Z", 24),
			want: "[REDACTED:bearer]",
		},
		{
			name: "sk api key",
			in:   fakeSecret("sk-live-", strings.Repeat("A", 20)),
			want: "[REDACTED:api_key]",
		},
		{
			name: "sk-ant api key",
			in:   fakeSecret("sk-ant-api03-", strings.Repeat("B", 20)),
			want: "[REDACTED:api_key]",
		},
		{
			name: "github token",
			in:   fakeSecret("ghp_", strings.Repeat("C", 36)),
			want: "[REDACTED:github_token]",
		},
		{
			name: "github pat",
			in:   fakeSecret("github_pat_", strings.Repeat("D", 30)),
			want: "[REDACTED:github_token]",
		},
		{
			name: "slack token",
			in:   fakeSecret("xoxb-", "123456789012-abcdefghijkl"),
			want: "[REDACTED:slack_token]",
		},
		{
			name: "jwt",
			in:   "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c",
			want: "[REDACTED:jwt]",
		},
		{
			name: "generic assignment",
			in:   "password = hunter2",
			want: "[REDACTED:credential]",
		},
		{
			name: "generic token assignment",
			in:   "export token=abc123xyz",
			want: "[REDACTED:credential]",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, n := redactString(c.in)
			if n == 0 {
				t.Fatalf("no redaction applied to %q", c.in)
			}
			if !strings.Contains(got, c.want) {
				t.Errorf("redacted = %q, want it to contain %q", got, c.want)
			}
		})
	}
}

func TestDefaultRedactorLeavesCleanTextAlone(t *testing.T) {
	in := `{"command":"go test ./...","note":"all green"}`
	if out, n := redactString(in); n != 0 || out != in {
		t.Errorf("clean text altered: n=%d out=%q", n, out)
	}
}

func TestDefaultRedactorJSONFieldNames(t *testing.T) {
	e := event.New(event.SourceGateway, event.KindToolResult, time.Now())
	e.Result = json.RawMessage(`{"password":"hunter2","api_key":"abcd","authorization":"Bearer x","safe":"keep"}`)

	got := DefaultRedactor(e)
	if !got.Redacted || got.RedactionCount == 0 {
		t.Fatalf("expected redaction: %+v", got)
	}
	var m map[string]string
	if err := json.Unmarshal(got.Result, &m); err != nil {
		t.Fatalf("result is not valid JSON after redaction: %v (%s)", err, got.Result)
	}
	for _, k := range []string{"password", "api_key", "authorization"} {
		if !strings.Contains(m[k], "[REDACTED:") {
			t.Errorf("field %q = %q, want redacted", k, m[k])
		}
	}
	if m["safe"] != "keep" {
		t.Errorf("non-secret field altered: %q", m["safe"])
	}
}

func TestDefaultRedactorPreservesValidJSONStructure(t *testing.T) {
	e := event.New(event.SourceGateway, event.KindToolCallIntent, time.Now())
	e.Arguments = json.RawMessage(`{"command":"aws configure","env":{"password":"hunter2","n":7},"list":["ok",1,true]}`)

	got := DefaultRedactor(e)
	var v any
	if err := json.Unmarshal(got.Arguments, &v); err != nil {
		t.Fatalf("arguments are not valid JSON after redaction: %v (%s)", err, got.Arguments)
	}
	obj, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("structure collapsed: %T", v)
	}
	if obj["command"] != "aws configure" {
		t.Errorf("command = %v", obj["command"])
	}
	if _, ok := obj["env"].(map[string]any); !ok {
		t.Errorf("env structure collapsed: %T", obj["env"])
	}
	list, ok := obj["list"].([]any)
	if !ok || len(list) != 3 {
		t.Errorf("list structure collapsed: %v", obj["list"])
	}
}

func TestDefaultRedactorLeavesInvalidJSONUntouched(t *testing.T) {
	e := event.New(event.SourceGateway, event.KindToolResult, time.Now())
	e.Result = json.RawMessage(`{"password":"hunter2"`) // truncated
	got := DefaultRedactor(e)
	if string(got.Result) != `{"password":"hunter2"` {
		t.Errorf("invalid JSON altered: %s", got.Result)
	}
	if got.Redacted {
		t.Error("invalid JSON should not be marked redacted")
	}
}

func TestDefaultRedactorNoOpOnEmptyPayloads(t *testing.T) {
	e := event.New(event.SourceGateway, event.KindToolCallIntent, time.Now())
	got := DefaultRedactor(e)
	if got.Redacted || got.RedactionCount != 0 {
		t.Errorf("unexpected redaction on empty event: %+v", got)
	}
}

// TestDefaultRedactorPreservesLargeIntegers proves redaction does not corrupt
// numeric payloads: decoding via float64 would re-encode this integer.
func TestDefaultRedactorPreservesLargeIntegers(t *testing.T) {
	e := event.New(event.SourceGateway, event.KindToolResult, time.Now())
	e.Result = json.RawMessage(`{"offset":1234567890123456789,"password":"hunter2"}`)

	got := DefaultRedactor(e)
	if !got.Redacted {
		t.Fatalf("expected redaction: %+v", got)
	}
	if !strings.Contains(string(got.Result), "1234567890123456789") {
		t.Errorf("large integer lost fidelity: %s", got.Result)
	}
	if strings.Contains(string(got.Result), "hunter2") {
		t.Errorf("secret survived: %s", got.Result)
	}
}

// TestDefaultRedactorBasicAuthorization covers raw-text HTTP Basic credentials.
func TestDefaultRedactorBasicAuthorization(t *testing.T) {
	in := "Authorization: Basic " + fakeSecret("dXNlcjpw", "YXNzd29yZA==")
	got, n := redactString(in)
	if n == 0 {
		t.Fatalf("no redaction applied to %q", in)
	}
	if !strings.Contains(got, "[REDACTED:basic_auth]") {
		t.Errorf("redacted = %q, want it to contain %q", got, "[REDACTED:basic_auth]")
	}
}

// TestDefaultRedactorURLUserinfo keeps the URL's user but redacts the password.
func TestDefaultRedactorURLUserinfo(t *testing.T) {
	in := "postgres://alice:" + fakeSecret("s3cr", "3tpass") + "@db.example.com:5432/app"
	got, n := redactString(in)
	if n == 0 {
		t.Fatalf("no redaction applied to %q", in)
	}
	if !strings.Contains(got, "alice") {
		t.Errorf("user was dropped: %q", got)
	}
	if strings.Contains(got, "s3cr3tpass") {
		t.Errorf("password survived: %q", got)
	}
	if !strings.Contains(got, "[REDACTED:url_password]") || !strings.Contains(got, "@db.example.com:5432/app") {
		t.Errorf("unexpected rewrite: %q", got)
	}
}

// TestDefaultRedactorQuotedAssignmentWithSpaces covers a quoted assignment
// whose value contains whitespace.
func TestDefaultRedactorQuotedAssignmentWithSpaces(t *testing.T) {
	got, n := redactString(`password = "correct horse battery staple"`)
	if n == 0 {
		t.Fatal("no redaction applied")
	}
	if strings.Contains(got, "correct horse") {
		t.Errorf("quoted value survived: %q", got)
	}
	if !strings.Contains(got, "[REDACTED:credential]") {
		t.Errorf("unexpected rewrite: %q", got)
	}
}
