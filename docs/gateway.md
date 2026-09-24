# tprsh-gateway

`tprsh-gateway` (`cmd/tprsh-gateway`) is a **record-only, harness-agnostic**
reverse proxy that sits in front of an LLM API. Point a coding agent's base URL
at it and it forwards every request and response byte-for-byte, while passively
extracting two observations into a JSONL log:

- `tool_call_intent` — the model asked the harness to call a tool (parsed from the
  streaming response).
- `tool_result` — the harness sent a tool result back (parsed from the next request).

It never modifies traffic, never blocks a call, and is a research component of the
tprsh runtime, **not a security boundary**. It works with any harness that speaks
the Anthropic Messages, OpenAI Responses, or OpenAI Chat Completions wire protocols
— selected by request path (`/v1/messages`, `/v1/responses`,
`/v1/chat/completions`). It does not care which harness produced the traffic.

## Run it

```bash
go build -o tprsh-gateway ./cmd/tprsh-gateway

# Loopback only; ephemeral port by default. Prints "listening http://127.0.0.1:PORT".
tprsh-gateway --upstream https://api.anthropic.com
```

Useful flags:

| Flag | Meaning |
|---|---|
| `--upstream URL` | Required. Upstream LLM API base URL. |
| `--listen HOST:PORT` | Default `127.0.0.1:0` (ephemeral). Loopback only unless `--allow-remote`. |
| `--log PATH` | JSONL event log. Default: `$XDG_STATE_HOME/tprsh/gateway/<session-id>.jsonl` (or `~/.local/state/...`). |
| `--no-log` | Disable recording entirely; forward without writing events. Warns. |
| `--session-id ID` | Session id stamped on every event (default: random). |
| `--no-redact` | Disable redaction (records raw payloads). Warns. |
| `--allow-remote` | Permit a non-loopback `--listen` bind. |

The log path is printed on stderr at startup. The log file is created `0600` and
its parent directory `0700`; if a pre-existing file (or the default state
directory) grants group/other access, it is tightened to `0600`/`0700` with a
warning. The parent of an explicit `--log` path is never chmod'ed.

## Wiring a harness

The gateway is transparent: set the harness's API base URL to the gateway's
address and keep the real API key in the harness. The key is forwarded upstream
unchanged.

**Claude Code** — override the Anthropic base URL:

```bash
tprsh-gateway --listen 127.0.0.1:8787 --upstream https://api.anthropic.com &
export ANTHROPIC_BASE_URL=http://127.0.0.1:8787
claude
```

**Codex** — OpenAI Responses API. Add a provider and select it:

```toml
# ~/.codex/config.toml
model_provider = "tprshgw"

[model_providers.tprshgw]
name = "tprsh-gateway"
base_url = "http://127.0.0.1:8787/v1"
wire_api = "responses"
```

Or without editing config:

```bash
codex -c model_provider="tprshgw" \
      -c 'model_providers.tprshgw={name="tprsh-gateway",base_url="http://127.0.0.1:8787/v1",wire_api="responses"}'
```

**Pi** — add a provider to `models.json` (e.g. `$PI_CODING_AGENT_DIR/models.json`):

```json
{
  "providers": {
    "tprshgw": {
      "baseUrl": "http://127.0.0.1:8787/v1",
      "api": "openai-completions",
      "apiKey": "real-key-here",
      "models": [ { "id": "tprsh-e2e" } ]
    }
  }
}
```

```bash
pi --provider tprshgw --model tprsh-e2e -p "run the check"
```

## What is recorded

One JSON object per line. `tool_call_intent` carries `protocol`, `tool_call_id`,
`tool_name`, `arguments`, `complete` (the protocol's terminating event was seen),
and `truncated`. `tool_result` carries `tool_call_id` and `result`. Every event
carries `id`, `time`, `session_id`, `source`, `kind`, `redacted`, and
`redaction_count`. Results are deduplicated within one gateway process (harnesses
resend the whole conversation each turn). A final `Stats` line
(`EventsEmitted`/`EventsDropped`/`SinkErrors`/`ExtractorErrors`) is printed on
stderr at shutdown.

## Redaction

Redaction is **on by default** and applies pattern-based substitution to
`arguments` and `result` only. It rewrites string values — never structure — so
recorded payloads stay valid JSON. Recognized shapes include PEM private keys,
AWS access-key ids and secret access keys, `Bearer` tokens, HTTP Basic
credentials, URL userinfo passwords, `sk-`/`sk-ant-`/GitHub/Slack/JWT tokens, and
generic `password`/`secret`/`token`/`api_key` assignments. Matches become
`[REDACTED:<kind>]`.

**Redaction is best-effort, not a guarantee.** It is pattern-based and will miss
secrets that do not match a known shape (arbitrary tokens, novel providers,
values split across JSON fields). Treat the log as sensitive regardless. Residual
notes: values redacted by regex are re-marshalled with `encoding/json`, which
HTML-escapes `<`, `>`, and `&` across the recorded payload; large integers are
preserved via `json.Number`. Use `--no-redact` only with throwaway credentials.

## Known limitations

- **Subscription / OAuth auth is untested.** Only plain API-key forwarding has
  been exercised end-to-end; interactive OAuth flows that rely on device
  callbacks or refresh round-trips are not validated.
- **The gateway sees intent, not effects.** It cannot confirm that a tool
  actually ran, or what it touched on the host. The effect side (the `Sensor`
  interface, the effect event schema and its contract) is described in
  [`sensor.md`](sensor.md). No live OS adapter has landed yet.
- **No dedup across gateway restarts.** Result dedup is per-process only, so a
  restart mid-conversation can re-record historical tool results.
- Encodings other than `identity`/gzip are forwarded but not parsed (no events
  extracted); extractor errors are counted, never surfaced as payload changes.
