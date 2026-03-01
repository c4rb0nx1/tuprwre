# CLI Reference

This document is the canonical reference for `tuprwre` command syntax, options, and behavior as implemented by the Cobra command definitions and runtime code in `cmd/tuprwre` and `internal/config/config.go`.

## Mental model
- `shell`: run commands in a PATH-aware session where dangerous install commands are intercepted and blocked with guidance.
- `install`: execute allowed install commands in an isolated container, then generate shims for installed binaries.
- `shims`: transparently map those binaries on the host so users keep familiar command names.
- `run`: execute shimmed commands by launching the corresponding container image with the selected runtime options.

## Command reference

### about

Show a short human-friendly overview of tuprwre.

Usage:

```text
tuprwre about [flags]
```

Flags:
- `-h, --help`: bool, default `false` — help for about.

Notes/gotchas:
- Output is static descriptive text; this command does not alter configuration or state.

Examples:
- `tuprwre about`

### clean

Remove orphaned Docker images created by tuprwre.

Usage:

```text
tuprwre clean [flags]
```

Flags:
- `--dry-run`: bool, default `false` — list images without removing.
- `--force`: bool, default `false` — skip confirmation prompt.
- `-h, --help`: bool, default `false` — help for clean.

Notes/gotchas:
- If stopped containers are present, they are removed first; this step happens even when cleaning images.
- The command exits early with `No tuprwre images found` when no images exist.

Examples:
- `tuprwre clean`
- `tuprwre clean --dry-run`
- `tuprwre clean --force`

### completion

Generate the autocompletion script for tuprwre for the specified shell.

Usage:

```text
tuprwre completion [command]
```

Flags:
- `-h, --help`: bool, default `false` — help for completion.

Notes/gotchas:
- Available subcommands are `bash`, `fish`, `powershell`, and `zsh`.

Examples:
- `tuprwre completion bash`
- `tuprwre completion fish`

### doctor

Run preflight environment checks.

Usage:

```text
tuprwre doctor [flags]
```

Flags:
- `--json`: bool, default `false` — emit machine-readable JSON output.
- `-h, --help`: bool, default `false` — help for doctor.

Notes/gotchas:
- Command exits with an error when critical checks fail.
- `--json` mode emits structured checks and exits non-zero if critical checks fail.

Examples:
- `tuprwre doctor`
- `tuprwre doctor --json`

### help

Show help for any command in the application.

Usage:

```text
tuprwre help [command] [flags]
```

Flags:
- `-h, --help`: bool, default `false` — help for help.

Examples:
- `tuprwre help install`
- `tuprwre help run`
- `tuprwre help`

### init

Initialize a tuprwre config file.

Usage:

```text
tuprwre init [flags]
```

Flags:
- `--global`: bool, default `false` — create config in `~/.tuprwre/` instead of current directory.
- `-h, --help`: bool, default `false` — help for init.

Notes/gotchas:
- `--global` mode resolves base config directory from the active loaded config and writes `~/.tuprwre/config.json`.
- Workspace mode writes `.tuprwre/config.json` in the current working directory.
- Command fails with `config already exists` when target file already exists.

Examples:
- `tuprwre init`
- `tuprwre init --global`

### install

Run an installation script in a sandbox and generate shims.

Usage:

```text
tuprwre install [flags] -- <command>
```

Flags:
- `-i, --base-image`: string, default `ubuntu:22.04` — base Docker image to use for the sandbox.
- `-c, --container`: string, default `""` — resume from an already-prepared container ID (advanced recovery/debugging flow).
- `-n, --image`: string, default `""` — name for the committed image (auto-generated if not provided).
- `-s, --script`: string, default `""` — path to a local shell script.
- `-f, --force`: bool, default `false` — overwrite existing shims.
- `--memory`: string, default `""` — memory limit for the install container (e.g. `512m`, `1g`).
- `--cpus`: float, default `0` — CPU limit for the install container (e.g. `0.5`, `1.0`, `2.0`).
- `-h, --help`: bool, default `false` — help for install.

