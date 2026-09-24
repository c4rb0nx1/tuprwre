#!/usr/bin/env bash
#
# End-to-end smoke test for cmd/tprsh-gateway.
#
# It starts a fake OpenAI Chat Completions upstream that streams a tool call for
# the harness's `bash` tool, points the real tprsh-gateway at it, and drives the
# gateway with a real Pi harness (PI_CODING_AGENT_DIR pointing at a throwaway
# models.json). It then asserts the recorded JSONL contains the tool-call intent
# and its matching tool result, that the secret-looking values in both were
# redacted, and that no event was dropped.
#
# Everything runs under a mktemp directory with fake credentials. When `pi` is
# unavailable (PI_BIN=missing), the script falls back to a stdlib-Python fake
# harness that replays the same two-turn loop; it prints a clear notice that the
# real-harness path did not run and why, and then exits 3 so a caller keying on
# exit status cannot mistake a fallback run for a real one.
#
# Env:
#   PI_BIN          harness binary (default: pi; set to "missing" to force the fallback)
#   ALLOW_FAKE=1    accept the fallback path as success (exit 0) instead of exit 3
#   UPDATE_EVIDENCE=1  refresh testdata/e2e/ from this run (default: leave committed evidence untouched)
#   KEEP=1          keep the temp directory on exit
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

PI_BIN="${PI_BIN:-pi}"
WORKDIR="$(mktemp -d "${TMPDIR:-/tmp}/tprsh-gateway-e2e.XXXXXX")"
UP_PID=""
GW_PID=""
HARNESS_MODE=""
FAILED=0

cleanup() {
    local status=$?
    [ -n "$GW_PID" ] && kill "$GW_PID" 2>/dev/null || true
    [ -n "$UP_PID" ] && kill "$UP_PID" 2>/dev/null || true
    wait 2>/dev/null || true
    if [ "$status" -ne 0 ] || [ "$FAILED" -ne 0 ]; then
        echo "e2e: FAILED (status=$status); artifacts kept in $WORKDIR" >&2
        return
    fi
    if [ "${KEEP:-0}" = "1" ]; then
        echo "e2e: temp dir kept at $WORKDIR"
    else
        rm -rf "$WORKDIR"
    fi
}
trap cleanup EXIT

fail() {
    FAILED=1
    echo "e2e: ASSERTION FAILED: $*" >&2
    exit 1
}

# wait_for_grep FILE PATTERN SECONDS: poll FILE for PATTERN, then print matches.
wait_for_grep() {
    local file="$1" pattern="$2" secs="$3" i=0
    while [ "$i" -lt "$secs" ]; do
        if [ -f "$file" ] && grep -q "$pattern" "$file"; then
            grep "$pattern" "$file"
            return 0
        fi
        i=$((i + 1))
        sleep 1
    done
    return 1
}

echo "e2e: workdir $WORKDIR"

# --- build the gateway ------------------------------------------------------
echo "e2e: building tprsh-gateway"
go build -buildvcs=false -o "$WORKDIR/tprsh-gateway" ./cmd/tprsh-gateway

# --- fake upstream ----------------------------------------------------------
cat > "$WORKDIR/fake_upstream.py" <<'PY'
#!/usr/bin/env python3
"""Fake OpenAI Chat Completions upstream.

Turn 1 streams a `bash` tool call whose arguments contain an AWS documented
example key and a token=... assignment. Turn 2 (a request that already carries a
role=tool message) streams a final assistant text and finish_reason=stop.
Body is delimited by connection close, which the clients handle correctly.
"""
import json
import sys
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

REQ_LOG = sys.argv[2]
TOOL_CALL_ID = "call_e2e_1"


def chunk(delta, finish=None):
    return {
        "id": "chatcmpl-e2e",
        "object": "chat.completion.chunk",
        "created": int(time.time()),
        "model": "tprsh-e2e",
        "choices": [{"index": 0, "delta": delta, "finish_reason": finish}],
    }


