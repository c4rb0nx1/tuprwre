# dsh (DeepSeek Harness) vs tprsh vs tuprwre — can tuprwre be retired?

*Research note, 2026-08-23. Question under evaluation: replace the `tuprwre + tprsh`
pairing with `tprsh + dsh`. Correction recorded: `dsh` in `internal/tprsh/confine.go`
is DeepSeek Harness, not a Docker product (comment fixed the same day).*

## What dsh actually is

DeepSeek Harness (`deepseek-ai/deepseek-harness`, developer preview) is an **agent
harness** — the layer connecting a model to filesystem, shell, editor, web, and other
agents. A peer of Claude Code and Codex CLI, not a shell and not a container platform.
Relevant properties:

- **Plugin microkernel** ("Cordis"): everything, including the agent loop, is a plugin.
- **Per-command OS sandbox**, fail-closed: bwrap + Landlock on Linux, Seatbelt on
  macOS, ACL-restricted tokens on Windows. "A runner must return enforcing arguments
  or fail" — silently unconfined commands are forbidden.
- **Sandbox modes**: `read-only`, `workspace-write` (default), `danger-full-access`.
  The vocabulary tprsh adopted. It governs *writes*; same conclusion tprsh reached
  about read-confinement on macOS.
- **Approval policies**, separate axis from sandbox mode (human escalation).
- **Replayable event log**: "anything the model saw must be reconstructable from the
  log" — session resume/fork/replay.
- Local-first, workspace-centric, no always-on component.

## The three-way overlap map

| capability | dsh | tprsh | tuprwre |
|---|---|---|---|
| OS write-confinement (Seatbelt/Landlock), fail-closed | ✅ | ✅ (same design, same vocabulary) | — (container boundary instead) |
| Per-command **argument-level allowlist** (deny-by-default flags, wrapper recursion, kubectl/aws verb policy) | ❌ (approval modes, not arg policy) | ✅ | ❌ (blocklist of install commands only) |
| **Tamper-evident** audit (hash chain, kernel-denied to the child) | ❌ (replayable log, but not tamper-evident) | ✅ | ❌ |
| Harness-agnostic gate (`-check` hook usable from Claude Code, Codex, anything) | ❌ (protects only agents running *under* dsh) | ✅ | ❌ |
| **Safe path to actually install system tools** | ❌ (workspace-write *blocks* `apt-get`; no alternative offered) | ❌ (denies installs; today its guidance points at `tuprwre install`) | ✅ (install in container → commit → shim) |
| Tools persist and feel native on the host across sessions | ❌ | ❌ | ✅ (shims + warm pool) |
| Being an agent harness (model loop, editor, web) | ✅ | ❌ | ❌ |

## The verdict

**`tprsh + dsh` does not cover tuprwre's ground — it doubles one layer and leaves
tuprwre's layer empty.**

1. **The pairing is redundant where it overlaps.** dsh and tprsh confine writes with
   the same OS mechanisms and the same vocabulary. Running tprsh inside dsh stacks
   two Seatbelt/Landlock write-confinement layers.
2. **And it has a hole where it doesn't.** Neither component *delivers* a tool. Under
   the pairing, `apt-get install jq` is denied twice (tprsh policy + dsh sandbox) and
   the agent still has no `jq`. tuprwre is the only affirmative answer: the sanctioned
   route that both gets the tool installed *and* keeps the host clean. Drop it and
   the deny message has nowhere to point.
3. **The real competitive pressure from dsh lands on tprsh, not tuprwre.** dsh ships
   confinement + approvals + replayable logs as part of a free harness. tprsh's
   surviving moats against that are exactly three: argument-level policy,
   tamper-evident audit, and harness-agnosticism. tuprwre's install/shim loop is
   untouched by dsh — nothing in dsh's architecture can replicate it.

**Caveat that could still justify retiring tuprwre:** if the accepted posture is
"agents use workspace-local toolchains only" (venvs, `node_modules`, GOBIN-in-workspace),
then system installs are rare enough to handle by human escalation, and
`workspace-write` covers daily work. That's a *policy choice to live without* the
capability, not a replacement of it — and it should be written down as such if taken.

**One interaction to design deliberately:** shims call `tuprwre run` → the Docker
daemon, and daemon-side effects happen *outside* any Seatbelt/Landlock sandbox. Under
dsh, a shim is therefore a sanctioned tunnel through the write-confinement. That's the
intended design (the container is its own boundary), but it must stay an explicit,
policy-visible tunnel — tprsh/dsh policy should treat `tuprwre`/`docker` invocations
as privileged, never generically allowed.

## Recommended composition (rather than replacement)

```
agent harness (dsh, Claude Code, …)      — the loop; may bring its own sandbox
  └─ tprsh                               — harness-agnostic verdicts + hash-chained audit
       └─ OS confinement                 — tprsh's own, or delegated to dsh's when under dsh
  └─ tuprwre                             — the sanctioned install path + native-feeling tools
```

Sources: deepseek-ai/deepseek-harness (GitHub), openclawlaunch.com/guides/deepseek-harness,
wincent's "List of coding agent sandboxes 2026-05" gist.
