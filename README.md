# tuprwre

[![Go Version](https://img.shields.io/badge/go-%3E%3D1.21-blue)](https://golang.org)
[![License](https://img.shields.io/badge/license-MIT-green)](LICENSE)

tuprwre is a high-performance CLI tool that provides a safe environment for executing shell scripts (e.g., `curl -L script.sh | bash`). It sandboxes the installation inside a Docker container and generates lightweight shim scripts on the host for transparent execution.

## The Problem

You want to install a CLI tool from a script:
```bash
curl -L https://example.com/install.sh | bash
```

**Concerns:**
- The script could do anything to your system
- It might install who-knows-what binaries
- Cleanup is difficult

## The Solution

tuprwre runs the installation in an isolated Docker container, commits the result to an image, discovers what was installed, and generates shims so the tool works transparently:

```bash
# One-time installation (safe!)
tuprwre install --base-image ubuntu:22.04 -- \
  "curl -L https://kimi.dev/install.sh | bash"

# Then use it like normal
kimi --version    # Runs kimi from the sandboxed container
kimi myfile.txt   # Transparent execution with file access
```

## How It Works

### Phase 1: Installation
```
tuprwre install -- "curl -L https://example.com/tool.sh | bash"
        │
        ▼
┌───────────────────┐
│ Create container  │  Ephemeral Docker container from base image
│ Run install script│  Script executes in isolated environment
│ Commit to image   │  State preserved as new Docker image
└─────────┬─────────┘
          ▼
```

### Phase 2: Discovery
The tool analyzes the container to find new executables that were installed, filtering out system binaries.

### Phase 3: Shim Generation
Lightweight proxy scripts are created in `~/.tuprwre/bin/`:
```bash
#!/bin/bash
exec tuprwre run --image "tool:latest" -- "tool" "$@"
```

### Phase 4: Transparent Execution
```
kimi --version                    # User types command
        │
        ▼
~/.tuprwre/bin/kimi              # Shim intercepts
        │
        ▼
tuprwre run --image kimi -- …    # Proxies to container
        │
        ▼
┌───────────────────┐
│ Docker container  │  Starts from committed image
│ kimi --version    │  Runs with stdin/stdout proxy
└───────────────────┘
```

## Installation

### From Source
```bash
git clone https://github.com/username/tuprwre
cd tuprwre
make build
make install
```

Or directly:
```bash
go install github.com/username/tuprwre/cmd/tuprwre@latest
```

### Setup

Add the shim directory to your PATH:
```bash
echo 'export PATH="$HOME/.tuprwre/bin:$PATH"' >> ~/.bashrc
source ~/.bashrc
```

## Usage

### Install a tool
```bash
tuprwre install --base-image ubuntu:22.04 -- \
  "curl -L https://kimi.dev/install.sh | bash"
```

Options:
- `--base-image, -i`: Base Docker image (default: ubuntu:22.04)
- `--container, -c`: Use existing container instead of creating new
- `--image, -n`: Name for the committed image (auto-generated if not provided)
- `--force, -f`: Overwrite existing shims

### Run a sandboxed binary (internal use)
```bash
tuprwre run --image kimi:latest -- kimi --help
```

Options:
- `--image, -i`: Docker image to run (required)
- `--container, -c`: Use existing container
- `--workdir, -w`: Working directory inside container
- `--env, -e`: Environment variables (KEY=VALUE)
- `--volume, -v`: Volume mounts (host:container)
- `--runtime, -r`: Container runtime (docker|containerd)

## Architecture

tuprwre consists of four main phases:

1. **Installation**: Runs scripts in ephemeral containers and commits state
2. **Discovery**: Finds new binaries by comparing against base image
3. **Shim Generation**: Creates proxy scripts for transparent execution
4. **Execution**: Proxies I/O between host and sandboxed container

See [ARCHITECTURE.md](ARCHITECTURE.md) for detailed design documentation.

## Project Structure

```
cmd/tuprwre/       CLI commands (cobra)
internal/
  config/          Configuration management
  sandbox/         Docker/container runtime
  discovery/       Binary discovery
  shim/            Shim generation
```

## Development

```bash
# Build
make build

# Run tests
make test

# Development build (faster, no optimization)
make dev

# Format code
make fmt

# Run linter
make lint
```

## Future Roadmap

- **Containerd Support**: Faster container startup for reduced shim latency
- **Shim Management**: `list`, `remove`, `update` commands
- **Caching**: Layer caching and binary signature verification
- **Configuration**: Per-shim environment and volume templates

## License

MIT License - see [LICENSE](LICENSE) for details.

## Acknowledgments

Inspired by [syumai/sbx](https://github.com/syumai/sbx) - a macOS sandbox-exec wrapper that demonstrated the power of transparent sandboxing.
