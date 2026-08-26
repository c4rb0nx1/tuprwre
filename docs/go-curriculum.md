# Go curriculum — from Python 3, through this codebase

*Learner profile: fluent Python 3 thinking, strong shell, year of vibe-coding. Goal:
read and write Go for real, using tuprwre/tprsh as the textbook. Driven by the
`/go-tutor` skill — the tutor reads this file, resumes at the first unchecked lesson,
and checks it off when you've demonstrated it (not just heard it).*

**Progress: check a box only after the learner has done the exercise and explained the
concept back in their own words.**

## Lessons

- [x] **L0 · The mental model.** Compiled, statically typed, one binary; `go run` /
  `go build` / `go test`; gofmt is law. *Python contrast:* no venv/pip — `go.mod` pins
  everything; no `__pycache__`, no interpreter on the target machine.
  *Exercise:* `make build`, run `./tprsh` REPL, trigger one allow and one deny, run
  `go test ./internal/tprsh/`.

- [ ] **L1 · Packages, visibility, main.** Package = directory, not file; Capitalized
  = exported (vs Python's `_underscore` convention); `cmd/` vs `internal/` layout.
  *Read:* `cmd/tprsh/main.go`. *Exercise:* add a `-quiet` or `-version` flag end to end.

- [ ] **L2 · Errors are values.** `if err != nil`, `%w` wrapping, `errors.As/Is`,
  custom error types; `defer` vs Python's `with`/`finally`. *Read:* `internal/tprsh/audit.go`
  (`LoadAndVerify`), `main.go`'s `errors.As(&de)`. *Exercise:* make `-verify` also print
  the timestamp of the last intact record.

- [ ] **L3 · Structs, methods, pointers.** Struct vs class; receiver vs `self`; pointer
  vs value receivers (Python names are already references — Go makes the choice explicit);
  zero values vs `__init__`. *Read:* `Auditor` in `audit.go`. *Exercise:* add
  `(*Auditor).Head()` returning the current chain head hash, with a test.

- [ ] **L4 · Maps, slices, config-as-data.** `map[string]ArgPolicy` vs dict; slices vs
  lists (append semantics!); the `v, ok := m[k]` idiom vs `KeyError`. *Read:*
  `internal/tprsh/policy.go` allowlist. *Exercise:* add one binary with two flags to the
  allowlist, plus a test proving a third flag is denied.

- [ ] **L5 · Table-driven tests.** The `[]struct{...}` cases loop vs
  `pytest.mark.parametrize`; `t.Run` subtests; `t.TempDir`. *Read:*
  `internal/tprsh/confine_srt_test.go` (`TestShQuote`). *Exercise:* add two adversarial
  cases to `TestShQuote` and predict pass/fail before running.

- [ ] **L6 · Functions as values, closures, init().** Function-typed struct fields
  (`Validate`), functions returning functions (`simpleInner`) vs Python
  closures/decorators; why `init()` exists (the wrapper/allowlist cycle). *Read:*
  `internal/tprsh/policy_wrappers.go`. *Exercise:* add a `nice`-style wrapper that
  recurses into its inner command, with tests.

- [ ] **L7 · Interfaces.** Structural typing = duck typing enforced at compile time; no
  `implements` keyword; the null object (`nopConfiner`); small interfaces as seams.
  *Read:* `Confiner` in `confine.go`, then `seatbelt` and `srtConfiner` side by side.
  *Exercise:* sketch (don't ship) a `loggingConfiner` that wraps another Confiner —
  the decorator pattern without decorators.

- [ ] **L8 · Concurrency, part 1.** `sync.Mutex` and why `Auditor` needs it; goroutines
  vs threads (no GIL) vs asyncio (no colored functions). *Read:* `audit.go` locking,
  then skim `internal/sandbox/exec.go` for `go func()` + `io.Copy`. *Exercise:* explain
  what breaks if the mutex is removed; write a `-race` test that proves it.

- [ ] **L9 · Consuming libraries; functional options; middleware.** `interp.New(interp.Dir(...))`
  vs kwargs; handler-wrapping (`ExecHandlers(func(next...))`) vs decorators. *Read:*
  `internal/tprsh/shell.go` + `check.go`. *Exercise:* trace `echo hi | wc -c` from parse
  to verdict to exec, narrating each hop.

- [ ] **L10 · context.Context and subprocesses.** `ctx` threading vs nothing-in-Python;
  `os/exec` vs `subprocess`; streaming stdio. *Read:* `internal/sandbox/sandbox.go`
  (`runViaExec`, `runWithContext`). *Exercise:* explain where a Ctrl-C lands and what
  cleans up.

- [ ] **L11 · Modules, build tags, tooling.** `go.mod`/`go.sum` vs requirements.txt +
  lockfile; `//go:build integration`; `deadcode`, `staticcheck`, `go vet` (all used on
  this repo on 2026-08-23). *Exercise:* run the full toolchain sweep yourself and read
  one report.

- [ ] **L12 · Capstone: policy-as-data.** Ship the real roadmap item: move the tprsh
  allowlist from Go source into a versioned JSON policy file with a loader, validation,
  and tests — designed and written by the learner, reviewed by the tutor. This touches
  L2 (errors), L3 (structs), L4 (maps/JSON tags), L5 (tests), L7 (a PolicySource
  interface if ambitious). *Done = merged locally with `go test ./...` green.*

## Tutor's log

(The tutor appends dated one-line notes here: what was covered, what wobbled, what to
revisit.)
- 2026-08-24 · L0 done: static linking (found the four macOS dylibs via `otool -L` and explained why they're exempt), ran tprsh REPL, saw flag-level pre-exec denial (`uname -x`), ran `go test` and met the cache. Wobbled on: cross-compilation — thought one binary runs everywhere; corrected to one binary per GOOS/GOARCH. Also briefly confused tuprwre's shim shell with tprsh — worth re-touching the two-binary layout in L1.
