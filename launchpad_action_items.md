# Launchpad Action Items

## Launch verdict

- 🚫 **No-go** for a public stable launch right now
- ✅ **Go** for an **alpha / technical preview** (explicitly labeled experimental)

## Today-only execution list (Top 5 unblockers)

> Objective: remove the biggest launch risks in one workday.

1. **Add a minimum CI gate** `[MISSING]`
   - Add `.github/workflows/ci.yml` with: `go test ./...`, `go vet ./...`, `go build ./cmd/tuprwre`
   - **Done when:** PRs fail on red checks and pass on green checks.

2. **Enforce runtime flag validity in `run`** `[NEEDS-WORK]`
   - Reject unknown runtime values at CLI boundary (`docker|containerd` only)
   - If `containerd` is selected, return explicit “not implemented yet” (until real support lands)
   - **Done when:** `tuprwre run --runtime totallyfake ...` fails fast with clear error.

3. **Make `doctor` truthful about runtime support** `[NEEDS-WORK]`
   - If runtime is `containerd`, mark health as failed (or degraded) until execution path exists
   - Keep Docker daemon check only for Docker mode
   - **Done when:** no healthy report is emitted for unsupported runtime paths.

4. **Fix `install` command parsing** `[NEEDS-WORK]`
   - Support full command after `--` even if user forgets outer quotes
   - Stop using only `args[0]`; reconstruct command from all args
   - **Done when:** both forms work:
     - `tuprwre install -- "echo AAA BBB"`
     - `tuprwre install -- echo AAA BBB`

5. **Resolve module/repo identity mismatch** `[NEEDS-WORK]`
   - Align `go.mod` module path and README repo path to the same canonical source
   - Update imports/tests accordingly
   - **Done when:** `go test ./...` passes and docs/install instructions point to one repo identity.

### If time remains today (quick risk reducer)

- **Tighten security messaging**: add explicit non-goals/threat model limitations to README to avoid over-claiming.

## Status legend

- `[READY]`
- `[NEEDS-WORK]`
- `[MISSING]`

---

## P0 — Launch blockers (fix before public launch)

1. **Infrastructure / CI-CD**
   - `[MISSING]` Automated pipeline
   - No GitHub Actions (or equivalent) release gate

2. **Product runtime integrity (`docker` vs `containerd`)**
   - `[NEEDS-WORK]`
   - `run --runtime totallyfake` still executes
   - Runtime flag is not enforced; behavior is misleading

3. **Preflight reliability (`doctor`)**
   - `[NEEDS-WORK]`
   - `doctor --json` can report healthy for `TUPRWRE_RUNTIME=containerd`
   - Current execution path still effectively depends on Docker
   - This creates false confidence

4. **Install command parsing correctness**
   - `[NEEDS-WORK]`
   - `install` uses only `args[0]`
   - Unquoted multi-word commands get truncated
   - High risk of user confusion/failure

5. **Packaging / module identity correctness**
   - `[NEEDS-WORK]`
   - `go.mod` uses `github.com/c4rb0nx1/tuprwre`
   - README points to `github.com/c4rb0nx1/tuprwre`
   - Public consumers may hit trust/install issues

6. **Security claim vs reality**
   - `[NEEDS-WORK]`
   - Bypass/scope caveats + RW cwd mount behavior
   - Likely external criticism if framed as hardened sandbox

---

## P1 — High priority (immediately after blockers)

1. **Core feature completeness**
   - `[NEEDS-WORK]`
   - Core commands exist and run, but runtime abstraction/interception are still MVP-level

2. **Critical bug surface coverage**
   - `[NEEDS-WORK]`
   - `go test -cover` shows 0% in:
     - `internal/config`
     - `internal/discovery`
     - `internal/shim`

3. **Error handling + UX consistency**
   - `[NEEDS-WORK]`
   - Inconsistencies around `run --container` messaging vs `--image` requirement

4. **Monitoring / alerting**
   - `[MISSING]`
   - No operational telemetry or alerting loop

5. **Logging strategy**
   - `[NEEDS-WORK]`
   - Mostly ad hoc stdout/stderr prints
   - No structured logging/support-bundle path

6. **Backup / recovery guidance**
   - `[MISSING]`
   - No documented recovery workflow for `~/.tuprwre` state/metadata

7. **Environment separation / release channels**
   - `[NEEDS-WORK]`
   - No explicit dev/beta/stable channel strategy

8. **Troubleshooting depth**
   - `[NEEDS-WORK]`
   - Some troubleshooting exists, not enough for first-wave support load

9. **FAQ**
   - `[MISSING]`
   - No FAQ doc

10. **Usage analytics**
    - `[MISSING]`
    - No usage/event instrumentation

11. **Error tracking (Sentry/etc.)**
    - `[MISSING]`
    - No crash/error collection pipeline

12. **Feedback channel**
    - `[MISSING]`
    - No explicit issue/contact/community route in launch docs

13. **NPS / survey mechanism**
    - `[MISSING]`
    - No post-launch sentiment loop

---

## P2 — Polish / non-blocking

1. `[READY]` README quality
2. `[READY]` Getting started guide
3. `[READY]` Empty states (e.g., `list` with no shims)
4. `[NEEDS-WORK]` Loading/progress output consistency
5. `[READY]` Mobile responsiveness (N/A for CLI)
6. `[READY]` SSL (N/A for local CLI)
7. `[READY]` Value proposition clarity
8. `[NEEDS-WORK]` CTA effectiveness (no launch funnel/site)
9. `[MISSING]` Pricing model clarity (if commercial intent)
10. `[NEEDS-WORK]` Competitor differentiation messaging
11. `[MISSING]` Dedicated landing page/site assets

---

## Day-one risk snapshot

1. **What breaks/confuses users first**
   - Install command quoting behavior and runtime flag mismatch

2. **What breaks under load/adoption**
   - Low test coverage in key internal packages + no CI guardrails

3. **What gets roasted publicly (HN risk)**
   - Security posture claims stronger than current practical guarantees

---

## What ships / what doesn’t

- **Ships now:** private alpha / technical preview to power users
- **Does not ship now:** broad public launch marketed as stable/reliable/security-hardened
