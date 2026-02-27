# Contributing

Thank you for contributing to `tuprwre`.

## Development setup

- Fork and clone this repository.
- Open the project in your preferred editor.
- Ensure your Go toolchain matches `go.mod`.

## Quick start checks

Run these before submitting changes:

```bash
go test ./...
go vet ./...
go build ./cmd/tuprwre
```

## Submitting changes

- Keep PRs focused and small where possible.
- Add or update docs for behavior changes.
- Use clear commit messages and reference related issues.

## Before you open a PR

- [ ] `go test ./...`
- [ ] `go vet ./...`
- [ ] `go build ./cmd/tuprwre`
- [ ] You explained how you validated the change
