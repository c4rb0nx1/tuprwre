# tprsh gate for Claude Code

Put tprsh's policy in front of an agent's shell commands **without changing your
login shell**. Nothing global is modified: the gate is a project-scoped hook, so
it applies only while the agent runs in the directory you choose, and removing
that directory removes the gate.

## What it does

Every `Bash` tool call is sent to `tprsh -check`, which renders a verdict using
the same parser and policy that `tprsh` uses to execute. Allowed commands pass
through untouched. Denied commands are blocked before they run, with tprsh's
reason shown to the agent. Either way the attempt lands in the audit log.

The check runs nothing — external commands are policy-checked and skipped, and
writes are discarded — so gating a command has no side effects.

## Setup

From the directory you want to protect:

```sh
mkdir -p .claude
TPRSH_REPO=/path/to/tuprwre
sed -e "s|__TPRSH_BIN__|$(command -v tprsh || echo "$TPRSH_REPO/tprsh")|" \
    -e "s|__WORKSPACE__|$PWD|" \
    -e "s|__AUDIT__|$PWD/.tprsh-audit.jsonl|" \
    -e "s|__GATE__|$TPRSH_REPO/contrib/claude-code/tprsh-gate.sh|" \
    "$TPRSH_REPO/contrib/claude-code/settings.template.json" > .claude/settings.json
```

Then start the agent in that directory. If the hook does not fire, open `/hooks`
once — Claude Code only watches `.claude/` in directories that had a settings
file when the session started.

## Removing it

```sh
rm -rf .claude/settings.json .tprsh-audit.jsonl
```

That is the whole footprint. No shell configuration, no dotfiles, no PATH
changes.

## Reading the audit log

Records are hash-chained; any edit to an interior record breaks verification of
everything after it.

```sh
jq -r '"\(.ts) \(.event)\t\(.args|join(" "))\t\(.reason // "")"' .tprsh-audit.jsonl
```

## Limits worth knowing

The gate enforces **policy**, not confinement: the approved command is then run
by the agent's normal shell, not by tprsh, so the OS sandbox and the in-process
interpreter are not in play. It answers "should this run?" and records the
answer. For the full boundary — confinement plus a complete ledger — the agent
has to execute *through* `tprsh` rather than being checked by it.

Binaries must resolve inside a root-owned directory (`/usr/bin`, `/bin`,
`/usr/sbin`, `/sbin`). Tools installed by Homebrew live in a group-writable
directory and are refused by design, since a writable tool directory would let
anything replace an approved binary.
