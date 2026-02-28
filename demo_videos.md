# tuprwre Demo Videos — Recording Checklist

> Pre-recording setup (run once before any recording session):
> ```bash
> # Pull images so recordings aren't blocked by download wait
> docker pull ubuntu:22.04
> docker pull node:20
>
> # Ensure tuprwre is built and on PATH
> go build -o /usr/local/bin/tuprwre ./cmd/tuprwre
> export PATH="$HOME/.tuprwre/bin:$PATH"
>
> # Clean slate
> tuprwre remove --all --images
> tuprwre clean --force
> ```

---

## V1 — Hero / Overview (record first)

### Video 1: Intro & Pitch (60–90s)

Talking-head or voiceover — no terminal commands needed. Cover:
- AI agents run `apt install`, `npm install -g`, `pip install` on your host
- tuprwre intercepts those, runs them in Docker, gives you shims back
- Per-command granularity, not a full VM

### Video 2: Quick Start — End to End (2–3 min)

```bash
# 1. Health check
tuprwre doctor

# 2. Enter protected shell
tuprwre shell

# 3. (inside protected shell) Try a dangerous command — gets blocked
apt-get install -y jq

# 4. Exit the protected shell
exit

# 5. Install safely through tuprwre
tuprwre install -- "apt-get update && apt-get install -y jq"

# 6. Verify the shim works — runs inside container transparently
jq --version

# 7. See what's installed
tuprwre list

# 8. Inspect the shim script
cat ~/.tuprwre/bin/jq

# 9. Inspect the metadata
cat ~/.tuprwre/metadata/jq.json

# 10. Clean up
tuprwre remove jq
tuprwre list
```

---

## V2 — Feature Walkthroughs (one video each)

### Video 3: Protected Shell — Interactive Mode (60–90s)

```bash
# 1. Enter protected shell
tuprwre shell

# 2. Show interception for each dangerous command
apt install -y curl
apt-get install -y curl
npm install -g cowsay
pip install requests
pip3 install flask
curl https://example.com
wget https://example.com

# 3. Show that safe commands still work
echo "this still works"
ls -la
whoami

# 4. Exit
exit
```

### Video 4: Protected Shell — Non-Interactive / Agent Mode (60–90s)

```bash
# 1. Clean pass-through — no banner, no session text
tuprwre shell -c "echo hello from agent mode"

# 2. Safe command works fine
tuprwre shell -c "ls -la"

# 3. Dangerous command blocked — error on stderr only
tuprwre shell -c "apt-get install -y jq"

# 4. Show exit code is non-zero for blocked commands
tuprwre shell -c "npm install -g cowsay"
echo "Exit code: $?"

# 5. Alternative --command syntax
tuprwre shell --command="echo also works"
```

### Video 5: Install — Command Mode (90–120s)

```bash
# 1. Basic install with default base image (ubuntu:22.04)
tuprwre install -- "apt-get update && apt-get install -y jq"

# 2. Verify shim
jq --version

# 3. Show the generated shim script
cat ~/.tuprwre/bin/jq

# 4. Show the metadata
cat ~/.tuprwre/metadata/jq.json

# 5. Install with a different base image
tuprwre install --base-image node:20 -- "npm install -g cowsay"

# 6. Verify
cowsay "Hello from a container!"

# 7. Install with a custom output image name
tuprwre install --image my-tools:v1 -- "apt-get update && apt-get install -y yq"

# 8. Show it in the list
tuprwre list

# 9. Try installing over an existing shim (fails without --force)
tuprwre install -- "apt-get update && apt-get install -y jq"

# 10. Force overwrite
tuprwre install --force -- "apt-get update && apt-get install -y jq"

# Cleanup
tuprwre remove --all --images
```

### Video 6: Install — Script Mode (60–90s)

