# tuprwre Architecture

## Overview

tuprwre is a CLI tool that sandboxes shell script installations and provides transparent execution through generated shim scripts. It bridges the gap between "I want to run this install script" and "I want the installed tools available on my host system" without polluting the host environment.

## Core Concept

The fundamental insight is that we can have the best of both worlds:
1. **Safety**: Run untrusted installation scripts in isolated Docker containers
2. **Convenience**: Generate lightweight proxies (shims) so installed tools feel native

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              HOST SYSTEM                                     │
│                                                                              │
│   ┌──────────────┐     ┌──────────────────┐     ┌───────────────────────┐  │
│   │ User types:  │────▶│ Shim script      │────▶│ tuprwre run           │  │
│   │ "tool"       │     │ (~/.tuprwre/bin)│     │ (proxy to Docker)     │  │
│   └──────────────┘     └──────────────────┘     └───────────┬───────────┘  │
│                                                             │               │
│                                                             ▼               │
│                                                  ┌──────────────────────┐  │
│                                                  │ Docker Engine API    │  │
│                                                  └───────────┬──────────┘  │
└──────────────────────────────────────────────────────────────┼─────────────┘
                                                               │
                                                               ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                           DOCKER CONTAINER                                   │
│                                                                              │
│   ┌──────────────────┐     ┌──────────────────────┐                         │
│   │ Installed binary │────▶│ tool / helper / etc  │                         │
│   │ (tool)           │     └──────────────────────┘                         │
│   └──────────────────┘                                                      │
│                                                                              │
│   • stdin/stdout/stderr proxied transparently                               │
│   • Arguments passed through unchanged ($@)                                  │
│   • Working directory mounted for file access                               │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

## Four-Phase Architecture

### Phase 1: Installation

**Purpose**: Execute the installation script in an isolated environment.

```
User Command:
  tuprwre install --base-image ubuntu:22.04 -- \
    "curl -fsSL https://example.com/install-tool.sh | bash"

Flow:
  1. Create ephemeral container from base image
     docker create --name tuprwre-<uuid> ubuntu:22.04

  2. Execute installation command in container
     docker exec tuprwre-<uuid> sh -c "curl -L ... | bash"

  3. Commit container to new image
     docker commit tuprwre-<uuid> tuprwre-tool:20240221

  4. (Optional) Remove ephemeral container
     docker rm tuprwre-<uuid>
```

**Key Design Decisions**:
- Ephemeral containers ensure clean state
- Commit preserves the installation state as a reusable image
- Network access is allowed during install (scripts need to download)

### Phase 2: Discovery

**Purpose**: Find what binaries were actually installed.

**Approach A: PATH Diffing (Current)**
```
1. Get baseline: List executables in base image's PATH
2. Get current: List executables in committed container's PATH
3. Diff: New executables = installed binaries
```

**Approach B: Filesystem Diffing (Future)**
```
1. Export base image filesystem
2. Export committed container filesystem
3. Find new files in common bin directories (/usr/bin, /usr/local/bin, /opt/bin)
4. Filter by executable bit
```

**Edge Cases Handled**:
- System binaries (sh, bash, curl) filtered out
- Version suffixes (tool-1.0, tool-latest) detected
- Symlinks in PATH followed

### Phase 3: Shim Generation

**Purpose**: Create host-side proxy scripts for transparent execution.

**Shim Template**:
```bash
#!/bin/bash
# Generated shim for tool
# Proxies execution to sandboxed container: tuprwre-tool:latest

set -e

IMAGE_NAME="tuprwre-tool:latest"
BINARY_NAME="tool"

# Forward all arguments to the sandboxed binary
exec tuprwre run --image "${IMAGE_NAME}" -- "${BINARY_NAME}" "$@"
```

**Generated at**: `~/.tuprwre/bin/<binary-name>`

**User Integration**:
```bash
# Add to shell profile (~/.bashrc, ~/.zshrc, etc.)
export PATH="$HOME/.tuprwre/bin:$PATH"
```

### Phase 4: Execution

