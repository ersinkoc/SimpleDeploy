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
```

**There is no template directory.** Dockerfile templates are Go constants in
`internal/buildpack/detect.go`; proxy and compose output is emitted by
`internal/proxy/*.go` and `internal/compose/generator.go`. A stale `templates/`
directory may still exist in old checkouts — it is dead code, is not embedded,
and must not be edited or revived (its copies lack the validation and escaping
the real generators perform).

### Key Components

**State Management** (`internal/state/`)
- State is stored in `~/.simpledeploy/state.json` as **plain JSON, mode 0600**
- Individual secret *fields* are encrypted, not the file: `git_token` and each
  entry of `db_credentials` go through `state.Encrypt`. Everything else —
  including `webhook_secret` — is plaintext, so the file is a credential and
  must not be copied off the host
- Encryption uses AES-256-GCM with machine-id-based key derivation, which means
  ciphertext is only decryptable on the machine that wrote it (see the
  `/etc/machine-id` bind mount in `internal/runner/service.go`)
- `State` struct contains `Apps` map and `GlobalConfig`
- `AppConfig` tracks: repo, branch, domain, port, databases, deployment history

**Configuration** (`internal/config/paths.go`)
- `BaseDir`: `/opt/simpledeploy` (or `SIMPLEDEPLOY_DIR` env var)
- `HomeDataDir()`: `~/.simpledeploy` (or `SIMPLEDEPLOY_STATE_DIR` env var)
- App data lives in `/opt/simpledeploy/apps/<app-name>/`
- The two are separate on purpose: the service container bind-mounts both at
  their host paths and sets both env vars, because `$HOME` inside the container
  is `/root` and would otherwise resolve state to an unmounted path.

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

1. `simpledeploy init` → Checks Docker **and the compose plugin** → sets up proxy → publishes the `/_qd/*` webhook route through that proxy
2. `simpledeploy deploy` → Detects app type → Clones repo → Generates Dockerfile + `.dockerignore` (if needed) → Builds image → Generates docker-compose.yml → Starts container → **polls until the container reports `running`**
3. `simpledeploy webhook start` → Listens for push events → Verifies signature → Pulls latest → Rebuilds → Redeploys → **rolls back to the previous image if the new container exits**

### Invariants worth preserving

- **App containers use `expose`, never `ports`.** A published port bypasses the
  proxy's TLS and security headers and exposes the app on the public IP.
- **Generated proxy/compose output must be deterministic.** Every map walked
  while emitting config goes through a sorted-keys helper (`sortedKeys` in
  `internal/compose`, `sortedHeaderKeys` in `internal/proxy`); unsorted
  iteration made every redeploy rewrite the file and reload the proxy.
- **Every `buildpack` type must appear in `mapDetectedDefault`.** An unmapped
  type falls through to the "use existing Dockerfile" menu entry, which skips
  Dockerfile generation and fails the build on a repo that has none.
- **Cleanup after a CLI command must be synchronous.** Background goroutines
  launched near the end of a command do not run before the process exits.
- **`sanitizeOutput` must never be called with an empty needle.**
  `strings.ReplaceAll` with `""` interleaves the replacement into every
  character of the input.
- **A `.env` is assembled once, at the end of the deploy wizard.** Writing
  partial content earlier gets truncated by the final write.
- **The webhook server refuses to deploy without a configured secret.** Every
  signature-verification branch is skipped when the secret is empty.
- **Compose `environment:` blocks use YAML map form (`KEY: "value"`), never
  list form (`- KEY="value"`).** In list form the whole item is one plain YAML
  scalar, so the quotes are literal characters; Compose splits on the first `=`
  and does not unquote, and the container receives `DATABASE_URL` *including*
  the double quotes. `environment` also overrides `env_file`, so the correct
  value in `.env` cannot rescue it.
- **The `GIT_ASKPASS` script must be `chmod`-ed executable explicitly.**
  `os.WriteFile`'s perm argument is ignored for a file that already exists, and
  `os.CreateTemp` creates it at 0600 — git then cannot exec the helper and every
  private-repo clone/pull fails with "could not read Username".
- **Redeploy re-asserts the Caddy route, not just a reload.** `AddCaddyApp`
  failure during deploy is only a warning, so an app can be recorded `running`
  with no Caddyfile block; reloading alone would re-read a config that still
  does not route it.
- **Image tags are stamped in UTC.** `ListImages` orders by reverse string sort
  and `CleanupOldImages` trusts that as recency; a local-time stamp repeats an
  hour at DST fall-back and breaks the ordering.
- **Header values must not contain braces.** `{...}` is Caddy placeholder
  syntax (expanded even inside quotes), and `filterCaddyDomain` finds a block's
  end by counting braces — an unbalanced one swallows the rest of the Caddyfile.
- **`lockStateFile` creates the state directory before opening the lock.** It
  runs before `saveStateLocked`'s `MkdirAll`, so on a fresh install the lock
  open returned ENOENT, which the retry loop misread as contention — every
  first-ever `simpledeploy init` died with "could not acquire state lock". Only
  `os.IsExist` is worth retrying; any other error is permanent.
- **The Caddyfile is written IN PLACE, never via temp+rename.** The proxy
  compose bind-mounts it as a single file, and Docker binds that to an inode at
  container creation. A rename orphans the mount, so the container never sees
  another byte: Caddy kept serving a stub config, reported "config is
  unchanged" on reload, and routed nothing.
- **A container is only "up" if it holds `running` for `containerStableFor`
  and its restart counter does not move.** `restart: unless-stopped` means a
  crash-looping container flickers between `restarting` and `running` and never
  reaches `exited`/`dead`, so a single status read reported a crash-looping
  deploy as a success and the rollback never fired. `restart` verifies the same
  way — `docker restart` exiting 0 only means the daemon started it.
- **A command that owns only part of an app record uses `state.UpdateApp`, never
  `SaveApp`.** `SaveApp` replaces the whole record with the caller's copy, and a
  deploy holds that copy for minutes (git pull + build). Two failures followed:
  a `remove` during a webhook redeploy was undone when the redeploy's final save
  re-inserted the deleted app, and a concurrent `stop` had its status silently
  overwritten. `UpdateApp` re-reads under the lock and errors if the app is gone.
- **`lockStateFile`'s unlock removes the lock only while it still carries that
  acquisition's token.** An unconditional remove deleted a lock a *different*
  live process had just created (after two waiters recovered the same stale
  lock), cascading into concurrent writers. Stale-lock recovery also re-stats
  and requires an unchanged mtime before removing.
- **Compose values have `$` doubled (`escapeComposeInterpolation`).** YAML
  quoting is not the only layer: Compose interpolates `$VAR`/`${VAR}` in values
  after the YAML parse. A bcrypt hash (`$2b$12$…`) made `docker compose up`
  abort with "invalid interpolation format" *after* the image build, `a$FOO`
  silently became `a`, and because Compose auto-loads the project directory's
  `.env` — the file holding the generated DB connection string — a `${...}` in
  an operator-supplied value could expand a password into a response header.
- **git argv values are checked at the sink.** `RunRedeployContext` passes
  `app.Branch` from state.json into `git fetch` and never calls
  `compose.Generate`, so nothing else guards that path: `Clone`/`Pull` call
  `state.ValidateBranch` themselves. The repo URL is only checked for
  option-injection shape there (leading `-`, control chars) — the full
  `ValidateRepoURL` policy belongs at the input layer, and enforcing it in the
  transport would forbid legitimate local-path clones.
- **Non-branch refs never deploy.** GitHub delivers TAG pushes as
  `X-GitHub-Event: push` with `ref: refs/tags/…` and cannot filter them; an
  empty branch used to skip the branch check entirely, so every tag push
  redeployed. Only a wholly absent ref still means "no branch information".
- **`extractRefFromPayload` handles both GitHub content types.** In
  `application/x-www-form-urlencoded` mode the body is `payload=<urlencoded
  JSON>`; parsing only raw JSON left the ref empty, which (per the point above)
  disabled the branch filter and redeployed on pushes to every branch.
- **The webhook body cap answers 413, never truncates.** `io.LimitReader`
  silently truncated at the cap, so a legitimate oversized payload had its HMAC
  computed over partial bytes and was reported as "Invalid signature" — sending
  operators after a secret mismatch that did not exist.
- **`app.WebhookEnabled` is enforced in the server.** It was collected by the
  wizard and shown by `list` but never consulted, so an app deployed with
  push-to-deploy declined still auto-deployed.
- **`Start` joins `srv.Shutdown` before `deployWg.Wait()`.** `ListenAndServe`
  returns `ErrServerClosed` when Shutdown is *called*, not when handlers finish,
  so `Wait` could return before a handler reached `deployWg.Add(1)` — killing
  that deploy at process exit (and racing `Add` against `Wait`). The join is
  guarded by a `shutdownStarted` channel so an `ErrServerClosed` from anywhere
  else (or a test double) does not hang.
- **A push arriving mid-deploy is queued, not dropped.** It used to be discarded
  while still answering `200 Deploy triggered`, so the provider's log showed
  success for a commit that was never deployed. One follow-up run is performed
  after the in-flight deploy (which deploys the branch tip, i.e. every queued
  commit) and the response is `202`.
- **Image tags carry millisecond precision.** Two builds of the same app in the
  same second — a CLI redeploy racing a webhook one, which nothing serializes
  across processes — produced the same tag, so which image was deployed was
  nondeterministic. The format stays lexically sortable for `ListImages`.
- **Deploy cleanup tears containers down before deleting the app directory.** The
  deferred cleanup fires on any pre-`deployed` failure, including a state-save
  error *after* startup; deleting the directory then orphaned running containers
  with no compose file to stop them and a live proxy route.

### Known limitations (deliberate, not defects)

- **No cross-process per-app deploy lock.** `deployLocks` is in the webhook
  server's memory only, so `simpledeploy redeploy X` run by hand during a
  webhook-triggered deploy of X still races it (concurrent `git pull` of the
  same source tree, and a rollback that can revert the peer's compose changes).
  Millisecond image tags remove the tag collision; the rest is unguarded.
- **The webhook listener binds all interfaces** and `service install` publishes
  the port, because one proxy route (`host.docker.internal:<port>`) serves both
  the host-process and containerised modes. The deploy trigger is HMAC-gated,
  but the port should be firewalled — GitLab-token mode sends the shared secret
  as a plain header.
- **The rate limiter keys on `RemoteAddr`.** Behind the proxy every request
  arrives from the bridge gateway, so 60/min is effectively global and is
  consumed before signature verification; sustained unauthenticated traffic can
  starve genuine deliveries with 429s.

### Testing gaps these bugs came from

Several of the above were invisible to the unit suite and only surfaced in a
real end-to-end run (`init` → `deploy` → `redeploy` → webhook → rollback against
a live Docker daemon). When touching these areas, prefer an integration check:

- Tests point `InitState` at a `t.TempDir()` that **already exists**, so no test
  ever exercised the fresh-install path.
- Nothing in the unit suite mounts a file into a container, so the inode/bind
  mount contract was unrepresented.
- Nothing asserted on what a container's environment actually **receives** —
  only on the YAML text SimpleDeploy emits.

### Environment Variables

- `SIMPLEDEPLOY_DIR`: Override base directory (default: `/opt/simpledeploy`)
- `SIMPLEDEPLOY_STATE_DIR`: Override the state directory (default: `~/.simpledeploy`)
- Both are used in tests to redirect state to temp directories

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
