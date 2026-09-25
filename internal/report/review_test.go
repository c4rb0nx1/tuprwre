package report

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/c4rb0nx1/tuprwre/internal/classify"
	"github.com/c4rb0nx1/tuprwre/internal/event"
	"github.com/c4rb0nx1/tuprwre/internal/rules"
)

// fakeClassifier says yes (0.95) to every question about text mentioning
// credentials, no (0.05) otherwise, and fails when fail is set.
type fakeClassifier struct {
	states []string
	fail   bool
}

func (f *fakeClassifier) Model() string { return "fake-kev" }

func (f *fakeClassifier) Ask(_ context.Context, state string, qs map[string]classify.Question) (map[string]classify.Answer, error) {
	f.states = append(f.states, state)
	if f.fail {
		return nil, errors.New("connection refused")
	}
	p := 0.05
	if strings.Contains(state, "credentials") {
		p = 0.95
	}
	out := map[string]classify.Answer{}
	for id := range qs {
		v := p
		out[id] = classify.Answer{Type: "noul", Noul: &v}
	}
	return out, nil
}

func tiers(r *Report) map[string]rules.Tier {
	out := map[string]rules.Tier{}
	for _, s := range r.Sessions {
		for _, in := range s.Intents {
			out[in.ID] = in.Tier
		}
		for _, ef := range s.Effects {
			out[ef.ID] = ef.Tier
		}
	}
	return out
}

func TestReviewAsksUncertainItemsAndKeepsTiers(t *testing.T) {
	r := build(t, Options{}, gatewayFixture, tetragonFixture)
	before := tiers(r)
	f := &fakeClassifier{}
	st := Review(context.Background(), r, f, ReviewOptions{})

	// Covert execs (cat, curl, sleep) and the four non-empty tool results
	// (toolu_02 returned ""); no intent in the fixture is a yellow command.
	// Flagged: cat's command and toolu_01's result both mention credentials.
	if st.Asked != 7 || st.Errors != 0 || st.Skipped != 0 || st.Flagged != 2 {
		t.Fatalf("stats = %+v", st)
	}
	if st.Model != "fake-kev" || st.Questions != classify.QuestionSet || st.Threshold != DefaultReviewThreshold {
		t.Errorf("stats provenance = %+v", st)
	}
	after := tiers(r)
	for id, tier := range before {
		if after[id] != tier {
			t.Errorf("%s: tier %s -> %s; the classifier must not change tiers", id, tier, after[id])
		}
	}
	s := session(t, r, "sess-tg-1")
	cat := effectBy(t, s, event.KindExec, 4005)
	if cat.Classifier == nil || strings.Join(cat.Classifier.Flagged, ",") != "exfiltration,irreversible" {
		t.Errorf("cat assessment = %+v", cat.Classifier)
	}
	if tf := effectBy(t, s, event.KindExec, 4002); tf.Classifier != nil {
		t.Error("an attributed red exec was sent to the classifier")
	}
	if in := s.Intents[2]; in.ResultClassifier == nil || in.ResultClassifier.Scores["injection"] != 0.05 {
		t.Errorf("result assessment = %+v", in.ResultClassifier)
	}

	var text bytes.Buffer
	if err := WriteText(&text, r); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"classifier fake-kev (questions tprsh-q1", "FLAGGED exfiltration,irreversible", "result classifier: injection 0.05", "result classifier: injection 0.95  FLAGGED injection"} {
		if !strings.Contains(text.String(), want) {
			t.Errorf("text output missing %q", want)
		}
	}
}

func TestReviewFailsOpenAndBounds(t *testing.T) {
	r := build(t, Options{}, gatewayFixture, tetragonFixture)
	st := Review(context.Background(), r, &fakeClassifier{fail: true}, ReviewOptions{MaxItems: 3})
	if st.Asked != 3 || st.Errors != 3 || st.Skipped != 4 || st.Flagged != 0 {
		t.Fatalf("stats = %+v", st)
	}
	cat := effectBy(t, session(t, r, "sess-tg-1"), event.KindExec, 4005)
	if cat.Classifier == nil || cat.Classifier.Error == "" || cat.Tier != rules.Yellow {
		t.Errorf("failed assessment = %+v tier %s", cat.Classifier, cat.Tier)
	}
	var text bytes.Buffer
	if err := WriteText(&text, r); err != nil || !strings.Contains(text.String(), "classifier: unavailable (connection refused)") {
		t.Errorf("text = %v %q", err, text.String())
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r = build(t, Options{}, gatewayFixture, tetragonFixture)
	if st := Review(ctx, r, &fakeClassifier{}, ReviewOptions{}); st.Asked != 0 || st.Skipped != 7 {
		t.Errorf("cancelled review = %+v", st)
	}
}

func TestReviewYellowIntentCommand(t *testing.T) {
	e := event.New(event.SourceGateway, event.KindToolCallIntent, time.Date(2026, 9, 21, 9, 0, 0, 0, time.UTC))
	e.SessionID, e.ToolCallID, e.ToolName, e.Complete = "s", "c1", "Bash", true
	e.Arguments = []byte(`{"command":"kubectl delete pod web-1"}`)
	r := Build([]event.Event{e}, LoadStats{}, Options{})
	f := &fakeClassifier{}
	if st := Review(context.Background(), r, f, ReviewOptions{}); st.Asked != 1 || f.states[0] != "kubectl delete pod web-1" {
		t.Errorf("stats = %+v states = %q", st, f.states)
	}
	if r.Sessions[0].Intents[0].Classifier == nil {
		t.Error("yellow intent not assessed")
	}
}

func TestResultText(t *testing.T) {
	if got := resultText([]byte(`"  plain text \n"`)); got != "plain text" {
		t.Errorf("string result = %q", got)
	}
	if got := resultText([]byte(`[ {"type": "text", "text": "x"} ]`)); got != `[{"type":"text","text":"x"}]` {
		t.Errorf("structured result = %q", got)
	}
	if resultText(nil) != "" {
		t.Error("empty result")
	}
}
