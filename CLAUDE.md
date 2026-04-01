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
