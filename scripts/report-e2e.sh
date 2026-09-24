#!/usr/bin/env bash
#
# Fixture-driven end-to-end test for the effect pipeline:
#
#   Tetragon JSON export --(tprsh-sensor tetragon)--> effect JSONL
#   effect JSONL + gateway JSONL --(tprsh-report)--> reconciled report
#
# It builds the real binaries, pipes the synthetic Tetragon export
# (internal/sensor/tetragon/testdata/session.jsonl) through tprsh-sensor on
# stdin, and runs tprsh-report over the result together with the synthetic
# gateway log. It asserts that the recorded effects are redacted and 0600,
# that the report finds the expected covert candidates and red verdicts, and
# that --fail-on red exits 3.
#
# No live Tetragon or eBPF is involved: the input is a synthetic fixture.
#
# Env:
#   KEEP=1   keep the temp directory on exit
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

WORKDIR="$(mktemp -d "${TMPDIR:-/tmp}/tprsh-report-e2e.XXXXXX")"
cleanup() {
    local status=$?
    if [ "$status" -ne 0 ]; then
        echo "e2e: FAILED (status=$status); artifacts kept in $WORKDIR" >&2
    elif [ "${KEEP:-0}" = "1" ]; then
        echo "e2e: temp dir kept at $WORKDIR"
    else
        rm -rf "$WORKDIR"
    fi
}
trap cleanup EXIT

fail() {
    echo "e2e: ASSERTION FAILED: $*" >&2
    exit 1
}

echo "e2e: building binaries"
go build -o "$WORKDIR/tprsh-sensor" ./cmd/tprsh-sensor
go build -o "$WORKDIR/tprsh-report" ./cmd/tprsh-report

EFFECTS="$WORKDIR/state/effects.jsonl"
echo "e2e: tprsh-sensor tetragon < synthetic export"
"$WORKDIR/tprsh-sensor" tetragon --session-id sess-tg-1 --log "$EFFECTS" \
    < internal/sensor/tetragon/testdata/session.jsonl 2> "$WORKDIR/sensor.stderr"
cat "$WORKDIR/sensor.stderr"
grep -q "emitted=25 invalid=0 sink_errors=0 native_errors=0 ignored=3" "$WORKDIR/sensor.stderr" \
    || fail "unexpected sensor stats"

mode="$(stat -c '%a' "$EFFECTS" 2>/dev/null || stat -f '%Lp' "$EFFECTS")"
[ "$mode" = "600" ] || fail "effect log mode is $mode, want 600"
if grep -q "fake-token-0123456789" "$EFFECTS"; then
    fail "bearer token survived redaction in the effect log"
fi
cmp -s "$EFFECTS" internal/sensor/tetragon/testdata/session.expected.jsonl \
    || fail "binary output differs from the adapter golden file"

echo "e2e: tprsh-report (text)"
"$WORKDIR/tprsh-report" internal/report/testdata/gateway.jsonl "$EFFECTS" > "$WORKDIR/report.txt"
cmp -s "$WORKDIR/report.txt" internal/report/testdata/report.golden.txt \
    || fail "text report differs from internal/report/testdata/report.golden.txt"

echo "e2e: tprsh-report --json --fail-on red"
set +e
"$WORKDIR/tprsh-report" --json --fail-on red internal/report/testdata/gateway.jsonl "$EFFECTS" > "$WORKDIR/report.json"
code=$?
set -e
[ "$code" -eq 3 ] || fail "--fail-on red exited $code, want 3"

python3 - "$WORKDIR/report.json" <<'PY'
import json, sys
rep = json.load(open(sys.argv[1]))
s = {x["session_id"]: x for x in rep["sessions"]}["sess-tg-1"]
eff = {e["id"]: e for e in s["effects"]}
covert = [eff[i]["kind"] + ":" + eff[i]["summary"].split()[0] for i in s["covert_candidates"]]
want = ["exec:/usr/bin/cat", "file_read_sensitive:cat", "exec:/usr/bin/curl", "net_connect:curl", "exec:/usr/bin/sleep"]
assert covert == want, covert
rules = sorted({i["rule"] for i in s["intents"] if i["tier"] == "red"})
assert rules == ["git-force-push-protected", "iac-apply", "rm-outside-workspace"], rules
assert s["worst"] == "red" and s["tiers"]["red"] == 12, s["tiers"]
print("e2e: summary", json.dumps({"covert": len(covert), "tiers": s["tiers"], "red_intent_rules": rules}))
PY

echo "e2e: PASS (synthetic fixtures; no live sensor)"