class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def do_POST(self):
        length = int(self.headers.get("Content-Length") or 0)
        raw = self.rfile.read(length) if length else b""
        try:
            obj = json.loads(raw)
        except Exception:
            obj = {}
        messages = [m for m in obj.get("messages", []) if isinstance(m, dict)]
        has_tool_result = any(m.get("role") == "tool" for m in messages)
        with open(REQ_LOG, "a") as f:
            f.write(json.dumps({
                "path": self.path,
                "roles": [m.get("role") for m in messages],
                "has_tool_result": has_tool_result,
            }) + "\n")

        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream")
        self.send_header("Cache-Control", "no-cache")
        self.send_header("Connection", "close")
        self.close_connection = True
        self.end_headers()

        def sse(event):
            self.wfile.write(b"data: " + json.dumps(event).encode() + b"\n\n")
            self.wfile.flush()

        if not has_tool_result:
            args = json.dumps({
                "command": "echo AKIAIOSFODNN7EXAMPLE tprsh-e2e-ok",
                "note": "token=FAKE-e2e-secret-123",
            })
            sse(chunk({"role": "assistant", "tool_calls": [{
                "index": 0, "id": TOOL_CALL_ID, "type": "function",
                "function": {"name": "bash", "arguments": args},
            }]}))
            sse(chunk({}, finish="tool_calls"))
        else:
            sse(chunk({"role": "assistant", "content": "done"}))
            sse(chunk({}, finish="stop"))
        self.wfile.write(b"data: [DONE]\n\n")
        self.wfile.flush()

    def log_message(self, *a):
        pass


def main():
    srv = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    print(srv.server_address[1], flush=True)
    srv.serve_forever()


if __name__ == "__main__":
    main()
PY

python3 "$WORKDIR/fake_upstream.py" "$WORKDIR" "$WORKDIR/upstream-requests.jsonl" \
    > "$WORKDIR/upstream.port" 2> "$WORKDIR/upstream.err" &
UP_PID=$!
UP_PORT="$(wait_for_grep "$WORKDIR/upstream.port" '[0-9]' 10 || true)"
if [ -z "$UP_PORT" ]; then
    cat "$WORKDIR/upstream.err" >&2 || true
    fail "fake upstream did not start"
fi
echo "e2e: fake upstream on 127.0.0.1:$UP_PORT"

# --- gateway ----------------------------------------------------------------
"$WORKDIR/tprsh-gateway" \
    --listen 127.0.0.1:0 \
    --upstream "http://127.0.0.1:$UP_PORT" \
    --log "$WORKDIR/gateway.jsonl" \
    --session-id e2e-session \
    > "$WORKDIR/gateway.out" 2> "$WORKDIR/gateway.err" &
GW_PID=$!
GW_LINE="$(wait_for_grep "$WORKDIR/gateway.out" '^listening ' 10 || true)"
if [ -z "$GW_LINE" ]; then
    cat "$WORKDIR/gateway.err" >&2 || true
    fail "gateway did not print its listening line"
fi
GW_URL="${GW_LINE#listening }"
echo "e2e: gateway at $GW_URL"

# --- harness ----------------------------------------------------------------
mkdir -p "$WORKDIR/pi-agent"
cat > "$WORKDIR/pi-agent/models.json" <<JSON
{
  "providers": {
    "tprshgw": {
      "baseUrl": "${GW_URL}/v1",
      "api": "openai-completions",
      "apiKey": "fake-e2e-key",
      "models": [ { "id": "tprsh-e2e" } ]
    }
  }
}
JSON

# --- fallback: stdlib-Python fake harness -----------------------------------
# Replays the same two-turn OpenAI Chat Completions loop without Pi: request 1
# gets the tool call, executes its command, and request 2 carries the result.
cat > "$WORKDIR/fake_harness.py" <<'PY'
#!/usr/bin/env python3
"""Minimal fake harness for the gateway e2e fallback."""
import http.client
import json
import subprocess
import sys


BASE = sys.argv[1]
HOST, PORT = BASE[len("http://"):].split(":")


def post(payload):
    conn = http.client.HTTPConnection(HOST, int(PORT), timeout=30)
    conn.request("POST", "/v1/chat/completions",
                 json.dumps(payload).encode(), {"Content-Type": "application/json"})
    resp = conn.getresponse()
    body = resp.read().decode()
    conn.close()
    return body


