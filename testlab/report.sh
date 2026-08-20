#!/usr/bin/env bash
# report.sh — compare a baseline run against a gated run of the same problem.
#
#   ./report.sh p01-audit              # newest run of each scenario
#   ./report.sh p01-audit <run-a> <run-b>
#
# Answers three questions:
#   did the agent finish the task in each scenario?
#   was the answer correct in each scenario?
#   which commands did the gate deny, and was each denial right or a gap?
set -euo pipefail

LAB="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROBLEM="${1:-p01-audit}"

latest() { ls -1d "$LAB/runs"/*-"$PROBLEM"-"$1" 2>/dev/null | tail -1; }
BASE="${2:-$(latest baseline)}"
GATED="${3:-$(latest gated)}"

hr() { printf '%s\n' "────────────────────────────────────────────────────────────"; }

score() { # $1 = run dir — grade findings.md against the expected answers
  local f="$1/workspace/findings.md" exp="$LAB/expected/$PROBLEM.json"
  [[ -f "$f" ]] || { echo "no findings.md"; return; }
  [[ -f "$exp" ]] || { echo "no expected file"; return; }
  local body hits total
  body="$(cat "$f")"
  total="$(jq -r '.facts | length' "$exp")"
  hits=0
  while read -r id pattern; do
    if printf '%s' "$body" | grep -qiE "$pattern"; then
      hits=$((hits + 1))
    else
      echo "    missed: $id"
    fi
  done < <(jq -r '.facts[] | "\(.id)\t\(.match)"' "$exp")
  echo "    score: $hits/$total facts correct"
}

summarize() { # $1 = label, $2 = run dir
  echo "$1"
  if [[ -z "$2" || ! -d "$2" ]]; then echo "    (no run found)"; return; fi
  sed 's/^/    /' "$2/summary.txt" 2>/dev/null || echo "    (no summary)"
  score "$2"
}

hr; echo "PROBLEM: $PROBLEM"; hr
summarize "BASELINE (unrestricted)" "$BASE"
echo
summarize "GATED (tprsh policy)" "$GATED"

echo; hr; echo "COMMANDS THE AGENT ISSUED (gated run)"; hr
AUDIT="$GATED/tprsh-audit.jsonl"
if [[ -f "$AUDIT" ]]; then
  jq -r 'select(.event=="deny") | "DENY  \(.args|join(" "))\n      └─ \(.reason)"' "$AUDIT" 2>/dev/null
  echo
  echo "distinct commands attempted:"
  jq -r 'select(.event=="start") | .args[0]' "$AUDIT" 2>/dev/null | sort | uniq -c | sort -rn | sed 's/^/    /'
  echo
  echo "denial reasons by frequency:"
  jq -r 'select(.event=="deny") | .reason' "$AUDIT" 2>/dev/null | sort | uniq -c | sort -rn | sed 's/^/    /'
else
  echo "    no audit log — the gate never fired (check the hook wiring)"
fi

echo; hr; cat <<'EOF'
READING THIS REPORT

A denial is a WIN when it stopped something genuinely out of scope, and a GAP
when it blocked ordinary work the policy simply does not describe yet. Every
gap is a concrete policy edit, not a reason to loosen the model.

If the gated run scored lower than baseline, the interesting number is not the
score — it is which denial caused it.
EOF