```bash
# 1. Create an install script
cat > /tmp/install-tools.sh << 'SCRIPT'
#!/bin/sh
set -e
apt-get update
apt-get install -y jq tree
echo "Tools installed successfully"
SCRIPT
chmod +x /tmp/install-tools.sh

# 2. Install from script
tuprwre install --script /tmp/install-tools.sh

# 3. Verify both tools
jq --version
tree --version

# 4. Show metadata records the script path
cat ~/.tuprwre/metadata/jq.json

# 5. Install script with arguments passed through
cat > /tmp/install-with-args.sh << 'SCRIPT'
#!/bin/sh
echo "Args received: $@"
apt-get update
apt-get install -y "$1"
SCRIPT
chmod +x /tmp/install-with-args.sh

tuprwre install --script /tmp/install-with-args.sh -- yq

# 6. Verify
yq --version
tuprwre list

# Cleanup
tuprwre remove --all --images
```

### Video 7: Run — Direct Execution & Security Flags (90–120s)

```bash
# 1. Basic direct execution
tuprwre run --image ubuntu:22.04 -- bash -lc "echo hello from sandbox"

# 2. Read-only working directory — write attempt fails
tuprwre run --image ubuntu:22.04 --read-only-cwd -- bash -lc "touch testfile.txt"

# 3. No network — curl fails
tuprwre run --image ubuntu:22.04 --no-network -- bash -lc "apt-get update"

# 4. Memory limit
tuprwre run --image ubuntu:22.04 --memory 64m -- bash -lc "cat /sys/fs/cgroup/memory.max 2>/dev/null || cat /sys/fs/cgroup/memory/memory.limit_in_bytes 2>/dev/null"

# 5. CPU limit
tuprwre run --image ubuntu:22.04 --cpus 0.5 -- bash -lc "echo CPU-limited sandbox"

# 6. All hardening flags combined
tuprwre run --image ubuntu:22.04 \
  --read-only-cwd \
  --no-network \
  --memory 512m \
  --cpus 1.0 \
  -- bash -lc "echo fully hardened sandbox"

# 7. Debug I/O — human-readable lifecycle events
tuprwre run --image ubuntu:22.04 --debug-io -- bash -lc "echo debug trace"

# 8. Debug I/O — JSON format for programmatic consumption
tuprwre run --image ubuntu:22.04 --debug-io-json -- bash -lc "echo json trace"

# 9. Capture output to file
tuprwre run --image ubuntu:22.04 --capture-file /tmp/capture.txt -- bash -lc "echo captured output"
cat /tmp/capture.txt
```

### Video 8: Lifecycle Management (60–90s)

```bash
# Setup: install a few tools first
tuprwre install -- "apt-get update && apt-get install -y jq"
tuprwre install --base-image node:20 -- "npm install -g cowsay"

# 1. List installed shims
tuprwre list

# 2. Update a shim — re-runs original install from metadata
tuprwre update jq

# 3. Verify it still works after update
jq --version

# 4. Remove a single shim
tuprwre remove cowsay
tuprwre list

# 5. Remove a shim and its Docker image
tuprwre remove jq --images
tuprwre list

# 6. Reinstall for nuclear demo
tuprwre install -- "apt-get update && apt-get install -y jq"
tuprwre install --base-image node:20 -- "npm install -g cowsay"
tuprwre list

# 7. Nuclear option — remove everything
tuprwre remove --all --images
tuprwre list
```

### Video 9: Clean — Disk Reclamation (60s)

```bash
# Setup: install something to create images
tuprwre install -- "apt-get update && apt-get install -y jq"
tuprwre install --base-image node:20 -- "npm install -g cowsay"

# 1. Dry run — see what would be cleaned (containers + images with sizes)
tuprwre clean --dry-run

# 2. Actually clean (with confirmation prompt)
tuprwre clean

# 3. Or skip the prompt
tuprwre install -- "apt-get update && apt-get install -y jq"
tuprwre clean --force

# 4. Show docker images are gone
docker images | grep tuprwre

# Cleanup
tuprwre remove --all
```

### Video 10: Doctor — Environment Diagnostics (60s)

