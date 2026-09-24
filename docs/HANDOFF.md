# Cloud handoff — tprsh-gateway

State as of 2026-09-25. This document is the starting point for a fresh agent
session (including a cloud session with no local continuity). Read
`docs/gateway.md` and `CLAUDE.md` alongside it.

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

Capabilities landed: record-only gateway; three wire extractors; request-side
`tool_result` extraction with dedup; async sink with `Stats`; drain-safe bounded
`Close`; default-on pattern redaction; `cmd/tprsh-gateway` binary; real-pi e2e.

Three independent reviews; last verdict: **no P0/P1**.

Residuals: redaction is pattern-based; re-marshal HTML-escapes `<`/`>`/`&`; no
dedup across restarts; an explicit `--log` parent dir is not chmod'ed.

## Next tasks for the cloud session (in order)

1. **Sensor interface + effect event kinds + contract fixtures** (recorded JSON
   samples) in a new package.
2. **Tetragon adapter** driven by recorded fixture events. Live eBPF is likely
   unavailable in cloud VMs — do **not** claim live verification.
3. **`cmd/tprsh-report`** — read gateway + sensor JSONL, group by session, list
   tool intents, effects, and unmatched effects (covert candidates), plus
   would-be green/yellow/red using a small fixed rule set for irreversible
   actions:
   - `terraform`/`tofu apply|destroy`
   - `kubectl delete/apply` on prod contexts
   - `git push --force` to protected branches
   - `rm` outside the workspace
   - reading credential files then network egress
4. **Keep `docs/gateway.md` current.**

Local-only (not cloud):

- `eslogger` adapter (needs macOS root + Full Disk Access).
- Dogfooding with real keys.
- Subscription-auth test.

## Explicitly not doing

- Confinement / enforce hardening.
- Per-harness plugins in core.
- Classifier in core.
- Automatic policy promotion without human review.
