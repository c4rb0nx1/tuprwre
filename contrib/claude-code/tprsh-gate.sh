#!/usr/bin/env bash
# tprsh-gate: a Claude Code PreToolUse hook that asks tprsh whether a Bash
# command is permitted, and blocks it with tprsh's reason when it is not.
#
# Nothing global is changed by using this. Point a project's
# .claude/settings.json at this script and the gate applies only while Claude
# Code runs in that directory; delete the directory and the gate is gone.
#
# Config via environment (set in the settings.json hook command):
#   TPRSH_BIN        path to the tprsh binary (default: tprsh on PATH)
#   TPRSH_WORKSPACE  workspace root (default: current directory)
#   TPRSH_AUDIT      audit log path (default: <workspace>/.tprsh-audit.jsonl)
set -uo pipefail

TPRSH_BIN="${TPRSH_BIN:-tprsh}"
TPRSH_WORKSPACE="${TPRSH_WORKSPACE:-$PWD}"
TPRSH_AUDIT="${TPRSH_AUDIT:-$TPRSH_WORKSPACE/.tprsh-audit.jsonl}"

payload="$(cat)"
command_text="$(printf '%s' "$payload" | jq -r '.tool_input.command // empty')"

# Nothing to judge (non-Bash call or empty command): stay out of the way.
if [[ -z "$command_text" ]]; then
  exit 0
fi

# Fail closed: if the gate itself cannot run, deny rather than silently
# allowing everything. A broken guard that permits is worse than no guard.
if ! command -v "$TPRSH_BIN" >/dev/null 2>&1 && [[ ! -x "$TPRSH_BIN" ]]; then
  jq -n --arg r "tprsh gate unavailable: $TPRSH_BIN not found" '{
    hookSpecificOutput: {
      hookEventName: "PreToolUse",
      permissionDecision: "deny",
      permissionDecisionReason: $r
    }
  }'
  exit 0
fi

verdict="$("$TPRSH_BIN" -workspace "$TPRSH_WORKSPACE" -audit "$TPRSH_AUDIT" \
  -check -c "$command_text" 2>&1)"
status=$?

if [[ $status -eq 0 ]]; then
  # Allowed by policy. Emit nothing and let the normal permission flow decide;
  # the attempt is already recorded in the audit log.
  exit 0
fi

jq -n --arg r "blocked by tprsh policy: $verdict" '{
  hookSpecificOutput: {
    hookEventName: "PreToolUse",
    permissionDecision: "deny",
    permissionDecisionReason: $r
  }
}'
exit 0
