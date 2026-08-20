# tprsh test lab

Runs a real agent against the same problem twice — once unrestricted, once
behind the tprsh gate — and reports where the policy helped, where it got in
the way, and what it cost.

The point is not to prove tprsh works. It is to find the places it does not.

## Design constraints

**Cheap.** Runs default to the smallest model and pass `--max-budget-usd`, a
hard cap enforced by the CLI rather than a promise. Every run reports its actual
cost from the result JSON. `run.sh` is a dry run unless you pass `--go`, so you
can always see what would happen before spending anything.

**Reproducible.** Each run copies the fixture fresh, so runs never contaminate
each other. Every run directory holds a `manifest.json` recording the problem,
scenario, model, tprsh commit and CLI version that produced it — a run from six
months ago can be understood and repeated today.

**Non-interactive.** Headless `--print`, no prompts, no babysitting. The agent
either finishes or hits the cap.

## Usage

```sh
./run.sh p01-audit baseline          # dry run, spends nothing
./run.sh p01-audit baseline --go     # ~cents
./run.sh p01-audit gated --go
./report.sh p01-audit
```

Override defaults with environment variables:

```sh
TPRSH_LAB_MODEL=sonnet TPRSH_LAB_BUDGET=1.00 ./run.sh p01-audit gated --go
```

## Scenarios

| | what runs |
|---|---|
| `baseline` | the agent, unrestricted, exactly as it normally behaves |
| `gated` | identical, plus the tprsh `PreToolUse` gate loaded via `--settings` |

The gate is passed with `--settings` rather than written into the workspace, so
a gated run leaves no configuration behind and the two scenarios differ by
exactly one flag.

## Adding a problem

1. `problems/<id>.txt` — the prompt, ending in an instruction to stop.
2. `expected/<id>.json` — the facts a correct answer must contain, each with a
   regex so `report.sh` can grade automatically.
3. Optionally a new fixture under `fixtures/`, selected with
   `TPRSH_LAB_FIXTURE`.

Keep problems small. A problem that takes twenty turns costs twenty turns every
time anyone reruns it, and teaches little more than a five-turn one.

## What to look for

The audit log is the real output. `report.sh` groups denials by reason, and each
distinct reason is a decision:

- **Win** — the policy stopped something genuinely out of scope.
- **Gap** — the policy blocked ordinary work because it does not describe that
  tool yet. Fix the policy, not the model.

A gated run scoring lower than baseline is expected early on. The useful
question is always *which denial caused it*.

## Known limits of this harness

The gate only sees `Bash` calls. An agent that reads files with its own Read
tool bypasses tprsh entirely — that is a property of gating a harness rather
than replacing its shell, and the reports will show it.

The gate enforces policy but does not confine execution: an approved command is
run by the agent's shell, not by tprsh. Confinement and a complete ledger
require executing *through* tprsh.
