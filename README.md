# tuprwre

> Keep host systems safe when automation installs software.

[![CI](https://github.com/c4rb0nx1/tuprwre/actions/workflows/ci.yml/badge.svg)](https://github.com/c4rb0nx1/tuprwre/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/c4rb0nx1/tuprwre)](https://github.com/c4rb0nx1/tuprwre/releases)
[![Go Version](https://img.shields.io/github/go-mod/go-version/c4rb0nx1/tuprwre)](https://golang.org)
[![License](https://img.shields.io/github/license/c4rb0nx1/tuprwre)](LICENSE)
[![Stars](https://img.shields.io/github/stars/c4rb0nx1/tuprwre)](https://github.com/c4rb0nx1/tuprwre/stargazers)


`tuprwre` is an action-level sandbox for AI and automation environments. It intercepts dangerous install commands on the host, runs those install scripts in disposable Docker containers, then publishes host-side shims for safe tool execution.

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

# Update a shim using metadata from its previous installation
tuprwre update my-tool
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

## License

MIT License - see [LICENSE](LICENSE).
