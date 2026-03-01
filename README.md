# tuprwre

> Keep host systems safe when automation installs software.

[![CI](https://github.com/c4rb0nx1/tuprwre/actions/workflows/ci.yml/badge.svg)](https://github.com/c4rb0nx1/tuprwre/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/tag/c4rb0nx1/tuprwre?label=release)](https://github.com/c4rb0nx1/tuprwre/releases)
[![Go Version](https://img.shields.io/github/go-mod/go-version/c4rb0nx1/tuprwre)](https://golang.org)
[![License](https://img.shields.io/github/license/c4rb0nx1/tuprwre)](LICENSE)
[![Stars](https://img.shields.io/github/stars/c4rb0nx1/tuprwre)](https://github.com/c4rb0nx1/tuprwre/stargazers)

`tuprwre` intercepts and blocks risky install commands (`apt`, `npm -g`, `pip`), guiding users to run them safely inside disposable Docker containers via `tuprwre install`.

[![Demo](https://asciinema.org/a/lGrhGS4fVlOgWuvO.svg)](https://asciinema.org/a/lGrhGS4fVlOgWuvO)

[More demos](docs/demos.md) · [Architecture](ARCHITECTURE.md) · [Releases](https://github.com/c4rb0nx1/tuprwre/releases) · [Discussions](https://github.com/c4rb0nx1/tuprwre/discussions)

## TL;DR

- Agent/user install attempts are blocked with guidance to use `tuprwre install` for sandboxed execution.
- `tuprwre install` runs in an ephemeral container and discovers installed executables.
- Shims are generated under `~/.tuprwre/bin`.
- After PATH setup, agents/users call tools directly (`jq`, `cowsay`, etc.); shims invoke `tuprwre run` internally.

## Security model

tuprwre provides **install isolation plus execution-path isolation for shimmed tools**.

What it does:

- Routes risky install commands into disposable containers.
- Avoids host-level global package installation side effects.
- Routes direct tool invocations through shim scripts (not host global binaries).
- Supports additional hardening during execution:
  - `--read-only-cwd`
  - `--no-network`
  - `--memory`
  - `--cpus`

What it does not do by default:

- Full system sandboxing for every command.
- Eliminate Docker daemon trust risks.
- Verify package supply chain integrity inside install scripts.

## Install

Prerequisites:

- Go 1.25+
- Docker CLI + running Docker daemon

```bash
git clone https://github.com/c4rb0nx1/tuprwre
cd tuprwre

make build
make install

# Optional
make test
sudo make install-system
```

## Quick start

```bash
# one-time PATH setup
echo 'export PATH="$HOME/.tuprwre/bin:$PATH"' >> ~/.bashrc   # use ~/.zshrc for zsh
source ~/.bashrc

# protected shell intercepts risky host installs
tuprwre shell
apt-get install -y jq
exit

# safe install path (can be run by user or agent)
tuprwre install -- "apt-get update && apt-get install -y jq"

# direct tool usage (shim handles sandbox execution)
jq --version
```

Compatibility: Cursor / Claude Code / OpenCode via `tuprwre shell -c "<command>"`.

If this project helps your agent workflows, please **⭐ star** this repo and consider **sponsoring** the project: [Star](https://github.com/c4rb0nx1/tuprwre) · [Sponsor](https://github.com/sponsors/c4rb0nx1)

> **Tip** If you see "Command blocked", that's expected — rerun with `tuprwre install -- "<command>"`.

> **Warning**
> `tuprwre` is early-stage software. Expect breaking changes and rough edges.

## Core commands

```bash
# interception shell (interactive)
tuprwre shell

# interception shell (non-interactive / IDE automation)
tuprwre shell -c "echo hello"

# sandboxed install
tuprwre install -- "apt-get update && apt-get install -y jq"
tuprwre install --memory 512m --cpus 1.0 -- "apt-get update && apt-get install -y jq"
tuprwre install --script ./install.sh

# direct tool execution through shim
jq --version

# shim lifecycle
tuprwre list
tuprwre update jq
tuprwre remove jq
tuprwre remove --all
tuprwre clean

# configuration
tuprwre init
tuprwre init --global

# diagnostics
tuprwre doctor
tuprwre doctor --json
```

Detailed command manual: [CLI Reference](docs/cli.md)
For terminal help: `tuprwre --help` and `tuprwre <command> --help`.

## Configuration

tuprwre uses layered JSON config (highest priority wins):

1. CLI flags (`--intercept`, `--allow`)
2. Env var (`TUPRWRE_INTERCEPT`)
3. Workspace config (`.tuprwre/config.json` in cwd or nearest parent)
4. Global config (`~/.tuprwre/config.json`)
5. Built-in defaults

Initialize a config file:

```bash
tuprwre init                    # workspace config in .tuprwre/config.json
tuprwre init --global           # global config in ~/.tuprwre/config.json
```

Customize the intercept list per-session:

```bash
tuprwre shell --intercept brew --allow curl
TUPRWRE_INTERCEPT="apt,npm,brew" tuprwre shell
```

### Resource defaults

Config files support default resource limits with absolute or host-relative values:

```json
{
  "default_memory": "25%",
  "default_cpus": "50%"
}
```

Percentage values resolve at runtime against the Docker host. On macOS Docker Desktop, percentages reflect VM limits, not full host hardware. CLI flags (`--memory`, `--cpus`) override config defaults.

Environment variables:

| Variable | Effect |
|----------|--------|
| `TUPRWRE_DIR` | Override base data directory (default `~/.tuprwre`) |
| `TUPRWRE_BASE_IMAGE` | Default base image (default `ubuntu:22.04`) |
| `TUPRWRE_RUNTIME` | Runtime selection (`docker` default) |
| `TUPRWRE_INTERCEPT` | Comma-separated intercept list override |
| `TUPRWRE_DEFAULT_MEMORY` | Default memory limit for containers (e.g. `512m`, `1g`, `25%`) |
| `TUPRWRE_DEFAULT_CPUS` | Default CPU limit for containers (e.g. `2.0`, `50%`) |

## How it works

```text
1) Agent/user command in protected shell
2) Risky install is intercepted
3) tuprwre install executes inside disposable container
4) discovered binaries become host shims in ~/.tuprwre/bin
5) direct tool calls invoke shim -> tuprwre run -> containerized execution
```

## FAQ

**Do agents need to call `tuprwre run` directly?**
No. After install + PATH setup, call tools directly by binary name. Shims invoke `tuprwre run` internally.

**Does tuprwre work without Docker?**
Not currently. Docker is required. Containerd/Podman support is roadmap work.

**Can I harden runtime execution further?**
Yes. Use `--read-only-cwd`, `--no-network`, `--memory`, and `--cpus` with `tuprwre run` or `tuprwre install`. You can also set persistent defaults via `default_memory` and `default_cpus` in config.

## Support

- **Bug reports**: [Open an issue](https://github.com/c4rb0nx1/tuprwre/issues/new?template=bug_report.yml)
- **Feature requests**: [Open an issue](https://github.com/c4rb0nx1/tuprwre/issues/new?template=feature_request.yml)
- **Questions & discussion**: [GitHub Discussions](https://github.com/c4rb0nx1/tuprwre/discussions)

## License

Apache License 2.0 - see [LICENSE](LICENSE).
