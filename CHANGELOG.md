# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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
