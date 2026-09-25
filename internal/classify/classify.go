// Package classify is tprsh's optional classifier plugin layer: the yellow
// tier's "log + async local classifier". It is never part of core. Nothing in
// the record path or the fixed rules depends on it, and a report built without
// a classifier is complete.
//
// The only wire protocol implemented is TypeSafe's System One API
// (POST /v1/systemone), which both Kev (github.com/jaredpalmer/kev, local
// open-weight decision models) and the hosted Jev serve. Classifier output is
// advisory: it annotates items but never changes a fixed-rule tier.
//
// Everything sent is passed through the default redaction patterns first. A
// non-loopback endpoint must be allowed explicitly, because the text then
// leaves the machine.
package classify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/c4rb0nx1/tuprwre/internal/gateway"
)

// MaxStateBytes bounds the text sent as one state. Kev was trained on states
// of up to 384 tokens and accepts 8,192; longer text is truncated here.
const MaxStateBytes = 6000

// Question is one System One question. Type is "noul" (yes/no), "choice" or
// "score". Criteria is optional for noul, a map of option name to description
// for choice, and an ordered list of levels for score.
type Question struct {
	Type         string `json:"type"`
	Instructions string `json:"instructions,omitempty"`
	Criteria     any    `json:"criteria,omitempty"`
}

// Answer is one System One answer. Noul is the probability of "yes" for a noul
// question; Choice, Score, Confidence and Probabilities are set for the other
// types.
type Answer struct {
	Type          string             `json:"type"`
	Noul          *float64           `json:"noul,omitempty"`
	Choice        string             `json:"choice,omitempty"`
	Score         *float64           `json:"score,omitempty"`
	Confidence    *float64           `json:"confidence,omitempty"`
	Probabilities map[string]float64 `json:"probabilities,omitempty"`
}

// Classifier answers questions about a piece of text.
type Classifier interface {
	// Ask returns one answer per question id. The state has already been
	// redacted by the caller or is redacted by the implementation.
	Ask(ctx context.Context, state string, questions map[string]Question) (map[string]Answer, error)
	// Model names the model answering, for provenance in reports.
	Model() string
}

// SystemOne is a Classifier speaking the System One API.
type SystemOne struct {
	endpoint string
	model    string
	apiKey   string
	client   *http.Client
}

// Options configures NewSystemOne.
type Options struct {
	// Model is the requested model name. Default "kev-latest".
	Model string
	// APIKey, when set, is sent as a bearer token (Kev's KEV_API_KEY).
	APIKey string
	// Timeout bounds one request. Default 10s.
	Timeout time.Duration
	// AllowRemote permits a non-loopback endpoint. Without it only
	// localhost/127.0.0.0/8/::1 are accepted, so text never leaves the
	// machine by accident.
	AllowRemote bool
}

// NewSystemOne returns a client for the System One server at baseURL, e.g.
// "http://127.0.0.1:8009" for a local Kev.
func NewSystemOne(baseURL string, opts Options) (*SystemOne, error) {
	u, err := url.Parse(baseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("classifier URL %q: want http(s)://host[:port]", baseURL)
	}
	if !opts.AllowRemote && !isLoopbackHost(u.Hostname()) {
		return nil, fmt.Errorf("classifier URL %q is not loopback; allow a remote endpoint explicitly (text would leave the machine)", baseURL)
	}
	if opts.Model == "" {
		opts.Model = "kev-latest"
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 10 * time.Second
	}
	u.Path = strings.TrimSuffix(u.Path, "/") + "/v1/systemone"
	return &SystemOne{
		endpoint: u.String(),
		model:    opts.Model,
		apiKey:   opts.APIKey,
		client:   &http.Client{Timeout: opts.Timeout},
	}, nil
}

// Model implements Classifier.
func (s *SystemOne) Model() string { return s.model }

// request is the System One request body.
type request struct {
	State     string              `json:"state"`
	Model     string              `json:"model"`
	Questions map[string]Question `json:"questions"`
}

// Ask implements Classifier. The state is redacted with the default patterns
// and truncated to MaxStateBytes before it is sent.
func (s *SystemOne) Ask(ctx context.Context, state string, questions map[string]Question) (map[string]Answer, error) {
	if len(questions) == 0 {
		return nil, errors.New("classify: no questions")
	}
	state, _ = gateway.RedactText(state)
	state = truncate(state, MaxStateBytes)
	body, err := json.Marshal(request{State: state, Model: s.model, Questions: questions})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if s.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+s.apiKey)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("classify: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("classify: read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("classify: %s: %s", resp.Status, truncate(strings.TrimSpace(string(raw)), 200))
	}
	var out struct {
		Answers map[string]Answer `json:"answers"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("classify: decode response: %w", err)
	}
	for id, q := range questions {
		a, ok := out.Answers[id]
		if !ok {
			return nil, fmt.Errorf("classify: no answer for question %q", id)
		}
		if a.Type != q.Type || (q.Type == "noul" && (a.Noul == nil || *a.Noul < 0 || *a.Noul > 1)) {
			return nil, fmt.Errorf("classify: malformed answer for question %q", id)
		}
	}
	return out.Answers, nil
}

func isLoopbackHost(h string) bool {
	if strings.EqualFold(h, "localhost") {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

// truncate cuts s to at most n bytes on a rune boundary, marking the cut.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && !utf8RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + " …[truncated]"
}

func utf8RuneStart(b byte) bool { return b&0xC0 != 0x80 }
