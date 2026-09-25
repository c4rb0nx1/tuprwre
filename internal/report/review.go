package report

import (
	"context"
	"sort"

	"github.com/c4rb0nx1/tuprwre/internal/classify"
	"github.com/c4rb0nx1/tuprwre/internal/event"
	"github.com/c4rb0nx1/tuprwre/internal/rules"
)

// Defaults for ReviewOptions.
const (
	DefaultReviewThreshold = 0.8
	DefaultReviewMaxItems  = 200
)

// Assessment is a classifier's advisory opinion about one item. It never
// changes the item's fixed-rule tier.
type Assessment struct {
	Model string `json:"model"`
	// Questions names the fixed question wording (classify.QuestionSet).
	Questions string `json:"questions"`
	// Scores holds the probability of "yes" per question id.
	Scores map[string]float64 `json:"scores,omitempty"`
	// Flagged lists question ids at or above the review threshold.
	Flagged []string `json:"flagged,omitempty"`
	// Error is set when the classifier could not answer; the review fails
	// open and the report is otherwise unchanged.
	Error string `json:"error,omitempty"`
}

// ReviewOptions configures Review.
type ReviewOptions struct {
	// Threshold is the probability at or above which a question is
	// flagged. Default DefaultReviewThreshold.
	Threshold float64
	// MaxItems bounds the number of classifier requests. Default
	// DefaultReviewMaxItems.
	MaxItems int
}

// ReviewStats counts what Review did.
type ReviewStats struct {
	Model     string  `json:"model"`
	Questions string  `json:"questions"`
	Threshold float64 `json:"threshold"`
	Asked     int     `json:"asked"`
	Flagged   int     `json:"flagged"`
	Errors    int     `json:"errors"`
	// Skipped counts items left unasked because MaxItems was reached.
	Skipped int `json:"skipped"`
}

// Review asks the classifier about the items the fixed rules leave
// uncertain: yellow tool intents and yellow or covert executions (the command
// questions), and every recorded tool result (the injection question). It
// runs after Build, fails open on every error, and never changes a tier.
func Review(ctx context.Context, rep *Report, c classify.Classifier, opts ReviewOptions) ReviewStats {
	if opts.Threshold <= 0 {
		opts.Threshold = DefaultReviewThreshold
	}
	if opts.MaxItems <= 0 {
		opts.MaxItems = DefaultReviewMaxItems
	}
	st := ReviewStats{Model: c.Model(), Questions: classify.QuestionSet, Threshold: opts.Threshold}

	type job struct {
		state string
		qs    map[string]classify.Question
		out   **Assessment
	}
	var jobs []job
	for _, s := range rep.Sessions {
		for _, in := range s.Intents {
			if in.Tier == rules.Yellow && in.Summary != "" && (len(in.facts.scripts) > 0 || len(in.facts.argvs) > 0) {
				jobs = append(jobs, job{in.Summary, classify.CommandQuestions, &in.Classifier})
			}
		}
		for _, ef := range s.Effects {
			if ef.Kind == event.KindExec && (ef.Tier == rules.Yellow || ef.Covert) {
				jobs = append(jobs, job{ef.Summary, classify.CommandQuestions, &ef.Classifier})
			}
		}
		for _, in := range s.Intents {
			if in.resultText != "" {
				jobs = append(jobs, job{in.resultText, classify.ResultQuestions, &in.ResultClassifier})
			}
		}
	}

	for i, j := range jobs {
		if i >= opts.MaxItems {
			st.Skipped = len(jobs) - i
			break
		}
		if ctx.Err() != nil {
			st.Skipped = len(jobs) - i
			break
		}
		st.Asked++
		a := &Assessment{Model: c.Model(), Questions: classify.QuestionSet}
		answers, err := c.Ask(ctx, j.state, j.qs)
		if err != nil {
			st.Errors++
			a.Error = err.Error()
			*j.out = a
			continue
		}
		a.Scores = map[string]float64{}
		for id, ans := range answers {
			if ans.Noul == nil {
				continue
			}
			a.Scores[id] = *ans.Noul
			if *ans.Noul >= opts.Threshold {
				a.Flagged = append(a.Flagged, id)
			}
		}
		sort.Strings(a.Flagged)
		if len(a.Flagged) > 0 {
			st.Flagged++
		}
		*j.out = a
	}
	rep.Review = &st
	return st
}
