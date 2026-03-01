# tuprwre

> Keep host systems safe when automation installs software.

[![CI](https://github.com/c4rb0nx1/tuprwre/actions/workflows/ci.yml/badge.svg)](https://github.com/c4rb0nx1/tuprwre/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/tag/c4rb0nx1/tuprwre?label=release)](https://github.com/c4rb0nx1/tuprwre/releases)
[![Go Version](https://img.shields.io/github/go-mod/go-version/c4rb0nx1/tuprwre)](https://golang.org)
[![License](https://img.shields.io/github/license/c4rb0nx1/tuprwre)](LICENSE)
[![Stars](https://img.shields.io/github/stars/c4rb0nx1/tuprwre)](https://github.com/c4rb0nx1/tuprwre/stargazers)


`tuprwre` is an action-level sandbox for AI and automation environments. It intercepts dangerous install commands on the host, runs those install scripts in disposable Docker containers, then publishes host-side shims for safe tool execution.

## TL;DR

- Agent tries `apt` / `npm -g` / `pip` on host → `tuprwre` intercepts it.
- You run `tuprwre install -- "<command>"` and it executes safely in an ephemeral Docker container.
- `tuprwre` discovers installed binaries and creates transparent host shims in `~/.tuprwre/bin`.
- Result: agents get their tools, your host stays clean.

[![Demo](https://asciinema.org/a/lGrhGS4fVlOgWuvO.svg)](https://asciinema.org/a/lGrhGS4fVlOgWuvO)

[More demos →](docs/demos.md)

## Why tuprwre?

AI agents regularly run install commands (`apt install`, `npm install -g`, `pip install`) during task execution. Today, you either trust the agent blindly or micromanage every install. tuprwre fills this gap.

| Approach | Scope | Agent-aware | Per-command | Setup cost |
|----------|-------|-------------|-------------|------------|
| **tuprwre** | Install isolation | Yes | Yes | Minimal |
| Dev Containers | Full environment | No | No | High |
| Nix | Reproducible builds | No | No | Steep |
| Firejail / bubblewrap | System sandbox | No | No | Moderate |
| "Just use Docker" | Full environment | No | No | Manual |

**tuprwre's differentiator:** Per-command granularity with transparent shims. You don't containerize your whole workflow — you sandbox just the installs and get native-feeling tools back.

---

## 30-second quickstart

```bash
git clone https://github.com/c4rb0nx1/tuprwre
cd tuprwre
go build -o tuprwre ./cmd/tuprwre
sudo install -m 0755 tuprwre /usr/local/bin/tuprwre

echo 'export PATH="$HOME/.tuprwre/bin:$PATH"' >> ~/.bashrc   # use ~/.zshrc for zsh
source ~/.bashrc

tuprwre --help
tuprwre shell
# try a dangerous command:
npm install -g cowsay
# [tuprwre] Intercepted... use "tuprwre install -- ..."
exit

tuprwre install -- "npm install -g cowsay"
cowsay "tuprwre is active"
```

Compatibility: Cursor / Claude Code / OpenCode are supported through wrapper interception and non-interactive mode (`tuprwre shell -c "<command>"`).

If this project helps your agent workflows, please **⭐ star** this repo and consider **sponsoring** the project: [Star](https://github.com/c4rb0nx1/tuprwre) · [Sponsor](https://github.com/sponsors/c4rb0nx1)

> **Warning**
> `tuprwre` is early-stage software. Expect breaking changes and rough edges.

## Prerequisites

- Go 1.25+
- Docker CLI + running Docker daemon
- A shell environment where you can prepend `~/.tuprwre/bin` to `PATH`

---

## Installation

### Using Makefile (recommended)

```bash
git clone https://github.com/c4rb0nx1/tuprwre
cd tuprwre

# Build ./build/tuprwre
make build

# Optional verification
make test

# Install to your Go bin directory (GOBIN or GOPATH/bin)
make install

# Optional: install system-wide (usually requires sudo)
sudo make install-system
```

Common targets:

- `make deps` - download and tidy modules
- `make build` - production build in `./build/`
- `make dev` - development build with debug info
- `make test` - run tests
- `make install` - install to your Go bin directory
- `make install-system` - install to `/usr/local/bin` (sudo may be required)
- `make uninstall` - remove installed binary from common user/system locations
- `make stress-output-race` - run stream stress check
- `make clean` - remove build/test artifacts

### From source

```bash
git clone https://github.com/c4rb0nx1/tuprwre
cd tuprwre
go build -o tuprwre ./cmd/tuprwre
sudo cp ./tuprwre /usr/local/bin/tuprwre
```

---

## Host Setup

`tuprwre` stores state under `~/.tuprwre` by default and writes generated shims to `~/.tuprwre/bin`.

Add shim directory to `PATH`:

```bash
echo 'export PATH="$HOME/.tuprwre/bin:$PATH"' >> ~/.bashrc
source ~/.bashrc
```

If you use zsh:

```bash
echo 'export PATH="$HOME/.tuprwre/bin:$PATH"' >> ~/.zshrc
source ~/.zshrc
```

---

## Usage

### 1) Protected shell (interactive)

```bash
tuprwre shell
```

Inside the protected shell, risky commands are intercepted and blocked with guidance to use `tuprwre install`.

Example:

```bash
$ npm install -g rimraf
[tuprwre] Intercepted: npm install -g rimraf
[tuprwre] For sandboxed execution, use: tuprwre install -- "npm install -g rimraf"

[tuprwre] Command blocked. Use 'tuprwre install' for safe execution.
```

### 2) POSIX proxy shell mode (non-interactive `-c`)

Use this mode for IDE/TUI automation that expects shell-compatible `-c` execution.

```bash
tuprwre shell -c "echo hello"
tuprwre shell -c "npm run build"
```

Behavior contract:

- no banner/session text on stdout or stderr in `-c` mode
- only child command output should appear
- if a command is explicitly blocked by wrappers, messaging is stderr-only

IDE automation example (VS Code terminal automation profile):

```json
{
  "terminal.integrated.automationProfile.osx": {
    "path": "/usr/local/bin/tuprwre",
    "args": ["shell"]
  }
}
```

`tuprwre shell -c "<cmd>"` must behave as a drop-in non-interactive shell entrypoint.

### 3) Install tools safely in sandbox

```bash
# install jq using ubuntu base image
tuprwre install --base-image ubuntu:22.04 -- "apt-get update && apt-get install -y jq"
# install from a local script file
tuprwre install --script ./install.sh

# installed binaries are shimmed on host
jq --version
```

You can override output image name and shim overwrite behavior:

```bash
tuprwre install --base-image ubuntu:22.04 --image my-tools:latest --force -- "apt-get update && apt-get install -y yq"
# pass args to local script
tuprwre install --script ./install.sh -- --verbose --dry-run
```

If a shim was installed with `--script`, `tuprwre update <shim>` re-runs the same script path and arguments.

### 4) Run command path used by shims

`run` is mostly internal, but useful for diagnostics and debugging:

```bash
tuprwre run --image ubuntu:22.04 -- bash -lc "echo ok"
```

### 5) Manage installed shims

```bash
# List all shims currently installed
tuprwre list

# Remove a specific shim and metadata
tuprwre remove my-tool

# Remove all shims and metadata at once
tuprwre remove --all

# Update a shim using metadata from its previous installation
tuprwre update my-tool

# Remove orphaned Docker images created by tuprwre
tuprwre clean
```

### 6) Health diagnostics

`tuprwre doctor` validates critical requirements before automation and troubleshooting.

```bash
tuprwre doctor

# machine-readable mode for scripts/automation
tuprwre doctor --json
```

---

## I/O Diagnostics (`tuprwre run`)

Diagnostics flags:

- `--debug-io` - human-readable lifecycle events.
- `--debug-io-json` - NDJSON lifecycle events.
- `--capture-file <path>` - captures combined stdout/stderr to a file.

Examples:

```bash
# text diagnostics
tuprwre run --image ubuntu:22.04 --debug-io -- bash -lc "echo ok"

# json diagnostics
tuprwre run --image ubuntu:22.04 --debug-io-json -- bash -lc "echo ok"

# capture combined output
tuprwre run --image ubuntu:22.04 --capture-file /tmp/tuprwre.out -- bash -lc "echo out; echo err >&2"
```

Sample `--debug-io` output:

```text
[tuprwre][debug-io] +0ms create
[tuprwre][debug-io] +1ms attach
[tuprwre][debug-io] +2ms wait-registered
[tuprwre][debug-io] +4ms start
[tuprwre][debug-io] +8ms wait-exit
[tuprwre][debug-io] +8ms stream-eof
[tuprwre][debug-io] +8ms cleanup
```

Troubleshooting workflow:

1. **Empty output**
   - Re-run with `--debug-io` and verify `start -> wait-exit -> stream-eof` is present.
   - Add `--capture-file` to confirm data is emitted even if your client UI misses it.

2. **Hang diagnosis**
   - Re-run with `--debug-io-json` and inspect event progression.
   - Check whether command is waiting for stdin/interactive input.

> **Caution**
> Diagnostics and capture files may contain command output and metadata. Avoid exposing secrets or sensitive environment values.

---

## How it works

```text
Agent/User
   |
   | 1) run in protected shell
   v
tuprwre shell (intercepts dangerous install commands)
   |
   | 2) run safe install command
   v
tuprwre install -- "<command>"
   |
   | 3) container run + commit + executable discovery
   v
~/.tuprwre/bin/<shim>
   |
   | 4) shim delegates execution
   v
tuprwre run --image <committed-image> -- <binary> [args...]
```

---

## Security Model

tuprwre provides **install isolation** — it ensures that agent-triggered package installations run inside disposable containers rather than on your host. It is not a full system sandbox.

### What tuprwre protects against

- **Host PATH pollution** — installed binaries live in container images, not on your host filesystem
- **Persistent system changes** — install commands run in ephemeral containers that are discarded after image commit
- **Unchecked agent installs** — dangerous `apt install`, `npm install -g`, `pip install` commands are intercepted and redirected

### What tuprwre does NOT protect against (non-goals)

- **Project file modification** — by default, your working directory is mounted read-write so tools can operate on project files. Use `--read-only-cwd` to restrict this
- **Network exfiltration during execution** — by default, containers have network access. Use `--no-network` to disable network during tool execution
- **Resource exhaustion** — by default, containers can consume host CPU and memory. Use `--memory` and `--cpus` to set limits
- **Docker daemon access** — tuprwre requires Docker and trusts the Docker socket. A malicious image could interact with the Docker daemon
- **Supply chain attacks** — tuprwre does not verify the integrity of packages installed inside containers

### Hardening flags

| Flag | Effect |
|------|--------|
| `--read-only-cwd` | Mount working directory as read-only inside the container |
| `--no-network` | Disable all network access during tool execution |
| `--memory 512m` | Limit container memory (e.g. `512m`, `1g`) |
| `--cpus 1.0` | Limit container CPU usage (e.g. `0.5`, `2.0`) |

Example hardened invocation:

```bash
tuprwre run --image toolset:latest --read-only-cwd --no-network --memory 512m --cpus 1.0 -- tool --version
```

### Threat model

tuprwre assumes the host user trusts Docker and the Docker daemon. The primary threat is an AI agent running arbitrary install commands that pollute the host system. tuprwre isolates these installs to containers and exposes tools through controlled shim scripts.

For full architectural details, see [ARCHITECTURE.md](ARCHITECTURE.md).

---

## Environment variables

- `TUPRWRE_DIR` - overrides base data directory (default `~/.tuprwre`)
- `TUPRWRE_BASE_IMAGE` - default base image for config (default `ubuntu:22.04`)
- `TUPRWRE_RUNTIME` - runtime selection (`docker` default; containerd path is scaffolded)

---

## System prompt snippet for agents

```markdown
### Environment & Tool Installation (tuprwre)

This environment is protected by `tuprwre`.

Rules:
1. Use `tuprwre install` for global/system-level installs.
2. Do not run host-level `apt-get install`, `npm install -g`, or `pip install` directly.
3. If a command is blocked inside `tuprwre shell`, re-run it through:
   tuprwre install -- "<install_command>"
```

---

## FAQ

**Q: How much overhead does each tool invocation add?**
Each shimmed tool call starts a Docker container, adding ~500ms-2s of latency. For tools called frequently, this can compound. A containerd runtime (lower overhead) is planned for a future release.

**Q: Can I use tuprwre with Cursor / Claude Code / OpenCode?**
Yes. Use `tuprwre shell -c "<command>"` as a POSIX-compatible proxy shell. See the [POSIX proxy shell mode](#2-posix-proxy-shell-mode-non-interactive--c) section.

**Q: Does tuprwre work without Docker?**
Not currently. Docker is a hard requirement. Containerd and Podman support are on the roadmap.

**Q: Is my working directory safe from sandboxed tools?**
By default, the working directory is mounted read-write so tools can operate on your files. Use `--read-only-cwd` to mount it read-only if the tool doesn't need to write files.

**Q: Can sandboxed tools access the network?**
By default, yes. Use `--no-network` to disable network access during tool execution.

---

## Support

- **Bug reports**: [Open an issue](https://github.com/c4rb0nx1/tuprwre/issues/new?template=bug_report.yml)
- **Feature requests**: [Open an issue](https://github.com/c4rb0nx1/tuprwre/issues/new?template=feature_request.yml)
- **Questions & discussion**: [GitHub Discussions](https://github.com/c4rb0nx1/tuprwre/discussions)

---

## License

Apache License 2.0 - see [LICENSE](LICENSE).
