# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

SimpleDeploy is a single-binary PaaS CLI tool written in Go. It enables users to deploy applications from Git repositories to their own servers with automatic Docker setup, reverse proxy configuration (Traefik or Caddy), SSL certificates, and webhook-based auto-deployment.

## Build Commands

```bash
# Build the binary
make build
# Or directly: CGO_ENABLED=0 go build -ldflags "-s -w" -o simpledeploy .

# Run tests
make test
# Or directly: go test -p=1 -count=1 ./...

# Run tests with coverage
make test-coverage

# Run linter
make lint
# Or directly: go vet ./...

# Clean build artifacts
make clean

# Build release binaries for all platforms
make release

# Build Docker image
make docker
```

## Test Structure

Tests use Go's standard testing package. Key patterns:
- Tests use `t.TempDir()` for isolation
- `SIMPLEDEPLOY_DIR` environment variable can override the base directory in tests
- The `-p=1` flag prevents parallel test execution to avoid state conflicts
- `-count=1` disables test caching

## Architecture

### Directory Structure

```
.
├── main.go              # Entry point, calls cli.Route()
├── main_test.go         # Entry point tests
├── internal/
│   ├── cli/             # CLI command handlers (root.go, deploy.go, init.go, etc.)
│   ├── wizard/          # Interactive prompts and ANSI color utilities
│   ├── git/             # Git clone/pull operations
│   ├── docker/          # Docker build, install, compose operations
│   ├── compose/         # docker-compose.yml generator
│   ├── proxy/           # Traefik/Caddy setup (traefik.go, caddy.go)
│   ├── webhook/         # HTTP webhook server for GitHub/GitLab/Gitea
│   ├── db/              # Database provisioning (MySQL, PostgreSQL, etc.)
│   ├── state/           # JSON state management with AES-256-GCM encryption
│   ├── buildpack/       # Auto-detection of app type (Node.js, Go, Python, etc.)
│   ├── config/          # Path constants and configuration
│   └── runner/          # Self-containerization as Docker service
└── templates/           # Embedded templates for Dockerfiles and proxy configs
```

### Key Components

**State Management** (`internal/state/`)
- State is stored in `~/.simpledeploy/state.json` (encrypted)
- `State` struct contains `Apps` map and `GlobalConfig`
- `AppConfig` tracks: repo, branch, domain, port, databases, deployment history
- Encryption uses AES-256-GCM with machine-id-based key derivation

**Configuration** (`internal/config/paths.go`)
- `BaseDir`: `/opt/simpledeploy` (or `SIMPLEDEPLOY_DIR` env var)
- `HomeDataDir()`: `~/.simpledeploy` for local state
- App data lives in `/opt/simpledeploy/apps/<app-name>/`

**CLI Routing** (`internal/cli/root.go`)
- `Route()` function dispatches to command handlers based on first argument
- Commands: init, deploy, list, redeploy, remove, restart, stop, exec, logs, status, service, webhook, version

**Buildpack Detection** (`internal/buildpack/detect.go`)
- Detects app type by file presence: Dockerfile → package.json → go.mod → requirements.txt/pyproject.toml → etc.
- Returns `AppType` with detected framework and default port

**Docker Operations** (`internal/docker/`)
- `BuildImage()`: Builds Docker images with timestamp tags
- `runner.go`: Runs docker-compose commands
- `installer.go`: Installs Docker on the host system

**Webhook Server** (`internal/webhook/`)
- Supports GitHub (HMAC-SHA256), GitLab (token), Gitea (HMAC-SHA256)
- Triggered on push events to auto-redeploy applications

### Data Flow

1. `simpledeploy init` → Checks Docker, sets up proxy, configures domain/webhook
2. `simpledeploy deploy` → Detects app type → Clones repo → Generates Dockerfile (if needed) → Builds image → Generates docker-compose.yml → Starts container
3. `simpledeploy webhook start` → Listens for push events → Verifies signature → Pulls latest → Rebuilds → Redeploys

### Environment Variables

- `SIMPLEDEPLOY_DIR`: Override base directory (default: `/opt/simpledeploy`)
- Used in tests to redirect state to temp directories

<!-- rtk-instructions v2 -->
# RTK (Rust Token Killer) - Token-Optimized Commands

## Golden Rule

**Always prefix commands with `rtk`**. If RTK has a dedicated filter, it uses it. If not, it passes through unchanged. This means RTK is always safe to use.

**Important**: Even in command chains with `&&`, use `rtk`:
```bash
# ❌ Wrong
git add . && git commit -m "msg" && git push

# ✅ Correct
rtk git add . && rtk git commit -m "msg" && rtk git push
```

## RTK Commands by Workflow

### Build & Compile (80-90% savings)
```bash
rtk cargo build         # Cargo build output
rtk cargo check         # Cargo check output
rtk cargo clippy        # Clippy warnings grouped by file (80%)
rtk tsc                 # TypeScript errors grouped by file/code (83%)
rtk lint                # ESLint/Biome violations grouped (84%)
rtk prettier --check    # Files needing format only (70%)
rtk next build          # Next.js build with route metrics (87%)
```

### Test (90-99% savings)
```bash
rtk cargo test          # Cargo test failures only (90%)
rtk vitest run          # Vitest failures only (99.5%)
rtk playwright test     # Playwright failures only (94%)
rtk test <cmd>          # Generic test wrapper - failures only
```

