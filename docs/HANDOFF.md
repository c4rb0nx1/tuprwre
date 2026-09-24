# Cloud handoff — tprsh-gateway

State as of 2026-09-25. This document is the starting point for a fresh agent
session (including a cloud session with no local continuity). Read
`CLAUDE.md`, `docs/gateway.md`, `docs/sensor.md` and `docs/report.md`
alongside it.

## Problem statement

Software factories will run coding, SRE and devops agents **unattended** on
Linux. Today's harness guardrails are advisory and per-harness, and they miss
native or covert tool calls. Operators have no harness-agnostic way to know what
an agent did, react proportionately, and bring it back on track.

Philosophy:

- Minimal restriction on what the agent owns.
- Record mistakes and make sure they never recur.
- Fixed rules first; a classifier only under them.

## Tiers

| Tier | Behavior |
|---|---|
| green | Fixed rules; instant allow/deny with the reason fed back to the LLM. |
| yellow | Log + async local classifier plugin (e.g. Laya; Jev optional). |
| red | Pause the agent, notify, resume with redirection. |

**Rebound** = keep the run on track + notify the breach. A drift monitor may be a
cloud model plugin (e.g. DeepSeek Flash) behind an OpenAI-compatible endpoint,
redacted, and **fail-open**.

## Architecture

Three independent streams feed **our own versioned `Event` schema**:

1. **gateway** — a local pass-through proxy on the LLM wire protocol. Observes
   *intent* (`tool_use` + `tool_result`).
2. **OS sensor** — observes *effects* (exec/file/net) via adapters. Consume
   existing tools now (`eslogger` on macOS, Tetragon on Linux) behind a `Sensor`
   interface, replaceable by our own eBPF later. Nothing downstream may depend
   on a consumed tool's format.
3. **tprsh shell** — in-band feedback to the agent.

Key differentiator: **intent-vs-effect reconciliation** — effects with no
matching tool call are covert actions.

## Harness facts (verified by spike)

| Harness | Mechanism | Wire |
|---|---|---|
| Claude Code | `ANTHROPIC_BASE_URL` | `POST /v1/messages?beta=true`, `x-api-key` |
| Codex | `-c model_providers.<n>.base_url` | `wire_api=responses`, `POST /v1/responses`, Bearer; retries aggressively on 5xx |
| Pi | `models.json` provider `baseUrl` | `POST /v1/chat/completions`, Bearer |

Subscription / OAuth login modes are **UNTESTED**.

## Done

Verified via `git log` on `feat/tprsh-gateway`:

- `09680dc` — record-only wire extractors and pass-through proxy.
- `6a709e7` — fixed encoding, host, completeness, dedup, async sink, redaction.
- `f4a9747` — drain-safe close, redaction and test hardening.
- `a9f6c61` — `tprsh-gateway` binary and e2e smoke test.
- `12ad852` — default log path, e2e strictness, docs.

Cloud session (all four handoff tasks):

- `1633dca`: `internal/sensor` — `Sensor` interface, `Record` (validate +
  default-on redaction + `Stats`), `Validate` contract, `Replay` reference
  sensor. Effect payloads (`sensor`, `process`, `file`, `net`, `exit`) added
  to `event.Event`; schema stays `"0"`. Argv redaction in `DefaultRedactor`.
  Synthetic contract fixtures.
- `01b2443`: `internal/sensor/tetragon` adapter, `sensor.IsSensitivePath`,
  and the `cmd/tprsh-sensor` binary. Tested against a synthetic export with
  a golden output; **not verified against a live Tetragon agent**. Also an
  illustrative, unverified TracingPolicy (`docs/tetragon/tprsh-effects.yaml`).
- `95fc84e`: `internal/rules` (fixed rule set + shell lexer), `internal/report`
  and `cmd/tprsh-report`. Links effects to tool calls, lists covert
  candidates, and gives would-be tiers. Also `scripts/report-e2e.sh`
  (fixture-driven, no live sensor).
- Docs: `docs/sensor.md`, `docs/report.md`; `docs/gateway.md` describes the
  three-piece pipeline.

Capabilities landed: record-only gateway; three wire extractors; request-side
`tool_result` extraction with dedup; async sink with `Stats`; drain-safe bounded
`Close`; default-on pattern redaction; `cmd/tprsh-gateway` binary; real-pi e2e.

Three independent reviews; last verdict: **no P0/P1**.

Residuals: redaction is pattern-based; re-marshal HTML-escapes `<`/`>`/`&`; no
dedup across restarts; an explicit `--log` parent dir is not chmod'ed.

## Next tasks

The four original cloud tasks are done (see above and `docs/sensor.md`,
`docs/report.md`). Candidates for the next session, roughly in order:

1. **Live Tetragon verification** on a Linux host with eBPF. Record a real
   export, compare it with the synthetic fixture shape, and fix the mapping.
   Open questions: does `process.arguments` quote arguments that contain
   spaces? Does `process_exit.status` hold the exit code or the raw wait
   status? Do the example policy's `Postfix`/`Equal` selectors behave as
   written? Commit a *redacted* real sample as a new fixture.
2. **Session attribution by process tree** in `tprsh-sensor` (e.g. a
   `--root-pid` or cgroup filter), so one host-wide Tetragon stream can be
   split across several agents instead of stamping one `--session-id`.
3. **Report precision**: per-process (not session-wide) credential taint; an
   allowlist for harness self-writes (state/cache dirs); configurable
   protected branches, prod-context pattern and workspace per session.
4. **Dedup across gateway restarts**; chmod of an explicit `--log` parent
   (gateway residuals above).

Scripts:

- `scripts/gateway-e2e.sh` needs the `pi` CLI. Cloud runs used `ALLOW_FAKE=1`.
- `scripts/report-e2e.sh` is fixture-only and needs just Go and `python3`.

Local-only (not cloud):

- `eslogger` adapter (needs macOS root + Full Disk Access). Implement
  `sensor.Sensor` and satisfy `sensor.Validate`.
- Dogfooding with real keys.
- Subscription-auth test.

## Explicitly not doing

- Confinement / enforce hardening.
- Per-harness plugins in core.
- Classifier in core.
- Automatic policy promotion without human review.
