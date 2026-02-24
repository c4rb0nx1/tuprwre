# tuprwre

### The AI Agent Sandbox

[![Go Version](https://img.shields.io/badge/go-%3E%3D1.21-blue)](https://golang.org)
[![License](https://img.shields.io/badge/license-MIT-green)](LICENSE)

> **Stop AI Agents from nuking your host machine.**

`tuprwre` is a high-performance sandbox designed specifically for autonomous AI agents (**Claude Code**, **SWE-agent**, **OpenDevin**, **Cursor**). It allows agents to install dependencies and run tools in isolated Docker containers while maintaining transparent access to your host's files.

---

## The Problem: The "Agentic" Security Gap

AI agents are powerful, but giving them shell access is like handing over the keys to your kingdom.

*   **Arbitrary Execution:** If an agent runs `npm install -g malicious-package`, it executes with *your* privileges.
*   **System Pollution:** Agents clutter your host with global binaries, libraries, and config files you didn't ask for.
*   **The VM Dilemma:** Full VMs are secure but heavy. They isolate the agent *too much*, breaking access to local project files.

## The Solution: Action-Level Sandboxing

`tuprwre` intercepts dangerous commands and traps them in ephemeral Docker containers, generating transparent **shims** that proxy execution back to the container.

*   **Action-Level Interception:** Automatically catches `npm`, `apt`, `pip`, `curl`, and other installation vectors.
*   **Host Context Mapping:** Tools run in Docker but see your current directory (`$PWD`). Files created are owned by *you*, not root.
*   **Smart Discovery:** Automatically detects new binaries installed by the agent and exposes *only* those to the host.
*   **Zero Latency:** Optimized for speed so agents don't time out.

---

## System Prompt for Agents

To ensure your AI agent uses `tuprwre` correctly, add the following to your **`.cursorrules`**, **`AGENTS.md`**, or system prompt.

> **Copy & Paste this block:**

```markdown
### Environment & Tool Installation (tuprwre)

This environment is protected by `tuprwre`. You cannot install global packages directly.

**Rules:**
1.  If you need to install system-level dependencies, CLI tools, or global packages (npm, pip, apt), you **MUST** use `tuprwre install`.
2.  Do NOT run `npm install -g`, `pip install`, or `apt-get install` directly.

**Command Format:**
tuprwre install --base-image <image> -- "<install_command>"

**Examples:**
# Install AWS CLI
tuprwre install --base-image ubuntu:22.04 -- "apt-get update && apt-get install -y awscli"

# Install a global Node package
tuprwre install -- "npm install -g @aws-amplify/cli"

Once installed, the tool will be available in your PATH automatically.
```

---

## Usage

The easiest way to protect yourself is to drop the agent into a `tuprwre shell`.

```bash
# Start a protected shell
tuprwre shell
```

Inside this shell, dangerous commands are intercepted:

```bash
# 1. Agent tries to install a tool
$ npm install -g rimraf

# 2. tuprwre intercepts & isolates:
#    [Host] -> [Docker Container] -> [Install] -> [Commit]

# 3. A shim is created at ~/.tuprwre/bin/rimraf

# 4. Now 'rimraf' works, but it runs safely in Docker!
$ rimraf ./dist
```

---

## How It Works

```text
+----------------+      (1) Intercept       +---------------------+
|  Agent / User  | -----------------------> |   tuprwre Shell     |
+----------------+                          +---------------------+
       |                                              |
       | (4) Run Shim                                 | (2) Isolate & Install
       v                                              v
+----------------+      (3) Discover        +---------------------+
|  Host System   | <----------------------- |  Docker Container   |
| (~/.tuprwre)   |    (New Binaries)        |  (Ephemeral)        |
+----------------+                          +---------------------+
```

1.  **Intercept**: `tuprwre` catches the installation command.
2.  **Isolate**: The command runs inside a clean Docker container.
3.  **Discover**: `tuprwre` diffs the container to find new binaries.
4.  **Shim**: Lightweight proxy scripts are created on the host to run the tool.

---

## Installation

### From Source

```bash
git clone https://github.com/username/tuprwre
cd tuprwre
make build
make install
```

### Setup

Add the shim directory to your `PATH` to ensure intercepted tools take precedence:

```bash
echo 'export PATH="$HOME/.tuprwre/bin:$PATH"' >> ~/.bashrc
source ~/.bashrc
```

---

## License

MIT License - see [LICENSE](LICENSE) for details.
