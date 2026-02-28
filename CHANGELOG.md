# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.0-alpha.3] - 2026-03-01

### Fixed
- Removed deprecated `--container` flag from `run` command (was unused, conflicted with required `--image`)
- Issue templates: added `assignees: []` for full GitHub community profile recognition

### Added
- README: documented `remove --all` and `clean` commands in usage section

### Changed
- Release badge switched from `github/v/release` to `github/v/tag` (prereleases now visible)
- Version bumped to 0.1.0-alpha.3

## [0.1.0-alpha.2] - 2026-03-01

### Fixed
- CLI help text: `--container` corrected to `--image` in root command example
- Release workflow YAML syntax error (`cd dist` → `working-directory: dist`)
- Issue templates now recognized by GitHub (added `title:` fields and `config.yml`)
- `tuprwre list` filters non-alphanumeric shim names (defense-in-depth for `[` bug)

### Added
- Read-only root filesystem enforced during `run` phase (`ReadonlyRootfs: true`, `/tmp` tmpfs)
- Support section in README (bug reports, feature requests, discussions links)
- Asciinema demo recordings in README and `docs/demos.md`
- Issue template `config.yml` routing blank issues to Discussions

### Changed
- Version bumped to 0.1.0-alpha.2

## [0.1.0-alpha.1] - 2026-02-28

### Added
- Initial technical preview release
- `tuprwre shell` for interactive interception of dangerous commands
- `tuprwre install` for sandboxed execution and shim generation
- `tuprwre run` for transparent execution of sandboxed binaries
- `tuprwre list`, `remove`, and `update` for shim lifecycle management
- `tuprwre doctor` for preflight environment checks
- `tuprwre clean` for container and image garbage collection
- Security hardening flags (`--read-only-cwd`, `--no-network`, `--memory`, `--cpus`)
- Support for local script installation via `--script`
- Comprehensive test coverage for core packages
- GitHub Actions CI/CD pipeline and automated releases
