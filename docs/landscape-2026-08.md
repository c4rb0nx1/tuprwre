# Agent-safety landscape vs tuprwre/tprsh — position and direction

*Research + ideation note, 2026-08-23. Companion to dsh-comparison.md. Source landscape:
wincent's "List of coding agent sandboxes 2026-05" (118+ tools) plus targeted research.*

## The landscape in four layers

| layer | examples | crowding | our position |
|---|---|---|---|
| **1. Boundaries** (microVM/container/hosted) | Firecracker, E2B, Modal, Docker Sandboxes, Daytona, +50 more | Saturated, VC-funded | Don't compete. Consume. |
| **2. OS effects sandboxes** (Seatbelt/Landlock/bwrap wrappers) | **Anthropic srt**, Codex CLI built-in, dsh built-in, Gemini CLI, ~15 hobby Seatbelt wrappers | Commoditized — every major harness ships one; Anthropic open-sourced srt | tprsh's Confiner competes here. **Stop building this; delegate.** |
| **3. Policy / approval / audit** | Cupcake (OPA/Rego hooks), nah (deterministic guard), punkgo-jack (Merkle audit receipts), shannot (approvals), predicate-secure, immunity-agent — **only ~8 entries, mostly young, mostly Claude-Code-hook-scoped** | **Emptiest category** | tprsh's real home: bash-AST policy + hash-chain audit + (planned) learning approvals |
| **4. Safe install / tool delivery** | — (container wrappers wrap *agents*, nobody projects *tools* back to the host) | **Effectively uncontested** | tuprwre is alone here; demand unproven |

## Key competitor facts

- **Anthropic srt** (`@anthropic-ai/sandbox-runtime`, research preview): OS-level FS+network
  confinement without containers — Seatbelt (macOS), bubblewrap (Linux), WFP (Windows) —
  **plus a host-side HTTP/SOCKS5 proxy giving per-host network allowlists and logging**.
  That is strictly stronger than tprsh's all-or-nothing `-no-network`, and it solves the
  exact gap confine.go documents. Process-tree-wide, programmatic API (SandboxManager).
- **Category-3 neighbors**: Cupcake gates harness hooks with generic Rego (no shell
  semantics — it sees tool-call strings, not a parsed AST). nah is a deterministic
  allow/ask/block guard for Claude Code (closest to tprsh -check; pattern-level, one
  harness). punkgo-jack does Merkle-logged audit receipts of hook events (validates the
  tamper-evident-ledger idea; audit-only, no policy). shannot is human approval gates
  (no persistence into policy). **Nobody in the category combines: (a) parsing the
  actual bash into per-command verdicts, (b) tamper-evident cross-session audit,
  (c) approvals that compound into scoped, expiring rules.** That triple is the white space.
- **dsh (DeepSeek Harness)**: see dsh-comparison.md — harness with built-in effects
  sandbox; pressure lands on layer 2, not on tprsh's layer-3 identity.

## What this says about each codebase

**tprsh** — right idea, one wrong turn. The verdict engine (bash AST, deny-by-default
args, wrapper recursion), the hash-chained ledger, and the harness-agnostic `-check`
gate are all layer-3 assets in the least crowded category. The Confiner backend is a
layer-2 asset in the most commoditized category — and srt now does that job with a
better network story. The `Confiner` interface is already the right seam: an
`srtConfiner` that shells out to (or links) srt would replace the planned Linux
mount-namespace work almost entirely.

**tuprwre** — uncontested but unproven. Nothing in 118+ tools delivers "install in a
sandbox, tool feels native on the host forever." Either that's a real unmet need or a
need nobody has. It costs little to keep as the "safe yes" behind tprsh's deny message.

**testlab** — sleeper asset. Nobody else publishes measured policy-friction data
(cost/token overhead of gating a real agent, wrongly-blocked-command findings). As the
category matures, evidence is credibility.

## Proposed direction (for discussion)

One product story: **"the policy brain and evidence ledger for any agent, with a safe
install path."** Agent asks → deterministic verdict → denial always offers a sanctioned
alternative (tuprwre install) → every verdict, approval, and shim invocation lands in
one tamper-evident chain → repeated scoped approvals become proposed rules (TTL'd,
fingerprint-bound, per the 2026-05 Slack approval plan).

Sequenced:

1. **Stop layer-2 investment.** Prototype `srtConfiner` implementing the existing
   Confiner interface; keep the Seatbelt backend as fallback. Re-scope "Linux read
   confinement" to "delegate to srt/bwrap".
2. **Know the neighbors.** One afternoon each on nah, Cupcake, punkgo-jack — confirm
   the white-space claim before betting on it.
3. **Policy as data.** Move the allowlist from Go source to a versioned, signed policy
   file. Prerequisite for everything adaptive; also makes policy diffs auditable.
4. **Close the loop.** Approval store (SQLite, fingerprint+context+TTL, fail-closed)
   per the May plan; local CLI approvals first, Slack later. Promotions recorded in
   the hash chain.
5. **Publish testlab results.** The A/B friction data is both roadmap input and the
   category's only public evidence base.
6. **tuprwre to maintenance**, wired in as the deny message's safe alternative;
   revisit investment only if shim demand shows up.

Sources: gist.github.com/wincent/2752d8d97727577050c043e4ff9e386e ·
github.com/anthropic-experimental/sandbox-runtime · docs/dsh-comparison.md
