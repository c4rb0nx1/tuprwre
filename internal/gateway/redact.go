package gateway

import (
	"bytes"
	"encoding/json"
	"io"
	"regexp"
	"strings"

	"github.com/c4rb0nx1/tuprwre/internal/event"
)

// Redactor transforms an event immediately before it is handed to a Sink. It
// must return the event to record; returning the input unchanged disables
// redaction for that event.
type Redactor func(event.Event) event.Event

// redactionPattern is one ordered secret-matching rule. replace is a
// regexp.ReplaceAllString expansion applied to the matched substring; it may
// reference capture groups.
type redactionPattern struct {
	re      *regexp.Regexp
	replace string
}

// redactionPatterns are applied in order. Specific credential shapes are
// matched before the generic "key = value" assignment so that the reported
// kind is as precise as possible.
var redactionPatterns = []redactionPattern{
	// PEM private-key blocks, including newlines.
	{regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?-----END [A-Z ]*PRIVATE KEY-----`), "[REDACTED:private_key]"},
	// AWS access key IDs.
	{regexp.MustCompile(`\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`), "[REDACTED:aws_access_key_id]"},
	// AWS secret access keys, keeping the field name.
	{regexp.MustCompile(`(?i)(aws_secret_access_key\s*[=:]\s*)["']?[A-Za-z0-9/+=]{20,}["']?`), "${1}[REDACTED:aws_secret_access_key]"},
	// Bearer tokens.
	{regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._~+/-]+=*`), "Bearer [REDACTED:bearer]"},
	// HTTP Basic credentials in raw text, keeping the scheme.
	{regexp.MustCompile(`(?i)\bAuthorization\s*:\s*Basic\s+[A-Za-z0-9+/]+=*`), "Authorization: Basic [REDACTED:basic_auth]"},
	// Credentials embedded in a URL's userinfo; the user is kept, the
	// password is replaced.
	{regexp.MustCompile(`([A-Za-z][A-Za-z0-9+.-]*://[^:/?#@\s]+:)([^@/?#\s]+)(@)`), "${1}[REDACTED:url_password]${3}"},
	// Anthropic-style and OpenAI-style API keys.
	{regexp.MustCompile(`\bsk-ant-[A-Za-z0-9_-]{8,}`), "[REDACTED:api_key]"},
	{regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{8,}`), "[REDACTED:api_key]"},
	// GitHub tokens.
	{regexp.MustCompile(`\b(?:ghp|gho|ghs|ghr|github_pat)_[A-Za-z0-9_]{10,}`), "[REDACTED:github_token]"},
	// Slack tokens.
	{regexp.MustCompile(`\bxox[baprs]-[A-Za-z0-9-]{8,}`), "[REDACTED:slack_token]"},
	// JSON Web Tokens.
	{regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{5,}\.[A-Za-z0-9_-]{5,}\.[A-Za-z0-9_-]{5,}`), "[REDACTED:jwt]"},
	// Generic key/token/secret/password assignments, keeping the key and the
	// assignment operator. A quoted value may contain spaces, so the pattern
	// prefers a balanced quote pair before the unquoted form.
	{regexp.MustCompile(`(?i)\b(password|passwd|secret|token|api[_-]?key)\b(\s*[:=]\s*)("[^"]*"|'[^']*'|[^\s"',;]+)`), "${1}${2}[REDACTED:credential]"},
}

// sensitiveKeyWords are substrings that mark a JSON object key as holding a
// credential; a string value under such a key is replaced wholesale.
var sensitiveKeyWords = []string{
	"password", "passwd", "secret", "token", "api_key", "apikey",
	"authorization", "credential", "access_key", "private_key",
}

// DefaultRedactor replaces secret-looking substrings in an event's Arguments,
// Result and effect Process.Argv with "[REDACTED:<kind>]" markers. JSON
// payloads stay valid JSON: only string values are rewritten, never structure.
// Payloads that are not valid JSON are left untouched. The input event's
// Process is never mutated; a redacted copy replaces it.
func DefaultRedactor(e event.Event) event.Event {
	count := 0
	if e.Process != nil && len(e.Process.Argv) > 0 {
		if argv, n := redactArgv(e.Process.Argv); n > 0 {
			p := *e.Process
			p.Argv = argv
			e.Process = &p
			count += n
		}
	}
	if len(e.Arguments) > 0 {
		if out, n := redactJSON(e.Arguments); n > 0 {
			e.Arguments = out
			count += n
		}
	}
	if len(e.Result) > 0 {
		if out, n := redactJSON(e.Result); n > 0 {
			e.Result = out
			count += n
		}
	}
	if count > 0 {
		e.Redacted = true
		e.RedactionCount += count
	}
	return e
}

// redactArgv redacts each argument string and, additionally, replaces the
// value that follows a credential-naming flag given as a separate argument
// (e.g. "--password", "hunter2") or an HTTP auth scheme that a sensor split
// off its token (e.g. "Bearer", "tok"). It returns a fresh slice and the
// number of replacements, or the input and 0 when nothing matched.
func redactArgv(argv []string) ([]string, int) {
	out := make([]string, len(argv))
	count := 0
	for i, arg := range argv {
		if i > 0 && !strings.HasPrefix(arg, "-") {
			if isCredentialFlag(argv[i-1]) {
				out[i] = "[REDACTED:credential]"
				count++
				continue
			}
			if isAuthScheme(argv[i-1]) {
				out[i] = "[REDACTED:" + strings.ToLower(argv[i-1]) + "]"
				count++
				continue
			}
		}
		red, n := redactString(arg)
		out[i] = red
		count += n
	}
	if count == 0 {
		return argv, 0
	}
	return out, count
}

// isCredentialFlag reports whether arg is a bare flag (no "=value") whose name
// marks its next argument as a credential, e.g. "--token" or "-password".
func isCredentialFlag(arg string) bool {
	if !strings.HasPrefix(arg, "-") || strings.Contains(arg, "=") {
		return false
	}
	name := strings.ReplaceAll(strings.TrimLeft(arg, "-"), "-", "_")
	return name != "" && isSensitiveKey(name)
}

// isAuthScheme reports whether arg is exactly an HTTP auth scheme whose
// credential follows as the next argument.
func isAuthScheme(arg string) bool {
	return strings.EqualFold(arg, "Bearer") || strings.EqualFold(arg, "Basic")
}

// redactJSON walks a JSON payload and redacts secrets within its string values.
// It returns the re-encoded payload and the number of replacements. A payload
// that is not valid JSON, or that contained no secrets, is returned unchanged.
func redactJSON(raw json.RawMessage) (json.RawMessage, int) {
	// UseNumber keeps integer fidelity: decoding to float64 would re-encode a
	// large integer (e.g. an ID or offset) in scientific notation.
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return raw, 0
	}
	// Match json.Unmarshal's strictness: reject trailing non-whitespace data.
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		return raw, 0
	}
	v, n := redactValue(v, false)
	if n == 0 {
		return raw, 0
	}
	out, err := event.MarshalNoEscape(v)
	if err != nil {
		return raw, 0
	}
	return out, n
}

