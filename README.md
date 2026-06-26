# HostMCP

[日本語版はこちら](README.ja.md)

**Secure Docker Container Access for AI Coding Assistants**

HostMCP is an MCP server that runs on the host OS, enabling AI assistants (Claude Code, Gemini Code Assist, etc.) inside an AI Sandbox to safely check logs and run tests in other Docker containers.

For the AI Sandbox template that uses HostMCP, see [ai-sandbox](https://github.com/YujiSuzuki/ai-sandbox).

---

## Table of Contents

- [Features](#features)
- [Installation](#installation)
- [Server Startup](#server-startup)
- [Connecting AI Assistants](#connecting-ai-assistants)
- [CLI Commands](#cli-commands)
  - [Setup Commands](#setup-commands)
  - [Host OS Commands (Direct Docker Access)](#host-os-commands-direct-docker-access)
  - [Client Commands (Via HTTP API)](#client-commands-via-http-api)
- [Security Modes](#security-modes)
- [Authentication](#authentication)
- [Configuration Reference](#configuration-reference)
  - [File Access Blocking (blocked_paths)](#file-access-blocking-blocked_paths)
  - [Output Masking](#output-masking)
  - [Host Path Masking](#host-path-masking)
  - [Permissions](#permissions)
  - [Default Commands (exec_whitelist)](#default-commands-exec_whitelist-)
  - [Dangerous Mode (exec_dangerously)](#dangerous-mode-exec_dangerously)
  - [Large Output Handling (host_tools)](#large-output-handling-host_tools)
- [Architecture](#architecture)
- [Design Philosophy](#design-philosophy)
- [Provided MCP Tools](#provided-mcp-tools)
- [Troubleshooting](#troubleshooting)
- [License](#license)

---

## Features

- **Security-first design** — Whitelist-based container and command access
- **Multi-AI support** — Works with Claude Code, Gemini Code Assist
- **Zero dependencies** — Single binary, no runtime requirements
- **Cross-platform** — Windows, macOS (Intel & Apple Silicon), Linux
- **Audit logging** — All operations can be logged for compliance
- **MCP standard** — Built on MCP for future extensibility

## Installation

Run on the host OS.

**Go Install (Recommended)**
```bash
go install github.com/YujiSuzuki/hostmcp@latest
```

<!-- Binary releases coming soon
**macOS (Apple Silicon)**
```bash
curl -L https://github.com/YujiSuzuki/hostmcp/releases/latest/download/hostmcp_darwin_arm64 -o hostmcp
chmod +x hostmcp
sudo mv hostmcp /usr/local/bin/
```

**macOS (Intel)**
```bash
curl -L https://github.com/YujiSuzuki/hostmcp/releases/latest/download/hostmcp_darwin_amd64 -o hostmcp
chmod +x hostmcp
sudo mv hostmcp /usr/local/bin/
```

**Windows**
1. Download `hostmcp_windows_amd64.exe` from [Releases](https://github.com/YujiSuzuki/hostmcp/releases)
2. Place in a folder (e.g., `C:\tools\`)
3. Add to PATH or use the full path

**Linux**
```bash
curl -L https://github.com/YujiSuzuki/hostmcp/releases/latest/download/hostmcp_linux_amd64 -o hostmcp
chmod +x hostmcp
sudo mv hostmcp /usr/local/bin/
```
-->

**Build from Source**
```bash
git clone https://github.com/YujiSuzuki/hostmcp.git
cd hostmcp
make install  # Installs to ~/go/bin/
```

## Server Startup

### Preparing the Configuration File

Use `hostmcp init` to generate a config file inside your workspace:

```bash
hostmcp init --workspace /path/to/workspace
```

This creates `.sandbox/config/hostmcp.yaml` in the workspace directory. To set a custom port at the same time:

```bash
hostmcp init --workspace /path/to/workspace --port 18080
```

| Flag | Description |
|------|-------------|
| `--workspace` | Target workspace directory (required) |
| `--port` | Port number to set in the generated config (default: 18080) |
| `--force` | Overwrite an existing config file |

Alternatively, copy the example config manually:

```bash
cp configs/hostmcp.example.yaml hostmcp.yaml
nano hostmcp.yaml
```

Example configuration:
```yaml
server:
  port: 18080
  host: "127.0.0.1"

security:
  mode: "moderate"  # strict, moderate, or permissive

  allowed_containers:
    - "myapp-*"
    - "mydb-*"

  exec_whitelist:
    "myapp-api":
      - "npm test"
      - "pytest /app/tests"
    "*":
      - "pwd"
```

### Starting the Server

```bash
# Run on host OS
hostmcp serve --config hostmcp.yaml
```

Output looks like:
```
2026-01-22 12:55:17 INFO  Starting HostMCP server version=dev security_mode=moderate port=18080 log_level=info
2026-01-22 12:55:17 INFO  Found accessible containers count=3
2026-01-22 12:55:17 INFO  MCP server listening url=http://127.0.0.1:18080 health_check=http://127.0.0.1:18080/health sse_endpoint=http://127.0.0.1:18080/sse
2026-01-22 12:55:17 INFO  Press Ctrl+C to stop
```

### Verbosity Levels

Use the `-v` flag to increase log verbosity for debugging:

```bash
hostmcp serve --config hostmcp.yaml -v      # Level 1: JSON request/response output
hostmcp serve --config hostmcp.yaml -vv     # Level 2: DEBUG level + JSON output
hostmcp serve --config hostmcp.yaml -vvv    # Level 3: Full debug (all noise shown)
```

| Level | Flag | Description |
|-------|------|-------------|
| 0 | (none) | Normal INFO level, minimal output |
| 1 | `-v` | JSON request/response display, uninitialized connections filtered |
| 2 | `-vv` | DEBUG level + JSON output, uninitialized connections filtered |
| 3 | `-vvv` | Full debug, all connections shown including noise |

**Note:** "Noise" refers to uninitialized SSE connections (e.g., VS Code extension probes). Levels 0-2 filter these to keep logs clean.

### Logging Features

**Request numbers:** Each request is assigned a unique number `[#N]` for tracking when multiple requests are processed concurrently:

```
═══ [#1] ═══════════════════════════════════════════════════════════
▼ REQUEST client=claude-code method=tools/call tool=list_containers id=2
...
═══ [#1] ═══════════════════════════════════════════════════════════
```

**Client identification:** The server displays client names (from MCP `clientInfo`) in logs:
- `claude-code` — Claude Code extension
- `hostmcp-go-client` — HostMCP CLI client (with `--client-suffix` for custom suffixes)

**Graceful shutdown:** When the server stops (Ctrl+C):
- Waits up to 2 seconds for active connections to close
- Force-closes remaining connections after timeout
- Displays a summary of uninitialized connection User-Agents:
  ```
  Uninitialized connection summary: claude-code/2.1.7: 81, node: 1
  ```

### Running Multiple Instances

Run multiple HostMCP servers simultaneously by using different ports and config files:

```bash
# Development instance (permissive)
hostmcp serve --port 18080 --config dev.yaml

# Another project (strict)
hostmcp serve --port 8081 --config strict.yaml
```

## Connecting AI Assistants

For MCP configuration steps inside AI Sandbox, see [ai-sandbox](https://github.com/YujiSuzuki/ai-sandbox).

After configuration, AI assistants can access containers:

```
User: "Check the myapp-api container logs"
Claude: [Uses HostMCP] "I can see 500 errors in the logs..."

User: "Run tests in the API container"
Claude: [Uses HostMCP] "Running npm test... 3 tests passed"
```

## CLI Commands

HostMCP provides two types of CLI commands:

### Setup Commands

```bash
# Generate a config file for a workspace
hostmcp init --workspace /path/to/workspace

# With a custom port
hostmcp init --workspace /path/to/workspace --port 18080

# Overwrite an existing config
hostmcp init --workspace /path/to/workspace --force
```

### Host OS Commands (Direct Docker Access)

These access the Docker socket directly and must be run **on the host OS**:

```bash
# List accessible containers
hostmcp list

# Get container logs
hostmcp logs myapp-api --tail 100

# Execute a whitelisted command
hostmcp exec myapp-api "npm test"

# Show container details with summary (default)
hostmcp inspect myapp-api

# Show container details as full JSON
hostmcp inspect myapp-api --json

# Get container stats
hostmcp stats myapp-api
```

**`list` output example:**
```
NAME              ID            IMAGE           STATE    STATUS          PORTS
myapp-api         4a2e541171d9  node:18-alpine  running  Up 5 minutes    0.0.0.0:3000->3000/tcp
myapp-proxy       8b3f621283e1  nginx:alpine    running  Up 5 minutes    0.0.0.0:80->80/tcp
```

**`inspect` summary output example:**
```
=== Container Summary: myapp-api ===

State:    running
Started:  2024-01-15T10:30:00Z
Image:    node:18-alpine

--- Network ---
  bridge:
    IP:      172.17.0.2
    Gateway: 172.17.0.1

--- Ports ---
  0.0.0.0:3000 -> 3000/tcp

--- Mounts ---
  /path/to/workspace -> /app (rw)

--- Full Details (JSON) ---
{ ... }
```

### Client Commands (Via HTTP API)

These connect to the HostMCP server via HTTP and can be used **inside an AI Sandbox**:

```bash
# List containers via HostMCP server
hostmcp client list

# Get container logs via server
hostmcp client logs securenote-api --tail 100

# Show container details via server (default: summary)
hostmcp client inspect securenote-api

# Show container details via server (full JSON)
hostmcp client inspect securenote-api --json

# Get container stats via server
hostmcp client stats securenote-api

# Execute a command via server
hostmcp client exec securenote-api "npm test"

# Custom server URL
hostmcp client list --url http://localhost:18080

# Or use an environment variable
export HOSTMCP_SERVER_URL=http://host.docker.internal:18080
hostmcp client list

# Set a custom timeout (seconds)
hostmcp client --timeout 120 exec securenote-api "npm run build"

# Or use an environment variable
export HOSTMCP_TIMEOUT=120
hostmcp client exec securenote-api "npm run build"
```

**Client command flags:**

| Flag | Env var | Default | Description |
|------|---------|---------|-------------|
| `--url` | `HOSTMCP_SERVER_URL` | `http://host.docker.internal:18080` | HostMCP server URL |
| `--client-suffix` / `-s` | `HOSTMCP_CLIENT_SUFFIX` | (none) | Suffix appended to client name |
| `--timeout` | `HOSTMCP_TIMEOUT` | `30` | Timeout in seconds for HTTP requests and tool call responses |

> **About timeout:** `--timeout` applies to both sending HTTP requests and waiting for responses via SSE. Increase it for long-running commands like `npm run build`. The SSE connection itself (session keep-alive) is not affected by this timeout.

**Which to use:**
- **Host OS commands**: When you have direct Docker socket access
- **Client commands**: Inside AI Sandbox, or environments without Docker socket access

## Security Modes

### Strict Mode
- Read-only operations (logs, inspect, stats)
- No command execution
- Most restrictive and safest

### Moderate Mode (Recommended)
- Read operations allowed
- Command execution limited to whitelisted commands
- Good balance of safety and functionality

### Permissive Mode
- All operations allowed
- Use only in trusted development environments

## Authentication

The current version **does not implement** authentication.

HostMCP is designed for local development environments and binds to localhost by default.

**Future plans:**
- Optional authentication via configuration file
- Token-based authentication for remote access
- Implementation based on user demand

If you need authentication, please start a [Discussion](https://github.com/YujiSuzuki/hostmcp/discussions).

## Configuration Reference

For complete configuration options, see [configs/hostmcp.example.yaml](configs/hostmcp.example.yaml):
- Container whitelist patterns
- Per-container command whitelists
- Audit logging
- Port and host settings

### File Access Blocking (blocked_paths)


#### Configuration Example

```yaml
security:
  blocked_paths:
    # Manually blocked paths
    manual:
      "securenote-api":
        - "/.env"
        - "/secrets/*"
      "*":  # Applies to all containers
        - "*.key"
        - "*.pem"

    # Auto-import from DevContainer settings
    auto_import:
      enabled: true
      workspace_root: "."

      # Files to scan
      scan_files:
        - ".devcontainer/docker-compose.yml"
        - ".devcontainer/devcontainer.json"

      # Global patterns (applied to all containers)
      global_patterns:
        - ".env"
        - "*.key"
        - "secrets/*"

      # Import from Claude Code settings
      claude_code_settings:
        enabled: true
        max_depth: 1  # Depth for scanning subdirectories
        settings_files:
          - ".claude/settings.json"
          - ".claude/settings.local.json"
```

#### max_depth Behavior

`max_depth` controls the depth for scanning Claude Code settings files:

```
/workspace/                          ← where hostmcp serve is launched
├── .claude/settings.json            ← ✅ scanned (depth 0)
├── demo-apps/
│   └── .claude/settings.json        ← ✅ scanned (depth 1)
├── demo-apps-ios/
│   └── .claude/settings.json        ← ✅ scanned (depth 1)
└── demo-apps/subproject/
    └── .claude/settings.json        ← ❌ not scanned (depth 2)
```

| max_depth | Behavior |
|-----------|----------|
| 0 | workspace_root only |
| 1 | Up to 1 level deep |
| 2 | Up to 2 levels deep |

#### Integration with Claude Code Settings

Patterns from `permissions.deny` in Claude Code's `.claude/settings.json` can be automatically imported:

```json
{
  "permissions": {
    "deny": [
      "Read(securenote-api/.env)",
      "Read(**/*.key)",
      "Read(**/secrets/**)"
    ]
  }
}
```

This unifies Claude Code's DevContainer settings with HostMCP's blocking policy.

#### Blocked Access Response

When access is blocked, a detailed reason is returned:

```json
{
  "blocked": true,
  "reason": "claude_code_settings_deny",
  "pattern": "**/*.key",
  "source": "demo-apps/.claude/settings.json",
  "hint": "This path is blocked by Claude Code settings (permissions.deny)..."
}
```

### Output Masking

HostMCP automatically masks sensitive data (passwords, API keys, tokens) in tool output before returning it to AI assistants.

```yaml
security:
  output_masking:
    enabled: true
    replacement: "[MASKED]"

    # Regex patterns to detect sensitive data
    patterns:
      - '(?i)(password|passwd|pwd)\s*[=:]\s*["'']?[^\s"''\n]+["'']?'
      - '(?i)(api[_-]?key|secret[_-]?key)\s*[=:]\s*["'']?[^\s"''\n]+["'']?'
      - '(?i)bearer\s+[a-zA-Z0-9._-]+'
      - 'sk-[a-zA-Z0-9]{20,}'
      - '(?i)(postgres|mysql|mongodb|redis)://[^:]+:[^@]+@'

    # Which outputs to mask
    apply_to:
      logs: true      # get_logs, search_logs
      exec: true      # exec_command
      inspect: true   # inspect_container (environment variables)
```

**Example:**
```
# Raw output
DATABASE_URL=postgres://admin:secret123@db:5432/app

# After masking
DATABASE_URL=[MASKED]db:5432/app
```

### Host Path Masking

When host OS paths contain the user's home directory, the home directory portion is masked to hide it from AI.

```yaml
security:
  host_path_masking:
    enabled: true           # Enable path masking (default: true)
    replacement: "[HOST_PATH]"
```

**Supported paths:**
- macOS: `/Users/username/...` → `[HOST_PATH]/...`
- Linux: `/home/username/...` → `[HOST_PATH]/...`
- Windows: `C:\Users\username\...` → `[HOST_PATH]\...`

**Example (inspect_container output):**
```json
// Raw output
{"Source": "/Users/john/workspace/myapp/.env"}

// After masking
{"Source": "[HOST_PATH]/workspace/myapp/.env"}
```

> **Note:** This masking applies only to MCP tool output. CLI commands (`hostmcp inspect`) show full paths since they are user-facing.

### Permissions

Control globally allowed operations:

```yaml
security:
  permissions:
    logs: true      # Allow log retrieval (get_logs, search_logs)
    inspect: true   # Allow container inspection
    stats: true     # Allow resource statistics
    exec: true      # Allow exec execution (subject to exec_whitelist)
```

### Default Commands (exec_whitelist `"*"`)

Using `"*"` as the container name defines commands available to all containers:

```yaml
security:
  exec_whitelist:
    # Container-specific commands
    "myapp-api":
      - "npm test"
      - "npm run lint"

    # Default commands for all containers
    "*":
      - "pwd"
      - "whoami"
      - "date"
```

> **Security warning:** Do not add `env`, `printenv`, or `echo *` to the default whitelist. These can expose all environment variables, including secrets.

### Dangerous Mode (exec_dangerously)

When commands like `tail`, `grep`, or `cat` that are not whitelisted are needed for debugging, HostMCP provides a "dangerous mode" that allows these commands while maintaining `blocked_paths` restrictions.

#### Why Is Dangerous Mode Needed?

Docker's `get_logs` only shows stdout/stderr. To view log files like `/var/log/app.log`, you need `tail` or `cat`. However, adding these to `exec_whitelist` would allow reading arbitrary files, including those containing secrets.

Dangerous mode solves this:
1. Allows specific commands (e.g., `tail`, `cat`, `grep`)
2. File paths are still checked against `blocked_paths`
3. Pipes (`|`), redirects (`>`), and path traversal (`..`) are blocked

#### Configuration

```yaml
security:
  exec_dangerously:
    enabled: false  # Global enable/disable
    commands:
      # Container-specific commands
      "securenote-api":
        - "tail"
        - "head"
        - "cat"
        - "grep"
      # Global commands (all containers)
      "*":
        - "tail"
        - "ls"
```

#### Server Startup Flags

Enable dangerous mode at startup without modifying the configuration file:

```bash
# Enable for specific containers
hostmcp serve --dangerously=securenote-api,demo-app

# Enable for all containers
hostmcp serve --dangerously-all
```

These flags:
- Set `exec_dangerously.enabled = true`
- Add default commands: `tail`, `head`, `cat`, `grep`, `less`, `wc`, `ls`, `find`

| Flag | Behavior |
|------|----------|
| `--dangerously=container1,container2` | **Clears** existing `exec_dangerously.commands` and enables only for specified containers |
| `--dangerously-all` | **Merges** with existing settings and adds commands to `"*"` (all containers) |

> To strictly limit dangerous mode to specific containers, use `--dangerously=container`. To broadly enable it while preserving container-specific settings, use `--dangerously-all`.

#### Usage

**MCP tools (Claude Code):**
```json
{
  "tool": "exec_command",
  "arguments": {
    "container": "securenote-api",
    "command": "tail -100 /var/log/app.log",
    "dangerously": true
  }
}
```

**CLI:**
```bash
# Direct (host OS)
hostmcp exec --dangerously securenote-api "tail -100 /var/log/app.log"

# Client (AI Sandbox)
hostmcp client exec --dangerously --url http://host.docker.internal:18080 securenote-api "tail -100 /var/log/app.log"
```

#### Security Model

```
Request with dangerously=true
    ↓
1. Is exec_dangerously.enabled = true? (server config)
    ↓
2. Is the base command in exec_dangerously.commands?
    ↓
3. Check for pipes (|), redirects (>), path traversal (..)
    ↓
4. Extract file paths from command
    ↓
5. Check each path against blocked_paths
    ↓
Execute if all checks pass
```

**Examples blocked by design:**
- `cat /secrets/key.pem` → blocked by `blocked_paths`
- `cat /etc/passwd | grep root` → pipes not allowed
- `cat ../secrets/key` → path traversal not allowed
- `rm /var/log/app.log` → `rm` is not in `exec_dangerously.commands`

> **Security note:** Clients must explicitly set `dangerously=true`. This "opt-in" design ensures conscious acknowledgment when using dangerous mode.

#### Hint Messages on Errors

When trying to execute a command that isn't whitelisted but is available in dangerous mode, a hint is shown:

```
command not whitelisted: tail (hint: this command is available with dangerously=true)
```

#### Checking Available Commands

Use `hostmcp client commands` to see both whitelisted and dangerous commands:

```bash
$ hostmcp client commands
CONTAINER           ALLOWED COMMANDS
---------           ----------------
* (all containers)  pwd
                    whoami
securenote-api      npm test
                    npm run lint

CONTAINER           DANGEROUS COMMANDS (requires dangerously=true)
---------           ----------------------------------------------
* (all containers)  tail
                    ls
securenote-api      tail
                    cat
                    grep

Note: Commands with '*' wildcard match any suffix. Dangerous commands require dangerously=true parameter.
```

### Large Output Handling (host_tools)

When a host tool produces output exceeding `max_output_bytes`, HostMCP saves the full output to a file and returns a path and preview to the AI instead. This prevents large build logs or test reports from overflowing the AI's context window.

```yaml
host_access:
  host_tools:
    max_output_bytes: 102400  # 100 KB; set to 0 to disable
    large_output_dir: ".sandbox/tmp"  # relative to workspace root
```

The AI receives a message like this:

```
Tool: my-build.sh
Exit Code: 0

⚠️  Output was large (N bytes) and has been saved to a file.
File: [HOST_PATH]/workspace/.sandbox/tmp/hostmcp-my-build-last.log
Use the Read or Grep tool to inspect the full output.

--- Preview (first/last 20 lines) ---
...
```

> **Note:** Each tool run overwrites the previous file (`hostmcp-<toolname>-last.log`), so only the most recent output is kept.

## Architecture

```
┌─────────────────────────────────┐
│ Host OS                         │
│  ┌──────────────────────────┐   │
│  │ HostMCP (Go binary)      │   │
│  │ - MCP server (HTTP/SSE)  │   │
│  │ - Security policies      │   │
│  └────────┬─────────────────┘   │
│           │ :18080               │
│  ┌────────┴─────────────────┐   │
│  │ Docker Engine            │   │
│  │  ├─ AI Sandbox            │   │
│  │  │   └─ Claude Code ─┐   │   │
│  │  ├─ app-api ←─────────┘   │   │
│  │  └─ app-db              │   │
│  └─────────────────────────┘   │
└─────────────────────────────────┘
```

## Design Philosophy

**Why doesn't HostMCP support `docker-compose up/down` or image rebuilds?**

HostMCP is built with a clear separation of responsibilities: AI observes and suggests, humans handle infrastructure changes. Access is granted in graduated levels, with each level opt-in.

### Core Design Principle

```
AI = eyes and mouth (observe, suggest)
Human = hands (execute infrastructure changes)
```

**What AI can do (by default):**
- Read logs, stats, and container information
- Execute whitelisted commands (tests, linting)
- Read files (with `blocked_paths` protection)
- Suggest changes and solutions

**What AI can do (opt-in):**
- Start/stop/restart containers (`lifecycle: true`)
- Run approved host tools (host_tools — enabled by default)
- Execute whitelisted host commands (host_commands)

**What humans do:**
- Rebuild images (`docker-compose build`)
- Recreate containers (`docker-compose up`)
- Approve host tools (`hostmcp tools sync`)
- Make infrastructure changes

### Graduated Access Model

HostMCP provides four levels of access, each more permissive than the last:

| Level | Operations | Default | Risk |
|-------|-----------|---------|------|
| **Read** | Logs, stats, inspect, file listing | Enabled | None |
| **Execute** | Whitelisted commands in containers | Enabled (moderate mode) | Low |
| **Lifecycle** | Start/stop/restart containers | **Disabled** | Medium |
| **Host tools** | Approved host tool scripts | Enabled | Medium |
| **Host commands** | Whitelisted host CLI commands | **Disabled** | High |

Lifecycle and host commands are disabled by default and require explicit opt-in via `hostmcp.yaml`. Host tools are enabled by default but require human approval (`hostmcp tools sync`) before any tool can run.

### Why Build/Recreate Remains Human-Only

#### 1. Dockerfile Changes Require Rebuilds

When a Dockerfile is modified, a simple `restart` won't reflect the changes:

```bash
# This won't apply Dockerfile changes
docker restart myapp  # still uses the old image

# What's actually needed
docker-compose build myapp
docker-compose up -d myapp
```

Container restart is useful for recovering a crashed container or applying config changes, but it cannot replace a full rebuild. HostMCP does not provide raw `docker-compose build` or `docker-compose up` as MCP tools to prevent the false assumption that restart solves everything.

> **Note:** Host tools can wrap these operations in human-reviewed scripts (e.g., `demo-build.sh`, `demo-up.sh`). This gives AI controlled access while ensuring the scripts are explicitly approved.

#### 2. Most Development Work Doesn't Need Container Operations

| Action | Solution | Container ops needed? |
|--------|----------|----------------------|
| Code changes | Hot reload / `exec npm run dev` | No |
| Config file changes | App reload command | No |
| Run tests | `exec npm test` | No |
| Check logs | `get_logs` | No |
| Container crashed | `restart_container` (opt-in) | Yes |
| Dockerfile changes | Rebuild + recreate | **Yes, by humans** |

Cases that truly require image rebuilds (Dockerfile changes, docker-compose.yml changes) are **infrastructure changes** and should go through human review.

#### 3. Risk vs. Frequency Trade-off

| Operation Level | Risk | Frequency During Development |
|-----------------|------|------------------------------|
| Reading logs/stats | None | Very high |
| Whitelisted command execution | Low | High |
| Container restart | Medium | Low |
| Build/recreate | High | Very low |

Container restart is available as opt-in for the cases where it's genuinely useful (recovering crashed containers, applying environment variable changes). Build/recreate remains human-only due to its high risk and low frequency.

#### 4. AI Investigates, Humans Act on Infrastructure

**Good workflow:**
1. AI investigates logs, stats, and error patterns
2. AI identifies the problem and suggests a solution
3. AI restarts the container if `lifecycle` is enabled and it's a simple recovery
4. For infrastructure changes, **humans** decide and execute

**Risky workflow:**
1. AI detects an error and immediately rebuilds/recreates the container
2. The build takes minutes, and the problem isn't resolved
3. Humans can't understand what changed

### About exec_command

`exec_command` lets you restrict allowed commands via whitelist:

```yaml
exec_whitelist:
  "myapp-api":
    - "npm test"
    - "npm run lint"
    - "npm run dev"  # Can restart the dev server
```

This enables:
- Running tests and linting
- Restarting development servers (via process manager)
- Health checks and debug commands

Not allowed:
- Arbitrary command execution
- File system modifications
- Network configuration changes

### Summary

HostMCP's design provides graduated access:
- **Read-only access** to container information (logs, stats, inspect)
- **Controlled command execution** via whitelists
- **File access** with `blocked_paths` protection
- **Container lifecycle** (start/stop/restart) — opt-in, disabled by default
- **Host tools** — enabled by default (requires human approval per tool)
- **Host commands** — opt-in, disabled by default
- **No image build/recreate operations** — always human-only

Each level can be enabled independently, letting you choose the right balance of AI autonomy and human control for your environment.

## Provided MCP Tools

| Tool | Description |
|------|-------------|
| `list_containers` | List accessible containers |
| `get_logs` | Get container logs |
| `get_stats` | Get resource usage statistics |
| `exec_command` | Execute whitelisted commands (`dangerously` mode supported) |
| `inspect_container` | Get detailed container information |
| `get_allowed_commands` | List whitelisted commands per container |
| `get_security_policy` | Show current security settings |
| `search_logs` | Search container logs by pattern |
| `list_files` | List files in a container directory (with blocking) |
| `read_file` | Read a file from a container (with blocking) |
| `get_blocked_paths` | Show blocked file paths |
| `restart_container` | Restart a container (requires `lifecycle: true`) |
| `stop_container` | Stop a running container (requires `lifecycle: true`) |
| `start_container` | Start a stopped container (requires `lifecycle: true`) |
| `list_host_tools` | List available host tools |
| `get_host_tool_info` | Get detailed info about a host tool |
| `run_host_tool` | Execute an approved host tool |
| `exec_host_command` | Execute a whitelisted host CLI command |

## Troubleshooting

### HostMCP Server Not Recognized

1. **Verify HostMCP is running on host:**
   ```bash
   curl http://localhost:18080/health
   # Should return 200 OK
   ```

2. **Check MCP configuration inside AI Sandbox:**
   ```bash
   cat ~/.claude.json | jq '.mcpServers.hostmcp'
   # Should show: "url": "http://host.docker.internal:18080/sse"
   ```

3. **Try MCP reconnect:**
   In Claude Code, run `/mcp` → select "Reconnect"

4. **Fully restart VS Code:**
   macOS: `Cmd + Q` / Windows/Linux: `Alt + F4`

### After Restarting the HostMCP Server

Restarting the HostMCP server drops SSE connections, so the AI assistant needs to reconnect:

- **Claude Code:** `/mcp` → select "Reconnect"
- **If that doesn't work:** Fully restart VS Code (Cmd+Q / Alt+F4)

### "Connection refused" Error

- Is HostMCP running on host? → `ps aux | grep hostmcp`
- Are you using `host.docker.internal` in the URL? (`localhost` won't work from AI Sandbox)
- Is port 18080 blocked by a firewall? → `lsof -i :18080`

### "Container not in allowed list"

Add the container name or pattern to `allowed_containers` in the config:
```yaml
security:
  allowed_containers:
    - "your-container-name"
```

### "Command not whitelisted"

Add the command to `exec_whitelist` in the config:
```yaml
security:
  exec_whitelist:
    "container-name":
      - "your command here"
```

## License

MIT License - See [LICENSE](LICENSE)

## Acknowledgments

- Built on [Model Context Protocol](https://modelcontextprotocol.io/)
- Docker integration via [docker/docker](https://github.com/docker/docker)
- CLI powered by [spf13/cobra](https://github.com/spf13/cobra)

---

**Note**: HostMCP provides controlled access, but use it responsibly. Always review your security settings before exposing containers to AI assistants.