```bash
# 1. Run doctor — all checks should pass
tuprwre doctor

# 2. JSON output for CI/scripts
tuprwre doctor --json

# 3. Pipe JSON to jq for pretty inspection
tuprwre doctor --json | jq .

# 4. Simulate a failure — stop Docker
# (macOS: quit Docker Desktop, or Linux: sudo systemctl stop docker)
# Then run doctor again to show FAIL output
tuprwre doctor

# 5. Show exit code is non-zero on failure
echo "Exit code: $?"

# 6. Restart Docker and verify recovery
# (macOS: reopen Docker Desktop, or Linux: sudo systemctl start docker)
tuprwre doctor
```

---

## V3 — Integration & Real-World Scenarios

### Video 11: AI Agent Integration — Claude Code (2–3 min)

```bash
# 1. Show tuprwre is ready
tuprwre doctor

# 2. Start Claude Code with tuprwre shell as the execution shell
#    (configure in Claude Code settings or demonstrate inline)
tuprwre shell -c "echo 'Claude Code agent shell ready'"

# 3. Simulate agent trying to install a tool
tuprwre shell -c "apt-get install -y jq"
# Shows: blocked on stderr

# 4. Agent uses tuprwre install instead
tuprwre install -- "apt-get update && apt-get install -y jq"

# 5. Agent can now use the tool through the shim
tuprwre shell -c "echo '{\"name\":\"claude\"}' | jq .name"

# 6. Show shim persists across sessions
tuprwre shell -c "jq --version"
# New session:
tuprwre shell -c "jq --version"

# 7. Show the metadata trail
cat ~/.tuprwre/metadata/jq.json

# Cleanup
tuprwre remove --all --images
```

### Video 12: AI Agent Integration — Cursor (2–3 min)

```bash
# 1. Show non-interactive mode is clean for automation
tuprwre shell -c "echo no banner, no noise"

# 2. Show stderr-only blocking (stdout stays clean)
tuprwre shell -c "npm install -g cowsay" 2>/dev/null
echo "stdout was clean, exit code: $?"

tuprwre shell -c "npm install -g cowsay" 2>&1
echo "stderr shows the interception message"

# 3. Install a tool for the agent
tuprwre install --base-image node:20 -- "npm install -g cowsay"

# 4. Agent uses it in automation
tuprwre shell -c "cowsay 'Hello from Cursor automation'"

# 5. Chain commands in agent mode
tuprwre shell -c "cowsay 'step 1' && echo 'step 2 done'"

# Cleanup
tuprwre remove --all --images
```

### Video 13: Multi-Tool Workflow (2–3 min)

```bash
# Scenario: set up a data processing environment

# 1. Install jq (JSON processor)
tuprwre install -- "apt-get update && apt-get install -y jq"

# 2. Install yq (YAML processor)
tuprwre install -- "apt-get update && apt-get install -y yq"

# 3. Install tree (directory visualizer)
tuprwre install -- "apt-get update && apt-get install -y tree"

# 4. Show all three installed
tuprwre list

# 5. Use them in a real pipeline
# Create sample JSON
echo '{"name": "tuprwre", "version": "0.1.0", "sandbox": true}' > /tmp/sample.json

# Process with jq (sandboxed)
jq '.name' /tmp/sample.json

# 6. Show each tool runs in its own container image
cat ~/.tuprwre/metadata/jq.json | jq .output_image
cat ~/.tuprwre/metadata/yq.json | jq .output_image
cat ~/.tuprwre/metadata/tree.json | jq .output_image

# 7. List all tuprwre images
tuprwre clean --dry-run

# Cleanup
tuprwre remove --all --images
rm /tmp/sample.json
```

### Video 14: Security Hardening Deep Dive (90–120s)