turn1 = {
    "model": "tprsh-e2e",
    "stream": True,
    "messages": [{"role": "user", "content": "run the check"}],
}
sse = post(turn1)
tool_call = None
for line in sse.splitlines():
    if not line.startswith("data: "):
        continue
    data = line[len("data: "):]
    if data == "[DONE]":
        continue
    obj = json.loads(data)
    for choice in obj.get("choices", []):
        for tc in choice.get("delta", {}).get("tool_calls", []) or []:
            tool_call = tc
if tool_call is None:
    sys.stderr.write("fake harness: no tool call in first response\n")
    sys.exit(1)

call_id = tool_call["id"]
arguments = tool_call["function"]["arguments"]
command = json.loads(arguments)["command"]
output = subprocess.run(command, shell=True, capture_output=True, text=True).stdout

turn2 = {
    "model": "tprsh-e2e",
    "stream": True,
    "messages": [
        {"role": "user", "content": "run the check"},
        {"role": "assistant", "content": None, "tool_calls": [
            {"id": call_id, "type": "function",
             "function": {"name": "bash", "arguments": arguments}},
        ]},
        {"role": "tool", "tool_call_id": call_id, "content": output},
    ],
}
post(turn2)
print("done")
PY

USE_PI=0
if [ "$PI_BIN" != "missing" ] && command -v "$PI_BIN" > /dev/null 2>&1; then
    USE_PI=1
fi

if [ "$USE_PI" = "1" ]; then
    HARNESS_MODE="pi"
    echo "e2e: driving the gateway with real harness '$PI_BIN' (cwd $WORKDIR)"
    # Hard 90s timeout without relying on the non-portable `timeout` tool.
    (
        cd "$WORKDIR"
        PI_CODING_AGENT_DIR="$WORKDIR/pi-agent" "$PI_BIN" \
            --provider tprshgw --model tprsh-e2e --no-session --approve \
            -p "run the check" > "$WORKDIR/harness.out" 2> "$WORKDIR/harness.err"
    ) &
    HARNESS_PID=$!
    HARNESS_WAIT=0
    while kill -0 "$HARNESS_PID" 2>/dev/null; do
        if [ "$HARNESS_WAIT" -ge 90 ]; then
            kill "$HARNESS_PID" 2>/dev/null || true
            wait "$HARNESS_PID" 2>/dev/null || true
            fail "harness did not finish within 90s"
        fi
        HARNESS_WAIT=$((HARNESS_WAIT + 1))
        sleep 1
    done
    wait "$HARNESS_PID" || fail "harness exited non-zero; see $WORKDIR/harness.err"
else
    HARNESS_MODE="fake"
    echo "e2e: WARNING: real harness not run: '$PI_BIN' is unavailable; using stdlib-Python fake harness" >&2
    python3 "$WORKDIR/fake_harness.py" "$GW_URL" > "$WORKDIR/harness.out" 2> "$WORKDIR/harness.err" \
        || fail "fake harness failed"
fi

echo "e2e: harness output: $(tr '\n' ' ' < "$WORKDIR/harness.out" | head -c 200)"

# --- shut the gateway down so it drains and prints Stats --------------------
kill -TERM "$GW_PID" 2>/dev/null || true
GW_WAIT=0
while kill -0 "$GW_PID" 2>/dev/null; do
    if [ "$GW_WAIT" -ge 15 ]; then
        kill -KILL "$GW_PID" 2>/dev/null || true
        break
    fi
    GW_WAIT=$((GW_WAIT + 1))
    sleep 1
done
wait "$GW_PID" 2>/dev/null || true
GW_PID=""

# --- raw-secret check (must find nothing) -----------------------------------
for secret in AKIAIOSFODNN7EXAMPLE FAKE-e2e-secret-123; do
    if grep -qF "$secret" "$WORKDIR/gateway.jsonl" "$WORKDIR/gateway.err"; then
        fail "raw secret '$secret' leaked into the gateway log"
    fi
done
echo "e2e: raw-secret grep found nothing (redaction held)"

# --- assertions -------------------------------------------------------------
cat > "$WORKDIR/check.py" <<'PY'
#!/usr/bin/env python3
import json
import sys

log_path, stats_path = sys.argv[1], sys.argv[2]
events = []
with open(log_path) as f:
    for line in f:
        line = line.strip()
        if line:
            events.append(json.loads(line))