// redactValue rewrites string values, recursing into objects and arrays.
// sensitiveKey marks a value whose enclosing object key names a credential.
func redactValue(v any, sensitiveKey bool) (any, int) {
	switch t := v.(type) {
	case string:
		if sensitiveKey {
			return "[REDACTED:field]", 1
		}
		return redactString(t)
	case map[string]any:
		count := 0
		for k, val := range t {
			nv, n := redactValue(val, isSensitiveKey(k))
			if n > 0 {
				t[k] = nv
				count += n
			}
		}
		return t, count
	case []any:
		count := 0
		for i, val := range t {
			nv, n := redactValue(val, false)
			if n > 0 {
				t[i] = nv
				count += n
			}
		}
		return t, count
	default:
		return v, 0
	}
}

// redactString applies every pattern to s, returning the rewritten string and
// the number of replacements.
func redactString(s string) (string, int) {
	count := 0
	for _, p := range redactionPatterns {
		var n int
		s, n = applyPattern(p, s)
		count += n
	}
	return s, count
}

func applyPattern(p redactionPattern, s string) (string, int) {
	count := 0
	out := p.re.ReplaceAllStringFunc(s, func(m string) string {
		count++
		return p.re.ReplaceAllString(m, p.replace)
	})
	return out, count
}

// isSensitiveKey reports whether a JSON object key names a credential.
func isSensitiveKey(key string) bool {
	lower := strings.ToLower(key)
	for _, w := range sensitiveKeyWords {
		if strings.Contains(lower, w) {
			return true
		}
	}
	return false
}