### Git (59-80% savings)
```bash
rtk git status          # Compact status
rtk git log             # Compact log (works with all git flags)
rtk git diff            # Compact diff (80%)
rtk git show            # Compact show (80%)
rtk git add             # Ultra-compact confirmations (59%)
rtk git commit          # Ultra-compact confirmations (59%)
rtk git push            # Ultra-compact confirmations
rtk git pull            # Ultra-compact confirmations
rtk git branch          # Compact branch list
rtk git fetch           # Compact fetch
rtk git stash           # Compact stash
rtk git worktree        # Compact worktree
```

Note: Git passthrough works for ALL subcommands, even those not explicitly listed.

### GitHub (26-87% savings)
```bash
rtk gh pr view <num>    # Compact PR view (87%)
rtk gh pr checks        # Compact PR checks (79%)
rtk gh run list         # Compact workflow runs (82%)
rtk gh issue list       # Compact issue list (80%)
rtk gh api              # Compact API responses (26%)
```

### JavaScript/TypeScript Tooling (70-90% savings)
```bash
rtk pnpm list           # Compact dependency tree (70%)
rtk pnpm outdated       # Compact outdated packages (80%)
rtk pnpm install        # Compact install output (90%)
rtk npm run <script>    # Compact npm script output
rtk npx <cmd>           # Compact npx command output
rtk prisma              # Prisma without ASCII art (88%)
```

### Files & Search (60-75% savings)
```bash
rtk ls <path>           # Tree format, compact (65%)
rtk read <file>         # Code reading with filtering (60%)
rtk grep <pattern>      # Search grouped by file (75%)
rtk find <pattern>      # Find grouped by directory (70%)
```

### Analysis & Debug (70-90% savings)
```bash
rtk err <cmd>           # Filter errors only from any command
rtk log <file>          # Deduplicated logs with counts
rtk json <file>         # JSON structure without values
rtk deps                # Dependency overview
rtk env                 # Environment variables compact
rtk summary <cmd>       # Smart summary of command output
rtk diff                # Ultra-compact diffs
```

### Infrastructure (85% savings)
```bash
rtk docker ps           # Compact container list
rtk docker images       # Compact image list
rtk docker logs <c>     # Deduplicated logs
rtk kubectl get         # Compact resource list
rtk kubectl logs        # Deduplicated pod logs
```

### Network (65-70% savings)
```bash
rtk curl <url>          # Compact HTTP responses (70%)
rtk wget <url>          # Compact download output (65%)
```

### Meta Commands
```bash
rtk gain                # View token savings statistics
rtk gain --history      # View command history with savings
rtk discover            # Analyze Claude Code sessions for missed RTK usage
rtk proxy <cmd>         # Run command without filtering (for debugging)
rtk init                # Add RTK instructions to CLAUDE.md
rtk init --global       # Add RTK to ~/.claude/CLAUDE.md
```

## Token Savings Overview

| Category | Commands | Typical Savings |
|----------|----------|-----------------|
| Tests | vitest, playwright, cargo test | 90-99% |
| Build | next, tsc, lint, prettier | 70-87% |
| Git | status, log, diff, add, commit | 59-80% |
| GitHub | gh pr, gh run, gh issue | 26-87% |
| Package Managers | pnpm, npm, npx | 70-90% |
| Files | ls, read, grep, find | 60-75% |
| Infrastructure | docker, kubectl | 85% |
| Network | curl, wget | 65-70% |

Overall average: **60-90% token reduction** on common development operations.
<!-- /rtk-instructions -->

<!-- dfmt:v1 begin -->
## Context Discipline

This project uses DFMT to keep tool output from flooding the context
window and to preserve session state across compactions. When working
in this project, follow these rules.

### Tool preferences

Prefer DFMT's MCP tools over native ones:

| Native     | DFMT replacement | `intent` required? |
|------------|------------------|--------------------|
| `Bash`     | `dfmt_exec`      | yes                |
| `Read`     | `dfmt_read`      | yes                |
| `WebFetch` | `dfmt_fetch`     | yes                |
| `Glob`     | `dfmt_glob`      | yes                |
| `Grep`     | `dfmt_grep`      | yes                |
| `Edit`     | `dfmt_edit`      | n/a                |
| `Write`    | `dfmt_write`     | n/a                |

Every `dfmt_*` call MUST pass an `intent` parameter — a short phrase
describing what you need from the output (e.g. "failing tests",
"error message", "imports"). Without `intent` the tool returns raw
bytes and the token savings are lost.

On DFMT failure, report it to the user (one short line — which call,
what error) and then fall back to the native tool so the session is
not blocked. The ban is on *silent* fallback — every switch must be
announced. After a fallback, drop a brief `dfmt_remember` note tagged
`gap` when practical, so the journal records that a call was bypassed.
If the native tool is also denied (permission rule, sandbox refusal),
stop and ask the user; do not retry blindly.

### Session memory

DFMT tracks tool calls automatically. After substantive decisions or
findings, call `dfmt_remember` with descriptive tags (`decision`,
`finding`, `summary`) so future sessions can recall the context after
compaction.

### When native tools are acceptable

Native `Bash` and `Read` are acceptable for outputs you know are small
(< 2 KB) and will not be referenced again. For everything else, DFMT
tools are preferred.
<!-- dfmt:v1 end -->
