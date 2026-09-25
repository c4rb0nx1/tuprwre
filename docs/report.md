# tprsh-report

`tprsh-report` (`cmd/tprsh-report`) reads recorded **intent** (`tprsh-gateway`
logs) and recorded **effects** (`tprsh-sensor` logs) and reconciles them per
session. For each session it shows:

- the **tool intents**: what the model asked the harness to run
- the **effects**: what actually happened on the host, each linked to the tool
  call that explains it
- the **covert candidates**: effects that no tool call explains
- the **would-be tier** of every item (green, yellow, or red) under a small,
  fixed rule set for irreversible actions

The report only reads logs. It never blocks, pauses, or changes anything, and
the tiers describe what the agent *would* have hit.

It reads the tprsh event schema only, never a harness's or sensor's native
format. It needs no harness-specific adapter, and it works with gateway logs
alone, sensor logs alone, or both.

## Run it

```bash
go build -o tprsh-report ./cmd/tprsh-report

# Record one session with the same id on both sides...
tprsh-gateway --upstream https://api.anthropic.com --session-id "$SID" --log gw.jsonl &
tetra getevents -o json | tprsh-sensor tetragon --session-id "$SID" --log fx.jsonl &

# ...then reconcile.
tprsh-report gw.jsonl fx.jsonl
tprsh-report --json --fail-on red gw.jsonl fx.jsonl   # machine-readable; exit 3 on any red
```

| Flag | Meaning |
|---|---|
| `LOG...` | Gateway and sensor JSONL logs, in any order and mix. `-` is stdin. |
| `--workspace DIR` | Workspace used by the `rm` rule. Default: inferred per session (below). |
| `--json` | Emit the report as JSON. |
| `--fail-on yellow\|red` | Exit `3` when any item reaches that tier. |
| `--window DUR` | For a tool call with no recorded result, how long afterwards its effects may occur. Default `10m`. |
| `--slack DUR` | Clock skew tolerated between gateway and sensor timestamps. Default `2s`. |
| `--taint-window DUR` | `credential-then-egress` fires only for egress within this long after the most recent credential read. Default `0`: the rest of the session. |
| `--ignore-path DIR` | Absolute `DIR`. File writes under it are expected background activity (e.g. a harness's own state or cache directory). They are shown as `[ignored path]` and are never covert candidates. Repeatable. Credential reads are never ignored. |
| `--protected-branch NAME` | Protected branch; a trailing `*` matches any suffix (`release/*`). Repeatable; replaces the default list. |
| `--prod-context SUBSTR` | Case-insensitive substring marking a kubectl context as production. Repeatable; replaces the default `prod`. |

Events are grouped by `session_id`; events without one go into session
`(none)`. Records with the same `id` are loaded once, so passing a log twice or
replaying a Tetragon export is harmless. Malformed lines are counted and
skipped. Sensor events that fail the effect contract (`sensor.Validate`) are
counted and dropped.

## Linking effects to tool calls

A tool call's **window** runs from its intent time until its result (or
`--window` if no result was recorded), widened by `--slack` on both sides.
Links are made in this order:

1. **descendant**: a process whose ancestor (via `parent_exec_id`, or `ppid`
   when there's no exec id) is already linked to a call inherits that call.
2. **command-line**: a process whose argv contains the call's whole command as
   a contiguous run of words, e.g. `bash -c "<command>"`.
3. **binary**: a process running a program the call's command names, e.g.
   `terraform` for `cd infra && terraform apply`.
4. **process**: a file, network, or exit effect of a linked process.
5. **path** / **address**: a file effect whose path the call names (as in
   `Write {"file_path": ...}`), or a connection whose IP address appears in the
   call's arguments.

Commands are found without harness-specific adapters. The report looks for
conventional argument keys:

- `command`, `cmd`, `script`: a shell string (Claude Code, Pi) or an argv array
  (Codex `shell`)
- `cwd`, `workdir`: the working directory
- keys containing `path` or `file`, plus any value that is a single absolute
  path

Shell strings are split with a small lexer: quotes, `&& || ; |`, `$(...)`,
and `cd` tracking. Wrappers (`sudo`, `env`, `timeout`, `xargs`, `sh -c`, ...)
are peeled off.

**Session root.** An exec whose parent was never exec'd in the session, and is
that parent's only such child, is the session's launch (normally the harness).
It is not expected to match a tool call. If several execs share an unseen
parent, that parent is a process that was already running when the sensor
started (for example a Tetragon `procFS` process). That parent is treated as
the harness, and its children stay ordinary processes. Effects of the harness
process itself are labelled `(by the harness process)`.

**Covert candidates** are unlinked effects of kind `exec`, `file_write`,
`file_read_sensitive`, or `net_connect`. These are excluded:

- session-root execs
- `proc_exit` events
- connections to loopback (the harness talking to the gateway)
- writes under `--ignore-path`

## Rules (`internal/rules`)

Rules are fixed, local, and deterministic, and every verdict names its rule and
reason. They apply to intents (the parsed command, and paths named in the
arguments) and to `exec` effects (argv and cwd).

| Rule | Tier | Matches |
|---|---|---|
| `iac-apply` | red | `terraform`/`tofu apply\|destroy`, `terragrunt [run-all] apply\|destroy` |
| `kubectl-prod` | red | `kubectl delete\|apply` with a `--context` containing `prod` (see `--prod-context`) |
| `kubectl-context-unknown` | yellow | `kubectl delete\|apply` without `--context` |
| `git-force-push-protected` | red | force push (`-f`, `--force`, `--force-with-lease`, `+refspec`, `--mirror`) to a protected branch. The defaults are `main`, `master`, `trunk`, `develop`, `prod`, `production`, `release/*`, and `release-*` (see `--protected-branch`). |
| `git-delete-protected` | red | `git push --delete` / `:branch` of a protected branch |
| `git-force-push` | yellow | force push to another branch, or the branch isn't named |
| `rm-outside-workspace` | red | an `rm` operand outside the workspace (`~`/`$HOME`, `..` escapes, absolute paths elsewhere). `/tmp`, `/var/tmp`, and `/dev/shm` contents are allowed. With no workspace known, only `/`, system directories, and home paths are red. |
| `rm-workspace-unknown` | yellow | an `rm` of another absolute path, or an upward escape, when no workspace is known |
| `credential-read` | yellow | a command or tool call naming a credential file, or a `file_read_sensitive` effect (see `sensor.IsSensitivePath`) |
| `credential-then-egress` | red | network egress after a credential read in the same session. Egress means a non-loopback `net_connect`, or a `curl`/`wget`/`nc`/`scp`/`ssh`/remote-`rsync`... command. It also fires when a single command both reads a credential file and sends data off the host. |

**Workspace.** `--workspace` wins. Otherwise the cwd of the session's root
exec is used, or failing that the cwd of the first child of an unseen harness.
In a gateway-only session the workspace is unknown, so the `rm` rule uses its
conservative mode.

## Limits

- **Heuristic linking.** Linking uses time windows and text matching. A
  concurrent, unrelated process that runs the same program inside a call's
  window is linked to that call. A tool that execs something the call never
  names (a build script's children aside, which are linked as descendants)
  shows up as covert.
- **Shell parsing is best-effort.** Variables, globs, aliases, functions, and
  scripts in files are not evaluated. `bash script.sh` is classified as
  `bash`, not by the script's contents.
- **Credential taint is session-wide by default.** After a credential read,
  every later egress in the session is red, including legitimate traffic such
  as `git push`. `--taint-window` narrows this; taint is still not tracked
  per process, because a covert read-then-send usually runs in two sibling
  processes (`cat`, then `curl`).
- **Harness writes are noisy.** Files the harness writes for itself (state,
  caches) are covert candidates unless a tool call names them or they fall
  under `--ignore-path`. They are labelled `(by the harness process)`. No
  harness-specific paths are built in.
- The rule set is intentionally small, and tiers are not policy: nothing is
  promoted into enforcement automatically.

## Test fixtures

- `internal/report/testdata/gateway.jsonl`: synthetic gateway log for session
  `sess-tg-1` (matching the Tetragon fixture) plus a gateway-only session. It
  includes one duplicate and one malformed line.
- `internal/report/testdata/report.golden.txt`: pinned text report over that
  log plus `internal/sensor/tetragon/testdata/session.expected.jsonl`.
  Regenerate with `go test ./internal/report -update`.
- `scripts/report-e2e.sh` builds `tprsh-sensor` and `tprsh-report`, and pipes
  the synthetic Tetragon export through both binaries. It checks the effect
  log (redacted, `0600`, equal to the adapter golden file), the text report
  (equal to the report golden file), the JSON covert candidates and red rules,
  and `--fail-on red` exiting `3`. No live sensor is involved.