intents = [e for e in events if e.get("kind") == "tool_call_intent"]
results = [e for e in events if e.get("kind") == "tool_result"]

bash_intents = [e for e in intents if e.get("tool_name") == "bash"]
if len(bash_intents) != 1:
    sys.exit(f"want exactly one bash tool_call_intent, got {len(bash_intents)}: {intents}")
intent = bash_intents[0]
if intent.get("complete") is not True:
    sys.exit(f"bash tool_call_intent not complete: {intent}")
if intent.get("session_id") != "e2e-session":
    sys.exit(f"unexpected session_id: {intent.get('session_id')}")

call_id = intent.get("tool_call_id")
if not call_id:
    sys.exit("tool_call_intent has no tool_call_id")

args = intent.get("arguments")
if isinstance(args, str):
    args = json.loads(args)
args_text = json.dumps(args)
if "[REDACTED:aws_access_key_id]" not in args_text:
    sys.exit(f"intent arguments missing aws redaction marker: {args_text}")
if "[REDACTED:credential]" not in args_text:
    sys.exit(f"intent arguments missing credential redaction marker: {args_text}")
if intent.get("redacted") is not True:
    sys.exit("intent not flagged redacted")

matching = [e for e in results if e.get("tool_call_id") == call_id]
if len(matching) != 1:
    sys.exit(f"want exactly one tool_result for {call_id}, got {len(matching)}: {results}")
result = matching[0]
result_text = json.dumps(result.get("result"))
if result_text.count("tprsh-e2e-ok") != 1:
    sys.exit(f"tool_result must contain tprsh-e2e-ok exactly once: {result_text}")
if "[REDACTED:aws_access_key_id]" not in result_text:
    sys.exit(f"tool_result missing aws redaction marker: {result_text}")

with open(stats_path) as f:
    stats = [json.loads(l) for l in f if l.strip().startswith("{")]
if not stats:
    sys.exit("no stats line found on stderr")
final = stats[-1]
if final.get("EventsDropped") != 0:
    sys.exit(f"events were dropped: {final}")
if final.get("EventsEmitted", 0) < 2:
    sys.exit(f"expected at least 2 emitted events: {final}")

print(json.dumps({
    "events": len(events),
    "intents": len(intents),
    "results": len(results),
    "tool_call_id": call_id,
    "stats": final,
}))
PY

SUMMARY="$(python3 "$WORKDIR/check.py" "$WORKDIR/gateway.jsonl" "$WORKDIR/gateway.err")" \
    || fail "assertions failed"
echo "e2e: assertions passed"
echo "e2e: harness_mode=$HARNESS_MODE"
echo "e2e: summary $SUMMARY"

# --- preserve redacted evidence --------------------------------------------
# Committed fixtures under testdata/e2e/ are the real-harness evidence; a local
# run must not clobber them unless it explicitly opts in.
if [ "${UPDATE_EVIDENCE:-0}" = "1" ]; then
    ART_DIR="$REPO_ROOT/testdata/e2e"
    mkdir -p "$ART_DIR"
    cp "$WORKDIR/gateway.jsonl" "$ART_DIR/gateway.jsonl"
    cp "$WORKDIR/gateway.err" "$ART_DIR/gateway.stderr"
    cp "$WORKDIR/upstream-requests.jsonl" "$ART_DIR/upstream-requests.jsonl"
    cp "$WORKDIR/harness.out" "$ART_DIR/harness.txt"
    {
        echo "harness_mode=$HARNESS_MODE"
        echo "$SUMMARY"
    } > "$ART_DIR/summary.txt"
    echo "e2e: updated committed evidence in $ART_DIR"
else
    echo "e2e: left testdata/e2e/ untouched (set UPDATE_EVIDENCE=1 to refresh it)"
fi

# --- exit status reflects which harness actually ran ------------------------
if [ "$HARNESS_MODE" = "pi" ]; then
    echo "e2e: PASS (pi harness)"
    exit 0
fi
if [ "${ALLOW_FAKE:-0}" = "1" ]; then
    echo "e2e: PASS (fake harness; ALLOW_FAKE=1)"
    exit 0
fi
echo "e2e: FAILED: the real harness did not run ('$PI_BIN' unavailable); set ALLOW_FAKE=1 to accept the fallback" >&2
exit 3
