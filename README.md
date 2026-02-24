# tuprwre: The AI Agent Sandbox

[![Go Version](https://img.shields.io/badge/go-%3E%3D1.21-blue)](https://golang.org)
[![License](https://img.shields.io/badge/license-MIT-green)](LICENSE)

**Stop AI Agents from nuking your host machine.**

`tuprwre` is a high-performance sandbox designed specifically for autonomous AI agents (Claude Code, SWE-agent, OpenDevin, Cursor). It allows agents to install dependencies and run tools in isolated Docker containers while maintaining transparent access to your host's files.

## The Problem: The "Agentic" Security Gap

AI agents are powerful, but they are also dangerous. When you give an agent shell access, you are giving it the keys to your kingdom.

- **Arbitrary Execution:** If an agent decides it needs a tool and runs `npm install -g some-package`, that package (and its post-install scripts) runs with your user's privileges on your host.
- **System Pollution:** Agents often install dozens of dependencies, leaving your host machine cluttered with global binaries and libraries you didn't ask for.
- **The VM Dilemma:** Full Virtual Machines are secure but heavy. They block the agent from easily reading/writing your local project files, breaking the developer experience.

## The Solution: Action-Level Sandboxing

`tuprwre` intercepts dangerous installation commands and traps them in ephemeral Docker containers. It then generates transparent **shims** on your host that proxy execution back into the container.

The agent *thinks* it's working on your host, but the tools it installs are safely trapped.

### Core Features

- **Action-Level Sandboxing:** Automatically intercepts `npm`, `apt`, `pip`, `curl`, and other installation vectors.
- **Host Context Mapping:** Sandboxed tools automatically mount your current directory (`$PWD`) and map your host UID/GID. Files created by the agent are owned by *you*, not root.
- **Discovery Engine:** `tuprwre` uses Docker diffing to automatically detect exactly what the agent installed and exposes only those new binaries to the host.
- **Zero Latency (Almost):** Designed for speed, so the agent doesn't time out waiting for container overhead.

## Usage: The `tuprwre shell`

The easiest way to protect yourself is to drop the agent into a `tuprwre shell`.

```bash
# Start a protected shell
tuprwre shell
```

Inside this shell, any attempt to install global packages or modify the system is intercepted.

```bash
# The agent tries to install a tool
$ npm install -g rimraf

# tuprwre intercepts:
# 1. Spins up a container
# 2. Runs the install
# 3. Commits the image
# 4. Creates a shim at ~/.tuprwre/bin/rimraf

# Now 'rimraf' works, but it's running in Docker!
$ rimraf ./dist
```

## System Prompt for Agents

To get the best results, add this to your agent's instructions (e.g., `.cursorrules`, `AGENTS.md`, or your system prompt):

```markdown
### Environment & Tool Installation
This environment is protected by `tuprwre`. If you need to install any system-level dependencies, CLI tools, or global packages (npm, pip, apt), you MUST use the following command:

```bash
tuprwre install --base-image ubuntu:22.04 -- "YOUR_INSTALL_COMMAND"
```

Example:
`tuprwre install -- "npm install -g @aws-amplify/cli"`

Do NOT attempt to install tools directly to the host. Once installed via `tuprwre`, the tool will be available in your PATH automatically.
```

## Installation

### From Source
```bash
git clone https://github.com/username/tuprwre
cd tuprwre
make build
make install
```

### Setup
Add the shim directory to your PATH:
```bash
echo 'export PATH="$HOME/.tuprwre/bin:$PATH"' >> ~/.bashrc
source ~/.bashrc
```

## How It Works

1. **Intercept**: `tuprwre` catches the installation command.
2. **Isolate**: The command runs inside a Docker container based on a clean image.
3. **Discover**: `tuprwre` compares the container state to the base image to find new binaries.
4. **Shim**: Lightweight proxy scripts are created on the host. When called, they `docker run` the tool with the host's `$PWD` mounted.

---

## License
MIT License - see [LICENSE](LICENSE) for details.
