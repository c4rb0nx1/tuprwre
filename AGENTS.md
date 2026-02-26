# tuprwre Agent Guidelines

Welcome to the `tuprwre` codebase. 

`tuprwre` is an action-level sandbox designed to protect host machines from autonomous AI agents by intercepting dangerous shell commands (like `npm install`) and routing them into ephemeral Docker containers.

## Current Architectural State
1. **Interception (`cmd/tuprwre/shell.go`)**: A subshell wrapper that injects a custom directory into `$PATH`. When an agent types `apt install`, it hits our bash wrapper instead of the host binary. The wrapper blocks the execution and instructs the agent to use `tuprwre install`.
2. **Sandbox (`cmd/tuprwre/install.go` & `internal/sandbox`)**: Runs the requested command inside a Docker container.
3. **Discovery (`internal/discovery`)**: Diffs the Docker container against its baseline to see what tools were newly installed.
4. **Shims (`internal/shim`)**: Creates host-side scripts (e.g., `~/.tuprwre/bin/jq`) that transparently route execution back into the sandbox.
5. **Stateful Sessions**: Tools installed via `--session <id>` co-exist in the same persistent Docker container.

## The Next Milestone: The "POSIX Proxy Shell"

**The Goal:** We need to upgrade `tuprwre shell` from an interactive-only sandbox into a fully POSIX-compliant proxy shell. This will allow `tuprwre shell` to act as a drop-in replacement for `/bin/bash` or `/bin/zsh` in IDEs (Cursor/VS Code) and TUIs (Claude Code).

### The Implementation Strategy

If you are tasked with implementing the "POSIX Proxy Shell", follow these strict guidelines:

#### 1. Implement the `-c` Flag
IDEs and Agents do not execute commands interactively. They use the `-c` flag (e.g., `/bin/bash -c "npm run build"`).
* You must modify `cmd/tuprwre/shell.go` to accept `-c` and a command string.
* *Note:* Cobra's standard flag parser (`cmd.Flags().StringVarP`) often strips quotes or mishandles complex bash strings. The safest approach is often to parse `os.Args` manually for the `-c` flag to preserve exact shell quoting, or carefully configure Cobra to accept arbitrary trailing args.

#### 2. Pass-Through Execution
If the `-c` flag is detected, do not spawn a hanging interactive shell.
Instead, pass the exact command string down to the underlying OS shell:
`exec.Command(shell, "-c", commandString)`

#### 3. Strict Silent Mode
When running in `-c` mode (non-interactive), `tuprwre` **MUST BE 100% SILENT**.
* Do not print `[tuprwre] Starting protected shell...`
* Do not print session IDs.
* Agents expect pristine `stdout` (often parsing it as JSON). If `tuprwre` prints banner text, it will fatally crash the agent's internal parser.
* Only print to `stderr` if a command is explicitly blocked.

#### 4. Environment Preservation
Ensure the `TUPRWRE_SESSION_ID` and the hijacked `PATH` are still perfectly exported to the `childCmd.Env` even in `-c` mode. The interception wrappers must still work.

### Definition of Done
If this is implemented correctly, a user can configure VS Code's `automationProfile` to use `/usr/local/bin/tuprwre shell`, and the VS Code AI Agent will seamlessly execute background tasks inside the sandbox without crashing or freezing.
