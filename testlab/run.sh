#!/usr/bin/env bash
# run.sh — run one problem under one scenario, headless and reproducible.
#
#   ./run.sh p01-audit baseline          # dry run: prints the command, spends nothing
#   ./run.sh p01-audit baseline --go     # actually runs it
#   ./run.sh p01-audit gated --go
#
# Scenarios
#   baseline  the agent runs unrestricted, exactly as it would normally
#   gated     identical, plus the tprsh PreToolUse gate via --settings
#
# Every run gets a fresh copy of the fixture, so runs never contaminate each
# other and a run from six months ago is reproducible today. Cost is capped by
# --max-budget-usd and reported from the result JSON.
set -euo pipefail

LAB="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="$(cd "$LAB/.." && pwd)"

PROBLEM="${1:-}"
SCENARIO="${2:-}"
GO="${3:-}"

MODEL="${TPRSH_LAB_MODEL:-haiku}"
BUDGET="${TPRSH_LAB_BUDGET:-0.25}"
FIXTURE="${TPRSH_LAB_FIXTURE:-svc-audit}"

if [[ -z "$PROBLEM" || -z "$SCENARIO" ]]; then
  sed -n '2,12p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
  exit 2
fi
if [[ "$SCENARIO" != "baseline" && "$SCENARIO" != "gated" ]]; then
  echo "scenario must be 'baseline' or 'gated'" >&2
  exit 2
fi

PROMPT_FILE="$LAB/problems/$PROBLEM.txt"
[[ -f "$PROMPT_FILE" ]] || { echo "no such problem: $PROMPT_FILE" >&2; exit 2; }

TPRSH_BIN="${TPRSH_BIN:-$REPO/tprsh}"
if [[ "$SCENARIO" == "gated" && ! -x "$TPRSH_BIN" ]]; then
  echo "building tprsh..." >&2
  (cd "$REPO" && go build -o tprsh ./cmd/tprsh)
fi

STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
RUN="$LAB/runs/$STAMP-$PROBLEM-$SCENARIO"
WORK="$RUN/workspace"

# --- assemble the invocation -------------------------------------------------
ARGS=(
  --print
  --output-format json
  --model "$MODEL"
  --max-budget-usd "$BUDGET"
  --permission-mode bypassPermissions
  --include-hook-events
)

if [[ "$SCENARIO" == "gated" ]]; then
  # Passed via --settings so the gate leaves no trace in the workspace.
  ARGS+=(--settings "$RUN/gate-settings.json")
fi

if [[ "$GO" != "--go" ]]; then
  cat <<EOF
DRY RUN — nothing was executed and nothing was spent.

  problem   $PROBLEM
  scenario  $SCENARIO
  model     $MODEL
  budget    \$$BUDGET (hard cap enforced by the CLI)
  fixture   $FIXTURE
  workspace $WORK

would run, with the prompt on stdin:
  claude ${ARGS[*]}

Re-run with --go to execute.
EOF
  exit 0
fi

# --- prepare an isolated, identical workspace --------------------------------
mkdir -p "$WORK"
cp -a "$LAB/fixtures/$FIXTURE/." "$WORK/"
cp "$PROMPT_FILE" "$RUN/prompt.txt"

AUDIT="$RUN/tprsh-audit.jsonl"
if [[ "$SCENARIO" == "gated" ]]; then
  sed -e "s|__TPRSH_BIN__|$TPRSH_BIN|" \
      -e "s|__WORKSPACE__|$WORK|" \
      -e "s|__AUDIT__|$AUDIT|" \
      -e "s|__GATE__|$REPO/contrib/claude-code/tprsh-gate.sh|" \
      "$REPO/contrib/claude-code/settings.template.json" > "$RUN/gate-settings.json"
fi

# --- record exactly what produced this run -----------------------------------
cat > "$RUN/manifest.json" <<EOF
{
  "problem": "$PROBLEM",
  "scenario": "$SCENARIO",
  "model": "$MODEL",
  "budget_usd": "$BUDGET",
  "fixture": "$FIXTURE",
  "started": "$STAMP",
  "tprsh_commit": "$(cd "$REPO" && git rev-parse --short HEAD 2>/dev/null || echo unknown)",
  "claude_version": "$(claude --version 2>/dev/null | head -1 || echo unknown)"
}
EOF

echo "running $PROBLEM / $SCENARIO (model=$MODEL, cap=\$$BUDGET)..." >&2
set +e
(cd "$WORK" && claude "${ARGS[@]}" < "$RUN/prompt.txt") > "$RUN/result.json" 2> "$RUN/stderr.log"
echo "$?" > "$RUN/exit_code"
set -e

# --- summarize ---------------------------------------------------------------
{
  echo "run:      $RUN"
  echo "exit:     $(cat "$RUN/exit_code")"
  if jq -e . "$RUN/result.json" >/dev/null 2>&1; then
    echo "cost_usd: $(jq -r '.total_cost_usd // "n/a"' "$RUN/result.json")"
    echo "turns:    $(jq -r '.num_turns // "n/a"' "$RUN/result.json")"
    echo "duration: $(jq -r '.duration_ms // "n/a"' "$RUN/result.json") ms"
  else
    echo "cost_usd: unparseable result (see stderr.log)"
  fi
  if [[ -f "$AUDIT" ]]; then
    echo "denials:  $(grep -c '"event":"deny"' "$AUDIT" 2>/dev/null || echo 0)"
  fi
  echo "findings: $([[ -f "$WORK/findings.md" ]] && echo written || echo MISSING)"
} | tee "$RUN/summary.txt"
