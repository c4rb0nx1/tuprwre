# Effect-event contract fixtures

Every file here is **synthetic**: hand-authored in the tprsh event schema
(`internal/event`), not captured from a live sensor. Paths, PIDs, exec ids and
addresses are illustrative (`203.0.113.0/24` is a documentation range); the only
credential-shaped value is a `fake-*` token used to exercise redaction.

These are the normalized form every sensor adapter must produce and every
downstream consumer (e.g. `tprsh-report`) may rely on. `contract_test.go` proves
each valid fixture passes `sensor.Validate`, decodes with no unknown fields, and
re-encodes byte-for-byte, so a schema change that drops or renames a field fails
here first.

Valid, one event per file:

- `exec.json` — process execution with full argv, cwd and exec-id lineage
- `file_write.json` — file write
- `file_read_sensitive.json` — read of a credential file
- `net_connect.json` — outbound TCP connection with source endpoint
- `proc_exit.json` — normal exit with status 0
- `proc_exit_signal.json` — signal death of a root process, no session id

Scenario stream:

- `session.jsonl` — one agent session: the harness exec, `terraform apply`,
  `rm -rf` outside the workspace, a credential read followed by egress (with a
  fake bearer token in argv), a workspace write, and `git push --force`.

Invalid, each violating exactly one contract rule (see `contract_test.go` for
the expected reason):

- `invalid/*.json`
