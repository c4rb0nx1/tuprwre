# tuprwre Agent Guidelines

Welcome to the `tuprwre` codebase. 

`tuprwre` is an action-level sandbox designed to protect host machines from autonomous AI agents by intercepting dangerous shell commands (like `npm install`) and routing them into ephemeral Docker containers.

## Current Architectural State
1. **Interception (`cmd/tuprwre/shell.go`)**: A subshell wrapper that injects a custom directory into `$PATH`. When an agent types `apt install`, it hits our bash wrapper instead of the host binary. The wrapper blocks the execution and instructs the agent to use `tuprwre install`.
2. **Sandbox (`cmd/tuprwre/install.go` & `internal/sandbox`)**: Runs the requested command inside a Docker container.
3. **Discovery (`internal/discovery`)**: Diffs the Docker container against its baseline to see what tools were newly installed.
4. **Shims (`internal/shim`)**: Creates host-side scripts (e.g., `~/.tuprwre/bin/jq`) that transparently route execution back into the sandbox.

## POSIX Proxy Shell (Completed)

`tuprwre shell` now supports non-interactive POSIX proxy execution, including `-c` mode for IDE/TUI automation flows and strict non-interactive output behavior.

### Completion Notes
- `-c` command execution is supported via shell pass-through.
- Non-interactive execution is silent on success (`stdout` is reserved for command output only).
- Interception behavior still preserves sandbox context through environment exports (`TUPRWRE_SESSION_ID` and sandboxed `PATH`) so wrappers remain effective.

### Session Support Status (Current)
- Session management is not currently exposed as a top-level command surface.

## Post-POSIX Roadmap (Prioritized)

### 1) Shim lifecycle commands (`list/remove/update`) [Completed]
- `tuprwre list` - enumerate installed shims.
- `tuprwre remove <shim>` - delete a shim and its associated metadata.
- `tuprwre update <shim>` - rerun install logic for an existing shim using stored metadata.

### 2) `tuprwre doctor` preflight command [Completed]
`tuprwre doctor` now provides actionable checks for:
- `tuprwre` binary/runtime prerequisites
- container availability and configuration
- PATH/shim interception health
- minimal diagnostics to guide setup and troubleshooting before first use

Both commands are visible from top-level `tuprwre --help` and part of the release gate for roadmap items 1, 3, and 5.

### 3) containerd runtime track [Priority: P3, future]
Expand runtime support for `containerd` beyond `docker`, including:
- runtime detection and selection ergonomics
- container lifecycle parity
- robust compatibility and migration notes for existing workflows
