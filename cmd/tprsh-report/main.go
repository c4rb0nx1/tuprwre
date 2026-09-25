// Command tprsh-report reconciles recorded tool-call intent (tprsh-gateway
// logs) with recorded host effects (tprsh-sensor logs), per session. It lists
// each session's tool intents and effects, links effects to the tool calls
// that explain them, flags effects that no tool call explains (covert-action
// candidates), and shows the tier (green/yellow/red) each item would have
// landed in under the fixed rule set. It only reads logs; it never blocks
// anything.
//
// Usage:
//
//	tprsh-report [flags] LOG...
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/c4rb0nx1/tuprwre/internal/classify"
	"github.com/c4rb0nx1/tuprwre/internal/report"
	"github.com/c4rb0nx1/tuprwre/internal/rules"
)

// exitFailOn is the exit status when --fail-on is met.
const exitFailOn = 3

type options struct {
	files     []string
	workspace string
	json      bool
	failOn    rules.Tier
	window    time.Duration
	slack     time.Duration
	taint     time.Duration
	ignore    []string
	rules     rules.Config

	classifier       string
	classifierModel  string
	classifierThresh float64
	classifierMax    int
	classifierTO     time.Duration
	classifierRemote bool
}

// classifierKeyEnv holds the optional bearer key for the classifier
// endpoint; it is read from the environment so it never appears in argv.
const classifierKeyEnv = "TPRSH_CLASSIFIER_API_KEY"

func parseFlags(args []string, stderr io.Writer) (*options, error) {
	fs := flag.NewFlagSet("tprsh-report", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: tprsh-report [flags] LOG...   (gateway and sensor JSONL logs, any order; - is stdin)")
		fs.PrintDefaults()
	}
	var (
		workspace = fs.String("workspace", "", "agent workspace directory for the rm rule (default: inferred per session from the root process cwd)")
		asJSON    = fs.Bool("json", false, "write the report as JSON")
		failOn    = fs.String("fail-on", "", `exit with status 3 when any item reaches this tier ("yellow" or "red")`)
		window    = fs.Duration("window", report.DefaultWindow, "how long after a tool call with no recorded result its effects may occur")
		slack     = fs.Duration("slack", report.DefaultSlack, "clock skew tolerated between gateway and sensor timestamps")
		taint     = fs.Duration("taint-window", 0, "credential-then-egress: only egress within this long after a credential read is red (0: rest of the session)")
		ignore    []string
		branches  []string
		prodCtx   []string

		classifier       = fs.String("classifier", "", "optional System One classifier (e.g. a local Kev: http://127.0.0.1:8009); advisory only, never changes a tier")
		classifierModel  = fs.String("classifier-model", "kev-latest", "model name sent to the classifier")
		classifierThresh = fs.Float64("classifier-threshold", report.DefaultReviewThreshold, "probability at or above which a classifier answer is flagged")
		classifierMax    = fs.Int("classifier-max", report.DefaultReviewMaxItems, "maximum classifier requests per report")
		classifierTO     = fs.Duration("classifier-timeout", 10*time.Second, "timeout per classifier request")
		classifierRemote = fs.Bool("classifier-allow-remote", false, "permit a non-loopback classifier URL (redacted text leaves the machine)")
	)
	fs.Func("ignore-path", "absolute directory whose file writes are expected background activity, never covert (repeatable)", func(v string) error {
		if !strings.HasPrefix(v, "/") {
			return fmt.Errorf("%q is not absolute (effect paths are absolute; expand ~ first)", v)
		}
		ignore = append(ignore, v)
		return nil
	})
	fs.Func("protected-branch", `protected branch name, trailing "*" for a prefix (repeatable; replaces the defaults `+
		strings.Join(rules.DefaultConfig.ProtectedBranches, ",")+")", func(v string) error {
		branches = append(branches, v)
		return nil
	})
	fs.Func("prod-context", `case-insensitive substring marking a kubectl context as production (repeatable; replaces the default "prod")`, func(v string) error {
		prodCtx = append(prodCtx, v)
		return nil
	})
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	opts := &options{files: fs.Args(), workspace: *workspace, json: *asJSON, window: *window, slack: *slack,
		taint: *taint, ignore: ignore, rules: rules.DefaultConfig,
		classifier: *classifier, classifierModel: *classifierModel, classifierThresh: *classifierThresh,
		classifierMax: *classifierMax, classifierTO: *classifierTO, classifierRemote: *classifierRemote}
	if opts.classifier != "" && (opts.classifierThresh <= 0 || opts.classifierThresh > 1 || opts.classifierMax <= 0 || opts.classifierTO <= 0) {
		return nil, errors.New("--classifier-threshold must be in (0,1]; --classifier-max and --classifier-timeout positive")
	}
	if len(branches) > 0 {
		opts.rules.ProtectedBranches = branches
	}
	if len(prodCtx) > 0 {
		opts.rules.ProdContexts = prodCtx
	}
	if len(opts.files) == 0 {
		fs.Usage()
		return nil, errors.New("no log files given")
	}
	switch t := rules.Tier(*failOn); t {
	case "", rules.Yellow, rules.Red:
		opts.failOn = t
	default:
		return nil, fmt.Errorf("--fail-on %q: want yellow or red", *failOn)
	}
	if opts.window <= 0 || opts.slack < 0 || opts.taint < 0 {
		return nil, errors.New("--window must be positive; --slack and --taint-window non-negative")
	}
	return opts, nil
}

func main() {
	opts, err := parseFlags(os.Args[1:], os.Stderr)
	if errors.Is(err, flag.ErrHelp) {
		os.Exit(0)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "tprsh-report:", err)
		os.Exit(2)
	}
	code, err := run(opts, os.Stdin, os.Stdout)
	if err != nil {
		fmt.Fprintln(os.Stderr, "tprsh-report:", err)
		os.Exit(1)
	}
	os.Exit(code)
}

// run builds and writes the report, returning the process exit status.
func run(opts *options, stdin io.Reader, stdout io.Writer) (int, error) {
	var readers []io.Reader
	for _, name := range opts.files {
		if name == "-" {
			readers = append(readers, stdin)
			continue
		}
		f, err := os.Open(name)
		if err != nil {
			return 0, err
		}
		defer f.Close()
		readers = append(readers, f)
	}
	events, st, err := report.Load(readers...)
	if err != nil {
		return 0, err
	}
	cfg := opts.rules
	if cfg.ProtectedBranches == nil && cfg.ProdContexts == nil {
		cfg = rules.DefaultConfig
	}
	rep := report.Build(events, st, report.Options{
		Workspace: opts.workspace, Window: opts.window, Slack: opts.slack,
		Rules: &cfg, IgnorePaths: opts.ignore, TaintWindow: opts.taint,
	})
	if opts.classifier != "" {
		c, err := classify.NewSystemOne(opts.classifier, classify.Options{
			Model: opts.classifierModel, APIKey: os.Getenv(classifierKeyEnv),
			Timeout: opts.classifierTO, AllowRemote: opts.classifierRemote,
		})
		if err != nil {
			return 0, err
		}
		// Fail open: classifier errors are recorded per item, never fatal.
		report.Review(context.Background(), rep, c, report.ReviewOptions{Threshold: opts.classifierThresh, MaxItems: opts.classifierMax})
	}
	if opts.json {
		err = report.WriteJSON(stdout, rep)
	} else {
		err = report.WriteText(stdout, rep)
	}
	if err != nil {
		return 0, err
	}
	if opts.failOn != "" && rep.Worst().Rank() >= opts.failOn.Rank() {
		return exitFailOn, nil
	}
	return 0, nil
}
