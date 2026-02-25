# tuprwre

> **Stop AI agents from nuking your host machine. Give them shell access without giving them the keys to your kingdom.**

[![Go Version](https://img.shields.io/badge/go-%3E%3D1.21-blue)](https://golang.org)
[![License](https://img.shields.io/badge/license-MIT-green)](LICENSE)

> **Warning**
> `tuprwre` is early-stage experimental software. Expect breaking changes, missing features, and rough edges.

`tuprwre` is a high-performance sandbox designed specifically for autonomous AI agents (**Claude Code**, **SWE-agent**, **OpenDevin**, **Cursor**). It allows agents to install dependencies and run tools in isolated, persistent Docker environments while maintaining transparent access to your host's files.

<div align="center">
  <video src="https://github.com/user-attachments/assets/bc8fd436-7576-46ee-a101-3fcd6bbd1cf7" width="100%" controls autoplay loop muted></video>
</div>


---

## The Problem: The "Agentic" Security Gap

AI agents are powerful, but giving them shell access is like handing over the keys to your kingdom.

*   **Arbitrary Execution:** If an agent runs `npm install -g malicious-package`, it executes with *your* privileges.
*   **System Pollution:** Agents clutter your host with global binaries, libraries, and config files you didn't ask for.
*   **The VM Dilemma:** Full VMs are secure but heavy. They isolate the agent *too much*, breaking access to local project files.

## The Solution: Action-Level Sandboxing

`tuprwre` intercepts dangerous commands and traps them in Docker containers, generating transparent **shims** that proxy execution back to the container. 

*   **Action-Level Interception:** Catch `npm`, `apt`, `pip`, and other installation vectors automatically via `tuprwre shell`.
*   **Stateful Sessions:** Tools installed across multiple commands co-exist in the same persistent container environment using `--session`.
*   **Host Context Mapping:** Sandboxed tools see your current directory (`$PWD`), and files created are owned by *you*, not root.
*   **Smart Discovery:** Automatically detects new binaries installed by the agent and exposes *only* those to the host.

---

## System Prompt for Agents

To ensure your AI agent uses `tuprwre` correctly, add the following to your **`.cursorrules`**, **`AGENTS.md`**, or system prompt.

> **Copy & Paste this block:**

```markdown
### Environment & Tool Installation (tuprwre)

This environment is protected by `tuprwre`. You cannot install global packages directly to the host.

**Rules:**
1.  If you need to install system-level dependencies, CLI tools, or global packages (npm, pip, apt), you **MUST** use `tuprwre install`.
2.  Do NOT run `npm install -g`, `pip install`, or `apt-get install` directly.
3.  You should use the `--session` flag to group related tool installations into the same persistent container.

**Command Format:**
tuprwre install --session <session-id> --base-image <image> -- "<install_command>"

**Examples:**
# Install AWS CLI into a session named "aws-task"
tuprwre install --session aws-task --base-image ubuntu:22.04 -- "apt-get update && apt-get install -y awscli"

# Install a Node package into the same session
tuprwre install --session aws-task -- "npm install -g @aws-amplify/cli"

Once installed, the tool will be available in your PATH automatically, but it will execute safely inside the sandbox.
```

---

## Usage

### 1. The Protected Shell (Recommended)

The easiest way to protect yourself is to drop the agent into a `tuprwre shell`.

```bash
# Start a protected shell
tuprwre shell
```

Inside this shell, dangerous commands are strictly blocked to enforce sandbox usage:

```bash
$ npm install -g rimraf
[tuprwre] Command blocked: npm install -g rimraf
[tuprwre] You are running inside a protected host environment.
[tuprwre] To install tools or dependencies safely, you MUST use the sandbox:

    tuprwre install --session "b4c8801c" -- "npm install -g rimraf"
```

### 2. Manual Installation

You can manually sandbox tools. The discovery engine will diff the container, find what was installed, and generate the shims.

```bash
# One-time installation (safe!)
tuprwre install --base-image ubuntu:22.04 -- "apt install -y jq"

# Then use it like normal on your host!
jq .status my_host_file.json 
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
| (~/.tuprwre)   |    (New Binaries)        |  (Session Image)    |
+----------------+                          +---------------------+
```

1.  **Intercept**: `tuprwre shell` traps the installation command and blocks it.
2.  **Isolate**: The agent uses `tuprwre install` to run the command inside a Docker container.
3.  **Discover**: `tuprwre` diffs the container against its baseline to find new binaries.
4.  **Shim**: Lightweight proxy scripts are created in `~/.tuprwre/bin` to transparently route host commands back into the sandbox.

---

## Installation

### From Source

```bash
git clone https://github.com/c4rb0nx1/tuprwre
cd tuprwre
go build -o tuprwre ./cmd/tuprwre
sudo cp ./tuprwre /usr/local/bin/tuprwre
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