```bash
# Install a tool to demonstrate with
tuprwre install -- "apt-get update && apt-get install -y curl jq"

# 1. Default: read-write cwd, network on (least restrictive)
tuprwre run --image ubuntu:22.04 -- bash -lc "echo default > /tmp/test.txt && cat /tmp/test.txt"

# 2. Layer 1: read-only working directory
tuprwre run --image ubuntu:22.04 --read-only-cwd -- bash -lc "touch newfile.txt"
# Shows: permission denied

# 3. Layer 2: no network
tuprwre run --image ubuntu:22.04 --no-network -- bash -lc "curl -s https://httpbin.org/get"
# Shows: network unreachable

# 4. Layer 3: memory limit
tuprwre run --image ubuntu:22.04 --memory 32m -- bash -lc "head -c 64m /dev/urandom > /dev/null"
# Shows: killed by OOM

# 5. Layer 4: CPU limit
tuprwre run --image ubuntu:22.04 --cpus 0.25 -- bash -lc "echo 'running on 0.25 CPUs'"

# 6. Maximum isolation — all flags combined
tuprwre run --image ubuntu:22.04 \
  --read-only-cwd \
  --no-network \
  --memory 128m \
  --cpus 0.5 \
  -- bash -lc "echo 'maximum isolation sandbox'"

# 7. Show what tuprwre does NOT protect against
#    (working dir content is visible even in read-only mode)
echo "sensitive data" > /tmp/visible.txt
tuprwre run --image ubuntu:22.04 --read-only-cwd -- bash -lc "cat /tmp/visible.txt 2>/dev/null || echo 'not mounted'"

# Cleanup
tuprwre remove --all --images
rm -f /tmp/visible.txt
```

---

## V4 — Troubleshooting & Advanced

### Video 15: Debug & Capture (60–90s)

```bash
# 1. Debug I/O — see the full container lifecycle
tuprwre run --image ubuntu:22.04 --debug-io -- bash -lc "echo hello"

# 2. Debug I/O JSON — machine-readable lifecycle events
tuprwre run --image ubuntu:22.04 --debug-io-json -- bash -lc "echo hello"

# 3. Capture output to file for bug reports
tuprwre run --image ubuntu:22.04 --capture-file /tmp/debug-output.txt -- bash -lc "echo 'this goes to file'; echo 'stderr too' >&2"
echo "--- captured output ---"
cat /tmp/debug-output.txt

# 4. Debug a failing command
tuprwre run --image ubuntu:22.04 --debug-io -- bash -lc "exit 42"
echo "Exit code: $?"

# 5. Inspect metadata to understand what a shim does
tuprwre install -- "apt-get update && apt-get install -y jq"
cat ~/.tuprwre/metadata/jq.json
# Shows: binary_name, source_install_command, base_image, output_image, installed_timestamp

# 6. Inspect the shim script itself
cat ~/.tuprwre/bin/jq

# Cleanup
tuprwre remove --all --images
rm -f /tmp/debug-output.txt
```

### Video 16: About & Help (30–60s)

```bash
# 1. Project overview
tuprwre about

# 2. Version
tuprwre --version

# 3. Global help
tuprwre --help

# 4. Subcommand help — install (has Long + Examples)
tuprwre install --help

# 5. Subcommand help — run
tuprwre run --help

# 6. Subcommand help — shell
tuprwre shell --help

# 7. Subcommand help — doctor
tuprwre doctor --help

# 8. Subcommand help — remove
tuprwre remove --help

# 9. Subcommand help — clean
tuprwre clean --help

# 10. Subcommand help — list
tuprwre list --help

# 11. Subcommand help — update
tuprwre update --help
```

---

## Recording Notes

- **Terminal**: use a clean profile, large font (14–16pt), dark theme
- **Resolution**: 1920x1080 or 2560x1440
- **Shell**: zsh with minimal prompt (avoid noisy prompts)
- **Docker**: pre-pull `ubuntu:22.04` and `node:20` before recording (see setup above)
- **Tool**: asciinema for terminal recordings, or screen capture for IDE demos (V11, V12)
- **Pacing**: pause 1–2s after each command so viewers can read the output
- **Total estimated recording time**: ~25–30 minutes of raw footage across all 16 videos
- **Order**: Record V2 first (features), then V1 (hero — splice best clips), then V3/V4