Notes/gotchas:
- Install requires a command unless `--script` is supplied.
- `--container` takes a pre-existing container ID; in this path existing container logs/metadata are committed and discovered as normal.
- If `--script` is set, the file is read and executed as `sh -s --` with any positional args passed as script arguments.
- This command is the only runtime that writes shim metadata used by `update`.
- Resource settings come from explicit flags first, then config defaults (`TUPRWRE_DEFAULT_MEMORY`, `TUPRWRE_DEFAULT_CPUS`).

Resource flags note:
Percentage-based defaults (e.g. '25%') resolve against Docker host limits. On macOS Docker Desktop, this means VM capacity, not full host hardware.

Examples:
- `tuprwre install -- "apt-get update && apt-get install -y jq"`
- `tuprwre install --base-image ubuntu:22.04 --image toolset:latest -- "curl -fsSL https://example.com/install-tool.sh | bash"`
- `tuprwre install --script ./install.sh`

### list

List installed shims.

Usage:

```text
tuprwre list [flags]
```

Flags:
- `--global`: bool, default `false` — show only shims installed outside a workspace context.
- `--workspace`: bool, default `false` — show only shims installed in the current workspace.
- `-h, --help`: bool, default `false` — help for list.

Notes/gotchas:
- `--global` and `--workspace` are mutually exclusive.
- Workspace-only filtering matches metadata workspace root; entries without workspace metadata are omitted from `--workspace`.

Examples:
- `tuprwre list`
- `tuprwre list --workspace`
- `tuprwre list --global`

### remove

Remove a generated shim.

Usage:

```text
tuprwre remove [shim] [flags]
```

Flags:
- `--all`: bool, default `false` — remove all shims and metadata.
- `--images`: bool, default `false` — remove Docker images referenced by shim metadata.
- `-h, --help`: bool, default `false` — help for remove.

Notes/gotchas:
- `--all` rejects extra positional arguments.
- Without `--all`, exactly one shim name is required.
- When `--images` is set, image removal is attempted after shim removal.

Examples:
- `tuprwre remove jq`
- `tuprwre remove --all`
- `tuprwre remove --all --images`

### run

Execute a command inside a sandboxed container (typically invoked by shims).

Usage:

```text
tuprwre run [flags] -- <binary> [args...]
```

Flags:
- `-i, --image`: string, default `""` — Docker image to run (required).
- `-w, --workdir`: string, default `` (empty, interpreted as current directory) — working directory inside container.
- `-e, --env`: stringArray, default `[]` — environment variables to pass (`KEY=VALUE`).
- `-v, --volume`: stringArray, default `[]` — volume mounts (`host:container`).
- `-r, --runtime`: string, default `docker` — container runtime (`docker|containerd`).
- `--debug-io`: bool, default `false` — print human-readable container I/O lifecycle diagnostics.
- `--debug-io-json`: bool, default `false` — emit container I/O diagnostics as NDJSON.
- `--capture-file`: string, default `""` — write combined stdout/stderr stream to a file.
- `--read-only-cwd`: bool, default `false` — mount working directory as read-only inside container.
- `--no-network`: bool, default `false` — disable container network access.
- `--memory`: string, default `""` — memory limit for the container (e.g. `512m`, `1g`).
- `--cpus`: float, default `0` — CPU limit for the container (e.g. `0.5`, `1.0`, `2.0`).
- `-h, --help`: bool, default `false` — help for run.

Notes/gotchas:
- `--image` is required and command fails if omitted.
- Current working directory is always mounted into the container, and default workdir is set to the host cwd.
- `--runtime containerd` is accepted by CLI parsing but currently returns a non-implemented runtime error in the run path.
- Same resource override precedence as install: CLI flags override config defaults.

Resource flags note:
Percentage-based defaults (e.g. '25%') resolve against Docker host limits. On macOS Docker Desktop, this means VM capacity, not full host hardware.

Examples:
- `tuprwre run --image toolset:latest -- node --version`
- `tuprwre run --image toolset:latest -v "$(pwd):/workspace" -- tool /workspace`
- `tuprwre run --image toolset:latest --workdir /tmp --memory 1g --cpus 1.0 -- tool --help`

### shell

Spawn an interactive shell with command interception enabled.

Usage:

```text
tuprwre shell [-c <command>] [flags]
```

