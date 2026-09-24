# Sensors and effect events

The gateway records a harness's **intent**: tool calls and tool results on the
LLM wire. A **sensor** records **effects** on the host: process executions,
file writes, reads of sensitive files, outbound connections, and process exits.
Comparing the two streams is how tprsh finds covert actions, meaning effects
with no matching tool call.

Sensors only observe. They never block, confine, or change what they see.

## Package layout

`internal/sensor` (Go standard library only):

| Symbol | Role |
|---|---|
| `Sensor` | Interface every adapter implements: `Name()` and `Run(ctx, out gateway.Sink)`. |
| `Record` | Runs a sensor. It stamps the sensor name, **validates**, **redacts (on by default)**, forwards to a `gateway.Sink`, and returns `Stats`. |
| `Validate` | The effect-event contract (below). |
| `Replay` | Reference `Sensor` that re-emits events already in the tprsh schema from JSONL. Used for fixtures and recorded logs. |
| `EffectKinds`, `IsEffect` | The effect kinds a sensor may emit. |

Adapters for Tetragon (Linux) and eslogger (macOS) will wrap each tool's native
output. **The adapter is the only code that knows a tool's native format.**
Downstream code sees only `event.Event`.

`Run` contract: it returns `nil` when the source is exhausted and `ctx.Err()`
when cancelled. Malformed native records are counted and skipped, never
returned as an error. `Run` fails only when the source itself cannot be read.

## Effect event schema

Effect events are ordinary `event.Event` records (schema version `"0"`; the
fields were added without breaking anything) with `source: "sensor"` and these
fields:

| Field | Set on | Meaning |
|---|---|---|
| `sensor` | all | Adapter name, e.g. `tetragon`. |
| `process` | all | `exec_id`, `parent_exec_id`, `pid`, `ppid`, `uid`, `binary`, `argv` (including argv[0]), `cwd`. `exec_id` is not reused, so it is the key for building a process tree. |
| `file` | `file_write`, `file_read_sensitive` | `path` (absolute). |
| `net` | `net_connect` | `protocol` (`tcp`/`udp`), `dst_addr` (IP), `dst_port`, optional `src_addr`/`src_port`. |
| `exit` | `proc_exit` | `code` and/or `signal`. |

For effect events, `time` is when the sensor observed the effect.
`session_id` is optional: a sensor usually cannot tell which harness session
caused an effect, so linking effects to sessions is left to the consumer (for
example by process tree).

A sample `exec` record:

```json
{"version":"0","id":"fx-exec-1","time":"2026-09-20T10:00:01.5Z","session_id":"sess-fixture-1","source":"sensor","kind":"exec","sensor":"fixture","process":{"exec_id":"fx:2001","parent_exec_id":"fx:2000","pid":2001,"ppid":2000,"uid":1000,"binary":"/usr/bin/git","argv":["git","push","--force","origin","main"],"cwd":"/home/agent/work"}}
```

## Contract

`Validate` rejects an event unless all of these hold. `Record` drops and counts
any event that fails, so nothing downstream sees one.

- `version` is current. `id`, `time`, and `sensor` are set. `source` is
  `sensor`. `kind` is an effect kind.
- `process` is present with `pid > 0`.
- `exec` has an absolute `process.binary`.
- File kinds have `file` with an absolute `path`.
- `net_connect` has `net` with `tcp`/`udp`, an IP `dst_addr`, and a port in
  1..65535.
- `proc_exit` has `exit` with a `code` or a `signal`.
- Each kind-specific payload appears only on its own kind.
- Gateway-only fields are absent: protocol, tool call, arguments, result.

## Contract fixtures

`internal/sensor/testdata/contract/` holds **synthetic** normalized samples:

- one valid record per effect kind
- `session.jsonl`, a scenario stream that hits the planned irreversible-action
  rules
- one invalid record per contract rule

Tests prove three things:

- Every valid fixture passes `Validate`.
- Every valid fixture decodes with no unknown fields and re-encodes
  byte-for-byte.
- Every invalid fixture is rejected for its intended reason.

An adapter's tests should translate native samples and compare the result with
fixtures in this form.

## Redaction

`Record` applies `gateway.DefaultRedactor` unless `Options.DisableRedaction` is
set. For effect events it redacts each `process.argv` string using the same
patterns as the gateway. It also replaces the argument after a bare
credential-naming flag, as in `--password hunter2` or `--token X`.

It does not redact `cwd`, `file.path`, or `binary`. Like all tprsh redaction,
it is pattern-based and best-effort. Short flags such as `mysql -pSECRET` are
not caught.
