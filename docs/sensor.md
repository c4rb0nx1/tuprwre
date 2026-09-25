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
| `IsSensitivePath` | Fixed, lexical list of credential locations (cloud/cluster configs, SSH private keys, token stores, `.env`, key and keystore files). Used by adapters to classify reads, and by `tprsh-report`. |
| `ReadLine` | Bounded line reader shared by line-delimited adapters. |
| `SubtreeFilter`, `ProcessKey`, `ParentKey` | Scope a stream to one process subtree; process identity by exec id, or pid when there is none. |

Adapters wrap each tool's native output. Tetragon (Linux) is implemented
(below); eslogger (macOS) is planned.

**The adapter is the only code that knows a tool's native format.**
Downstream code sees only `event.Event`.

`Run` contract: it returns `nil` when the source is exhausted and `ctx.Err()`
when cancelled. Malformed native records are counted and skipped, never
returned as an error. `Run` fails only when the source itself cannot be read.

## Tetragon adapter (Linux)

`internal/sensor/tetragon` reads Tetragon's JSON event export: one
`GetEventsResponse` per line, from `tetra getevents -o json` or from the
agent's export file.

| Tetragon record | Effect kind |
|---|---|
| `process_exec` | `exec` (skipped when flagged `procFS`, i.e. the process already existed when Tetragon started) |
| `process_exit` | `proc_exit`: `signal` if set, otherwise `code` from `status` (a missing `status` means 0) |
| `process_kprobe` on a `*connect*` hook with `sock_arg` / `sockaddr_arg` | `net_connect` (`IPPROTO_TCP`/`IPPROTO_UDP`, or inferred from the hook name) |
| `process_connect` (legacy/enterprise event type) | `net_connect` |
| `process_kprobe` `security_file_permission` with mask `MAY_WRITE`, `security_path_truncate`, or a `sys_write`-family syscall hook | `file_write` |
| `process_kprobe` `security_file_permission` with mask `MAY_READ`, `security_file_open`, `fd_install`, or a `sys_read`-family syscall hook, on a path `sensor.IsSensitivePath` accepts | `file_read_sensitive` |

Every other record is counted as ignored. This includes `process_loader`, any
other hook, and reads of files that aren't sensitive. A record that can't be
translated is counted as an error, never emitted: bad JSON, no time, no pid, no
path, no address, or an unknown protocol.

The integer mask is read from `int_arg`, `uint_arg`, or `long_arg` (64-bit
integers arrive as JSON strings). Tetragon renders some file paths without the
leading slash (`etc/passwd`); the adapter restores it. The flags `nocwd` and
`errorCWD` drop the cwd.

Event IDs are derived from a hash of the native line, so reading the same
export twice produces the same IDs. Consumers can deduplicate on `id`.

**argv.** Tetragon joins arguments with single spaces and wraps any argument
that contains a space in double quotes, without escaping; an empty argument is
written as `""`. The adapter inverts that encoding, so argv is exact unless an
argument itself contains a double quote next to a space. Older Tetragon
releases joined without quoting, and the adapter reads those as
space-separated words. `argv[0]` is the binary path. Redaction still handles a
token split off from its `Bearer`/`Basic` scheme.

### What has been verified

| Check | Status |
|---|---|
| Export format against Tetragon source (`cilium/tetragon` a58fbc7): argument quoting (`pkg/sensors/exec` `resolveArgs`), exit `status` = wait status `>> 8` and `signal` names (`pkg/grpc/exec`), kprobe argument and socket field names (`api/v1/tetragon`), flag strings (`pkg/reader/exec`) | Done |
| Real recorded events: the 34 exec/exit/kprobe/connect samples in Tetragon's documentation (`docs/security-observability-with-ebpf`) all translate and pass the contract. Run with `TPRSH_TETRAGON_SRC=/path/to/tetragon go test ./internal/sensor/tetragon -run Upstream` (samples are not vendored). | Done |
| `docs/tetragon/tprsh-effects.yaml` parses and validates with Tetragon's own loader (`pkg/tracingpolicy.FromFile`); a bad operator is rejected. | Done |
| A live Tetragon agent on this pipeline | **Not done.** Tetragon and its BPF objects build in the cloud VM (root, BTF present), but mounting bpffs/tracefs to start the agent was not permitted there. |

The synthetic session fixture
(`internal/sensor/tetragon/testdata/session.jsonl`) uses Tetragon's real
argument encoding. Its normalized, redacted output is pinned in
`session.expected.jsonl` (regenerate with
`go test ./internal/sensor/tetragon -update`).

[`docs/tetragon/tprsh-effects.yaml`](tetragon/tprsh-effects.yaml) is an
example TracingPolicy (observe-only) that produces the kprobe events above. It
uses the `Mask` operator, so an open for read and write (mask 6) matches both
selectors.

### Running it: `tprsh-sensor`

```bash
go build -o tprsh-sensor ./cmd/tprsh-sensor

# Pair with the gateway by giving both the same session id.
tetra getevents -o json | tprsh-sensor tetragon --session-id "$SID"

# Or translate an export file after the fact.
tprsh-sensor tetragon --input /var/run/cilium/tetragon/tetragon.log --session-id "$SID" --log effects.jsonl
```

| Flag | Meaning |
|---|---|
| `--input PATH` | Native stream; `-` (default) is stdin. To follow a growing file, pipe `tail -F`. |
| `--log PATH` | JSONL effect log, created `0600`. Default: `$XDG_STATE_HOME/tprsh/sensor/<session-id>.jsonl`. |
| `--session-id ID` | Stamped on every event. Default: random. |
| `--root-pid PID` | Record only that process and its descendants, e.g. the harness. `0` (default) records everything. |
| `--no-redact` | Record raw argv. Prints a warning. |

The stream is attributed to one session as a whole. On a host running several
agents, run one `tprsh-sensor` per agent with `--root-pid`:

```bash
claude -p "fix the build" &
tetra getevents -o json | tprsh-sensor tetragon --root-pid $! --session-id "$SID"
```

`--root-pid` uses `sensor.SubtreeFilter`. A process joins the subtree when:

- its pid is the root pid,
- its parent is already in the subtree, or
- its parent pid is the root pid. This covers a root that was exec'd before
  the stream started.

After the root exits, its pid stops matching, so a later process that reuses
the pid is not adopted. Only processes seen in the stream can be adopted, so
start the stream before the agent spawns tools.

On shutdown, `tprsh-sensor` prints a stats line to stderr: `emitted`,
`invalid`, `sink_errors`, `native_errors`, `ignored`, and `outside_subtree`
(events left out by `--root-pid`; these still count in `emitted`).

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
caused an effect. `tprsh-sensor --session-id` stamps one id on a whole stream,
and `tprsh-report` ([`report.md`](report.md)) pairs it with the gateway log
that has the same id.

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
credential-naming flag, as in `--password hunter2` or `--token X`, and the
argument after a bare `Bearer`/`Basic` scheme.

It does not redact `cwd`, `file.path`, or `binary`. Like all tprsh redaction,
it is pattern-based and best-effort. Short flags such as `mysql -pSECRET` are
not caught.