Flags:
- `--allow`: stringArray, default `[]` — commands to exclude from interception.
- `-c, --command`: string, default `""` — run command string in non-interactive mode.
- `--intercept`: stringArray, default `[]` — additional commands to intercept.
- `-h, --help`: bool, default `false` — help for shell.

Notes/gotchas:
- Intercepted command behavior is to print a block message with guidance and exit with failure; it does not auto-route the command for execution.
- Intercept list starts from config and is extended/reduced by `--intercept` and `--allow`.
- `-c/--command` runs once in non-interactive POSIX proxy mode and is designed to stay quiet except when a command is explicitly blocked.

Examples:
- `tuprwre shell`
- `tuprwre shell -c "apt-get install -y jq"`
- `tuprwre shell --intercept brew --allow curl`

### update

Re-run install for a shim using stored metadata.

Usage:

```text
tuprwre update <shim> [flags]
```

Flags:
- `-h, --help`: bool, default `false` — help for update.

Notes/gotchas:
- Requires one shim name.
- If metadata is missing or incomplete, command explains how to reinstall with `tuprwre install`.

Examples:
- `tuprwre update jq`

## Configuration precedence

tuprwre uses layered JSON config (highest priority wins):

- CLI flags (`--intercept`, `--allow`)
- Env var (`TUPRWRE_INTERCEPT`)
- Workspace config (`.tuprwre/config.json` in cwd or nearest parent)
- Global config (`~/.tuprwre/config.json`)
- Built-in defaults

Code-level details:
- `--intercept`/`--allow` are shell command flags that affect only shell interception in that invocation.
- `TUPRWRE_INTERCEPT` replaces the intercept list from loaded config.
- Workspace config overrides global config where set.
- `TUPRWRE_DIR` overrides base data dir before config loading.
- `TUPRWRE_BASE_IMAGE`, `TUPRWRE_RUNTIME`, `TUPRWRE_DEFAULT_MEMORY`, and `TUPRWRE_DEFAULT_CPUS` are applied after file config with fallback defaults.

## Environment variables

- `TUPRWRE_DIR`: Override base data directory used for state and shim storage (default `~/.tuprwre`).
- `TUPRWRE_BASE_IMAGE`: Override default base image for installs.
- `TUPRWRE_RUNTIME`: Override runtime selection.
- `TUPRWRE_DEFAULT_MEMORY`: Override default memory setting (supports values such as `512m`, `1g`, `25%`).
- `TUPRWRE_DEFAULT_CPUS`: Override default CPU setting (supports values such as `2.0`, `50%`).
- `TUPRWRE_INTERCEPT`: Comma-separated intercept list override.

## Security model

`tuprwre` provides install and execution isolation for shimmed tools, with explicit limits:

- Dangerous install-style commands in shell mode are blocked and replaced with a guidance message.
- Install runs happen inside containers, and tool execution flows through generated shims that call `tuprwre run`.
- Additional execution hardening exists through `--read-only-cwd`, `--no-network`, `--memory`, and `--cpus`.

What `tuprwre` does not do:
- It does not automatically run blocked install commands; it blocks them and instructs users to call `tuprwre install`.
- It does not provide full-system sandboxing for all commands.
- It does not remove container-runtime trust requirements for the Docker daemon.
- It does not verify package supply-chain integrity inside install scripts.

## Troubleshooting

- If `go run ./cmd/tuprwre install --` reports “no installation command provided”, run it as `tuprwre install -- "..."` or use `--script`.
- If `tuprwre run` exits with “no binary specified” or “sandbox execution failed”, include the binary and re-run; for shim calls verify the shim exists in `~/.tuprwre/bin`.
- If `tuprwre remove` says `missing shim name`, call with a shim argument or add `--all`.
- If `tuprwre shell -c` or interactive mode still appears to do nothing, ensure `.tuprwre/bin` is on PATH and review `tuprwre doctor` for shim directory/runtime checks.
- If `tuprwre doctor` fails with runtime issues, check `TUPRWRE_RUNTIME`, Docker daemon availability, and verify config file JSON is valid.

## See also

- README.md
- ARCHITECTURE.md
- docs/demos.md