**Purpose**: Proxy host invocations to the sandboxed container.

```
User types: tool --version

Shim executes: tuprwre run --image tuprwre-tool:latest -- tool --version

tuprwre run flow:
  1. Start container from image (or use existing)
  2. Mount current working directory for file access
  3. Pass through environment variables (selective)
  4. Forward stdin/stdout/stderr
  5. Execute: tool --version
  6. Return exit code
```

**I/O Handling**:
```
stdin  ────────▶  container.stdin
stdout ◀────────  container.stdout
stderr ◀────────  container.stderr
```

**Argument Passing**:
- All arguments after `--` passed verbatim
- Preserves quoting and escaping

## Project Structure

```
cmd/tuprwre/
├── root.go      # Root cobra command, CLI entry
├── install.go   # Install subcommand implementation
└── run.go       # Run subcommand implementation

internal/
├── config/      # Configuration management
│   └── config.go
├── sandbox/     # Docker/container runtime abstraction
│   └── sandbox.go
├── discovery/   # Binary discovery and diffing
│   └── discovery.go
└── shim/        # Shim script generation
    └── shim.go
```

## Package Responsibilities

### `internal/config`
- Environment variable handling
- Directory management (~/.tuprwre/)
- Runtime selection (docker vs containerd)

### `internal/sandbox`
- Container lifecycle (create, exec, commit, cleanup)
- Image management
- Execution with I/O streaming
- **Interface designed for runtime swapping**

### `internal/discovery`
- Baseline/after comparison
- Executable detection
- Version extraction
- System binary filtering

### `internal/shim`
- Template-based script generation
- Path management
- PATH validation

## Runtime Abstraction

The sandbox package is designed to support multiple container runtimes:

```go
type Runtime interface {
    Create(image string) (ContainerID, error)
    Exec(containerID string, cmd string) error
    Commit(containerID, imageName string) error
    Run(opts RunOptions) (exitCode int, error)
}

// Implementations:
// - dockerRuntime (current)
// - containerdRuntime (future)
```

### Docker (Current)
- Uses Docker Engine API / CLI
- Mature ecosystem
- Higher overhead per container start

### Containerd (Future)
- Uses containerd client library
- Lower-level API
- Faster container startup
- Better for shim invocation latency

**Migration Path**:
1. Implement containerdRuntime
2. Add runtime detection: `tuprwre config --runtime containerd`
3. Shim generation selects appropriate template
4. Gradual rollout with fallback

## Security Considerations

### Installation Phase
- Container runs with limited privileges
- Network access required (for downloads)
- No host filesystem access (except explicit mounts)

### Execution Phase
- Container is ephemeral (no state persistence)
- Current directory mounted read-write by default (for file operations); use `--read-only-cwd` to restrict
- Network access enabled by default; use `--no-network` to isolate
- No resource limits by default; use `--memory` and `--cpus` to constrain
- Selective environment variable pass-through
- No host binary access (isolated PATH)

### Shim Scripts
- Generated with known-good templates
- No user input in template execution
- Set executable bit (0755)

### Non-Goals
tuprwre is designed for **install isolation**, not full system sandboxing:
- Does not protect against Docker daemon compromise
- Does not verify package integrity or supply chain
- Does not provide network-level filtering (only on/off)
- Does not sandbox the host process itself

## Performance Characteristics

### Installation (One-time)
- ~5-30 seconds (depends on script)
- Network-bound (downloads)
- Image stored locally

### Execution (Per invocation)
- Docker: ~500ms-2s startup overhead
- Containerd (future): ~100-300ms startup overhead
- I/O: Native speed (streamed)
- Memory: Container runtime overhead only

**Optimization Strategy**:
- Containerd reduces startup latency
- Keep containers warm (optional daemon)
- Image layering for shared dependencies

## Future Extensions

### Multi-Runtime Support
- Containerd for speed
- Podman for rootless
- nerdctl compatibility

### Caching
- Layer caching for repeated installs
- Binary signature verification

### Configuration
- Per-shim environment variables
- Volume mount templates
- Network policies
