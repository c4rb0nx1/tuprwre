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

Cloud session, first round (the four original handoff tasks):

- `1633dca`: `internal/sensor` — `Sensor` interface, `Record` (validate +
  default-on redaction + `Stats`), `Validate` contract, `Replay` reference
  sensor. Effect payloads (`sensor`, `process`, `file`, `net`, `exit`) added
  to `event.Event`; schema stays `"0"`. Argv redaction in `DefaultRedactor`.
  Synthetic contract fixtures.
- `01b2443`: `internal/sensor/tetragon` adapter, `sensor.IsSensitivePath`,
  and the `cmd/tprsh-sensor` binary; example TracingPolicy.
- `95fc84e`: `internal/rules` (fixed rule set + shell lexer), `internal/report`
  and `cmd/tprsh-report`, plus `scripts/report-e2e.sh`.
- `e2f2e99`: docs (`docs/sensor.md`, `docs/report.md`, pipeline overview in
  `docs/gateway.md`).

Cloud session, second round (the follow-ups listed after round one):

- `18f3741`: Tetragon mapping checked against Tetragon's source
  (`cilium/tetragon` a58fbc7) and its recorded documentation samples; fixed
  argv decoding (Tetragon quotes arguments that contain a space), rootless
  file paths, syscall read/write hooks, 64-bit mask arguments, `errorCWD`,
  and legacy `process_connect`; keystores count as credentials. All 34
  upstream samples translate (opt-in `TestUpstreamRecordedSamples`, with
  `TPRSH_TETRAGON_SRC`). The policy uses `Mask` and validates with Tetragon's
  own loader.
- `27d3a86`: `tprsh-sensor --root-pid` (`sensor.SubtreeFilter`) scopes a
  host-wide stream to one agent's process subtree.
- `13f016d`: report precision — `rules.Config` (`--protected-branch`,
  `--prod-context`), `--ignore-path` for harness self-writes,
  `--taint-window` for credential-then-egress.
- `6afe535`: gateway dedup across restarts (it seeds from the log it appends
  to, for the same session id).
- `f65b00f`: logs keep `<`, `>`, `&` literal (no HTML escaping).

Capabilities landed: record-only gateway; three wire extractors; request-side
`tool_result` extraction with dedup (also across restarts); async sink with
`Stats`; drain-safe bounded `Close`; default-on pattern redaction (payloads
and effect argv); Tetragon effect adapter; `tprsh-sensor`; `tprsh-report` with
fixed rules; real-pi gateway e2e and fixture-driven pipeline e2e.

Residuals: redaction is pattern-based; an explicit `--log` parent dir is not
chmod'ed (by design: it may be shared, like `/tmp`); report linking is
heuristic (see `docs/report.md` Limits).

## Next tasks

1. **Live Tetragon run.** In the cloud VM, Tetragon and its BPF objects build
   from source (`make tetragon-bpf LOCAL_CLANG=1 && make tetragon tetra`,
   clang 18, root, BTF present). Starting the agent needs bpffs/tracefs
   mounted (`/sys/fs/bpf`, `/sys/kernel/tracing`), and that mount was **not
   permitted** in the cloud session. On a Linux host where it is allowed:
   1. Run `tetragon --bpf-lib bpf/objs --export-filename out.json
      --tracing-policy docs/tetragon/tprsh-effects.yaml`.
   2. Drive a harness through `tprsh-gateway`.
   3. Pipe the export through `tprsh-sensor tetragon --root-pid <harness pid>`.
   4. Run `tprsh-report`.
   5. Commit a *redacted* real export as a fixture.
2. **eslogger adapter** (local-only: macOS root + Full Disk Access). Implement
   `sensor.Sensor` and satisfy `sensor.Validate`; model the tests on
   `internal/sensor/tetragon`.
3. **Report**: per-session workspace overrides; linking effects by cwd as well
   as by time.
4. Dogfooding with real keys; subscription-auth test (local).

Scripts:

- `scripts/gateway-e2e.sh` needs the `pi` CLI. Cloud runs used `ALLOW_FAKE=1`.
- `scripts/report-e2e.sh` is fixture-only and needs just Go and `python3`.

## Explicitly not doing

- Confinement / enforce hardening.
- Per-harness plugins in core.
- Classifier in core.
- Automatic policy promotion without human review.
