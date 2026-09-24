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
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

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
}

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
	)
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	opts := &options{files: fs.Args(), workspace: *workspace, json: *asJSON, window: *window, slack: *slack}
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
	if opts.window <= 0 || opts.slack < 0 {
		return nil, errors.New("--window must be positive and --slack non-negative")
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
	rep := report.Build(events, st, report.Options{Workspace: opts.workspace, Window: opts.window, Slack: opts.slack})
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
