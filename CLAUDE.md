# tuprwre — agent rules

This repo hosts two tools.

- **tuprwre** (Docker installs + shims) is **FROZEN**: maintenance only.
- Active work is **tprsh**: a local-first, harness-agnostic record / react /
  rebound layer for unattended AI agents.

tprsh is **not a sandbox**. Do not add confinement/enforce hardening
(Seatbelt/srt) or sandbox features.

## Git

- Never push to or merge into `main`. Work on feature branches; the owner merges PRs.
- No `Co-Authored-By` trailers.
- Commit identity is the owner's — do not change `git config`.

## Secrets

- Never commit secrets, API keys, or tokens. Keep personal absolute paths out of
  tracked files (generic mentions only).
- Tests use fake keys (AWS documented example key, `fake-*` tokens).

## Dependencies and checks

- Go stdlib only unless the owner approves a dependency.
- Always run, for touched packages:
  - `gofmt -l`
  - `go vet`
  - `go test -race -count=1`
- `scripts/gateway-e2e.sh` needs the `pi` CLI. Without it, run with
  `ALLOW_FAKE=1` and say so explicitly in your report.

## Architecture constraints

- Core must work with **zero** harness-specific adapters; hooks are optional extras.
- Remote classifiers / LLM monitors are optional plugins, never core.
- Redaction is default-on before anything is persisted or leaves the machine.

## Local edits

- Do not modify `cmd/tprsh/main.go` — the owner has an uncommitted local edit
  there. Add new code in new packages or binaries.
